package pipeline

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

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
	d := New(store)
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
