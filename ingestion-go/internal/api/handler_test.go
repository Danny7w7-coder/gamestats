package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gamestats/ingestion/internal/pipeline"
	"gamestats/ingestion/internal/rejected"
	"gamestats/ingestion/internal/state"
)

const validEv = `{"eventId":"e1","timestamp":"2026-10-05T14:01:22Z","gameId":"racing",
	"gameVersion":"1.0","matchId":"race-001","playerId":"p1","type":"PLAYER_ACTION",
	"action":"LAP_COMPLETED","data":{"lap":1}}`

// newTestHandler crea un Handler que escribe los logs en una carpeta
// temporal. Devuelve la ruta del log de rechazados.
func newTestHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rejected.jsonl")
	l, err := rejected.New(path)
	if err != nil {
		t.Fatal(err)
	}
	unknown, err := rejected.New(filepath.Join(dir, "unknown_actions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	store := state.NewMatchStore()
	d := pipeline.New(store, time.Hour)
	// Alguien tiene que leer los lotes terminados; aquí solo se descartan.
	go func() {
		for range d.Out() {
		}
	}()
	t.Cleanup(func() {
		d.Close()
		l.Close()
		unknown.Close()
	})
	return &Handler{Rejected: l, Unknown: unknown, Store: store, Pipeline: d}, path
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
	want := rejection{EventID: "e1", Reason: `la acción "GOAL" no existe en el juego "racing" (es una acción de "football")`}
	if len(resp.Rejected) != 1 || resp.Rejected[0] != want {
		t.Fatalf("rejected = %+v, se esperaba %+v", resp.Rejected, want)
	}
}

// Solo los eventos aceptados deben llegar al store.
func TestMatchesAfterEvents(t *testing.T) {
	h, _ := newTestHandler(t)
	goalInRacing := strings.Replace(validEv, `"LAP_COMPLETED"`, `"GOAL"`, 1)
	postEvents(h, "["+validEv+","+goalInRacing+"]")

	req := httptest.NewRequest(http.MethodGet, "/matches", nil)
	rec := httptest.NewRecorder()
	h.Matches(rec, req)

	var got []state.MatchInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("respuesta no es JSON: %v", err)
	}
	if len(got) != 1 || got[0].MatchID != "race-001" || got[0].EventCount != 1 || got[0].Status != state.StatusInProgress {
		t.Fatalf("GET /matches = %+v", got)
	}
}

// Un evento que llega después de MATCH_FINISHED (aunque sea en la misma
// petición) se rechaza con "partida ya finalizada" y se anota en el log.
func TestLateEventRejected(t *testing.T) {
	h, logPath := newTestHandler(t)
	finished := `{"eventId":"f","timestamp":"2026-10-05T14:05:00Z","gameId":"racing",
		"gameVersion":"1.0","matchId":"race-001","type":"MATCH_FINISHED"}`
	rec := postEvents(h, "["+validEv+","+finished+","+validEv+"]")

	var resp eventsResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	want := rejection{EventID: "e1", Reason: "partida ya finalizada"}
	if resp.Accepted != 2 || len(resp.Rejected) != 1 || resp.Rejected[0] != want {
		t.Fatalf("respuesta = %+v", resp)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "partida ya finalizada") {
		t.Fatalf("el log no tiene el rechazo: %s", data)
	}
}

func TestEventsBodyTooLarge(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := postEvents(h, strings.Repeat(" ", maxBodyBytes+1))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

// Una acción que no existe en ningún juego se acepta, aparece en "warnings"
// y queda anotada en unknown_actions.jsonl. Una acción de otro juego se
// sigue rechazando.
func TestUnknownActionAcceptedWithWarning(t *testing.T) {
	h, path := newTestHandler(t)
	unknownEv := strings.Replace(validEv, `"LAP_COMPLETED"`, `"BOOST_JUMP"`, 1)
	otherGameEv := strings.Replace(strings.Replace(validEv, `"LAP_COMPLETED"`, `"TOP_OUT"`, 1), `"e1"`, `"e2"`, 1)

	rec := postEvents(h, "["+unknownEv+","+otherGameEv+"]")

	var resp eventsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Accepted != 1 || len(resp.Warnings) != 1 || resp.Warnings[0].EventID != "e1" {
		t.Fatalf("respuesta = %+v, se esperaba e1 aceptado con aviso", resp)
	}
	if len(resp.Rejected) != 1 || resp.Rejected[0].EventID != "e2" {
		t.Fatalf("rechazados = %+v, se esperaba e2 (acción de tetris)", resp.Rejected)
	}

	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), "unknown_actions.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(raw), "\n"); lines != 1 || !strings.Contains(string(raw), "BOOST_JUMP") {
		t.Fatalf("unknown_actions.jsonl = %q", raw)
	}
}
