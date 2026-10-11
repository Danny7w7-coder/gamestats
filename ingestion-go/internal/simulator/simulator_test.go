package simulator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"gamestats/ingestion/internal/model"
	"gamestats/ingestion/internal/validation"
)

func TestSameSeedSameMatches(t *testing.T) {
	n := Counts{Racing: 3, Football: 3, Fighting: 3}
	a := Generate(42, n)
	b := Generate(42, n)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("la misma semilla generó partidas distintas")
	}
	if reflect.DeepEqual(a, Generate(7, n)) {
		t.Fatal("semillas distintas generaron las mismas partidas")
	}
}

// Revisa muchas semillas para atrapar casos raros.
func TestGeneratedMatchesFollowTheRules(t *testing.T) {
	for seed := int64(1); seed <= 50; seed++ {
		for _, m := range Generate(seed, Counts{Racing: 2, Football: 2, Fighting: 2}) {
			checkCommon(t, m)
			switch m.GameID {
			case "racing":
				checkRacing(t, m)
			case "football":
				checkFootball(t, m)
			case "fighting":
				checkFighting(t, m)
			}
		}
	}
}

// refKeys son los campos de data que nombran a otro jugador de la partida.
var refKeys = []string{"targetId", "overtakenId", "otherId", "lappedId", "attackerId", "assistId"}

// checkCommon: todos válidos, ids únicos, timestamps en orden, empieza con
// MATCH_STARTED y termina con MATCH_FINISHED. Además, las reglas de contexto
// de todos los juegos: pausas bien formadas, ninguna acción durante una
// pausa, y toda referencia a otro jugador es a alguien de la partida
// distinto del que hace la acción.
func checkCommon(t *testing.T, m Match) {
	t.Helper()
	players := map[string]bool{}
	for _, e := range m.Events {
		if e.PlayerID != "" {
			players[e.PlayerID] = true
		}
	}

	seen := map[string]bool{}
	paused := false
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

		switch e.Type {
		case "MATCH_PAUSED":
			if paused {
				t.Fatalf("%s: %s pausa una partida ya pausada", m.ID, e.EventID)
			}
			paused = true
		case "MATCH_RESUMED":
			if !paused {
				t.Fatalf("%s: %s reanuda sin una pausa antes", m.ID, e.EventID)
			}
			paused = false
		case "PLAYER_ACTION":
			if paused {
				t.Fatalf("%s: %s es una acción durante la pausa", m.ID, e.EventID)
			}
			d := data(t, e)
			for _, k := range refKeys {
				if ref, ok := d[k]; ok && (ref == e.PlayerID || !players[ref.(string)]) {
					t.Fatalf("%s: %s tiene %s=%v inválido", m.ID, e.EventID, k, ref)
				}
			}
		}
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

// checkRacing revisa las reglas de contexto de racing (las mismas que
// aplica Scala): vueltas dentro del total, from ≠ to, un objeto a la vez,
// RESPAWN solo tras un choque o trompo propio en esa vuelta, nada después de
// un DNF y el que abandona no va en el ranking.
func checkRacing(t *testing.T, m Match) {
	t.Helper()
	start := data(t, m.Events[0])
	laps := int(start["laps"].(float64))
	if start["track"] == "" || laps < 3 || laps > 5 {
		t.Fatalf("%s: MATCH_STARTED con data %v", m.ID, start)
	}

	drivers := 0
	hand := map[string]string{}  // objeto que tiene cada piloto en la mano
	incident := map[string]int{} // vuelta del último choque o trompo sin RESPAWN
	out := map[string]bool{}     // pilotos que abandonaron
	lapsDone := map[string]int{} // LAP_COMPLETED de cada piloto

	for _, e := range m.Events {
		p := e.PlayerID
		if out[p] {
			t.Fatalf("%s: %s es de %s después de su DNF", m.ID, e.EventID, p)
		}
		if e.Type == "PLAYER_JOINED" {
			drivers++
		}
		if e.Type != "PLAYER_ACTION" {
			continue
		}
		d := data(t, e)
		lap := 0
		if l, ok := d["lap"]; ok {
			lap = int(l.(float64))
			if lap < 1 || lap > laps {
				t.Fatalf("%s: %s en la vuelta %d de %d", m.ID, e.EventID, lap, laps)
			}
		}

		switch e.Action {
		case "POSITION_CHANGED":
			if d["from"] == d["to"] {
				t.Fatalf("%s: %s con from = to", m.ID, e.EventID)
			}
		case "ITEM_PICKUP":
			if hand[p] != "" {
				t.Fatalf("%s: %s agarra un objeto con %s en la mano", m.ID, e.EventID, hand[p])
			}
			hand[p] = d["item"].(string)
		case "ITEM_USED":
			if hand[p] != d["item"] {
				t.Fatalf("%s: %s usa %v pero tiene %q", m.ID, e.EventID, d["item"], hand[p])
			}
			if _, ok := d["durationMs"]; d["item"] == "boost" && !ok {
				t.Fatalf("%s: %s es un nitro sin durationMs", m.ID, e.EventID)
			}
			delete(hand, p)
		case "COLLISION", "SPIN":
			incident[p] = lap
		case "RESPAWN":
			if l, ok := incident[p]; !ok || l != lap {
				t.Fatalf("%s: %s sin choque o trompo antes en esa vuelta", m.ID, e.EventID)
			}
			delete(incident, p)
		case "DNF":
			out[p] = true
		case "LAP_COMPLETED":
			lapsDone[p]++
		}
	}

	if drivers < 3 || drivers > 6 {
		t.Fatalf("%s: %d pilotos", m.ID, drivers)
	}
	ranking := data(t, m.Events[len(m.Events)-1])["ranking"].([]any)
	if len(ranking) != drivers-len(out) {
		t.Fatalf("%s: ranking %v con %d pilotos y %d abandonos", m.ID, ranking, drivers, len(out))
	}
	for _, r := range ranking {
		p := r.(string)
		if out[p] || lapsDone[p] != laps {
			t.Fatalf("%s: %s en el ranking con DNF=%v y %d vueltas", m.ID, p, out[p], lapsDone[p])
		}
	}
}

// Entre muchas carreras al azar aparecen todas las acciones de racing del
// catálogo (así el simulador no se olvida ninguna).
func TestRacingUsesAllActions(t *testing.T) {
	used := map[string]bool{}
	for seed := int64(1); seed <= 50; seed++ {
		for _, m := range Generate(seed, Counts{Racing: 3}) {
			if m.Script != "" {
				continue
			}
			for _, e := range m.Events {
				used[e.Action] = true
			}
		}
	}
	for action := range validation.GameActions["racing"] {
		if !used[action] {
			t.Errorf("ninguna carrera al azar usó %s", action)
		}
	}
}

// checkFootball revisa las reglas de contexto de football (las mismas que
// aplica Scala): 6 por equipo y todos entran antes de jugar, el teamId de
// cada acción es el de su equipo, todo GOAL va precedido de un tiro al arco
// del mismo jugador (≥1 s antes), pases y asistencias entre compañeros, y
// nada después de una roja.
func checkFootball(t *testing.T, m Match) {
	t.Helper()
	start := data(t, m.Events[0])
	if start["homeTeam"] != homeTeam || start["awayTeam"] != awayTeam {
		t.Fatalf("%s: MATCH_STARTED con data %v", m.ID, start)
	}

	teamOf := map[string]string{}
	teamSize := map[string]int{}
	sentOff := map[string]bool{}
	lastOnTargetShot := map[string]time.Time{} // jugador -> hora de su último tiro al arco

	for _, e := range m.Events {
		if sentOff[e.PlayerID] {
			t.Fatalf("%s: %s generó %s después de su roja", m.ID, e.PlayerID, e.EventID)
		}
		if e.Type == "PLAYER_JOINED" {
			team := data(t, e)["teamId"].(string)
			teamOf[e.PlayerID] = team
			teamSize[team]++
			continue
		}
		if e.Type != "PLAYER_ACTION" {
			continue
		}
		if teamOf[e.PlayerID] == "" {
			t.Fatalf("%s: %s juega sin haber entrado a un equipo", m.ID, e.PlayerID)
		}

		d := data(t, e)
		if d["teamId"] != teamOf[e.PlayerID] {
			t.Fatalf("%s: %s tiene teamId %v pero juega en %s", m.ID, e.EventID, d["teamId"], teamOf[e.PlayerID])
		}
		ts, _ := time.Parse(time.RFC3339, e.Timestamp)

		switch e.Action {
		case "SHOT", "POWER_SHOT":
			if d["onTarget"] == true {
				lastOnTargetShot[e.PlayerID] = ts
			}
		case "GOAL":
			shotAt, ok := lastOnTargetShot[e.PlayerID]
			if !ok || ts.Sub(shotAt) < time.Second {
				t.Fatalf("%s: GOAL %s sin tiro al arco previo (≥1 s) del mismo jugador", m.ID, e.EventID)
			}
			delete(lastOnTargetShot, e.PlayerID) // un tiro, un gol
			if a, ok := d["assistId"]; ok && teamOf[a.(string)] != teamOf[e.PlayerID] {
				t.Fatalf("%s: asistencia de un rival en %s", m.ID, e.EventID)
			}
		case "PASS":
			if teamOf[d["targetId"].(string)] != teamOf[e.PlayerID] {
				t.Fatalf("%s: pase a un rival en %s", m.ID, e.EventID)
			}
		case "TACKLE":
			if teamOf[d["targetId"].(string)] == teamOf[e.PlayerID] {
				t.Fatalf("%s: barrida a un compañero en %s", m.ID, e.EventID)
			}
		case "FOUL":
			if d["card"] == "red" {
				sentOff[e.PlayerID] = true
			}
		}
	}
	if teamSize[homeTeam] != 6 || teamSize[awayTeam] != 6 {
		t.Fatalf("%s: equipos de %d y %d jugadores, se esperaban 6 y 6", m.ID, teamSize[homeTeam], teamSize[awayTeam])
	}
}

// Entre muchos partidos al azar aparecen todas las acciones de football del
// catálogo.
func TestFootballUsesAllActions(t *testing.T) {
	used := map[string]bool{}
	for seed := int64(1); seed <= 50; seed++ {
		for _, m := range Generate(seed, Counts{Football: 3}) {
			if m.Script != "" {
				continue
			}
			for _, e := range m.Events {
				used[e.Action] = true
			}
		}
	}
	for action := range validation.GameActions["football"] {
		if !used[action] {
			t.Errorf("ningún partido al azar usó %s", action)
		}
	}
}

// checkFighting revisa las reglas de contexto de fighting (las mismas que
// aplica Scala): 2 luchadores con personaje, round ≤ rounds, golpe fallado
// con damage 0, COMBO_BREAKER solo tras un COMBO del rival en ese round,
// FATAL_BLOW una vez por luchador y con health ≤ 30, un ROUND_WON por round,
// y la FATALITY la hace el ganador y es la última acción.
func checkFighting(t *testing.T, m Match) {
	t.Helper()
	start := data(t, m.Events[0])
	rounds := int(start["rounds"].(float64))
	if start["stage"] == "" || (rounds != 1 && rounds != 3 && rounds != 5) {
		t.Fatalf("%s: MATCH_STARTED con data %v", m.ID, start)
	}

	joined := 0
	comboOn := map[string]string{} // víctima -> "atacante/round" de su último combo sin cortar
	fatalUsed := map[string]bool{}
	roundDone := map[int]bool{}
	wins := map[string]int{}
	finished := false

	for _, e := range m.Events {
		if e.Type == "PLAYER_JOINED" {
			joined++
			if data(t, e)["character"] == "" {
				t.Fatalf("%s: %s sin personaje", m.ID, e.EventID)
			}
		}
		if e.Type != "PLAYER_ACTION" {
			continue
		}
		if finished {
			t.Fatalf("%s: %s después de la FATALITY", m.ID, e.EventID)
		}
		d := data(t, e)
		round := 0
		if r, ok := d["round"]; ok {
			round = int(r.(float64))
			if round < 1 || round > rounds {
				t.Fatalf("%s: %s en el round %d de %d", m.ID, e.EventID, round, rounds)
			}
		}
		if d["hit"] == false && d["damage"] != 0.0 {
			t.Fatalf("%s: %s falló pero tiene damage %v", m.ID, e.EventID, d["damage"])
		}
		key := e.PlayerID + "/" + strconv.Itoa(round)

		switch e.Action {
		case "COMBO":
			comboOn[d["targetId"].(string)] = key
		case "COMBO_BREAKER":
			if comboOn[e.PlayerID] != d["attackerId"].(string)+"/"+strconv.Itoa(round) {
				t.Fatalf("%s: %s corta un combo que no existe", m.ID, e.EventID)
			}
			delete(comboOn, e.PlayerID)
		case "FATAL_BLOW":
			if fatalUsed[e.PlayerID] || d["health"].(float64) > 30 {
				t.Fatalf("%s: %s FATAL_BLOW repetido o con health %v", m.ID, e.EventID, d["health"])
			}
			fatalUsed[e.PlayerID] = true
		case "ROUND_WON":
			if roundDone[round] {
				t.Fatalf("%s: el round %d tiene dos ganadores", m.ID, round)
			}
			roundDone[round] = true
			wins[e.PlayerID]++
		case "FATALITY":
			if wins[e.PlayerID] < rounds/2+1 {
				t.Fatalf("%s: FATALITY de %s, que no ganó la pelea", m.ID, e.PlayerID)
			}
			finished = true
		}
	}
	if joined != 2 {
		t.Fatalf("%s: %d luchadores", m.ID, joined)
	}
	if wins["p1"] < rounds/2+1 && wins["p2"] < rounds/2+1 {
		t.Fatalf("%s: nadie ganó la pelea (rounds ganados %v de %d)", m.ID, wins, rounds)
	}
}

// Entre muchas peleas al azar aparecen todas las acciones de fighting del
// catálogo.
func TestFightingUsesAllActions(t *testing.T) {
	used := map[string]bool{}
	for seed := int64(1); seed <= 50; seed++ {
		for _, m := range Generate(seed, Counts{Fighting: 3}) {
			if m.Script != "" {
				continue
			}
			for _, e := range m.Events {
				used[e.Action] = true
			}
		}
	}
	for action := range validation.GameActions["fighting"] {
		if !used[action] {
			t.Errorf("ninguna pelea al azar usó %s", action)
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

	matches := Generate(42, Counts{Racing: 2, Football: 2, Fighting: 2})
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
