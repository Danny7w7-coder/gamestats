package simulator

import (
	"fmt"
	"time"
)

// Escenarios guionizados: partidas fijas que se generan SIEMPRE y no usan el
// azar. Sirven para que Danny pruebe sus reglas en Scala y para la demo.
// La semilla solo aparece en el matchId (para poder correr varias semillas
// contra el mismo servidor); el contenido es siempre el mismo.

// scriptBase es la hora de inicio de los escenarios guionizados.
var scriptBase = time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)

func scriptedMatches(seed int64) []Match {
	id := func(game, name string) string { return fmt.Sprintf("%s-s%d-%s", game, seed, name) }
	return []Match{
		raceComeback(id("race", "comeback"), scriptBase),
		racePenaltyChangesResult(id("race", "penalty"), scriptBase.Add(10*time.Minute)),
		footballComeback(id("foot", "comeback"), scriptBase.Add(1*time.Hour)),
		footballDraw(id("foot", "draw"), scriptBase.Add(3*time.Hour)),
		footballHatTrick(id("foot", "hattrick"), scriptBase.Add(5*time.Hour)),
	}
}

// ---------------------------------------------------------------------------
// Racing

// raceComeback: p4 sale último (4°), en la vuelta 2 sube a 2° y en la 3 gana.
// Ranking final: p4, p2, p1, p3.
func raceComeback(id string, start time.Time) Match {
	lapTimes := [][]int{
		//  p1     p2     p3     p4
		{81000, 81200, 81400, 81600}, // vuelta 1: p1, p2, p3, p4
		{81500, 81400, 81600, 80950}, // vuelta 2: p1, p4, p2, p3
		{82000, 81800, 81900, 80200}, // vuelta 3: p4, p2, p1, p3
	}
	m := buildRace(id, start, "interlagos", lapTimes, nil)
	m.Script = "remontada de posición: p4 sale último y gana"
	return m
}

// racePenaltyChangesResult: p1 es el más rápido en pista (243.0 s contra
// 246.0 s de p2), pero recibe 5 s de penalización en la vuelta 2 y termina
// segundo. Sin la penalización, ganaría p1. Ranking final: p2, p1, p3.
func racePenaltyChangesResult(id string, start time.Time) Match {
	lapTimes := [][]int{
		//  p1     p2     p3
		{81000, 82000, 83000},
		{81000, 82000, 83000},
		{81000, 82000, 83000},
	}
	penalties := []racePenalty{{driver: 0, lap: 2, seconds: 5, reason: "track_limits"}}
	m := buildRace(id, start, "monza", lapTimes, penalties)
	m.Script = "penalización que cambia el resultado: p1 llega primero pero pierde con +5 s"
	return m
}

// ---------------------------------------------------------------------------
// Football

// footballScript ayuda a escribir un partido evento por evento.
// Equipo A: p1, p2, p3. Equipo B: p4, p5, p6.
type footballScript struct {
	b       *builder
	kickoff time.Time
	teamOf  map[string]string
}

func newFootballScript(id string, start time.Time) *footballScript {
	s := &footballScript{b: newBuilder(id, "football"), kickoff: start.Add(time.Minute), teamOf: map[string]string{}}
	s.b.add(start, "MATCH_STARTED", "", "", map[string]any{"homeTeam": homeTeam, "awayTeam": awayTeam})
	for i, p := range playerIDs(6) {
		team := homeTeam
		if i >= 3 {
			team = awayTeam
		}
		s.teamOf[p] = team
		s.b.add(start.Add(time.Duration(i+1)*time.Second), "PLAYER_JOINED", p, "", map[string]any{"teamId": team})
	}
	return s
}

// at devuelve la hora del minuto y segundo de juego.
func (s *footballScript) at(minute, second int) time.Time {
	return s.kickoff.Add(time.Duration(minute)*time.Minute + time.Duration(second)*time.Second)
}

func (s *footballScript) pass(minute, second int, from, to string, success bool, distance float64) {
	s.b.add(s.at(minute, second), "PLAYER_ACTION", from, "PASS", map[string]any{
		"teamId": s.teamOf[from], "targetId": to, "success": success, "distance": distance, "minute": minute,
	})
}

func (s *footballScript) shot(minute, second int, player string, onTarget bool) {
	s.b.add(s.at(minute, second), "PLAYER_ACTION", player, "SHOT", map[string]any{
		"teamId": s.teamOf[player], "onTarget": onTarget, "minute": minute,
	})
}

// goal agrega el SHOT al arco y, 2 s después, el GOAL del mismo jugador
// (regla 5.3). assist vacío = gol sin asistencia.
func (s *footballScript) goal(minute, second int, player, assist string) {
	s.shot(minute, second, player, true)
	data := map[string]any{"teamId": s.teamOf[player], "minute": minute}
	if assist != "" {
		data["assistId"] = assist
	}
	s.b.add(s.at(minute, second+2), "PLAYER_ACTION", player, "GOAL", data)
}

func (s *footballScript) foul(minute, second int, player, target, card string) {
	s.b.add(s.at(minute, second), "PLAYER_ACTION", player, "FOUL", map[string]any{
		"teamId": s.teamOf[player], "targetId": target, "card": card, "minute": minute,
	})
}

func (s *footballScript) finish(script string) Match {
	s.b.add(s.at(92, 0), "MATCH_FINISHED", "", "", map[string]any{})
	m := s.b.build()
	m.Script = script
	return m
}

// footballComeback: B gana 1-0 y A remonta 2-1. Además:
//   - tarjeta roja a p6 (minuto 30); después no vuelve a aparecer;
//   - gol de p2 sin asistencia (minuto 55).
func footballComeback(id string, start time.Time) Match {
	s := newFootballScript(id, start)
	s.pass(10, 0, "p4", "p5", true, 18.0)
	s.goal(10, 15, "p5", "p4") // 0-1
	s.pass(22, 0, "p1", "p2", false, 25.5)
	s.foul(30, 0, "p6", "p2", "red") // p6 expulsado
	s.pass(41, 0, "p3", "p2", true, 12.0)
	s.shot(41, 20, "p2", false)
	s.goal(55, 0, "p2", "") // 1-1, sin asistencia
	s.pass(70, 0, "p3", "p1", true, 20.0)
	s.shot(70, 10, "p1", false)
	s.pass(78, 0, "p3", "p1", true, 16.5)
	s.goal(78, 10, "p1", "p3") // 2-1
	s.shot(85, 0, "p5", false)
	return s.finish("remontada: B gana 1-0 y A gana 2-1; roja a p6; gol de p2 sin asistencia")
}

// footballDraw: empate 1-1. p3 y p6 juegan (pases, faltas) pero no tiran.
func footballDraw(id string, start time.Time) Match {
	s := newFootballScript(id, start)
	s.pass(5, 0, "p3", "p2", true, 14.0)
	s.pass(12, 0, "p2", "p1", true, 22.0)
	s.goal(12, 10, "p1", "p2") // 1-0
	s.foul(20, 0, "p3", "p4", "yellow")
	s.pass(33, 0, "p6", "p4", true, 10.5)
	s.shot(33, 15, "p4", false)
	s.pass(50, 0, "p6", "p5", true, 19.0)
	s.goal(50, 10, "p5", "p6") // 1-1
	s.pass(66, 0, "p3", "p1", false, 30.0)
	s.shot(80, 0, "p2", false)
	s.foul(88, 0, "p6", "p1", "none")
	return s.finish("empate 1-1; p3 y p6 no tiran al arco")
}

// footballHatTrick: p1 mete 3 goles y A gana 3-1.
func footballHatTrick(id string, start time.Time) Match {
	s := newFootballScript(id, start)
	s.pass(15, 0, "p2", "p1", true, 17.0)
	s.goal(15, 10, "p1", "p2") // 1-0
	s.shot(30, 0, "p1", false)
	s.goal(44, 0, "p1", "") // 2-0
	s.pass(60, 0, "p5", "p4", true, 21.0)
	s.goal(60, 10, "p4", "p5") // 2-1
	s.pass(80, 0, "p3", "p1", true, 15.0)
	s.goal(80, 10, "p1", "p3") // 3-1, hat-trick
	s.foul(86, 0, "p5", "p1", "yellow")
	return s.finish("hat-trick de p1; A gana 3-1")
}
