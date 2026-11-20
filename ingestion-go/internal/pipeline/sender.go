package pipeline

import (
	"errors"
	"log"
	"sync"
	"time"

	"gamestats/ingestion/internal/client"
	"gamestats/ingestion/internal/model"
	"gamestats/ingestion/internal/state"
)

// ResendEvery es cada cuánto se vuelve a mandar una partida que Scala no
// recibió (después de los 3 reintentos rápidos del cliente).
const ResendEvery = 30 * time.Second

// RunSenders arranca n goroutines (los "senders") que leen lotes del canal
// batches y los envían a Scala. Varios senders permiten enviar varias
// partidas a la vez: si una está esperando un reintento, las demás avanzan.
//
// Todos leen del MISMO canal; Go garantiza que cada lote lo recibe un solo
// sender. RunSenders se bloquea hasta que batches se cierra y todos los
// senders terminan (por eso se llama con "go RunSenders(...)").
//
// Si Scala no responde (caído, timeout o 5xx), la partida queda como
// reintentando y se vuelve a mandar cada resendEvery, sin límite, hasta que
// Scala la reciba. La espera no ocupa a ningún sender: un temporizador
// devuelve el lote al canal resend. Si Scala la rechaza (4xx), queda
// error_envio y no se reintenta.
//
// Al apagarse (batches cerrado), las partidas que esperaban su reenvío se
// sueltan: con DATA_DIR siguen en disco (reintentando no es final) y se
// envían al próximo arranque; sin DATA_DIR se pierden, y se avisa en el log.
func RunSenders(n int, batches <-chan model.Batch, scala *client.ScalaClient, store *state.MatchStore, resendEvery time.Duration) {
	r := &resender{every: resendEvery, ch: make(chan model.Batch), stop: make(chan struct{}), timers: map[string]*time.Timer{}}

	var wg sync.WaitGroup
	for i := 1; i <= n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case b, ok := <-batches:
					if !ok {
						return
					}
					send(id, b, scala, store, r)
				case b := <-r.ch:
					send(id, b, scala, store, r)
				}
			}
		}(i)
	}
	wg.Wait()
	if left := r.shutdown(); left > 0 {
		log.Printf("apagado: %d partidas sin llegar a Scala; con DATA_DIR se reenvían al reiniciar, sin DATA_DIR se pierden", left)
	}
}

// send envía un lote y deja el resultado en el store.
func send(senderID int, b model.Batch, scala *client.ScalaClient, store *state.MatchStore, r *resender) {
	log.Printf("sender %d: enviando %s (%d eventos) a Scala", senderID, b.MatchID, len(b.Events))
	err := scala.Send(b)
	switch {
	case err == nil:
		log.Printf("sender %d: %s enviada a Scala", senderID, b.MatchID)
		store.SetStatus(b.MatchID, state.StatusSent)
	case errors.Is(err, client.ErrRejected):
		log.Printf("sender %d: ERROR, %s no se reintenta: %v", senderID, b.MatchID, err)
		store.SetStatus(b.MatchID, state.StatusSendError)
	default:
		log.Printf("sender %d: %s no llegó a Scala (%v); se reenvía en %s", senderID, b.MatchID, err, r.every)
		store.SetStatus(b.MatchID, state.StatusRetrying)
		r.later(b)
	}
}

// resender guarda las partidas que esperan su reenvío. Cada una tiene un
// temporizador; al vencer, el lote vuelve a los senders por ch.
type resender struct {
	every time.Duration
	ch    chan model.Batch // sin buffer: el lote lo toma un sender libre
	stop  chan struct{}    // se cierra al apagar: suelta a los temporizadores

	mu     sync.Mutex
	timers map[string]*time.Timer // matchId -> temporizador pendiente
	closed bool
}

// later programa el reenvío de b. La función del temporizador corre en su
// propia goroutine, así que esperar a un sender libre no frena a nadie.
func (r *resender) later(b model.Batch) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	var t *time.Timer
	t = time.AfterFunc(r.every, func() {
		select {
		case r.ch <- b:
			r.mu.Lock()
			// Solo si sigue siendo el suyo: si el reenvío ya falló otra vez,
			// el sender programó un temporizador nuevo para esta partida.
			if r.timers[b.MatchID] == t {
				delete(r.timers, b.MatchID)
			}
			r.mu.Unlock()
		case <-r.stop: // apagado: el lote se queda donde está (en disco, si hay DATA_DIR)
		}
	})
	r.timers[b.MatchID] = t
}

// shutdown detiene los reenvíos pendientes y devuelve cuántos había. Se
// llama cuando ya no queda ningún sender.
func (r *resender) shutdown() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	close(r.stop)
	for _, t := range r.timers {
		t.Stop()
	}
	return len(r.timers)
}
