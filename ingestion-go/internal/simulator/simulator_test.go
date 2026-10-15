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
	n := Counts{Racing: 3, Connect4: 3, Fighting: 3, Tetris: 3}
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
		for _, m := range Generate(seed, Counts{Racing: 2, Connect4: 2, Fighting: 2, Tetris: 2}) {
			checkCommon(t, m)
			switch m.GameID {
			case "racing":
				checkRacing(t, m)
			case "connect4":
				checkConnect4(t, m)
			case "fighting":
				checkFighting(t, m)
			case "tetris":
				checkTetris(t, m)
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
// aplica Scala): vueltas dentro del total, from ≠ to, RESPAWN solo tras un
// choque en esa vuelta en el que participó el piloto (uno por choque), nada
// después de un DNF y el que abandona no va en el ranking.
func checkRacing(t *testing.T, m Match) {
	t.Helper()
	start := data(t, m.Events[0])
	laps := int(start["laps"].(float64))
	if start["track"] == "" || laps < 3 || laps > 5 {
		t.Fatalf("%s: MATCH_STARTED con data %v", m.ID, start)
	}

	drivers := 0
	crashed := map[string]int{}  // vuelta del último choque de cada piloto sin RESPAWN
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
		case "COLLISION": // participan los dos: el que choca y el chocado
			crashed[p] = lap
			crashed[d["otherId"].(string)] = lap
		case "RESPAWN":
			if l, ok := crashed[p]; !ok || l != lap {
				t.Fatalf("%s: %s sin un choque suyo antes en esa vuelta", m.ID, e.EventID)
			}
			delete(crashed, p)
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

// c4Summary resume una partida de connect4 ya revisada por checkConnect4.
type c4Summary struct {
	winner   string         // "" si fue empate
	discs    map[string]int // fichas de cada jugador en el tablero final
	maxOpen  map[string]int // máximo de amenazas abiertas a la vez de cada jugador
	created  map[string]int // amenazas que armó cada jugador
	blocked  map[string]int // amenazas que tapó cada jugador
	undos    int
	timeouts int
	hints    int
}

// c4State es el tablero mientras checkConnect4 recorre los eventos.
type c4State struct {
	discs   map[c4Cell]string
	threats map[c4Cell]map[string]bool
	turn    string
	last    c4Cell
}

func (s c4State) clone() c4State {
	c := c4State{discs: map[c4Cell]string{}, threats: map[c4Cell]map[string]bool{}, turn: s.turn, last: s.last}
	for k, v := range s.discs {
		c.discs[k] = v
	}
	for k, owners := range s.threats {
		c.threats[k] = map[string]bool{}
		for p := range owners {
			c.threats[k][p] = true
		}
	}
	return c
}

// connects dice si una ficha de p en cell queda en una línea de 4 o más.
// Es independiente de c4Game, para que el test no dependa del código que
// revisa.
func connects(discs map[c4Cell]string, cell c4Cell, p string) bool {
	for _, d := range [][2]int{{1, 0}, {0, 1}, {1, 1}, {1, -1}} {
		count := 1
		for _, sign := range []int{1, -1} {
			for c := (c4Cell{cell.col + sign*d[0], cell.row + sign*d[1]}); discs[c] == p; c = (c4Cell{c.col + sign*d[0], c.row + sign*d[1]}) {
				count++
			}
		}
		if count >= 4 {
			return true
		}
	}
	return false
}

// checkConnect4 reconstruye el tablero evento por evento y revisa las reglas
// de contexto de connect4 (las mismas que aplica Scala): dos jugadores y
// turnos (empieza el primero que entró), gravedad, THREAT_CREATED en una
// casilla vacía que de verdad completaría 4 y sin repetir, THREAT_BLOCKED de
// una amenaza abierta con una ficha propia, UNDO solo del humano contra la
// IA y de la última ficha, y CONNECT_FOUR real, de quien soltó la última
// ficha y como última acción. Si nadie ganó, el tablero tiene que estar lleno.
func checkConnect4(t *testing.T, m Match) c4Summary {
	t.Helper()
	sum := c4Summary{discs: map[string]int{}, maxOpen: map[string]int{}, created: map[string]int{}, blocked: map[string]int{}}
	var players []string
	bots := map[string]bool{}
	st := c4State{discs: map[c4Cell]string{}, threats: map[c4Cell]map[string]bool{}}
	var history []c4State

	for _, e := range m.Events {
		p := e.PlayerID
		if e.Type == "PLAYER_JOINED" {
			players = append(players, p)
			if data(t, e)["bot"] == true {
				bots[p] = true
			}
			if st.turn == "" {
				st.turn = p
			}
			continue
		}
		if e.Type != "PLAYER_ACTION" {
			continue
		}
		if sum.winner != "" {
			t.Fatalf("%s: %s después del 4 en línea", m.ID, e.EventID)
		}
		rival := players[0]
		if p == players[0] {
			rival = players[1]
		}
		d := data(t, e)
		var cell c4Cell
		if c, ok := d["column"]; ok && d["row"] != nil {
			cell = c4Cell{int(c.(float64)), int(d["row"].(float64))}
		}
		myTurn := func() {
			if st.turn != p {
				t.Fatalf("%s: %s %s fuera de turno", m.ID, e.EventID, e.Action)
			}
		}

		switch e.Action {
		case "DISC_DROPPED":
			myTurn()
			free := 1
			for st.discs[c4Cell{cell.col, free}] != "" {
				free++
			}
			if cell.row != free || free > 6 {
				t.Fatalf("%s: %s cae en la fila %d, pero la libre es la %d", m.ID, e.EventID, cell.row, free)
			}
			history = append(history, st.clone())
			st.discs[cell] = p
			st.last = cell
			st.turn = rival
		case "TURN_TIMEOUT":
			myTurn()
			sum.timeouts++
			st.turn = rival
		case "HINT_USED":
			myTurn()
			sum.hints++
		case "THREAT_CREATED":
			if st.discs[cell] != "" || st.threats[cell][p] || !connects(st.discs, cell, p) {
				t.Fatalf("%s: %s no es una amenaza nueva de verdad", m.ID, e.EventID)
			}
			if st.threats[cell] == nil {
				st.threats[cell] = map[string]bool{}
			}
			st.threats[cell][p] = true
			sum.created[p]++
		case "THREAT_BLOCKED":
			attacker := d["attackerId"].(string)
			if !st.threats[cell][attacker] || st.discs[cell] != p {
				t.Fatalf("%s: %s tapa una amenaza que no existe", m.ID, e.EventID)
			}
			delete(st.threats[cell], attacker)
			sum.blocked[p]++
		case "UNDO":
			if bots[p] || !bots[rival] || st.last != cell || len(history) == 0 {
				t.Fatalf("%s: %s es un UNDO inválido", m.ID, e.EventID)
			}
			st = history[len(history)-1]
			history = history[:len(history)-1]
			sum.undos++
		case "CONNECT_FOUR":
			if st.discs[st.last] != p || !connects(st.discs, st.last, p) {
				t.Fatalf("%s: %s no es un 4 en línea de %s", m.ID, e.EventID, p)
			}
			sum.winner = p
		}

		for _, who := range players {
			open := 0
			for _, owners := range st.threats {
				if owners[who] {
					open++
				}
			}
			sum.maxOpen[who] = max(sum.maxOpen[who], open)
		}
	}

	if len(players) != 2 {
		t.Fatalf("%s: %d jugadores", m.ID, len(players))
	}
	if sum.winner == "" && len(st.discs) != c4Cols*c4Rows {
		t.Fatalf("%s: termina sin ganador con %d fichas", m.ID, len(st.discs))
	}
	for _, p := range st.discs {
		sum.discs[p]++
	}
	return sum
}

// Entre muchas partidas al azar aparecen todas las acciones de connect4 del
// catálogo.
func TestConnect4UsesAllActions(t *testing.T) {
	used := map[string]bool{}
	for seed := int64(1); seed <= 50; seed++ {
		for _, m := range Generate(seed, Counts{Connect4: 3}) {
			if m.Script != "" {
				continue
			}
			for _, e := range m.Events {
				used[e.Action] = true
			}
		}
	}
	for action := range validation.GameActions["connect4"] {
		if !used[action] {
			t.Errorf("ninguna partida al azar usó %s", action)
		}
	}
}

// checkFighting revisa las reglas de contexto de fighting (las mismas que
// aplica Scala): 2 luchadores con personaje, golpes solo punch o kick, golpe
// fallado con damage 0, round ≤ rounds, un solo resultado por round
// (ROUND_WON o ROUND_DRAW) y nada después del ROUND_WON que decide la pelea.
func checkFighting(t *testing.T, m Match) {
	t.Helper()
	start := data(t, m.Events[0])
	rounds := int(start["rounds"].(float64))
	if start["stage"] == "" || (rounds != 1 && rounds != 3 && rounds != 5) {
		t.Fatalf("%s: MATCH_STARTED con data %v", m.ID, start)
	}

	joined := 0
	roundDone := map[int]bool{}
	wins := map[string]int{}
	champion := ""

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
		if champion != "" {
			t.Fatalf("%s: %s después de que %s ganó la pelea", m.ID, e.EventID, champion)
		}
		d := data(t, e)
		round := int(d["round"].(float64))
		if round < 1 || round > rounds {
			t.Fatalf("%s: %s en el round %d de %d", m.ID, e.EventID, round, rounds)
		}
		if d["hit"] == false && d["damage"] != 0.0 {
			t.Fatalf("%s: %s falló pero tiene damage %v", m.ID, e.EventID, d["damage"])
		}

		switch e.Action {
		case "ATTACK":
			if d["move"] != "punch" && d["move"] != "kick" {
				t.Fatalf("%s: %s con move %v", m.ID, e.EventID, d["move"])
			}
		case "ROUND_WON", "ROUND_DRAW":
			if roundDone[round] {
				t.Fatalf("%s: el round %d tiene dos resultados", m.ID, round)
			}
			roundDone[round] = true
			if e.Action == "ROUND_WON" {
				wins[e.PlayerID]++
				if wins[e.PlayerID] >= rounds/2+1 {
					champion = e.PlayerID
				}
			}
		}
	}
	if joined != 2 {
		t.Fatalf("%s: %d luchadores", m.ID, joined)
	}
	// La pelea termina cuando alguien gana la mayoría o cuando se jugaron
	// todos los rounds (con empates puede no haber ganador).
	if champion == "" && len(roundDone) != rounds {
		t.Fatalf("%s: sin ganador y con %d de %d rounds jugados", m.ID, len(roundDone), rounds)
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

// checkTetris revisa las reglas de contexto de tetris (las mismas que aplica
// Scala): un solo jugador, LEVEL_UP siempre a un nivel mayor (arrancando en
// startLevel) y nada después de TOP_OUT, que tiene que estar.
func checkTetris(t *testing.T, m Match) {
	t.Helper()
	level := int(data(t, m.Events[0])["startLevel"].(float64))
	if level < 1 || level > 15 {
		t.Fatalf("%s: startLevel %d", m.ID, level)
	}

	player := ""
	toppedOut := false
	for _, e := range m.Events {
		if e.PlayerID == "" {
			continue
		}
		if player == "" {
			player = e.PlayerID
		}
		if e.PlayerID != player {
			t.Fatalf("%s: %s es de %s, pero la partida es de %s", m.ID, e.EventID, e.PlayerID, player)
		}
		if toppedOut {
			t.Fatalf("%s: %s después del TOP_OUT", m.ID, e.EventID)
		}
		switch e.Action {
		case "LEVEL_UP":
			next := int(data(t, e)["level"].(float64))
			if next <= level {
				t.Fatalf("%s: LEVEL_UP a %d desde el nivel %d", m.ID, next, level)
			}
			level = next
		case "TOP_OUT":
			toppedOut = true
		}
	}
	if !toppedOut {
		t.Fatalf("%s: la partida termina sin TOP_OUT", m.ID)
	}
}

// Entre muchas partidas al azar aparecen todas las acciones de tetris del
// catálogo.
func TestTetrisUsesAllActions(t *testing.T) {
	used := map[string]bool{}
	for seed := int64(1); seed <= 50; seed++ {
		for _, m := range Generate(seed, Counts{Tetris: 2}) {
			if m.Script != "" {
				continue
			}
			for _, e := range m.Events {
				used[e.Action] = true
			}
		}
	}
	for action := range validation.GameActions["tetris"] {
		if !used[action] {
			t.Errorf("ninguna partida al azar usó %s", action)
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

	matches := Generate(42, Counts{Racing: 2, Connect4: 2, Fighting: 2, Tetris: 2})
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
