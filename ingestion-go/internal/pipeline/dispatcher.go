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
//     Lo cierra al reenviar MATCH_FINISHED, al abandonar la partida por
//     inactividad o al apagarse.
//   - out: el router, después de que TODAS las goroutines de partida
//     terminaron (las espera con un WaitGroup), porque ellas son las que
//     envían a out.
package pipeline

import (
	"errors"
	"log"
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

// ErrMatchAbandoned se devuelve cuando llega un evento de una partida que se
// abandonó por inactividad.
var ErrMatchAbandoned = errors.New("partida abandonada por inactividad")

// outBuffer deja que unas cuantas partidas terminadas esperen en el canal de
// salida si los senders están ocupados (por ejemplo, reintentando con Scala).
const outBuffer = 16

// submission es lo que viaja por el canal de entrada: el evento y un canal
// por donde el router responde si lo aceptó.
type submission struct {
	event model.Event
	reply chan error
}

// openMatch es una partida en curso, vista desde el router.
type openMatch struct {
	ch       chan model.Event // canal de la goroutine de la partida
	lastSeen time.Time        // hora (real, del servidor) de su último evento
}

// Dispatcher conecta el handler HTTP con las goroutines de cada partida.
type Dispatcher struct {
	in          chan submission
	out         chan model.Batch
	store       *state.MatchStore
	idleTimeout time.Duration // sin eventos durante este tiempo, la partida se abandona
}

// New crea el dispatcher y arranca la goroutine del router. idleTimeout
// (IDLE_TIMEOUT) debe ser mayor que 0.
func New(store *state.MatchStore, idleTimeout time.Duration) *Dispatcher {
	d := &Dispatcher{
		in:          make(chan submission),
		out:         make(chan model.Batch, outBuffer),
		store:       store,
		idleTimeout: idleTimeout,
	}
	go d.route()
	return d
}

// Submit entrega un evento (ya validado) al router y espera su respuesta:
// nil si lo aceptó, o ErrMatchFinished / ErrMatchAbandoned si la partida ya
// no está en curso.
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
//
// El select espera dos cosas a la vez: un evento nuevo (in) o el tic del
// reloj que revisa si hay partidas inactivas.
func (d *Dispatcher) route() {
	matches := make(map[string]*openMatch) // matchId -> partida en curso
	var wg sync.WaitGroup                  // goroutines de partida vivas

	// Se revisa cada medio IDLE_TIMEOUT: una partida se abandona entre 1 y
	// 1.5 veces IDLE_TIMEOUT después de su último evento.
	ticker := time.NewTicker(d.idleTimeout / 2)
	defer ticker.Stop()

	for {
		select {
		case sub, ok := <-d.in:
			if !ok {
				// in se cerró (apagado): cerramos las partidas que quedaron
				// abiertas (no se envían), esperamos a que todas las
				// goroutines terminen y recién entonces cerramos out
				// (nadie más va a enviar ahí).
				if len(matches) > 0 {
					log.Printf("apagado: %d partidas sin terminar no se envían a Scala", len(matches))
				}
				for _, m := range matches {
					close(m.ch)
				}
				wg.Wait()
				close(d.out)
				return
			}
			sub.reply <- d.dispatch(sub.event, matches, &wg)

		case now := <-ticker.C:
			d.abandonIdle(now, matches)
		}
	}
}

// dispatch entrega un evento a la goroutine de su partida (creándola si es
// la primera vez) y devuelve la respuesta para Submit. Solo la llama el router.
func (d *Dispatcher) dispatch(e model.Event, matches map[string]*openMatch, wg *sync.WaitGroup) error {
	// Evento tardío: si la partida ya no está en curso, su lote ya salió (o
	// se abandonó). Se rechaza y NO se crea una goroutine nueva.
	if status, ok := d.store.Status(e.MatchID); ok && status != state.StatusInProgress {
		if status == state.StatusAbandoned {
			return ErrMatchAbandoned
		}
		return ErrMatchFinished
	}

	// El router es el único que registra eventos en el store, así el
	// estado siempre refleja el orden en que el router los procesó.
	d.store.Record(e)

	m, ok := matches[e.MatchID]
	if !ok {
		// Primera vez que vemos esta partida: creamos su goroutine.
		m = &openMatch{ch: make(chan model.Event)}
		matches[e.MatchID] = m
		wg.Add(1)
		go d.collect(m.ch, wg)
	}
	m.lastSeen = time.Now()
	m.ch <- e

	if e.Type == validation.TypeMatchFinished {
		// No habrá más eventos para esta partida: cerrar su canal hace
		// que su goroutine salga del range y arme el lote.
		close(m.ch)
		delete(matches, e.MatchID)
	}
	return nil
}

// abandonIdle abandona las partidas que llevan idleTimeout sin eventos: las
// marca como abandonadas y cierra su canal. Como no llegó MATCH_FINISHED, su
// goroutine termina sin armar lote (no se envían a Scala). Solo la llama el
// router.
func (d *Dispatcher) abandonIdle(now time.Time, matches map[string]*openMatch) {
	for id, m := range matches {
		if now.Sub(m.lastSeen) >= d.idleTimeout {
			d.store.SetStatus(id, state.StatusAbandoned)
			close(m.ch)
			delete(matches, id) // borrar durante un range es válido en Go
			log.Printf("partida %s abandonada: sin eventos durante %s", id, d.idleTimeout)
		}
	}
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
