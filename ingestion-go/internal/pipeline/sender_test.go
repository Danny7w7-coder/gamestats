package pipeline

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"gamestats/ingestion/internal/client"
	"gamestats/ingestion/internal/model"
	"gamestats/ingestion/internal/state"
)

// Flujo completo: Submit -> router -> partida -> out -> senders -> Scala.
// La partida "ok" queda enviada; la "bad" recibe un 400 y queda error_envio
// (los 4xx no se reintentan, así el test no espera).
func TestSendersUpdateStatus(t *testing.T) {
	var mu sync.Mutex
	received := map[string]int{} // matchId -> eventos recibidos por "Scala"
	scala := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b model.Batch
		json.NewDecoder(r.Body).Decode(&b)
		if b.MatchID == "bad" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		received[b.MatchID] = len(b.Events)
		mu.Unlock()
	}))
	defer scala.Close()

	store := state.NewMatchStore()
	d := New(store, time.Hour)
	done := make(chan struct{})
	go func() {
		RunSenders(2, d.Out(), client.New(scala.URL), store)
		close(done)
	}()

	for _, id := range []string{"ok", "bad"} {
		d.Submit(ev(id+"1", id, "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
		d.Submit(ev(id+"2", id, "2026-10-05T14:00:01Z", "PLAYER_JOINED", "p1"))
		d.Submit(ev(id+"3", id, "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))
	}
	d.Close()
	<-done // los senders terminan cuando out se cierra

	statuses := map[string]string{}
	for _, m := range store.List() {
		statuses[m.MatchID] = m.Status
	}
	if statuses["ok"] != state.StatusSent || statuses["bad"] != state.StatusSendError {
		t.Fatalf("estados = %v", statuses)
	}
	if received["ok"] != 3 {
		t.Fatalf("Scala recibió %d eventos de ok, se esperaban 3", received["ok"])
	}
}

// Apagado ordenado: al cerrar el dispatcher, los lotes que ya estaban
// terminados se envían igual, aunque Scala sea lento, y recién entonces
// RunSenders termina. Es lo que hace main al recibir Ctrl+C.
func TestCloseSendsPendingBatches(t *testing.T) {
	var mu sync.Mutex
	received := map[string]bool{}
	scala := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Scala lento
		var b model.Batch
		json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		received[b.MatchID] = true
		mu.Unlock()
	}))
	defer scala.Close()

	store := state.NewMatchStore()
	d := New(store, time.Hour)
	done := make(chan struct{})
	go func() {
		RunSenders(1, d.Out(), client.New(scala.URL), store) // un solo sender: van en fila
		close(done)
	}()

	ids := []string{"m1", "m2", "m3"}
	for _, id := range ids {
		d.Submit(ev(id+"-s", id, "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
		d.Submit(ev(id+"-f", id, "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))
	}
	d.Close() // apagado inmediato, con los 3 lotes todavía sin enviar

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunSenders no terminó después de Close")
	}

	for _, id := range ids {
		if !received[id] {
			t.Errorf("Scala no recibió %s", id)
		}
		if status, _ := store.Status(id); status != state.StatusSent {
			t.Errorf("estado de %s = %q, se esperaba enviada", id, status)
		}
	}
}
