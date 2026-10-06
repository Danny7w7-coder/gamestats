// Servidor de ingestión de GameStats.
// Recibe eventos de partidas, los valida y los envía a Scala para su análisis.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

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
	dispatcher := pipeline.New(store, cfg.IdleTimeout)

	// WORKERS senders leen los lotes terminados y los envían a Scala.
	// El WaitGroup permite esperar, al apagar, a que terminen de enviar.
	var senders sync.WaitGroup
	senders.Add(1)
	go func() {
		defer senders.Done()
		pipeline.RunSenders(cfg.Workers, dispatcher.Out(), client.New(cfg.ScalaURL), store)
	}()

	h := &api.Handler{Rejected: rejectedLog, Store: store, Pipeline: dispatcher}

	mux := http.NewServeMux()
	// Desde Go 1.22 el patrón puede incluir el método: otro método recibe 405.
	mux.HandleFunc("GET /health", h.Health)
	mux.HandleFunc("POST /events", h.Events)
	mux.HandleFunc("GET /matches", h.Matches)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: mux}

	// ctx se cancela con Ctrl+C (os.Interrupt) o con SIGTERM (lo que manda
	// "docker stop").
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("servidor de ingestión escuchando en %s (SCALA_URL=%s, WORKERS=%d, IDLE_TIMEOUT=%s)",
			srv.Addr, cfg.ScalaURL, cfg.Workers, cfg.IdleTimeout)
		// Al apagar, ListenAndServe devuelve ErrServerClosed: no es un error.
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	stop() // desde aquí, un segundo Ctrl+C cierra el programa de inmediato
	log.Print("apagando: no se aceptan más eventos")

	// Apagado ordenado, en este orden:
	// 1. Dejar de aceptar peticiones y esperar a las que están en curso.
	//    Sin timeout a propósito: Close (paso 2) no puede ocurrir mientras un
	//    handler todavía pueda llamar a Submit (enviaría a un channel cerrado).
	if err := srv.Shutdown(context.Background()); err != nil {
		log.Printf("error al cerrar el servidor HTTP: %v", err)
	}
	// 2. Cerrar el pipeline: el router cierra las partidas abiertas, espera a
	//    sus goroutines y cierra el canal de salida.
	dispatcher.Close()
	// 3. Esperar a que los senders envíen los lotes que quedaban pendientes.
	senders.Wait()
	log.Print("apagado completo")
}
