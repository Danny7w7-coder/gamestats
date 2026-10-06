package pipeline

import (
	"log"
	"sync"

	"gamestats/ingestion/internal/client"
	"gamestats/ingestion/internal/model"
	"gamestats/ingestion/internal/state"
)

// RunSenders arranca n goroutines (los "senders") que leen lotes del canal
// batches y los envían a Scala. Varios senders permiten enviar varias
// partidas a la vez: si una está esperando un reintento, las demás avanzan.
//
// Todos leen del MISMO canal; Go garantiza que cada lote lo recibe un solo
// sender. RunSenders se bloquea hasta que batches se cierra y todos los
// senders terminan (por eso se llama con "go RunSenders(...)").
func RunSenders(n int, batches <-chan model.Batch, scala *client.ScalaClient, store *state.MatchStore) {
	var wg sync.WaitGroup
	for i := 1; i <= n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for b := range batches {
				send(id, b, scala, store)
			}
		}(i)
	}
	wg.Wait()
}

// send envía un lote y deja el resultado en el store.
func send(senderID int, b model.Batch, scala *client.ScalaClient, store *state.MatchStore) {
	log.Printf("sender %d: enviando %s (%d eventos) a Scala", senderID, b.MatchID, len(b.Events))
	if err := scala.Send(b); err != nil {
		log.Printf("sender %d: ERROR al enviar %s: %v", senderID, b.MatchID, err)
		store.SetStatus(b.MatchID, state.StatusSendError)
		return
	}
	log.Printf("sender %d: %s enviada a Scala", senderID, b.MatchID)
	store.SetStatus(b.MatchID, state.StatusSent)
}
