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
	"gamestats/ingestion/internal/rejected"
	"gamestats/ingestion/internal/validation"
)

// maxBodyBytes limita el tamaño del cuerpo de POST /events (1 MB) para que
// un cliente no pueda mandar un cuerpo gigante y agotar la memoria.
const maxBodyBytes = 1 << 20

// Handler agrupa los endpoints y lo que necesitan para trabajar.
type Handler struct {
	Rejected *rejected.Logger // donde se anotan los eventos rechazados
}

// rejection es un evento rechazado dentro de la respuesta de POST /events.
type rejection struct {
	EventID string `json:"eventId"`
	Reason  string `json:"reason"`
}

// eventsResponse es la respuesta de POST /events.
type eventsResponse struct {
	Received int         `json:"received"`
	Accepted int         `json:"accepted"`
	Rejected []rejection `json:"rejected"`
}

// Health responde {"status":"ok"} para indicar que el servidor está vivo.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Events recibe uno ({...}) o varios ([...]) eventos, valida cada uno y
// responde cuántos aceptó y por qué rechazó los demás.
func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
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

	raws, err := splitEvents(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}

	resp := eventsResponse{Received: len(raws), Rejected: []rejection{}}
	for _, raw := range raws {
		var e model.Event
		if err := json.Unmarshal(raw, &e); err != nil {
			// Por ejemplo "timestamp": 5. Se rechaza solo este evento.
			h.reject(&resp, raw, e.EventID, "el evento no tiene el formato esperado: "+err.Error())
			continue
		}
		if err := validation.Validate(e); err != nil {
			h.reject(&resp, raw, e.EventID, err.Error())
			continue
		}

		resp.Accepted++
		log.Printf("evento aceptado: matchId=%s eventId=%s type=%s action=%s",
			e.MatchID, e.EventID, e.Type, e.Action)
	}

	writeJSON(w, http.StatusOK, resp)
}

// reject agrega el rechazo a la respuesta y lo anota en rejected.jsonl.
// Si el archivo falla, se avisa en consola pero la petición sigue adelante.
func (h *Handler) reject(resp *eventsResponse, raw json.RawMessage, eventID, reason string) {
	resp.Rejected = append(resp.Rejected, rejection{EventID: eventID, Reason: reason})
	log.Printf("evento rechazado: eventId=%s motivo=%s", eventID, reason)
	if err := h.Rejected.Log(raw, reason); err != nil {
		log.Printf("no se pudo escribir en el log de rechazados: %v", err)
	}
}

// splitEvents separa el cuerpo en eventos crudos, sin interpretarlos todavía.
// Se guardan crudos para poder anotar en el log el evento tal como llegó.
// Si el primer carácter que no es espacio es '[', es una lista; si no, un
// solo evento.
func splitEvents(body []byte) ([]json.RawMessage, error) {
	// Algunos programas de Windows (por ejemplo PowerShell 5.1) agregan una
	// marca BOM de UTF-8 al inicio del archivo. encoding/json no la acepta,
	// así que se quita antes de decodificar.
	body = bytes.TrimPrefix(body, []byte("\xEF\xBB\xBF"))
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errors.New("cuerpo vacío")
	}

	if trimmed[0] == '[' {
		var raws []json.RawMessage
		if err := json.Unmarshal(trimmed, &raws); err != nil {
			return nil, err
		}
		return raws, nil
	}

	var raw json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, err
	}
	return []json.RawMessage{raw}, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
