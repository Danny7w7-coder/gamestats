// Servidor de ingestión de GameStats.
// Recibe eventos de partidas, los valida y los envía a Scala para su análisis.
package main

import (
	"log"
	"net/http"

	"gamestats/ingestion/internal/api"
)

func main() {
	mux := http.NewServeMux()
	// Desde Go 1.22 el patrón puede incluir el método: otro método recibe 405.
	mux.HandleFunc("GET /health", api.Health)
	mux.HandleFunc("POST /events", api.Events)

	// Por ahora el puerto es fijo; en el Bloque 6 se leerá de la variable PORT.
	addr := ":8080"
	log.Printf("servidor de ingestión escuchando en %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
