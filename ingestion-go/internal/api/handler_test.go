package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gamestats/ingestion/internal/rejected"
)

const validEv = `{"eventId":"e1","timestamp":"2026-10-05T14:01:22Z","gameId":"racing",
	"gameVersion":"1.0","matchId":"race-001","playerId":"p1","type":"PLAYER_ACTION",
	"action":"LAP_COMPLETED","data":{"lap":1}}`

// newTestHandler crea un Handler que escribe el log en una carpeta temporal.
func newTestHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rejected.jsonl")
	l, err := rejected.New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return &Handler{Rejected: l}, path
}

func postEvents(h *Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Events(rec, req)
	return rec
}

func TestEvents(t *testing.T) {
	gameGoalInRacing := strings.Replace(validEv, `"LAP_COMPLETED"`, `"GOAL"`, 1)
	badTimestampType := strings.Replace(validEv, `"2026-10-05T14:01:22Z"`, `5`, 1)

	tests := []struct {
		name         string
		body         string
		wantStatus   int
		wantReceived int
		wantAccepted int
		wantRejected int
	}{
		{"un objeto válido", validEv, http.StatusOK, 1, 1, 0},
		{"lista de válidos", "[" + validEv + "," + validEv + "]", http.StatusOK, 2, 2, 0},
		{"lista vacía", "[]", http.StatusOK, 0, 0, 0},
		{"con BOM de UTF-8", "\xEF\xBB\xBF[" + validEv + "]", http.StatusOK, 1, 1, 0},
		{"un objeto inválido", gameGoalInRacing, http.StatusOK, 1, 0, 1},
		{"lista mezclada", "[" + validEv + "," + gameGoalInRacing + "]", http.StatusOK, 2, 1, 1},
		{"campo con tipo equivocado", "[" + badTimestampType + "," + validEv + "]", http.StatusOK, 2, 1, 1},
		{"elemento que no es objeto", `["hola"]`, http.StatusOK, 1, 0, 1},
		{"JSON inválido", `{"eventId":`, http.StatusBadRequest, 0, 0, 0},
		{"cuerpo vacío", "", http.StatusBadRequest, 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, logPath := newTestHandler(t)
			rec := postEvents(h, tt.body)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, se esperaba %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus != http.StatusOK {
				return
			}

			var resp eventsResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("respuesta no es JSON: %v", err)
			}
			if resp.Received != tt.wantReceived || resp.Accepted != tt.wantAccepted || len(resp.Rejected) != tt.wantRejected {
				t.Fatalf("respuesta = %+v, se esperaba received=%d accepted=%d rejected=%d",
					resp, tt.wantReceived, tt.wantAccepted, tt.wantRejected)
			}

			// Cada rechazo debe quedar anotado en el log.
			data, _ := os.ReadFile(logPath)
			if got := strings.Count(string(data), "\n"); got != tt.wantRejected {
				t.Fatalf("líneas en el log = %d, se esperaban %d", got, tt.wantRejected)
			}
		})
	}
}

func TestEventsRejectedReason(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := postEvents(h, strings.Replace(validEv, `"LAP_COMPLETED"`, `"GOAL"`, 1))

	var resp eventsResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	want := rejection{EventID: "e1", Reason: `la acción "GOAL" no existe en el juego "racing"`}
	if len(resp.Rejected) != 1 || resp.Rejected[0] != want {
		t.Fatalf("rejected = %+v, se esperaba %+v", resp.Rejected, want)
	}
}

func TestEventsBodyTooLarge(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := postEvents(h, strings.Repeat(" ", maxBodyBytes+1))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}
