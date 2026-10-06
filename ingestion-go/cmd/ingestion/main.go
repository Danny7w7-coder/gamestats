// Servidor de ingestión de GameStats.
// Recibe eventos de partidas, los valida y los envía a Scala para su análisis.
package main

import (
	"log"
	"net/http"

	"gamestats/ingestion/internal/api"
	"gamestats/ingestion/internal/client"
	"gamestats/ingestion/internal/config"
	"gamestats/ingestion/internal/pipeline"
	"gamestats/ingestion/internal/rejected"
	"gamestats/ingestion/internal/state"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	rejectedLog, err := rejected.New("logs/rejected/rejected.jsonl")
	if err != nil {
		log.Fatal(err)
	}
	defer rejectedLog.Close()

	store := state.NewMatchStore()
	dispatcher := pipeline.New(store)

	// WORKERS senders leen los lotes terminados y los envían a Scala.
	go pipeline.RunSenders(cfg.Workers, dispatcher.Out(), client.New(cfg.ScalaURL), store)

	h := &api.Handler{Rejected: rejectedLog, Store: store, Pipeline: dispatcher}

	mux := http.NewServeMux()
	// Desde Go 1.22 el patrón puede incluir el método: otro método recibe 405.
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("POST /events", h.Events)
	mux.HandleFunc("GET /matches", h.Matches)

	addr := ":" + cfg.Port
	log.Printf("servidor de ingestión escuchando en %s (SCALA_URL=%s, WORKERS=%d)", addr, cfg.ScalaURL, cfg.Workers)
	log.Fatal(http.ListenAndServe(addr, mux))
}
