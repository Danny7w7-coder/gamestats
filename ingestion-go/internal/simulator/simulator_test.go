package simulator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"gamestats/ingestion/internal/model"
	"gamestats/ingestion/internal/validation"
)

func TestSameSeedSameMatches(t *testing.T) {
	a := Generate(42, 3, 3)
	b := Generate(42, 3, 3)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("la misma semilla generó partidas distintas")
	}
	if reflect.DeepEqual(a, Generate(7, 3, 3)) {
		t.Fatal("semillas distintas generaron las mismas partidas")
	}
}

// Revisa muchas semillas para atrapar casos raros.
func TestGeneratedMatchesFollowTheRules(t *testing.T) {
	for seed := int64(1); seed <= 50; seed++ {
		for _, m := range Generate(seed, 2, 2) {
			checkCommon(t, m)
			switch m.GameID {
			case "racing":
				checkRacing(t, m)
			case "football":
				checkFootball(t, m)
			}
		}
	}
}

// checkCommon: todos válidos, ids únicos, timestamps en orden, empieza con
// MATCH_STARTED y termina con MATCH_FINISHED.
func checkCommon(t *testing.T, m Match) {
	t.Helper()
	seen := map[string]bool{}
	var prev time.Time
	for i, e := range m.Events {
		if err := validation.Validate(e); err != nil {
			t.Fatalf("%s: evento %s inválido: %v", m.ID, e.EventID, err)
		}
		if seen[e.EventID] {
			t.Fatalf("%s: eventId repetido %s", m.ID, e.EventID)
		}
		seen[e.EventID] = true
		ts, _ := time.Parse(time.RFC3339, e.Timestamp)
		if i > 0 && ts.Before(prev) {
			t.Fatalf("%s: %s está fuera de orden", m.ID, e.EventID)
		}
		prev = ts
	}
	if first, last := m.Events[0], m.Events[len(m.Events)-1]; first.Type != "MATCH_STARTED" || last.Type != "MATCH_FINISHED" {
		t.Fatalf("%s: empieza con %s y termina con %s", m.ID, first.Type, last.Type)
	}
}

func data(t *testing.T, e model.Event) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal(e.Data, &d); err != nil {
		t.Fatalf("%s: data inválido: %v", e.EventID, err)
	}
	return d
}

func checkRacing(t *testing.T, m Match) {
	t.Helper()
	start := data(t, m.Events[0])
	laps := int(start["laps"].(float64))
	if start["track"] == "" || laps < 3 || laps > 5 {
		t.Fatalf("%s: MATCH_STARTED con data %v", m.ID, start)
	}

	drivers, lapEvents := 0, 0
	for _, e := range m.Events {
		switch {
		case e.Type == "PLAYER_JOINED":
			drivers++
		case e.Action == "LAP_COMPLETED":
			lapEvents++
		}
	}
	if drivers < 3 || drivers > 6 {
		t.Fatalf("%s: %d pilotos", m.ID, drivers)
	}
	if lapEvents != drivers*laps {
		t.Fatalf("%s: %d LAP_COMPLETED, se esperaban %d", m.ID, lapEvents, drivers*laps)
	}
	ranking := data(t, m.Events[len(m.Events)-1])["ranking"].([]any)
	if len(ranking) != drivers {
		t.Fatalf("%s: ranking %v", m.ID, ranking)
	}
}

// checkFootball revisa las reglas de la sección 5.3.
func checkFootball(t *testing.T, m Match) {
	t.Helper()
	start := data(t, m.Events[0])
	if start["homeTeam"] != homeTeam || start["awayTeam"] != awayTeam {
		t.Fatalf("%s: MATCH_STARTED con data %v", m.ID, start)
	}

	teamOf := map[string]string{}
	sentOff := map[string]bool{}
	lastOnTargetShot := map[string]time.Time{} // jugador -> hora de su último tiro al arco

	for _, e := range m.Events {
		if sentOff[e.PlayerID] {
			t.Fatalf("%s: %s generó %s después de su roja", m.ID, e.PlayerID, e.EventID)
		}
		if e.Type == "PLAYER_JOINED" {
			teamOf[e.PlayerID] = data(t, e)["teamId"].(string)
			continue
		}
		if e.Type != "PLAYER_ACTION" {
			continue
		}

		d := data(t, e)
		if d["teamId"] != teamOf[e.PlayerID] {
			t.Fatalf("%s: %s tiene teamId %v pero juega en %s", m.ID, e.EventID, d["teamId"], teamOf[e.PlayerID])
		}
		ts, _ := time.Parse(time.RFC3339, e.Timestamp)

		switch e.Action {
		case "SHOT":
			if d["onTarget"] == true {
				lastOnTargetShot[e.PlayerID] = ts
			}
		case "GOAL":
			shotAt, ok := lastOnTargetShot[e.PlayerID]
			if !ok || ts.Sub(shotAt) < time.Second {
				t.Fatalf("%s: GOAL %s sin SHOT al arco previo (≥1 s) del mismo jugador", m.ID, e.EventID)
			}
			delete(lastOnTargetShot, e.PlayerID) // un tiro, un gol
			if a, ok := d["assistId"]; ok && (a == e.PlayerID || teamOf[a.(string)] != teamOf[e.PlayerID]) {
				t.Fatalf("%s: asistencia inválida en %s", m.ID, e.EventID)
			}
		case "PASS":
			if d["targetId"] == e.PlayerID || teamOf[d["targetId"].(string)] != teamOf[e.PlayerID] {
				t.Fatalf("%s: pase inválido en %s", m.ID, e.EventID)
			}
		case "FOUL":
			if d["card"] == "red" {
				sentOff[e.PlayerID] = true
			}
		}
	}
}

// Send manda cada partida completa, en varios POST, y suma las respuestas.
func TestSend(t *testing.T) {
	var mu sync.Mutex
	got := map[string]int{} // matchId -> eventos recibidos
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var events []model.Event
		json.NewDecoder(r.Body).Decode(&events)
		mu.Lock()
		for _, e := range events {
			got[e.MatchID]++
		}
		mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"received": len(events), "accepted": len(events), "rejected": []any{}})
	}))
	defer srv.Close()

	matches := Generate(42, 2, 2)
	results := Send(srv.URL, matches)

	for i, m := range matches {
		r := results[i]
		if r.Err != nil || r.Received != len(m.Events) || r.Accepted != len(m.Events) {
			t.Fatalf("%s: resultado %+v, se esperaban %d eventos", m.ID, r, len(m.Events))
		}
		if got[m.ID] != len(m.Events) {
			t.Fatalf("%s: el servidor recibió %d eventos, se esperaban %d", m.ID, got[m.ID], len(m.Events))
		}
	}
}
