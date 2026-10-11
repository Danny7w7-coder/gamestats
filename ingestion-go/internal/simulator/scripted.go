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
		raceClean(id("race", "clean"), scriptBase.Add(20*time.Minute)),
		raceItemMaster(id("race", "items"), scriptBase.Add(30*time.Minute)),
		racePauseDNF(id("race", "dnf"), scriptBase.Add(40*time.Minute)),
		footballComeback(id("foot", "comeback"), scriptBase.Add(1*time.Hour)),
		footballDraw(id("foot", "draw"), scriptBase.Add(3*time.Hour)),
		footballHatTrick(id("foot", "hattrick"), scriptBase.Add(5*time.Hour)),
		footballCleanSheet(id("foot", "cleansheet"), scriptBase.Add(7*time.Hour)),
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
	m := buildRace(id, start, racePlan{track: "rainbow_road", lapTimes: lapTimes})
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
	m := buildRace(id, start, racePlan{track: "neon_city", lapTimes: lapTimes, penalties: penalties})
	m.Script = "penalización que cambia el resultado: p1 llega primero pero pierde con +5 s"
	return m
}

// raceClean: p1 gana de punta a punta sin choques ni penalizaciones
// (CLEAN_RACE). Los demás sí tienen problemas: p2 choca con p3 en la vuelta
// 2 y vuelve a la pista; p3 recibe 5 s en la vuelta 3. Ranking: p1, p3, p2, p4.
func raceClean(id string, start time.Time) Match {
	lapTimes := [][]int{
		//  p1     p2     p3     p4
		{80500, 81500, 82000, 83000},
		{80500, 87000, 82500, 83000}, // p2 pierde 5.5 s por el choque
		{80500, 81500, 82000, 83000},
	}
	actions := []raceAction{
		{driver: 0, lap: 1, at: 0.1, action: "DRIFT", data: map[string]any{"lap": 1, "corner": 3, "durationMs": 1800}},
		{driver: 0, lap: 2, at: 0.2, action: "ITEM_PICKUP", data: map[string]any{"lap": 2, "item": "boost"}},
		{driver: 0, lap: 2, at: 0.35, action: "ITEM_USED", data: map[string]any{"lap": 2, "item": "boost", "durationMs": 2500}},
		{driver: 1, lap: 2, at: 0.45, action: "COLLISION", data: map[string]any{"lap": 2, "otherId": "p3", "damage": 40}},
		{driver: 1, lap: 2, at: 0.47, action: "RESPAWN"},
		{driver: 3, lap: 3, at: 0.1, action: "DRIFT", data: map[string]any{"lap": 3, "corner": 6, "durationMs": 1200}},
	}
	penalties := []racePenalty{{driver: 2, lap: 3, seconds: 5, reason: "corner_cutting"}}
	m := buildRace(id, start, racePlan{track: "desert_dunes", lapTimes: lapTimes, actions: actions, penalties: penalties})
	m.Script = "carrera limpia: p1 gana sin choques ni penalizaciones"
	return m
}

// raceItemMaster: p2 usa los 3 objetos (nitro, escudo y aceite, uno por
// vuelta) y gana (ITEM_MASTER). p3 hace un trompo con el aceite y vuelve a la
// pista. p4 es muy lento: p1 y p2 le sacan una vuelta (LAPPED).
// Ranking: p2, p1, p3, p4.
func raceItemMaster(id string, start time.Time) Match {
	lapTimes := [][]int{
		//  p1     p2     p3      p4
		{81000, 80500, 82000, 125000},
		{81000, 80500, 82000, 125000},
		{81000, 80500, 87000, 125000}, // p3 pierde 5 s por el trompo
	}
	actions := []raceAction{
		{driver: 1, lap: 1, at: 0.2, action: "ITEM_PICKUP", data: map[string]any{"lap": 1, "item": "boost"}},
		{driver: 1, lap: 1, at: 0.35, action: "ITEM_USED", data: map[string]any{"lap": 1, "item": "boost", "durationMs": 3000}},
		{driver: 0, lap: 2, at: 0.1, action: "DRIFT", data: map[string]any{"lap": 2, "corner": 2, "durationMs": 2200}},
		{driver: 1, lap: 2, at: 0.2, action: "ITEM_PICKUP", data: map[string]any{"lap": 2, "item": "shield"}},
		{driver: 1, lap: 2, at: 0.35, action: "ITEM_USED", data: map[string]any{"lap": 2, "item": "shield"}},
		{driver: 1, lap: 3, at: 0.2, action: "ITEM_PICKUP", data: map[string]any{"lap": 3, "item": "oil"}},
		{driver: 1, lap: 3, at: 0.3, action: "ITEM_USED", data: map[string]any{"lap": 3, "item": "oil"}},
		{driver: 2, lap: 3, at: 0.45, action: "SPIN", data: map[string]any{"lap": 3, "corner": 5}},
		{driver: 2, lap: 3, at: 0.47, action: "RESPAWN"},
	}
	m := buildRace(id, start, racePlan{track: "jungle_ruins", lapTimes: lapTimes, actions: actions})
	m.Script = "item master: p2 usa nitro, escudo y aceite; p4 recibe una vuelta (LAPPED)"
	return m
}

// racePauseDNF: en la vuelta 2, p3 (que iba primero) choca con p1, hace un
// trompo y abandona (DNF). La carrera se pausa 30 s para limpiar la pista.
// p2 adelanta a p1 y gana. p3 no aparece en el ranking: p2, p1, p4.
func racePauseDNF(id string, start time.Time) Match {
	lapTimes := [][]int{
		//  p1     p2     p3     p4
		{81000, 81200, 80800, 82500}, // vuelta 1: p3, p1, p2, p4
		{84000, 81000, 82000, 82500}, // p1 pierde tiempo por el golpe
		{81000, 80800, 82000, 82500},
	}
	actions := []raceAction{
		{driver: 2, lap: 2, at: 0.4, action: "COLLISION", data: map[string]any{"lap": 2, "otherId": "p1", "damage": 70}},
		{driver: 2, lap: 2, at: 0.42, action: "SPIN", data: map[string]any{"lap": 2, "corner": 7}},
	}
	plan := racePlan{
		track: "frozen_peak", lapTimes: lapTimes, actions: actions,
		dnf:   &raceDNF{driver: 2, lap: 2, reason: "crash"}, // abandona a los 130.0 s
		pause: &racePause{atMs: 131000, seconds: 30, reason: "crash_cleanup"},
	}
	m := buildRace(id, start, plan)
	m.Script = "pausa y abandono: p3 choca, abandona (DNF) y se pausa la carrera para limpiar la pista"
	return m
}

// ---------------------------------------------------------------------------
// Football
//
// Todos los partidos son 6 vs 6 con entretiempo (newFootballGame).
// Equipo A: p1 (portero), p2 a p6. Equipo B: p7 (portero), p8 a p12.

// scripted le pone la descripción al partido terminado.
func scripted(m Match, script string) Match {
	m.Script = script
	return m
}

// footballComeback: B gana 1-0 y A remonta 2-1. Además:
//   - tarjeta roja a p12 (minuto 30); después no vuelve a aparecer;
//   - gol de p3 sin asistencia (minuto 55).
func footballComeback(id string, start time.Time) Match {
	g := newFootballGame(id, start)
	g.pass(10, 0, "p8", "p9", true, 18.0)
	g.goal(10, 15, "p9", "p8", false) // 0-1
	g.pass(22, 0, "p2", "p3", false, 25.5)
	g.interception(22, 3, "p10")
	g.foul(30, 0, "p12", "p3", "red") // p12 expulsado
	g.pass(41, 0, "p4", "p3", true, 12.0)
	g.save(41, 20, "p3", false)    // ataja p7
	g.goal(55, 0, "p3", "", false) // 1-1, sin asistencia
	g.pass(70, 0, "p4", "p2", true, 20.0)
	g.shot(70, 10, "p2", false, false)
	g.corner(77, 0, "p5")
	g.goal(78, 10, "p2", "p4", false) // 2-1
	g.shot(85, 0, "p9", false, false)
	return scripted(g.finish(92), "remontada: B gana 1-0 y A gana 2-1; roja a p12; gol de p3 sin asistencia")
}

// footballDraw: empate 1-1. p4 y p10 juegan (pases, faltas, barridas) pero
// no tiran al arco.
func footballDraw(id string, start time.Time) Match {
	g := newFootballGame(id, start)
	g.pass(5, 0, "p4", "p3", true, 14.0)
	g.pass(12, 0, "p3", "p2", true, 22.0)
	g.goal(12, 10, "p2", "p3", false) // 1-0
	g.foul(20, 0, "p4", "p8", "yellow")
	g.dribble(28, 0, "p11", true)
	g.pass(33, 0, "p10", "p8", true, 10.5)
	g.shot(33, 15, "p8", false, false)
	g.pass(50, 0, "p10", "p9", true, 19.0)
	g.goal(50, 10, "p9", "p10", false) // 1-1
	g.tackle(60, 0, "p4", "p9", true)
	g.pass(66, 0, "p4", "p2", false, 30.0)
	g.save(80, 0, "p2", false) // ataja p7
	g.foul(88, 0, "p10", "p2", "none")
	return scripted(g.finish(92), "empate 1-1; p4 y p10 no tiran al arco")
}

// footballHatTrick: p4 mete 3 goles (uno de tiro potente) y A gana 3-1.
func footballHatTrick(id string, start time.Time) Match {
	g := newFootballGame(id, start)
	g.pass(15, 0, "p2", "p4", true, 17.0)
	g.goal(15, 10, "p4", "p2", false) // 1-0
	g.shot(30, 0, "p4", false, false)
	g.goal(44, 0, "p4", "", true) // 2-0, tiro potente
	g.pass(60, 0, "p9", "p8", true, 21.0)
	g.goal(60, 10, "p8", "p9", false) // 2-1
	g.pass(80, 0, "p3", "p4", true, 15.0)
	g.goal(80, 10, "p4", "p3", false) // 3-1, hat-trick
	g.foul(86, 0, "p9", "p4", "yellow")
	return scripted(g.finish(92), "hat-trick de p4; A gana 3-1")
}

// footballCleanSheet: A gana 3-0 sin recibir goles (valla invicta). Los goles
// son un autogol de p10 (B), un tiro potente de p5 y un gol de p3 con
// asistencia de p2. p1 (portero de A) ataja 2 tiros y p12 (B) ve la roja.
// Usa las 11 acciones de football.
func footballCleanSheet(id string, start time.Time) Match {
	g := newFootballGame(id, start)
	g.pass(3, 0, "p2", "p3", true, 18.5)
	g.dribble(3, 20, "p3", true)
	g.save(4, 0, "p3", false) // ataja p7
	g.corner(6, 0, "p5")
	g.pass(8, 0, "p8", "p9", false, 25.5)
	g.interception(8, 5, "p6")
	g.save(10, 0, "p9", false) // ataja p1
	g.ownGoal(12, 0, "p10")    // 1-0 (autogol de B)
	g.foul(20, 0, "p11", "p2", "yellow")
	g.tackle(25, 0, "p12", "p3", true)
	g.goal(30, 0, "p5", "", true) // 2-0, tiro potente
	g.dribble(55, 0, "p8", false)
	g.tackle(55, 2, "p6", "p8", true)
	g.foul(62, 0, "p12", "p6", "red") // p12 expulsado
	g.shot(70, 0, "p9", true, false)
	g.corner(75, 0, "p8")
	g.save(75, 20, "p11", false) // ataja p1
	g.pass(80, 0, "p2", "p3", true, 20.5)
	g.goal(80, 15, "p3", "p2", false) // 3-0
	g.shot(88, 0, "p9", false, false)
	return scripted(g.finish(91), "valla invicta: A gana 3-0 con autogol de p10, tiro potente de p5 y 2 atajadas de p1")
}
