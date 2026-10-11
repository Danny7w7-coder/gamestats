package simulator

import (
	"math/rand"
	"time"
)

var pieces = []string{"I", "O", "T", "S", "Z", "J", "L"}

var sides = []string{"left", "right"}

var rotations = []string{"cw", "ccw"}

// tetrisStep es el tiempo entre una acción y la siguiente dentro de una pieza.
const tetrisStep = 500 * time.Millisecond

// tetrisGame escribe una partida de tetris de UN jugador (p1) evento por
// evento. No simula el tablero celda por celda: lleva la altura de la pila,
// las líneas, el nivel, el combo y el back-to-back. Con eso salen solos los
// puntos, LEVEL_UP, ALL_CLEAR (la pila queda en 0) y TOP_OUT (la pila llega
// a 20). Lo usan el generador al azar y los escenarios guionizados.
type tetrisGame struct {
	b          *builder
	t          time.Time // reloj: hora del último evento
	startLevel int
	level      int
	lines      int    // líneas limpiadas en total
	stack      int    // altura de la pila (0 a 20)
	combo      int    // limpiezas seguidas menos 1 (-1 = la última pieza no limpió)
	difficult  bool   // la última limpieza fue un tetris o un T-spin (para back-to-back)
	current    string // pieza que está cayendo
	held       string // pieza guardada con HOLD ("" = ninguna)
	placed     int    // piezas colocadas
}

func newTetrisGame(id string, start time.Time, startLevel int) *tetrisGame {
	g := &tetrisGame{b: newBuilder(id, "tetris"), startLevel: startLevel, level: startLevel, combo: -1}
	g.b.add(start, "MATCH_STARTED", "", "", map[string]any{"startLevel": startLevel})
	g.b.add(start.Add(time.Second), "PLAYER_JOINED", "p1", "", map[string]any{})
	g.t = start.Add(3 * time.Second)
	return g
}

// wait adelanta el reloj (el tiempo entre una pieza y la siguiente).
func (g *tetrisGame) wait(d time.Duration) { g.t = g.t.Add(d) }

func (g *tetrisGame) act(action string, data map[string]any) {
	g.t = g.t.Add(tetrisStep)
	g.b.add(g.t, "PLAYER_ACTION", "p1", action, data)
}

// spawn: aparece una pieza nueva arriba del tablero.
func (g *tetrisGame) spawn(piece string) {
	g.current = piece
	g.act("PIECE_SPAWNED", map[string]any{"piece": piece})
}

// hold guarda la pieza actual. Si ya había una guardada, sale esa; si no,
// sale next. Devuelve true si usó next.
func (g *tetrisGame) hold(next string) bool {
	g.act("HOLD", map[string]any{"piece": g.current})
	out := g.held
	g.held = g.current
	if out == "" {
		g.spawn(next)
		return true
	}
	g.spawn(out)
	return false
}

func (g *tetrisGame) move(direction string) {
	g.act("PIECE_MOVED", map[string]any{"direction": direction})
}

func (g *tetrisGame) rotate(direction string) {
	g.act("PIECE_ROTATED", map[string]any{"direction": direction})
}

// place baja la pieza (de golpe con HARD_DROP o acelerada con SOFT_DROP) y
// la coloca; la pila sube "rows" filas. La distancia de caída depende de la
// altura de la pila: con la pila más alta, la pieza cae menos.
func (g *tetrisGame) place(hard bool, rows int) {
	cells := max(0, 18-g.stack)
	if hard {
		g.act("HARD_DROP", map[string]any{"cells": cells})
	} else {
		g.act("SOFT_DROP", map[string]any{"cells": max(1, cells/2)})
	}
	g.act("PIECE_PLACED", map[string]any{"piece": g.current, "hardDrop": hard})
	g.stack += rows
	g.placed++
}

// clear se llama después de colocar cada pieza: lines = líneas que limpió
// (0 = ninguna) y tspin = si fue un T-spin. Calcula los puntos:
//   - base: 100/300/500/800 por 1/2/3/4 líneas (T-spin: 800/1200/1600),
//     multiplicado por el nivel;
//   - back-to-back: un tetris o T-spin justo después de otro vale 1.5 veces.
func (g *tetrisGame) clear(lines int, tspin bool) {
	if tspin {
		g.act("T_SPIN", map[string]any{"lines": lines})
	}
	if lines == 0 {
		g.combo = -1 // se corta el combo, pero no el back-to-back
		return
	}

	g.combo++
	difficult := lines == 4 || tspin
	points := []int{0, 100, 300, 500, 800}[lines]
	if tspin {
		points = []int{0, 800, 1200, 1600}[lines]
	}
	points *= g.level
	if difficult && g.difficult {
		points = points * 3 / 2
	}
	g.difficult = difficult
	g.act("LINES_CLEARED", map[string]any{"lines": lines, "points": points, "combo": g.combo})

	g.stack -= lines
	if g.stack == 0 {
		g.act("ALL_CLEAR", map[string]any{"lines": lines})
	}

	// Cada 10 líneas se sube un nivel (máximo 30).
	g.lines += lines
	if level := min(30, g.startLevel+g.lines/10); level > g.level {
		g.level = level
		g.act("LEVEL_UP", map[string]any{"level": level})
	}
}

// reportStack informa la altura actual de la pila.
func (g *tetrisGame) reportStack() {
	g.act("STACK_HEIGHT", map[string]any{"rows": min(20, g.stack)})
}

// topOut: la pila llegó arriba y el jugador pierde. Es lo último que hace.
func (g *tetrisGame) topOut() {
	g.stack = 20
	g.reportStack()
	g.act("TOP_OUT", map[string]any{})
}

func (g *tetrisGame) pause(reason string, d time.Duration) {
	g.b.add(g.t.Add(time.Second), "MATCH_PAUSED", "", "", map[string]any{"reason": reason})
	g.t = g.t.Add(time.Second + d)
	g.b.add(g.t, "MATCH_RESUMED", "", "", map[string]any{})
}

func (g *tetrisGame) finish() Match {
	g.b.add(g.t.Add(2*time.Second), "MATCH_FINISHED", "", "", map[string]any{})
	return g.b.build()
}

// generateTetris simula una partida de un jugador desde un nivel inicial de
// 1 a 10 hasta que pierde (TOP_OUT). Cada pieza sube la pila 0 a 2 filas y
// el jugador limpia líneas según su habilidad, así la pila sube despacio
// hasta llegar a 20. Desde la pieza 100 la pila sube 2 filas más por pieza
// (el juego se vuelve muy rápido), así ninguna partida es eterna.
// Reglas que se cumplen por construcción:
//   - un solo jugador (p1);
//   - LEVEL_UP solo cuando el nivel sube (cada 10 líneas);
//   - TOP_OUT es lo último.
func generateTetris(r *rand.Rand, id string, start time.Time) Match {
	g := newTetrisGame(id, start, 1+r.Intn(10))
	skill := 20 + r.Intn(16) // % de piezas que limpian líneas
	pauseAt := -1            // en el 15 % de las partidas hay una pausa
	if r.Intn(100) < 15 {
		pauseAt = 5 + r.Intn(20)
	}

	next := pick(r, pieces)
	for {
		g.wait(randSeconds(r, 1, 3))
		g.spawn(next)
		next = pick(r, pieces)
		if r.Intn(100) < 10 && g.hold(next) {
			next = pick(r, pieces)
		}

		tspin := g.current == "T" && r.Intn(100) < 25
		for k := r.Intn(3); k > 0; k-- {
			g.move(pick(r, sides))
		}
		if tspin || r.Intn(100) < 40 {
			g.rotate(pick(r, rotations))
		}
		rows := r.Intn(3)
		if g.placed >= 100 {
			rows += 2
		}
		g.place(!tspin && r.Intn(100) < 60, rows)

		g.clear(randomLines(r, g, skill, tspin), tspin)

		if g.stack >= 20 {
			g.topOut()
			break
		}
		if g.placed%5 == 0 {
			g.reportStack()
		}
		if g.placed == pauseAt {
			g.pause("player_request", randSeconds(r, 10, 40))
		}
	}
	return g.finish()
}

// randomLines decide cuántas líneas limpia la pieza recién colocada. Nunca
// más de las que hay en la pila; dejar el tablero vacío (ALL_CLEAR) es raro.
func randomLines(r *rand.Rand, g *tetrisGame, skill int, tspin bool) int {
	lines := 0
	switch {
	case tspin:
		lines = r.Intn(3) // T-spin de 0, 1 o 2 líneas
	case r.Intn(100) < skill:
		lines = []int{1, 1, 1, 2, 2, 3, 4, 4}[r.Intn(8)]
	}
	if lines > g.stack || (lines == g.stack && r.Intn(100) >= 5) {
		return 0
	}
	return lines
}
