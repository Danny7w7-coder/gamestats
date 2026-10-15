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
		raceWireToWire(id("race", "wire"), scriptBase.Add(30*time.Minute)),
		racePauseDNF(id("race", "dnf"), scriptBase.Add(40*time.Minute)),
		footballComeback(id("foot", "comeback"), scriptBase.Add(1*time.Hour)),
		footballDraw(id("foot", "draw"), scriptBase.Add(3*time.Hour)),
		footballHatTrick(id("foot", "hattrick"), scriptBase.Add(5*time.Hour)),
		footballCleanSheet(id("foot", "cleansheet"), scriptBase.Add(7*time.Hour)),
		fightFlawless(id("fight", "flawless"), scriptBase.Add(9*time.Hour)),
		fightComeback(id("fight", "comeback"), scriptBase.Add(9*time.Hour+10*time.Minute)),
		tetrisBackToBack(id("tetris", "b2b"), scriptBase.Add(10*time.Hour)),
		tetrisPerfectClear(id("tetris", "perfect"), scriptBase.Add(10*time.Hour+30*time.Minute)),
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
	crashes := []raceCrash{{driver: 1, other: 2, lap: 2, at: crashAt, damage: 40, respawnDriver: true}}
	penalties := []racePenalty{{driver: 2, lap: 3, seconds: 5, reason: "corner_cutting"}}
	m := buildRace(id, start, racePlan{track: "desert_dunes", lapTimes: lapTimes, crashes: crashes, penalties: penalties})
	m.Script = "carrera limpia: p1 gana sin choques ni penalizaciones"
	return m
}

// raceWireToWire: p1 sale primero y lidera todas las vueltas, sin perder
// nunca el primer lugar (WIRE_TO_WIRE). Detrás, p3 pasa a p2 en la vuelta 2,
// pero en la 3 choca contra p4 y p2 recupera el segundo lugar.
// Ranking: p1, p2, p3, p4.
func raceWireToWire(id string, start time.Time) Match {
	lapTimes := [][]int{
		//  p1     p2     p3     p4
		{80000, 81500, 81800, 83000}, // vuelta 1: p1, p2, p3, p4
		{80200, 82500, 81000, 83200}, // vuelta 2: p1, p3, p2, p4
		{80100, 81000, 85500, 82800}, // vuelta 3: p3 pierde tiempo por el choque
	}
	crashes := []raceCrash{{driver: 2, other: 3, lap: 3, at: crashAt, damage: 30, respawnDriver: true}}
	m := buildRace(id, start, racePlan{track: "jungle_ruins", lapTimes: lapTimes, crashes: crashes})
	m.Script = "de punta a punta: p1 lidera todas las vueltas sin perder el primer lugar"
	return m
}

// racePauseDNF: en la vuelta 2, p3 (que iba primero) choca con p1 y
// abandona (DNF); p1, el chocado, vuelve a la pista (RESPAWN). La carrera se
// pausa 30 s para limpiar la pista. p2 adelanta a p1 y gana. p3 no aparece
// en el ranking: p2, p1, p4.
func racePauseDNF(id string, start time.Time) Match {
	lapTimes := [][]int{
		//  p1     p2     p3     p4
		{81000, 81200, 80800, 82500}, // vuelta 1: p3, p1, p2, p4
		{84000, 81000, 82000, 82500}, // p1 pierde tiempo por el golpe
		{81000, 80800, 82000, 82500},
	}
	crashes := []raceCrash{{driver: 2, other: 0, lap: 2, at: 0.4, damage: 70, respawnOther: true}}
	plan := racePlan{
		track: "frozen_peak", lapTimes: lapTimes, crashes: crashes,
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

// ---------------------------------------------------------------------------
// Fighting
//
// Basados en los fixtures de Danny (fighting_match_ok y fighting_comeback).
// fightGame lleva la vida de cada luchador: remainingHealth y health salen
// solos del daño de cada golpe.

// fightFlawless: Scorpion (p1) contra Sub-Zero (p2), al mejor de 3. p1 gana el
// round 1 sin recibir daño (flawless victory: vida 100) y el round 2 por KO
// con vida 25, después de cortar un combo y usar su FATAL_BLOW. Termina con
// una FATALITY. Usa las 10 acciones.
func fightFlawless(id string, start time.Time) Match {
	g := newFightGame(id, start, "the_pit", 3, fighters[0], fighters[1])
	// Round 1: p2 no acierta ni un golpe.
	g.attack("p1", "punch", 10)
	g.attack("p2", "uppercut", 0)
	g.block("p1")
	g.special("p1", "spear", 15)
	g.combo("p1", 4, 30)
	g.throw("p1", 15)
	g.knockdown("p1")
	g.attack("p1", "uppercut", 30) // p2 queda en 0
	g.roundWon("p1", "ko")         // vida 100: flawless
	// Round 2: p2 casi lo da vuelta, pero p1 corta el combo y usa su golpe fatal.
	g.special("p2", "ice_ball", 10)
	g.combo("p2", 5, 35)
	g.comboBreaker("p1")
	g.attack("p2", "kick", 30) // p1 queda con 25
	g.fatalBlow("p1", 40)      // health 25
	g.combo("p1", 3, 25)
	g.attack("p2", "punch", 0)
	g.attack("p1", "uppercut", 35) // p2 queda en 0
	g.roundWon("p1", "ko")
	g.fatality("p1")
	return scripted(g.finish(), "flawless + fatality: p1 gana el round 1 sin recibir daño y remata con una fatality")
}

// fightComeback: Liu Kang (p1) contra Kitana (p2), al mejor de 3. p1 gana el
// round 1 por tiempo; p2 gana los rounds 2 y 3 (remontada). Entre el 2 y el 3
// hay una pausa. En el round 3 p2 queda con vida 25 y da vuelta la pelea con
// su FATAL_BLOW. Sin fatality.
func fightComeback(id string, start time.Time) Match {
	g := newFightGame(id, start, "living_forest", 3, fighters[2], fighters[3])
	// Round 1: se va a tiempo; p1 termina con 60 y p2 con 45.
	g.attack("p1", "punch", 15)
	g.attack("p2", "kick", 20)
	g.combo("p1", 3, 30)
	g.block("p2")
	g.throw("p1", 10)
	g.attack("p2", "punch", 20)
	g.wait(70 * time.Second)
	g.roundWon("p1", "timeout")
	// Round 2: p2 gana por KO con vida 90.
	g.combo("p2", 5, 45)
	g.attack("p1", "kick", 0)
	g.attack("p1", "punch", 10)
	g.special("p2", "fan_toss", 25)
	g.knockdown("p2")
	g.attack("p2", "uppercut", 30) // p1 queda en 0
	g.roundWon("p2", "ko")
	g.pause("player_pause", 20*time.Second)
	// Round 3: p1 deja a p2 con 25 y p2 lo da vuelta con su golpe fatal.
	g.combo("p1", 4, 40)
	g.attack("p1", "punch", 15)
	g.special("p1", "dragon_fire", 20) // p2 queda con 25
	g.fatalBlow("p2", 50)              // health 25
	g.combo("p2", 3, 30)
	g.attack("p2", "uppercut", 20) // p1 queda en 0
	g.roundWon("p2", "ko")
	return scripted(g.finish(), "remontada: p1 gana el round 1 y p2 gana los rounds 2 y 3")
}

// ---------------------------------------------------------------------------
// Tetris
//
// Basados en los fixtures de Danny (tetris_match_ok y tetris_perfect_clear).
// tetrisGame lleva la pila, las líneas y el nivel: los puntos, LEVEL_UP y
// ALL_CLEAR salen solos.

// tetrisFill coloca piezas que no limpian líneas, cada una sube la pila una fila.
func tetrisFill(g *tetrisGame, ps ...string) {
	for i, p := range ps {
		g.wait(2 * time.Second)
		g.spawn(p)
		g.move(sides[i%2])
		g.place(true, 1)
		g.clear(0, false)
	}
}

// tetrisBackToBack: p1 desde el nivel 1 arma una pila de 10 filas y hace dos
// tetris seguidos (back-to-back), usando HOLD para guardar una O y sacar la
// I. Con un T-spin doble llega a 10 líneas y sube al nivel 2. Después la
// pila crece hasta 20 y pierde (TOP_OUT). Sin ALL_CLEAR.
func tetrisBackToBack(id string, start time.Time) Match {
	g := newTetrisGame(id, start, 1)
	tetrisFill(g, "O", "S", "Z", "L", "J", "O", "L", "J", "S", "Z") // pila 10
	g.reportStack()
	// Primer tetris: pila 10 -> 6.
	g.wait(2 * time.Second)
	g.spawn("I")
	g.move("right")
	g.rotate("cw")
	g.place(true, 0)
	g.clear(4, false)
	// Sale una O, la guarda y sale la I: segundo tetris (back-to-back), 6 -> 2.
	g.wait(2 * time.Second)
	g.spawn("O")
	g.hold("I")
	g.place(true, 0)
	g.clear(4, false)
	// T-spin doble: la T sube la pila a 3 y limpia 2 -> 1. Son 10 líneas: nivel 2.
	g.wait(2 * time.Second)
	g.spawn("T")
	g.rotate("ccw")
	g.place(false, 1)
	g.clear(2, true)
	// La pila crece 3 filas por pieza hasta llegar arriba.
	for _, p := range []string{"Z", "S", "L", "J", "Z", "S", "O"} {
		g.wait(2 * time.Second)
		g.spawn(p)
		g.move("left")
		g.place(false, 3)
		g.clear(0, false)
		if g.stack >= 20 {
			break
		}
		g.reportStack()
	}
	g.topOut()
	return scripted(g.finish(), "back-to-back: dos tetris seguidos (con HOLD), sube al nivel 2 y pierde")
}

// tetrisPerfectClear: p1 desde el nivel 5 hace 3 T-spins (de 0, 1 y 2
// líneas: T-spin master) y un tetris que deja el tablero vacío (ALL_CLEAR,
// perfect clear). Pausa la partida en el medio y al final pierde.
func tetrisPerfectClear(id string, start time.Time) Match {
	g := newTetrisGame(id, start, 5)
	tetrisFill(g, "L", "J", "O") // pila 3
	tSpin := func(lines int) {
		g.wait(2 * time.Second)
		g.spawn("T")
		g.rotate("cw")
		g.place(false, 1)
		g.clear(lines, true)
	}
	tSpin(0) // pila 4, sin líneas
	tSpin(1) // pila 5 -> 4
	g.pause("player_request", 30*time.Second)
	// Tetris con la pila en 4: el tablero queda vacío.
	g.wait(2 * time.Second)
	g.spawn("I")
	g.move("left")
	g.place(true, 0)
	g.clear(4, false)
	tetrisFill(g, "S", "Z") // pila 2
	tSpin(2)                // pila 3 -> 1
	for _, p := range []string{"O", "L", "J", "S", "Z", "I", "O"} {
		g.wait(2 * time.Second)
		g.spawn(p)
		g.place(false, 3)
		g.clear(0, false)
		if g.stack >= 20 {
			break
		}
	}
	g.topOut()
	return scripted(g.finish(), "perfect clear + T-spin master: 3 T-spins y un tetris que deja el tablero vacío")
}
