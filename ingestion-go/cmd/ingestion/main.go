// Servidor de ingestión de GameStats.
// Recibe eventos de partidas, los valida y los envía a Scala para su análisis.
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// healthHandler responde {"status":"ok"} para indicar que el servidor está vivo.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func main() {
	mux := http.NewServeMux()
	// Desde Go 1.22 el patrón puede incluir el método: solo acepta GET.
	mux.HandleFunc("GET /health", healthHandler)

	// Por ahora el puerto es fijo; en el Bloque 6 se leerá de la variable PORT.
	addr := ":8080"
	log.Printf("servidor de ingestión escuchando en %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
