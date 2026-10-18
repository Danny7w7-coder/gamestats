package simulator

import (
	"math/rand"
	"time"
)

var characters = []string{"scorpion", "sub-zero", "liu_kang", "kitana", "raiden", "sonya"}

var stages = []string{"the_pit", "living_forest", "dead_pool", "shaolin_temple", "goros_lair"}

var moves = []string{"punch", "kick"}

// fightStep es el tiempo entre una acción y la siguiente.
const fightStep = 2 * time.Second

// fightGame escribe una pelea 1 vs 1 (p1 contra p2) evento por evento. Lleva
// la vida de cada luchador en el round actual, así remainingHealth de
// ROUND_WON siempre es coherente con el daño recibido. Lo usan el generador
// al azar y los escenarios guionizados.
type fightGame struct {
	b      *builder
	t      time.Time // reloj: hora del último evento
	rounds int       // la pelea es al mejor de "rounds"
	round  int       // round actual
	health map[string]int
	wins   map[string]int
}

func newFightGame(id string, start time.Time, stage string, rounds int, c1, c2 string) *fightGame {
	g := &fightGame{
		b: newBuilder(id, "fighting"), t: start, rounds: rounds, round: 1,
		health: map[string]int{"p1": 100, "p2": 100},
		wins:   map[string]int{},
	}
	g.b.add(start, "MATCH_STARTED", "", "", map[string]any{"stage": stage, "rounds": rounds})
	g.b.add(start.Add(1*time.Second), "PLAYER_JOINED", "p1", "", map[string]any{"character": c1})
	g.b.add(start.Add(2*time.Second), "PLAYER_JOINED", "p2", "", map[string]any{"character": c2})
	g.t = start.Add(8 * time.Second)
	return g
}

func rivalOf(p string) string {
	if p == "p1" {
		return "p2"
	}
	return "p1"
}

// wait adelanta el reloj (por ejemplo, un round que se va a tiempo).
func (g *fightGame) wait(d time.Duration) { g.t = g.t.Add(d) }

// act agrega una acción de p, fightStep después de la anterior.
func (g *fightGame) act(p, action string, data map[string]any) {
	g.t = g.t.Add(fightStep)
	g.b.add(g.t, "PLAYER_ACTION", p, action, data)
}

// attack: golpe simple (punch o kick) de p al rival. damage 0 = falló
// (hit: false con damage 0, como pide el contrato).
func (g *fightGame) attack(p, move string, damage int) {
	g.health[rivalOf(p)] -= damage
	g.act(p, "ATTACK", map[string]any{
		"targetId": rivalOf(p), "move": move, "hit": damage > 0, "damage": damage, "round": g.round,
	})
}

// combo: varios golpes seguidos (hits ≥ 2) que suman damage.
func (g *fightGame) combo(p string, hits, damage int) {
	g.health[rivalOf(p)] -= damage
	g.act(p, "COMBO", map[string]any{"targetId": rivalOf(p), "hits": hits, "damage": damage, "round": g.round})
}

// block: p bloquea un golpe del rival.
func (g *fightGame) block(p string) {
	g.act(p, "BLOCK", map[string]any{"attackerId": rivalOf(p), "round": g.round})
}

// knockdown: p derriba al rival.
func (g *fightGame) knockdown(p string) {
	g.act(p, "KNOCKDOWN", map[string]any{"targetId": rivalOf(p), "round": g.round})
}

// roundWon cierra el round: gana p con la vida que le queda, por KO o por
// tiempo.
func (g *fightGame) roundWon(p, method string) {
	g.act(p, "ROUND_WON", map[string]any{"round": g.round, "remainingHealth": g.health[p], "method": method})
	g.wins[p]++
	g.nextRound()
}

// roundDraw cierra el round empatado (doble KO o tiempo con la misma vida).
// Lo manda p (cualquiera de los dos) y no suma para nadie.
func (g *fightGame) roundDraw(p, method string) {
	g.act(p, "ROUND_DRAW", map[string]any{"round": g.round, "method": method, "otherId": rivalOf(p)})
	g.nextRound()
}

// nextRound pasa al round siguiente con las vidas en 100.
func (g *fightGame) nextRound() {
	g.round++
	g.health = map[string]int{"p1": 100, "p2": 100}
	g.wait(10 * time.Second) // presentación del round siguiente
}

// winner devuelve quién ya ganó la mayoría de los rounds ("" si nadie todavía).
func (g *fightGame) winner() string {
	for _, p := range []string{"p1", "p2"} {
		if g.wins[p] >= g.rounds/2+1 {
			return p
		}
	}
	return ""
}

// over dice si la pelea terminó: alguien ganó la mayoría o ya no quedan
// rounds (con empates, una pelea puede terminar sin ganador).
func (g *fightGame) over() bool {
	return g.winner() != "" || g.round > g.rounds
}

// pause pausa la pelea entre rounds.
func (g *fightGame) pause(reason string, d time.Duration) {
	g.b.add(g.t.Add(time.Second), "MATCH_PAUSED", "", "", map[string]any{"reason": reason})
	g.t = g.t.Add(time.Second + d)
	g.b.add(g.t, "MATCH_RESUMED", "", "", map[string]any{})
}

func (g *fightGame) finish() Match {
	g.b.add(g.t.Add(time.Second), "MATCH_FINISHED", "", "", map[string]any{})
	return g.b.build()
}

// generateFighting simula una pelea al azar, al mejor de 1, 3 o 5 rounds,
// entre dos personajes distintos. Reglas que se cumplen por construcción:
//   - los rounds se juegan en orden y cada uno termina con un solo resultado
//     (ROUND_WON o ROUND_DRAW), así que round ≤ rounds;
//   - la pelea termina en cuanto alguien gana la mayoría: después del round
//     decisivo no hay más acciones.
func generateFighting(r *rand.Rand, id string, start time.Time) Match {
	rounds := []int{1, 3, 3, 3, 5}[r.Intn(5)]
	i := r.Intn(len(characters))
	j := (i + 1 + r.Intn(len(characters)-1)) % len(characters) // otro personaje
	g := newFightGame(id, start, pick(r, stages), rounds, characters[i], characters[j])

	for !g.over() {
		playRound(r, g)
		if !g.over() && r.Intn(100) < 10 {
			g.pause("player_pause", randSeconds(r, 10, 40))
		}
	}
	return g.finish()
}

// playRound juega un round: los luchadores se turnan al azar hasta que uno
// queda sin vida (KO) o se acaba el tiempo (entre 10 y 24 intercambios).
func playRound(r *rand.Rand, g *fightGame) {
	limit := 10 + r.Intn(15)
	for exchange := 0; exchange < limit; exchange++ {
		p := []string{"p1", "p2"}[r.Intn(2)]
		rival := rivalOf(p)
		g.wait(randSeconds(r, 0, 3))

		switch roll := r.Intn(100); {
		case roll < 70:
			damage := hitOrMiss(r, 75, 5, 20)
			g.attack(p, pick(r, moves), damage)
			switch {
			case damage == 0 && r.Intn(100) < 60:
				g.block(rival)
			case damage >= 15 && g.health[rival] > 0 && r.Intn(100) < 40:
				g.knockdown(p)
			}
		default:
			hits := 2 + r.Intn(3) // 2..4
			// Un combo largo (5 o 6 golpes) es raro: 4 % de los combos.
			if r.Intn(100) < 4 {
				hits = 5 + r.Intn(2)
			}
			g.combo(p, hits, 6*hits+r.Intn(6))
		}

		if g.health[rival] <= 0 {
			// Doble KO: a veces (10 %) el rival contesta a la vez y deja en 0 al
			// que tenía poca vida. El round queda empatado. Es raro a propósito:
			// con más empates, muchas peleas terminan sin ganador.
			if g.health[p] <= 15 && r.Intn(100) < 10 {
				g.attack(rival, pick(r, moves), g.health[p])
				g.roundDraw(pick(r, []string{"p1", "p2"}), "double_ko")
				return
			}
			g.roundWon(p, "ko")
			return
		}
	}
	// Se acabó el tiempo: gana el que tiene más vida; con la misma vida, empate.
	g.wait(20 * time.Second)
	switch {
	case g.health["p1"] > g.health["p2"]:
		g.roundWon("p1", "timeout")
	case g.health["p2"] > g.health["p1"]:
		g.roundWon("p2", "timeout")
	default:
		g.roundDraw(pick(r, []string{"p1", "p2"}), "timeout")
	}
}

// hitOrMiss devuelve 0 (falló) o un daño entre lo y hi, con hitPct % de acierto.
func hitOrMiss(r *rand.Rand, hitPct, lo, hi int) int {
	if r.Intn(100) >= hitPct {
		return 0
	}
	return lo + r.Intn(hi-lo+1)
}
