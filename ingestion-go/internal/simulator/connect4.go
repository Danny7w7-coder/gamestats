package simulator

import (
	"math/rand"
	"time"
)

// Tablero de connect4: 7 columnas por 6 filas. La fila 1 es la de abajo.
const (
	c4Cols = 7
	c4Rows = 6
)

// c4Cell es una casilla del tablero.
type c4Cell struct{ col, row int }

// centerOrder: columnas de la más central a la del borde. La IA prefiere el
// centro porque ahí caben más líneas de 4.
var centerOrder = []int{4, 3, 5, 2, 6, 1, 7}

// c4Snapshot es cómo estaba la partida antes de una ficha. Se guarda uno por
// ficha para poder deshacerla con UNDO.
type c4Snapshot struct {
	discs   map[c4Cell]string
	threats map[c4Cell]map[string]bool
	turn    int
	last    c4Cell
}

// c4Game escribe una partida de connect4 evento por evento sobre un tablero
// real. Después de cada ficha calcula solo las amenazas nuevas, los bloqueos
// y el 4 en línea, así THREAT_CREATED, THREAT_BLOCKED y CONNECT_FOUR siempre
// son ciertos. Lo usan el generador al azar y los escenarios guionizados.
type c4Game struct {
	b       *builder
	t       time.Time // reloj: hora del último evento
	players [2]string // en orden de entrada: players[0] empieza
	bots    map[string]bool
	discs   map[c4Cell]string          // casilla ocupada -> jugador
	threats map[c4Cell]map[string]bool // amenazas abiertas: casilla -> jugadores a los que les completaría 4
	turn    int                        // índice en players de quien tiene el turno
	last    c4Cell                     // última ficha del tablero
	history []c4Snapshot               // un snapshot por ficha, el último al final
	over    bool                       // hubo 4 en línea o el tablero se llenó
}

// newC4Game arranca una partida. difficulty dice qué jugadores son la IA y
// con qué dificultad (los que no están son humanos). El primero es rojo y
// el segundo amarillo.
func newC4Game(id string, start time.Time, players [2]string, difficulty map[string]string) *c4Game {
	g := &c4Game{
		b: newBuilder(id, "connect4"), players: players, bots: map[string]bool{},
		discs: map[c4Cell]string{}, threats: map[c4Cell]map[string]bool{},
	}
	g.b.add(start, "MATCH_STARTED", "", "", map[string]any{})
	for i, p := range players {
		data := map[string]any{"color": []string{"red", "yellow"}[i]}
		if d, ok := difficulty[p]; ok {
			data["bot"] = true
			data["difficulty"] = d
			g.bots[p] = true
		}
		g.b.add(start.Add(time.Duration(i+1)*time.Second), "PLAYER_JOINED", p, "", data)
	}
	g.t = start.Add(3 * time.Second)
	return g
}

func (g *c4Game) current() string { return g.players[g.turn] }

func (g *c4Game) rival(p string) string {
	if p == g.players[0] {
		return g.players[1]
	}
	return g.players[0]
}

// wait adelanta el reloj (lo que el jugador piensa su jugada).
func (g *c4Game) wait(d time.Duration) { g.t = g.t.Add(d) }

// act agrega una acción de p, un segundo después de la anterior.
func (g *c4Game) act(p, action string, data map[string]any) {
	g.t = g.t.Add(time.Second)
	g.b.add(g.t, "PLAYER_ACTION", p, action, data)
}

// nextRow es la primera fila libre de la columna (7 si está llena): ahí cae
// la ficha por la gravedad.
func (g *c4Game) nextRow(col int) int {
	row := 1
	for g.discs[c4Cell{col, row}] != "" {
		row++
	}
	return row
}

func (g *c4Game) playable(col int) bool { return g.nextRow(col) <= c4Rows }

// fourThrough dice si una ficha de p en cell queda en una línea de 4 o más,
// y en qué dirección ("" si no). Cuenta las fichas de p que hay a cada lado
// de cell, así sirve tanto para una ficha ya puesta como para una hipotética.
func (g *c4Game) fourThrough(cell c4Cell, p string) string {
	directions := []struct {
		dc, dr int
		name   string
	}{
		{1, 0, "horizontal"}, {0, 1, "vertical"}, {1, 1, "diagonal"}, {1, -1, "diagonal"},
	}
	for _, d := range directions {
		count := 1
		for _, sign := range []int{1, -1} {
			c := c4Cell{cell.col + sign*d.dc, cell.row + sign*d.dr}
			for g.discs[c] == p {
				count++
				c = c4Cell{c.col + sign*d.dc, c.row + sign*d.dr}
			}
		}
		if count >= 4 {
			return d.name
		}
	}
	return ""
}

// winningCells son las casillas vacías que le completarían 4 en línea a p
// (estén o no al alcance todavía), en orden de columna y fila.
func (g *c4Game) winningCells(p string) []c4Cell {
	var cells []c4Cell
	for col := 1; col <= c4Cols; col++ {
		for row := 1; row <= c4Rows; row++ {
			c := c4Cell{col, row}
			if g.discs[c] == "" && g.fourThrough(c, p) != "" {
				cells = append(cells, c)
			}
		}
	}
	return cells
}

// drop: el jugador que tiene el turno suelta una ficha en la columna col.
// La ficha cae en la primera fila libre. Después:
//   - si hizo 4 en línea, CONNECT_FOUR y se termina la partida;
//   - si cayó en una amenaza del rival, THREAT_BLOCKED (la amenaza se cierra);
//   - por cada casilla nueva que le completaría 4, THREAT_CREATED.
func (g *c4Game) drop(col int) {
	p := g.current()
	cell := c4Cell{col, g.nextRow(col)}
	g.history = append(g.history, g.snapshot())
	g.discs[cell] = p
	g.last = cell
	g.act(p, "DISC_DROPPED", map[string]any{"column": cell.col, "row": cell.row})

	if dir := g.fourThrough(cell, p); dir != "" {
		g.act(p, "CONNECT_FOUR", map[string]any{"direction": dir})
		g.over = true
		return
	}

	rival := g.rival(p)
	if g.threats[cell][rival] {
		delete(g.threats[cell], rival)
		g.act(p, "THREAT_BLOCKED", map[string]any{"column": cell.col, "row": cell.row, "attackerId": rival})
	}
	for _, c := range g.winningCells(p) {
		if !g.threats[c][p] {
			if g.threats[c] == nil {
				g.threats[c] = map[string]bool{}
			}
			g.threats[c][p] = true
			g.act(p, "THREAT_CREATED", map[string]any{"column": c.col, "row": c.row})
		}
	}

	g.turn = 1 - g.turn
	if len(g.discs) == c4Cols*c4Rows { // tablero lleno sin 4 en línea: empate
		g.over = true
	}
}

// timeout: al que tiene el turno se le acabó el tiempo y lo pierde.
func (g *c4Game) timeout() {
	g.act(g.current(), "TURN_TIMEOUT", map[string]any{"seconds": 30})
	g.turn = 1 - g.turn
}

// hint: el que tiene el turno pide una pista (la jugada que haría la IA).
func (g *c4Game) hint() {
	p := g.current()
	g.act(p, "HINT_USED", map[string]any{"column": g.bestColumn(p)})
}

// undo: el humano (en una partida contra la IA) deshace la última ficha del
// tablero. El tablero, las amenazas y el turno vuelven a como estaban.
func (g *c4Game) undo() {
	human := g.players[0]
	if g.bots[human] {
		human = g.players[1]
	}
	g.act(human, "UNDO", map[string]any{"column": g.last.col, "row": g.last.row})
	prev := g.history[len(g.history)-1]
	g.history = g.history[:len(g.history)-1]
	g.discs, g.threats, g.turn, g.last = prev.discs, prev.threats, prev.turn, prev.last
}

// snapshot copia el estado actual (los maps se copian para que la partida
// siga cambiando sin tocar el snapshot).
func (g *c4Game) snapshot() c4Snapshot {
	discs := map[c4Cell]string{}
	for c, p := range g.discs {
		discs[c] = p
	}
	threats := map[c4Cell]map[string]bool{}
	for c, owners := range g.threats {
		threats[c] = map[string]bool{}
		for p := range owners {
			threats[c][p] = true
		}
	}
	return c4Snapshot{discs: discs, threats: threats, turn: g.turn, last: g.last}
}

// bestColumn es la IA simple: si p puede ganar, gana; si no, tapa la jugada
// ganadora del rival; si no, juega lo más al centro posible.
func (g *c4Game) bestColumn(p string) int {
	for _, who := range []string{p, g.rival(p)} {
		for _, col := range centerOrder {
			if g.playable(col) && g.fourThrough(c4Cell{col, g.nextRow(col)}, who) != "" {
				return col
			}
		}
	}
	for _, col := range centerOrder {
		if g.playable(col) {
			return col
		}
	}
	return 0 // no pasa: si el tablero está lleno la partida ya terminó
}

// chooseColumn: con randomPct % de probabilidad juega una columna al azar
// (un error); si no, la jugada de la IA.
func (g *c4Game) chooseColumn(r *rand.Rand, p string, randomPct int) int {
	if r.Intn(100) < randomPct {
		var cols []int
		for col := 1; col <= c4Cols; col++ {
			if g.playable(col) {
				cols = append(cols, col)
			}
		}
		return cols[r.Intn(len(cols))]
	}
	return g.bestColumn(p)
}

func (g *c4Game) pause(reason string, d time.Duration) {
	g.b.add(g.t.Add(time.Second), "MATCH_PAUSED", "", "", map[string]any{"reason": reason})
	g.t = g.t.Add(time.Second + d)
	g.b.add(g.t, "MATCH_RESUMED", "", "", map[string]any{})
}

func (g *c4Game) finish() Match {
	g.b.add(g.t.Add(3*time.Second), "MATCH_FINISHED", "", "", map[string]any{})
	return g.b.build()
}

// botErrors: % de jugadas al azar según la dificultad de la IA. Un humano
// juega como la IA pero se equivoca un 25 % de las veces.
var botErrors = map[string]int{"easy": 40, "medium": 20, "hard": 5}

const humanErrors = 25

// generateConnect4 simula una partida al azar: la mitad son un humano (p1)
// contra la IA (cpu) y la otra mitad dos humanos (p1 y p2). Cualquiera puede
// empezar. Los dos eligen sus jugadas con la IA simple, con algunos errores.
// Los humanos a veces piden una pista o se quedan sin tiempo, y contra la
// IA a veces deshacen su última jugada (UNDO de la ficha de la IA y de la
// propia). Reglas que se cumplen por construcción:
//   - turnos, gravedad, amenazas, bloqueos y 4 en línea los calcula c4Game;
//   - UNDO solo lo hace el humano contra la IA, y nunca después del final.
func generateConnect4(r *rand.Rand, id string, start time.Time) Match {
	players := [2]string{"p1", "p2"}
	difficulty := map[string]string{}
	vsBot := r.Intn(2) == 0
	if vsBot {
		players[1] = "cpu"
		difficulty["cpu"] = pick(r, []string{"easy", "medium", "hard"})
	}
	if r.Intn(2) == 0 { // el que entra primero empieza
		players[0], players[1] = players[1], players[0]
	}
	g := newC4Game(id, start, players, difficulty)

	pauseAt := -1 // en el 10 % de las partidas hay una pausa
	if r.Intn(100) < 10 {
		pauseAt = 4 + r.Intn(15)
	}
	undos := 0

	for !g.over {
		p := g.current()
		g.wait(randSeconds(r, 2, 6))
		errors := humanErrors
		if g.bots[p] {
			errors = botErrors[difficulty[p]]
		} else {
			if r.Intn(100) < 3 {
				g.timeout()
				continue
			}
			if r.Intn(100) < 6 {
				g.hint()
			}
		}
		g.drop(g.chooseColumn(r, p, errors))

		// El humano se arrepiente: deshace la jugada de la IA y la suya.
		if vsBot && g.bots[p] && !g.over && len(g.history) >= 2 && undos < 2 && r.Intn(100) < 8 {
			g.undo()
			g.undo()
			undos++
		}
		if len(g.discs) == pauseAt && !g.over {
			g.pause("phone_call", randSeconds(r, 10, 40))
			pauseAt = -1
		}
	}
	return g.finish()
}
