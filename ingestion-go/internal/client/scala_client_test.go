package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gamestats/ingestion/internal/model"
)

var testBatch = model.Batch{
	MatchID: "race-001", GameID: "racing", GameVersion: "1.0",
	StartedAt: "2026-10-05T14:00:00Z", FinishedAt: "2026-10-05T14:04:10Z",
	Players: []string{"p1"},
	Events:  []model.Event{{EventID: "e1", MatchID: "race-001", Type: "MATCH_STARTED"}},
}

// fakeScala simula Scala: responde con statuses[i] en el intento i (el
// último se repite) y cuenta los intentos.
func fakeScala(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var attempts atomic.Int32 // el handler corre en otra goroutine
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(attempts.Add(1)) - 1
		if r.Method != http.MethodPost || r.URL.Path != "/analyze" {
			t.Errorf("petición inesperada: %s %s", r.Method, r.URL.Path)
		}
		var got model.Batch
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got.MatchID != testBatch.MatchID {
			t.Errorf("cuerpo inesperado: %+v (%v)", got, err)
		}
		if n >= len(statuses) {
			n = len(statuses) - 1
		}
		w.WriteHeader(statuses[n])
	}))
	t.Cleanup(srv.Close)
	return srv, &attempts
}

// newTestClient usa esperas cortas para que los tests sean rápidos.
func newTestClient(url string) *ScalaClient {
	return New(url).WithBackoff(time.Millisecond, 2*time.Millisecond, 4*time.Millisecond)
}

func TestSend(t *testing.T) {
	tests := []struct {
		name         string
		statuses     []int
		wantErr      bool
		wantRejected bool // el error dice que reenviar no sirve (ErrRejected)
		wantAttempts int32
	}{
		{"éxito al primer intento", []int{200}, false, false, 1},
		{"fallo transitorio que se recupera", []int{503, 500, 200}, false, false, 3},
		{"fallo total: 1 intento + 3 reintentos", []int{503}, true, false, 4},
		{"4xx no se reintenta", []int{400}, true, true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, attempts := fakeScala(t, tt.statuses...)
			err := newTestClient(srv.URL).Send(testBatch)

			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, se esperaba error: %v", err, tt.wantErr)
			}
			if got := errors.Is(err, ErrRejected); got != tt.wantRejected {
				t.Fatalf("errors.Is(err, ErrRejected) = %v, se esperaba %v (err = %v)", got, tt.wantRejected, err)
			}
			if got := attempts.Load(); got != tt.wantAttempts {
				t.Fatalf("intentos = %d, se esperaban %d", got, tt.wantAttempts)
			}
		})
	}
}

// Scala tarda más que el timeout del cliente: cuenta como fallo y se reintenta.
func TestSendTimeout(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			time.Sleep(200 * time.Millisecond)
		}
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(srv.URL)
	c.httpClient.Timeout = 50 * time.Millisecond
	if err := c.Send(testBatch); err != nil {
		t.Fatalf("se esperaba éxito en el reintento, err = %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("intentos = %d, se esperaban 2", got)
	}
}

// Scala apagado: error de conexión en todos los intentos.
func TestSendScalaDown(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // ya no escucha nadie en esa dirección

	err := newTestClient(url).Send(testBatch)
	if err == nil {
		t.Fatal("se esperaba error con Scala apagado")
	}
	if errors.Is(err, ErrRejected) {
		t.Fatalf("Scala apagado no es un rechazo: el lote se debe poder reenviar (err = %v)", err)
	}
}
