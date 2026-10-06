package simulator

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// scriptedByName devuelve los escenarios de la semilla 42 por nombre
// ("comeback", "penalty", ...), separados por juego.
func scriptedByName(t *testing.T) (racing, football map[string]Match) {
	t.Helper()
	racing, football = map[string]Match{}, map[string]Match{}
	for _, m := range Generate(42, 0, 0) {
		name := m.ID[strings.LastIndex(m.ID, "-")+1:]
		if m.GameID == "racing" {
			racing[name] = m
		} else {
			football[name] = m
		}
	}
	return racing, football
}

func TestScriptedAlwaysGenerated(t *testing.T) {
	ms := Generate(42, 0, 0)
	if len(ms) != 5 {
		t.Fatalf("escenarios = %d, se esperaban 5", len(ms))
	}
	for _, m := range ms {
		if m.Script == "" {
			t.Fatalf("%s no tiene descripción de guion", m.ID)
		}
	}
	// Con partidas aleatorias, los escenarios siguen estando.
	if got := len(Generate(42, 2, 3)); got != 5+2+3 {
		t.Fatalf("partidas = %d, se esperaban 10", got)
	}
}

// El contenido del guion no depende de la semilla (solo cambian los ids).
func TestScriptedIndependentOfSeed(t *testing.T) {
	a, b := Generate(1, 0, 0), Generate(99, 0, 0)
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
	racing, _ := scriptedByName(t)

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
}

// footballSummary resume un partido: el marcador después de cada gol, goles
// por jugador, tiros por jugador, rojas y goles sin asistencia.
type footballSummary struct {
	scoreTimeline []string // ej. ["0-1", "1-1", "2-1"] (A-B)
	goals         map[string]int
	shots         map[string]int
	reds          []string
	unassisted    int
}

func summarize(t *testing.T, m Match) footballSummary {
	t.Helper()
	s := footballSummary{goals: map[string]int{}, shots: map[string]int{}}
	score := map[string]int{}
	for _, e := range m.Events {
		if e.Type != "PLAYER_ACTION" {
			continue
		}
		d := data(t, e)
		switch e.Action {
		case "SHOT":
			s.shots[e.PlayerID]++
		case "GOAL":
			score[d["teamId"].(string)]++
			s.goals[e.PlayerID]++
			s.scoreTimeline = append(s.scoreTimeline, strconv.Itoa(score["A"])+"-"+strconv.Itoa(score["B"]))
			if _, ok := d["assistId"]; !ok {
				s.unassisted++
			}
		case "FOUL":
			if d["card"] == "red" {
				s.reds = append(s.reds, e.PlayerID)
			}
		}
	}
	return s
}

func TestScriptedFootball(t *testing.T) {
	_, football := scriptedByName(t)

	cb := summarize(t, football["comeback"])
	if !reflect.DeepEqual(cb.scoreTimeline, []string{"0-1", "1-1", "2-1"}) {
		t.Errorf("comeback: marcador %v, se esperaba B adelante y A ganando 2-1", cb.scoreTimeline)
	}
	if !reflect.DeepEqual(cb.reds, []string{"p6"}) {
		t.Errorf("comeback: rojas %v, se esperaba [p6]", cb.reds)
	}
	if cb.unassisted != 1 {
		t.Errorf("comeback: goles sin asistencia = %d, se esperaba 1", cb.unassisted)
	}

	draw := summarize(t, football["draw"])
	if final := draw.scoreTimeline[len(draw.scoreTimeline)-1]; final != "1-1" {
		t.Errorf("draw: marcador final %s, se esperaba 1-1", final)
	}
	for _, p := range []string{"p3", "p6"} {
		if draw.shots[p] != 0 {
			t.Errorf("draw: %s tiene %d tiros, se esperaban 0", p, draw.shots[p])
		}
	}

	ht := summarize(t, football["hattrick"])
	if ht.goals["p1"] != 3 {
		t.Errorf("hattrick: p1 tiene %d goles, se esperaban 3", ht.goals["p1"])
	}
	if final := ht.scoreTimeline[len(ht.scoreTimeline)-1]; final != "3-1" {
		t.Errorf("hattrick: marcador final %s, se esperaba 3-1", final)
	}
}
