package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEvents(t *testing.T) {
	const ev = `{"eventId":"e1","timestamp":"2026-10-05T14:01:22Z","gameId":"racing",
		"gameVersion":"1.0","matchId":"race-001","playerId":"p1","type":"PLAYER_ACTION",
		"action":"LAP_COMPLETED","data":{"lap":1}}`

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{"un objeto", ev, http.StatusOK, `{"received":1}`},
		{"una lista", "[" + ev + "," + ev + "]", http.StatusOK, `{"received":2}`},
		{"lista vacía", "[]", http.StatusOK, `{"received":0}`},
		{"JSON inválido", `{"eventId":`, http.StatusBadRequest, ""},
		{"cuerpo vacío", "", http.StatusBadRequest, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			Events(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, se esperaba %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && strings.TrimSpace(rec.Body.String()) != tt.wantBody {
				t.Errorf("body = %s, se esperaba %s", rec.Body.String(), tt.wantBody)
			}
		})
	}
}

func TestEventsBodyTooLarge(t *testing.T) {
	big := strings.Repeat(" ", maxBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(big))
	rec := httptest.NewRecorder()

	Events(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, se esperaba %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}
