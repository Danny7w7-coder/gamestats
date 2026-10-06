// Servidor de ingestión de GameStats.
// Recibe eventos de partidas, los valida y los envía a Scala para su análisis.
package main

import (
	"log"
	"net/http"

	"gamestats/ingestion/internal/api"
	"gamestats/ingestion/internal/model"
	"gamestats/ingestion/internal/pipeline"
	"gamestats/ingestion/internal/rejected"
	"gamestats/ingestion/internal/state"
)

func main() {
	rejectedLog, err := rejected.New("logs/rejected/rejected.jsonl")
	if err != nil {
		log.Fatal(err)
	}
	defer rejectedLog.Close()

	store := state.NewMatchStore()
	dispatcher := pipeline.New(store)

	// Consumidor temporal: imprime cada lote. En el Bloque 6 lo reemplazan
	// los senders que envían a Scala.
	go printBatches(dispatcher.Out())

	h := &api.Handler{Rejected: rejectedLog, Store: store, Pipeline: dispatcher}

	mux := http.NewServeMux()
	// Desde Go 1.22 el patrón puede incluir el método: otro método recibe 405.
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("POST /events", h.Events)
	mux.HandleFunc("GET /matches", h.Matches)

	// Por ahora el puerto es fijo; en el Bloque 6 se leerá de la variable PORT.
	addr := ":8080"
	log.Printf("servidor de ingestión escuchando en %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

// printBatches lee los lotes terminados hasta que el canal se cierre.
func printBatches(batches <-chan model.Batch) {
	for b := range batches {
		ids := make([]string, len(b.Events))
		for i, e := range b.Events {
			ids[i] = e.EventID
		}
		log.Printf("LOTE LISTO matchId=%s gameId=%s eventos=%d jugadores=%v startedAt=%s finishedAt=%s\n  orden: %v",
			b.MatchID, b.GameID, len(b.Events), b.Players, b.StartedAt, b.FinishedAt, ids)
	}
}
