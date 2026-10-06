// Package api contiene los handlers HTTP del servidor de ingestión.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"gamestats/ingestion/internal/model"
)

// maxBodyBytes limita el tamaño del cuerpo de POST /events (1 MB) para que
// un cliente no pueda mandar un cuerpo gigante y agotar la memoria.
const maxBodyBytes = 1 << 20

// Health responde {"status":"ok"} para indicar que el servidor está vivo.
func Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Events recibe uno ({...}) o varios ([...]) eventos, los imprime en consola
// y responde cuántos recibió.
func Events(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "el cuerpo supera el tamaño máximo permitido")
			return
		}
		writeError(w, http.StatusBadRequest, "no se pudo leer el cuerpo")
		return
	}

	events, err := parseEvents(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}

	for _, e := range events {
		log.Printf("evento recibido: matchId=%s eventId=%s type=%s action=%s",
			e.MatchID, e.EventID, e.Type, e.Action)
	}

	writeJSON(w, http.StatusOK, map[string]int{"received": len(events)})
}

// parseEvents acepta un objeto o una lista. Mira el primer carácter que no es
// espacio: si es '[' decodifica una lista; si no, un solo evento.
func parseEvents(body []byte) ([]model.Event, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errors.New("cuerpo vacío")
	}

	if trimmed[0] == '[' {
		var events []model.Event
		if err := json.Unmarshal(trimmed, &events); err != nil {
			return nil, err
		}
		return events, nil
	}

	var e model.Event
	if err := json.Unmarshal(trimmed, &e); err != nil {
		return nil, err
	}
	return []model.Event{e}, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
