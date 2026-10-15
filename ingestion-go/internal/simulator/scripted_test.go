package simulator

import (
	"reflect"
	"strings"
	"testing"

	"gamestats/ingestion/internal/validation"
)

// scriptedFor devuelve los escenarios de la semilla 42 de un juego, por
// nombre ("comeback", "penalty", ...).
func scriptedFor(t *testing.T, gameID string) map[string]Match {
	t.Helper()
	byName := map[string]Match{}
	for _, m := range Generate(42, Counts{}) {
		if m.GameID == gameID {
			byName[m.ID[strings.LastIndex(m.ID, "-")+1:]] = m
		}
	}
	return byName
}

func TestScriptedAlwaysGenerated(t *testing.T) {
	ms := Generate(42, Counts{})
	if len(ms) != 14 {
		t.Fatalf("escenarios = %d, se esperaban 14", len(ms))
	}
	for _, m := range ms {
		if m.Script == "" {
			t.Fatalf("%s no tiene descripción de guion", m.ID)
		}
	}
	// Con partidas aleatorias, los escenarios siguen estando.
	if got := len(Generate(42, Counts{Racing: 2, Connect4: 3, Fighting: 1, Tetris: 1})); got != 14+2+3+1+1 {
		t.Fatalf("partidas = %d, se esperaban 21", got)
	}
}

// El contenido del guion no depende de la semilla (solo cambian los ids).
func TestScriptedIndependentOfSeed(t *testing.T) {
	a, b := Generate(1, Counts{}), Generate(99, Counts{})
	for i := range a {
		if len(a[i].Events) != len(b[i].Events) {
			t.Fatalf("%s y %s tienen distinta cantidad de eventos", a[i].ID, b[i].ID)
		}
		for j := range a[i].Events {
			ea, eb := a[i].Events[j], b[i].Events[j]
			ea.EventID, ea.MatchID, eb.EventID, eb.MatchID = "", "", "", ""
			if !reflect.DeepEqual(ea, eb) {
				t.Fatalf("%s evento %d cambia con la semilla", a[i].ID, j)
			}
		}
	}
}

// lapPositions devuelve, para una vuelta, la posición de cada piloto.
func lapPositions(t *testing.T, m Match, lap int) map[string]int {
	t.Helper()
	pos := map[string]int{}
	for _, e := range m.Events {
		if e.Action == "LAP_COMPLETED" {
			d := data(t, e)
			if int(d["lap"].(float64)) == lap {
				pos[e.PlayerID] = int(d["position"].(float64))
			}
		}
	}
	return pos
}

func finalRanking(t *testing.T, m Match) []any {
	t.Helper()
	return data(t, m.Events[len(m.Events)-1])["ranking"].([]any)
}

func TestScriptedRacing(t *testing.T) {
	racing := scriptedFor(t, "racing")

	// Remontada: p4 último en la vuelta 1 y primero al final.
	cb := racing["comeback"]
	if pos := lapPositions(t, cb, 1); pos["p4"] != 4 {
		t.Fatalf("comeback: p4 en vuelta 1 = %d, se esperaba 4", pos["p4"])
	}
	if got := finalRanking(t, cb); !reflect.DeepEqual(got, []any{"p4", "p2", "p1", "p3"}) {
		t.Fatalf("comeback: ranking = %v", got)
	}

	// Penalización: p1 tiene el menor tiempo en pista pero termina segundo.
	pen := racing["penalty"]
	driven := map[string]float64{}
	penalties := 0
	for _, e := range pen.Events {
		switch e.Action {
		case "LAP_COMPLETED":
			driven[e.PlayerID] += data(t, e)["lapTimeMs"].(float64)
		case "PENALTY":
			penalties++
		}
	}
	if penalties != 1 || driven["p1"] >= driven["p2"] {
		t.Fatalf("penalty: penalizaciones=%d tiempos=%v", penalties, driven)
	}
	if got := finalRanking(t, pen); !reflect.DeepEqual(got, []any{"p2", "p1", "p3"}) {
		t.Fatalf("penalty: ranking = %v", got)
	}

	// Carrera limpia: el ganador no tiene choques (ni hechos ni recibidos) ni
	// penalizaciones, pero en la carrera sí hubo de ambos.
	clean := racing["clean"]
	winner := finalRanking(t, clean)[0]
	collisions, penalties := 0, 0
	for _, e := range clean.Events {
		switch e.Action {
		case "COLLISION":
			collisions++
			if e.PlayerID == winner || data(t, e)["otherId"] == winner {
				t.Fatalf("clean: el ganador %v estuvo en el choque %s", winner, e.EventID)
			}
		case "PENALTY":
			penalties++
			if e.PlayerID == winner {
				t.Fatalf("clean: el ganador %v tiene una penalización", winner)
			}
		}
	}
	if winner != "p1" || collisions == 0 || penalties == 0 {
		t.Fatalf("clean: ganador=%v choques=%d penalizaciones=%d", winner, collisions, penalties)
	}

	// De punta a punta: el ganador (p1) nunca cambia de posición y termina
	// cada vuelta primero.
	wire := racing["wire"]
	if got := finalRanking(t, wire); got[0] != "p1" {
		t.Fatalf("wire: ranking = %v", got)
	}
	for lap := 1; lap <= 3; lap++ {
		if pos := lapPositions(t, wire, lap); pos["p1"] != 1 {
			t.Fatalf("wire: p1 en la vuelta %d = %d", lap, pos["p1"])
		}
	}
	for _, e := range wire.Events {
		if e.Action == "POSITION_CHANGED" && e.PlayerID == "p1" {
			t.Fatalf("wire: p1 cambió de posición en %s", e.EventID)
		}
	}

	// Pausa y abandono: hay pausa, p3 abandona y no está en el ranking, y p1
	// (el chocado) vuelve a la pista.
	dnf := racing["dnf"]
	hasPause, dnfBy, respawnBy := false, "", ""
	for _, e := range dnf.Events {
		switch {
		case e.Type == "MATCH_PAUSED":
			hasPause = true
		case e.Action == "DNF":
			dnfBy = e.PlayerID
		case e.Action == "RESPAWN":
			respawnBy = e.PlayerID
		}
	}
	if got := finalRanking(t, dnf); !hasPause || dnfBy != "p3" || respawnBy != "p1" || !reflect.DeepEqual(got, []any{"p2", "p1", "p4"}) {
		t.Fatalf("dnf: pausa=%v DNF de %q RESPAWN de %q ranking=%v", hasPause, dnfBy, respawnBy, got)
	}
}

func TestScriptedConnect4(t *testing.T) {
	c4 := scriptedFor(t, "connect4")

	// Quick win: p1 gana con 4 fichas.
	if q := checkConnect4(t, c4["quick"]); q.winner != "p1" || q.discs["p1"] > 7 {
		t.Errorf("quick: ganador %q con %d fichas", q.winner, q.discs["p1"])
	}

	// Double threat: p1 tiene 2 amenazas abiertas a la vez y gana; antes
	// deshace dos fichas y pide una pista.
	if d := checkConnect4(t, c4["double"]); d.winner != "p1" || d.maxOpen["p1"] < 2 || d.undos != 2 || d.hints != 1 {
		t.Errorf("double: %+v", d)
	}

	// Perfect defense: p1 arma 2 o más amenazas y p2 las tapa todas.
	if d := checkConnect4(t, c4["defense"]); d.created["p1"] < 2 || d.blocked["p2"] < d.created["p1"] || d.timeouts != 1 {
		t.Errorf("defense: %+v", d)
	}

	// Empate: tablero lleno sin ganador.
	if d := checkConnect4(t, c4["draw"]); d.winner != "" || d.discs["p1"]+d.discs["p2"] != 42 {
		t.Errorf("draw: %+v", d)
	}

	// Entre los escenarios se usan las 7 acciones.
	used := map[string]bool{}
	for _, m := range c4 {
		for _, e := range m.Events {
			used[e.Action] = true
		}
	}
	for action := range validation.GameActions["connect4"] {
		if !used[action] {
			t.Errorf("ningún escenario de connect4 usa %s", action)
		}
	}
}

// roundResults devuelve, en orden, el resultado de cada round: el ganador
// (o "empate") y la vida que le quedó al ganador (0 en un empate).
func roundResults(t *testing.T, m Match) (winners []string, health []int) {
	t.Helper()
	for _, e := range m.Events {
		switch e.Action {
		case "ROUND_WON":
			winners = append(winners, e.PlayerID)
			health = append(health, int(data(t, e)["remainingHealth"].(float64)))
		case "ROUND_DRAW":
			winners = append(winners, "empate")
			health = append(health, 0)
		}
	}
	return winners, health
}

func TestScriptedFighting(t *testing.T) {
	fighting := scriptedFor(t, "fighting")

	// Flawless: p1 gana 2-0, el round 1 con vida 100 y el 2 con 35.
	winners, health := roundResults(t, fighting["flawless"])
	if !reflect.DeepEqual(winners, []string{"p1", "p1"}) || !reflect.DeepEqual(health, []int{100, 35}) {
		t.Errorf("flawless: rounds %v con vida %v", winners, health)
	}

	// Remontada: p1 gana el round 1 por tiempo y p2 los rounds 2 y 3; hay una
	// pausa.
	cb := fighting["comeback"]
	winners, health = roundResults(t, cb)
	if !reflect.DeepEqual(winners, []string{"p1", "p2", "p2"}) || !reflect.DeepEqual(health, []int{60, 90, 25}) {
		t.Errorf("comeback: rounds %v con vida %v", winners, health)
	}
	paused := false
	for _, e := range cb.Events {
		paused = paused || e.Type == "MATCH_PAUSED"
	}
	if !paused {
		t.Errorf("comeback: sin pausa")
	}

	// Combo master: round 1 empatado por doble KO, un combo de 6 golpes y p1
	// gana 2-0. Usa las 6 acciones de fighting.
	co := fighting["combo"]
	winners, _ = roundResults(t, co)
	if !reflect.DeepEqual(winners, []string{"empate", "p1", "p1"}) {
		t.Errorf("combo: rounds %v", winners)
	}
	maxHits := 0.0
	used := map[string]bool{}
	for _, e := range co.Events {
		used[e.Action] = true
		if e.Action == "COMBO" {
			maxHits = max(maxHits, data(t, e)["hits"].(float64))
		}
		if e.Action == "ROUND_DRAW" && data(t, e)["method"] != "double_ko" {
			t.Errorf("combo: empate por %v", data(t, e)["method"])
		}
	}
	if maxHits < 5 {
		t.Errorf("combo: el combo más largo tiene %v golpes", maxHits)
	}
	for action := range validation.GameActions["fighting"] {
		if !used[action] {
			t.Errorf("combo: no usa %s", action)
		}
	}
}

// tetrisSummary cuenta, en una partida de tetris, las líneas de cada
// limpieza (en orden), los T-spins, los ALL_CLEAR, los HOLD, las pausas y los
// niveles a los que subió.
type tetrisSummary struct {
	clears                   []int
	tSpins, allClears, holds int
	pauses                   int
	levels                   []int
}

func summarizeTetris(t *testing.T, m Match) tetrisSummary {
	t.Helper()
	var s tetrisSummary
	for _, e := range m.Events {
		switch {
		case e.Type == "MATCH_PAUSED":
			s.pauses++
		case e.Action == "LINES_CLEARED":
			s.clears = append(s.clears, int(data(t, e)["lines"].(float64)))
		case e.Action == "T_SPIN":
			s.tSpins++
		case e.Action == "ALL_CLEAR":
			s.allClears++
		case e.Action == "HOLD":
			s.holds++
		case e.Action == "LEVEL_UP":
			s.levels = append(s.levels, int(data(t, e)["level"].(float64)))
		}
	}
	return s
}

func TestScriptedTetris(t *testing.T) {
	tetris := scriptedFor(t, "tetris")

	// Back-to-back: dos tetris seguidos (con HOLD), un T-spin doble, sube al
	// nivel 2 y sin ALL_CLEAR.
	b2b := summarizeTetris(t, tetris["b2b"])
	if !reflect.DeepEqual(b2b.clears, []int{4, 4, 2}) || b2b.holds != 1 || b2b.allClears != 0 || !reflect.DeepEqual(b2b.levels, []int{2}) {
		t.Errorf("b2b: %+v", b2b)
	}

	// Perfect clear + T-spin master: 3 T-spins, un ALL_CLEAR y una pausa.
	pc := summarizeTetris(t, tetris["perfect"])
	if pc.tSpins != 3 || pc.allClears != 1 || pc.pauses != 1 {
		t.Errorf("perfect: %+v", pc)
	}

	// Entre los dos escenarios se usan las 13 acciones.
	used := map[string]bool{}
	for _, m := range tetris {
		for _, e := range m.Events {
			used[e.Action] = true
		}
	}
	for action := range validation.GameActions["tetris"] {
		if !used[action] {
			t.Errorf("ningún escenario de tetris usa %s", action)
		}
	}
}
