// Package pipeline reparte los eventos por partida y arma un lote (Batch)
// cuando cada partida termina.
//
// Flujo (como una oficina de correos):
//
//	Submit ──▶ [in] ──▶ router ──┬──▶ goroutine partida 1 ──┐
//	                             ├──▶ goroutine partida 2 ──┤
//	                             └──▶ goroutine partida N ──┤
//	                                                        ▼
//	                                                  [out: Batch] ──▶ senders
//
// Dueños de los channels (quién los cierra):
//   - in:  Close(), llamado por quien apaga el servidor. Se cierra una vez.
//   - canal de cada partida: el router, que es el único que les envía.
//     Lo cierra al reenviar MATCH_FINISHED (o al apagarse).
//   - out: el router, después de que TODAS las goroutines de partida
//     terminaron (las espera con un WaitGroup), porque ellas son las que
//     envían a out.
package pipeline

import (
	"errors"
	"sort"
	"sync"
	"time"

	"gamestats/ingestion/internal/model"
	"gamestats/ingestion/internal/state"
	"gamestats/ingestion/internal/validation"
)

// ErrMatchFinished se devuelve cuando llega un evento de una partida que ya
// terminó: su lote ya se armó, así que el evento se rechaza.
var ErrMatchFinished = errors.New("partida ya finalizada")

// outBuffer deja que unas cuantas partidas terminadas esperen en el canal de
// salida si los senders están ocupados (por ejemplo, reintentando con Scala).
const outBuffer = 16

// submission es lo que viaja por el canal de entrada: el evento y un canal
// por donde el router responde si lo aceptó.
type submission struct {
	event model.Event
	reply chan error
}

// Dispatcher conecta el handler HTTP con las goroutines de cada partida.
type Dispatcher struct {
	in    chan submission
	out   chan model.Batch
	store *state.MatchStore
}

// New crea el dispatcher y arranca la goroutine del router.
func New(store *state.MatchStore) *Dispatcher {
	d := &Dispatcher{
		in:    make(chan submission),
		out:   make(chan model.Batch, outBuffer),
		store: store,
	}
	go d.route()
	return d
}

// Submit entrega un evento (ya validado) al router y espera su respuesta:
// nil si lo aceptó, o ErrMatchFinished si la partida ya había terminado.
// Se espera la respuesta porque solo el router conoce el orden real de
// llegada, y así POST /events puede informar el rechazo con exactitud.
// No se debe llamar después de Close.
func (d *Dispatcher) Submit(e model.Event) error {
	// Buffer de 1: el router puede responder sin quedarse esperando.
	reply := make(chan error, 1)
	d.in <- submission{event: e, reply: reply}
	return <-reply
}

// Out es el canal de salida con los lotes terminados. Es de solo lectura
// (<-chan): quien consume no puede enviar ni cerrarlo. Se cierra cuando el
// dispatcher termina, así un "for b := range d.Out()" acaba solo.
// Alguien debe leerlo siempre; si no, las partidas se quedan esperando.
func (d *Dispatcher) Out() <-chan model.Batch {
	return d.out
}

// Close cierra el canal de entrada. El router termina lo pendiente, cierra
// los canales de partida, espera a sus goroutines y cierra out.
func (d *Dispatcher) Close() {
	close(d.in)
}

// route es la goroutine del router. El mapa matches lo usa SOLO esta
// goroutine, por eso no necesita mutex.
func (d *Dispatcher) route() {
	matches := make(map[string]chan model.Event) // matchId -> canal de la partida
	var wg sync.WaitGroup                        // goroutines de partida vivas

	for sub := range d.in {
		e := sub.event

		// Evento tardío: si la partida ya no está en curso, su lote ya salió.
		// Se rechaza y NO se crea una goroutine nueva.
		if status, ok := d.store.Status(e.MatchID); ok && status != state.StatusInProgress {
			sub.reply <- ErrMatchFinished
			continue
		}

		// El router es el único que registra eventos en el store, así el
		// estado siempre refleja el orden en que el router los procesó.
		d.store.Record(e)

		ch, ok := matches[e.MatchID]
		if !ok {
			// Primera vez que vemos esta partida: creamos su goroutine.
			ch = make(chan model.Event)
			matches[e.MatchID] = ch
			wg.Add(1)
			go d.collect(ch, &wg)
		}
		ch <- e

		if e.Type == validation.TypeMatchFinished {
			// No habrá más eventos para esta partida: cerrar su canal hace
			// que su goroutine salga del range y arme el lote.
			close(ch)
			delete(matches, e.MatchID)
		}

		sub.reply <- nil
	}

	// in se cerró (apagado): cerramos las partidas que quedaron abiertas,
	// esperamos a que todas las goroutines terminen y recién entonces
	// cerramos out (nadie más va a enviar ahí).
	for _, ch := range matches {
		close(ch)
	}
	wg.Wait()
	close(d.out)
}

// collect es la goroutine de UNA partida: acumula sus eventos hasta que el
// router cierra su canal. Si la partida terminó, arma el lote y lo envía.
func (d *Dispatcher) collect(in <-chan model.Event, wg *sync.WaitGroup) {
	defer wg.Done()

	var events []model.Event
	finished := false
	for e := range in {
		events = append(events, e)
		if e.Type == validation.TypeMatchFinished {
			finished = true
		}
	}

	// Si el canal se cerró por apagado y no por MATCH_FINISHED, la partida
	// está incompleta: no se envía a Scala.
	if !finished {
		return
	}
	d.out <- buildBatch(events)
}

// buildBatch ordena los eventos por timestamp y arma el lote.
func buildBatch(events []model.Event) model.Batch {
	// SliceStable: si dos eventos tienen el mismo timestamp, conservan el
	// orden de llegada. Se compara como time.Time y no como texto, porque
	// "…:22Z" y "…:22.000Z" son el mismo instante pero como texto no.
	sort.SliceStable(events, func(i, j int) bool {
		return parseTime(events[i].Timestamp).Before(parseTime(events[j].Timestamp))
	})

	first := events[0]
	b := model.Batch{
		MatchID:     first.MatchID,
		GameID:      first.GameID,
		GameVersion: first.GameVersion,
		StartedAt:   first.Timestamp, // si nunca llegó MATCH_STARTED, el primer evento
		Players:     []string{},
		Events:      events,
	}

	seen := make(map[string]bool)
	for _, e := range events {
		switch e.Type {
		case validation.TypeMatchStarted:
			b.StartedAt = e.Timestamp
		case validation.TypeMatchFinished:
			b.FinishedAt = e.Timestamp
		}
		if e.PlayerID != "" && !seen[e.PlayerID] {
			seen[e.PlayerID] = true
			b.Players = append(b.Players, e.PlayerID)
		}
	}
	sort.Strings(b.Players)
	return b
}

// parseTime convierte un timestamp ya validado. El error se ignora porque
// Validate ya comprobó que está en RFC 3339.
func parseTime(ts string) time.Time {
	t, _ := time.Parse(time.RFC3339, ts)
	return t
}
