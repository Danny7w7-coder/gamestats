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
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"gamestats/ingestion/internal/model"
	"gamestats/ingestion/internal/persist"
	"gamestats/ingestion/internal/state"
	"gamestats/ingestion/internal/validation"
)

// ErrMatchFinished se devuelve cuando llega un evento de una partida que ya
// terminó: su lote ya se armó, así que el evento se rechaza.
var ErrMatchFinished = errors.New("partida ya finalizada")

// ErrMatchAbandoned se devuelve cuando llega un evento de una partida que se
// abandonó por inactividad.
var ErrMatchAbandoned = errors.New("partida abandonada por inactividad")

// ErrUnknownMatch se devuelve cuando el primer evento que se ve de una partida
// no es MATCH_STARTED: la partida empezó sin que ingestion la viera (por
// ejemplo, antes de un reinicio). Si se aceptara, se mandaría a Scala una
// partida truncada con estadísticas falsas, así que se rechaza y no se crea.
var ErrUnknownMatch = errors.New("partida desconocida: no se vio MATCH_STARTED")

// ErrPersist se devuelve cuando DATA_DIR está activo y el evento no se pudo
// guardar en disco: no se acepta un evento que un reinicio perdería.
var ErrPersist = errors.New("no se pudo guardar el evento en disco")

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
	timeout  time.Duration    // inactividad que tolera, según su juego
}

// Dispatcher conecta el handler HTTP con las goroutines de cada partida.
type Dispatcher struct {
	in       chan submission
	out      chan model.Batch
	store    *state.MatchStore
	timeouts IdleTimeouts     // sin eventos durante este tiempo (según el juego), la partida se abandona
	journal  *persist.Journal // archivos de las partidas en curso; nil = sin persistencia

	// matches y wg los usa SOLO el router (y el constructor antes de
	// arrancarlo), por eso no necesitan mutex.
	matches map[string]*openMatch // matchId -> partida en curso
	wg      sync.WaitGroup        // goroutines de partida vivas
}

// Restored resume lo que NewPersistent reconstruyó desde DATA_DIR.
type Restored struct {
	InProgress int // partidas que siguen en curso
	Finished   int // partidas que ya tenían MATCH_FINISHED: se envían a Scala
	Events     int // eventos restaurados
	Skipped    int // líneas corruptas o eventos que no se pudieron restaurar
	Discarded  int // archivos de los que no se restauró nada (se apartan)
}

// New crea el dispatcher sin persistencia y arranca la goroutine del router.
// Todos los timeouts (IDLE_TIMEOUT y los de IDLE_TIMEOUT_BY_GAME) deben ser
// mayores que 0.
func New(store *state.MatchStore, timeouts IdleTimeouts) *Dispatcher {
	d := newDispatcher(store, timeouts, nil)
	go d.route()
	return d
}

// NewPersistent crea el dispatcher con persistencia en j (DATA_DIR):
//   - reconstruye el store y las partidas en curso con los archivos de j,
//     pasando sus eventos por la misma lógica que un evento nuevo, pero sin
//     volver a guardarlos. El timeout de abandono de cada partida cuenta
//     desde la hora (del servidor) de su último evento guardado;
//   - cada evento aceptado desde ahora se guarda en disco antes de que
//     Submit responda;
//   - el archivo de una partida se borra cuando llega a un estado final
//     (enviada, abandonada o error_envio).
//
// Debe llamarse antes de que alguien más use el store. Quien llama debe
// empezar a leer Out() enseguida: las partidas restauradas que ya tenían
// MATCH_FINISHED se envían a Scala.
func NewPersistent(store *state.MatchStore, timeouts IdleTimeouts, j *persist.Journal) (*Dispatcher, Restored, error) {
	saved, err := j.Load()
	if err != nil {
		return nil, Restored{}, err
	}
	// Antes de restaurar: un estado final (que llega después, desde el
	// router o los senders) borra el archivo de la partida.
	store.OnFinal(j.Remove)

	d := newDispatcher(store, timeouts, j)
	r := d.restore(saved)
	go d.route()
	return d, r, nil
}

func newDispatcher(store *state.MatchStore, timeouts IdleTimeouts, j *persist.Journal) *Dispatcher {
	return &Dispatcher{
		in:       make(chan submission),
		out:      make(chan model.Batch, outBuffer),
		store:    store,
		timeouts: timeouts,
		journal:  j,
		matches:  make(map[string]*openMatch),
	}
}

// restore pasa por dispatch los eventos guardados de cada partida, sin
// volver a guardarlos. Corre antes de que arranque el router, en la misma
// goroutine que el constructor, así que puede usar matches y wg.
func (d *Dispatcher) restore(saved []persist.Saved) Restored {
	var r Restored
	for _, s := range saved {
		r.Skipped += s.Skipped
		matchID, accepted := "", 0
		for _, e := range s.Events {
			err := validation.Validate(e)
			if err == nil {
				err = d.dispatch(e, s.LastSeen, false)
			}
			if err != nil {
				r.Skipped++
				log.Printf("aviso: %s: el evento %s no se restaura: %v", s.File, e.EventID, err)
				continue
			}
			matchID = e.MatchID
			accepted++
		}
		if accepted == 0 {
			r.Discarded++
			log.Printf("aviso: %s no tiene eventos que restaurar; se aparta como %s.descartado", s.File, s.File)
			d.journal.Discard(s.File)
			continue
		}
		r.Events += accepted
		if status, _ := d.store.Status(matchID); status == state.StatusInProgress {
			r.InProgress++
		} else {
			r.Finished++
		}
	}
	return r
}

// Submit entrega un evento (ya validado) al router y espera su respuesta:
// nil si lo aceptó, ErrMatchFinished / ErrMatchAbandoned si la partida ya
// no está en curso, o ErrUnknownMatch si es el primer evento de una partida
// y no es MATCH_STARTED.
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
// los canales de partida, espera a sus goroutines y cierra out. Con
// persistencia, las partidas en curso quedan en disco para el próximo arranque.
func (d *Dispatcher) Close() {
	close(d.in)
}

// route es la goroutine del router. matches lo usa SOLO esta goroutine, por
// eso no necesita mutex.
//
// El select espera dos cosas a la vez: un evento nuevo (in) o el tic del
// reloj que revisa si hay partidas inactivas.
func (d *Dispatcher) route() {
	// Se revisa cada medio timeout del juego más impaciente: una partida se
	// abandona nunca antes del timeout de SU juego, y a más tardar medio
	// timeout del juego más impaciente después.
	ticker := time.NewTicker(d.timeouts.min() / 2)
	defer ticker.Stop()

	for {
		select {
		case sub, ok := <-d.in:
			if !ok {
				// in se cerró (apagado): cerramos las partidas que quedaron
				// abiertas (no se envían), esperamos a que todas las
				// goroutines terminen y recién entonces cerramos out
				// (nadie más va a enviar ahí).
				if n := len(d.matches); n > 0 {
					if d.journal != nil {
						log.Printf("apagado: %d partidas en curso quedan guardadas en %s y se retoman al reiniciar", n, d.journal.Dir())
					} else {
						log.Printf("apagado: %d partidas sin terminar no se envían a Scala", n)
					}
				}
				for _, m := range d.matches {
					close(m.ch)
				}
				d.wg.Wait()
				close(d.out)
				return
			}
			sub.reply <- d.dispatch(sub.event, time.Now(), true)

		case now := <-ticker.C:
			d.abandonIdle(now)
		}
	}
}

// dispatch entrega un evento a la goroutine de su partida (creándola si es
// la primera vez) y devuelve la respuesta para Submit. receivedAt es la hora
// del servidor al recibirlo (cuenta para el abandono por inactividad). Con
// save, el evento se guarda en disco antes de aceptarlo; restore lo llama sin
// save porque el evento ya está guardado. Solo la llaman el router y restore.
func (d *Dispatcher) dispatch(e model.Event, receivedAt time.Time, save bool) error {
	// Evento tardío: si la partida ya no está en curso, su lote ya salió (o
	// se abandonó). Se rechaza y NO se crea una goroutine nueva.
	status, known := d.store.Status(e.MatchID)
	if known && status != state.StatusInProgress {
		if status == state.StatusAbandoned {
			return ErrMatchAbandoned
		}
		return ErrMatchFinished
	}
	// Partida que el store no conoce: solo MATCH_STARTED puede crearla.
	if !known && e.Type != validation.TypeMatchStarted {
		return ErrUnknownMatch
	}

	// Primero al disco: si no se puede guardar, el evento no se acepta (un
	// reinicio lo perdería sin que la fuente lo sepa).
	if save {
		if err := d.journal.Append(e, receivedAt); err != nil {
			log.Printf("ERROR al guardar el evento %s de %s: %v", e.EventID, e.MatchID, err)
			return fmt.Errorf("%w: %v", ErrPersist, err)
		}
	}

	// El router es el único que registra eventos en el store, así el
	// estado siempre refleja el orden en que el router los procesó.
	d.store.Record(e)

	m, ok := d.matches[e.MatchID]
	if !ok {
		// Primera vez que vemos esta partida: creamos su goroutine.
		m = &openMatch{ch: make(chan model.Event), timeout: d.timeouts.For(e.GameID)}
		d.matches[e.MatchID] = m
		d.wg.Add(1)
		go d.collect(m.ch, &d.wg)
	}
	m.lastSeen = receivedAt
	m.ch <- e

	if e.Type == validation.TypeMatchFinished {
		// No habrá más eventos para esta partida: cerrar su canal hace
		// que su goroutine salga del range y arme el lote.
		close(m.ch)
		delete(d.matches, e.MatchID)
	}
	return nil
}

// abandonIdle abandona las partidas que llevan el timeout de su juego sin eventos: las
// marca como abandonadas y cierra su canal. Como no llegó MATCH_FINISHED, su
// goroutine termina sin armar lote (no se envían a Scala). Marcarla como
// abandonada borra su archivo en DATA_DIR. Solo la llama el router.
func (d *Dispatcher) abandonIdle(now time.Time) {
	for id, m := range d.matches {
		if now.Sub(m.lastSeen) >= m.timeout {
			d.store.SetStatus(id, state.StatusAbandoned)
			close(m.ch)
			delete(d.matches, id) // borrar durante un range es válido en Go
			log.Printf("partida %s abandonada: sin eventos durante %s", id, m.timeout)
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
		StartedAt:   first.Timestamp, // se reemplaza abajo por el de MATCH_STARTED
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
