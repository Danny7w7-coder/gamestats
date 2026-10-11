package simulator

import (
	"math/rand"
	"time"
)

// fighter es un personaje con sus golpes especiales y su fatality.
type fighter struct {
	name     string
	specials []string
	fatality string
}

var fighters = []fighter{
	{"scorpion", []string{"spear", "hellfire"}, "toasty"},
	{"sub-zero", []string{"ice_ball", "slide"}, "spine_rip"},
	{"liu_kang", []string{"dragon_fire", "bicycle_kick"}, "dragon_bite"},
	{"kitana", []string{"fan_toss", "square_wave"}, "kiss_of_death"},
	{"raiden", []string{"lightning", "teleport"}, "electrocution"},
	{"sonya", []string{"energy_rings", "leg_grab"}, "kiss"},
}

var stages = []string{"the_pit", "living_forest", "dead_pool", "shaolin_temple", "goros_lair"}

var moves = []string{"punch", "kick", "uppercut"}

// fightStep es el tiempo entre una acción y la siguiente.
const fightStep = 2 * time.Second

// fightGame escribe una pelea 1 vs 1 (p1 contra p2) evento por evento. Lleva
// la vida de cada luchador en el round actual, así los datos que dependen de
// ella (remainingHealth de ROUND_WON y health de FATAL_BLOW) siempre son
// coherentes con el daño recibido. Lo usan el generador al azar y los
// escenarios guionizados.
type fightGame struct {
	b         *builder
	t         time.Time // reloj: hora del último evento
	rounds    int       // la pelea es al mejor de "rounds"
	round     int       // round actual
	health    map[string]int
	wins      map[string]int
	fatalUsed map[string]bool // ya usó su FATAL_BLOW (uno por pelea)
	fighterOf map[string]fighter
}

func newFightGame(id string, start time.Time, stage string, rounds int, f1, f2 fighter) *fightGame {
	g := &fightGame{
		b: newBuilder(id, "fighting"), t: start, rounds: rounds, round: 1,
		health:    map[string]int{"p1": 100, "p2": 100},
		wins:      map[string]int{},
		fatalUsed: map[string]bool{},
		fighterOf: map[string]fighter{"p1": f1, "p2": f2},
	}
	g.b.add(start, "MATCH_STARTED", "", "", map[string]any{"stage": stage, "rounds": rounds})
	g.b.add(start.Add(1*time.Second), "PLAYER_JOINED", "p1", "", map[string]any{"character": f1.name})
	g.b.add(start.Add(2*time.Second), "PLAYER_JOINED", "p2", "", map[string]any{"character": f2.name})
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

// strike agrega un golpe de p al rival y le resta la vida.
func (g *fightGame) strike(p, action string, damage int, data map[string]any) {
	data["targetId"] = rivalOf(p)
	data["round"] = g.round
	data["damage"] = damage
	g.health[rivalOf(p)] -= damage
	g.act(p, action, data)
}

// attack: golpe simple. damage 0 = falló (hit: false con damage 0, como
// pide el contrato). Lo mismo vale para special y fatalBlow.
func (g *fightGame) attack(p, move string, damage int) {
	g.strike(p, "ATTACK", damage, map[string]any{"move": move, "hit": damage > 0})
}

func (g *fightGame) special(p, name string, damage int) {
	g.strike(p, "SPECIAL_MOVE", damage, map[string]any{"name": name, "hit": damage > 0})
}

func (g *fightGame) combo(p string, hits, damage int) {
	g.strike(p, "COMBO", damage, map[string]any{"hits": hits})
}

func (g *fightGame) throw(p string, damage int) {
	g.strike(p, "THROW", damage, map[string]any{})
}

// fatalBlow: golpe muy fuerte, una vez por pelea, solo con la vida propia en
// 30 o menos (health es la vida de quien lo usa).
func (g *fightGame) fatalBlow(p string, damage int) {
	g.fatalUsed[p] = true
	g.strike(p, "FATAL_BLOW", damage, map[string]any{"hit": damage > 0, "health": g.health[p]})
}

// block: p bloquea un golpe del rival.
func (g *fightGame) block(p string) {
	g.act(p, "BLOCK", map[string]any{"attackerId": rivalOf(p), "round": g.round})
}

// comboBreaker: p corta el combo que le estaba haciendo el rival.
func (g *fightGame) comboBreaker(p string) {
	g.act(p, "COMBO_BREAKER", map[string]any{"attackerId": rivalOf(p), "round": g.round})
}

// knockdown: p derriba al rival.
func (g *fightGame) knockdown(p string) {
	g.act(p, "KNOCKDOWN", map[string]any{"targetId": rivalOf(p), "round": g.round})
}

// roundWon cierra el round: gana p con la vida que le queda, por KO o por
// tiempo. Después empieza el siguiente round con las vidas en 100.
func (g *fightGame) roundWon(p, method string) {
	g.act(p, "ROUND_WON", map[string]any{"round": g.round, "remainingHealth": g.health[p], "method": method})
	g.wins[p]++
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

// fatality: el remate final del ganador; después ya no hay acciones.
func (g *fightGame) fatality(p string) {
	g.act(p, "FATALITY", map[string]any{"targetId": rivalOf(p), "name": g.fighterOf[p].fatality})
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
//   - los rounds se juegan en orden y la pelea termina cuando alguien gana
//     la mayoría, así que round ≤ rounds y hay un ROUND_WON por round;
//   - COMBO_BREAKER va justo después de un COMBO del rival;
//   - FATAL_BLOW solo con la vida en 30 o menos y una vez por luchador;
//   - la FATALITY la hace el ganador y es la última acción.
func generateFighting(r *rand.Rand, id string, start time.Time) Match {
	rounds := []int{1, 3, 3, 3, 5}[r.Intn(5)]
	i := r.Intn(len(fighters))
	j := (i + 1 + r.Intn(len(fighters)-1)) % len(fighters) // otro personaje
	g := newFightGame(id, start, pick(r, stages), rounds, fighters[i], fighters[j])

	for g.winner() == "" {
		playRound(r, g)
		if g.winner() == "" && r.Intn(100) < 10 {
			g.pause("player_pause", randSeconds(r, 10, 40))
		}
	}
	if w := g.winner(); r.Intn(100) < 40 {
		g.fatality(w)
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

		if g.health[p] <= 30 && !g.fatalUsed[p] && r.Intn(2) == 0 {
			g.fatalBlow(p, hitOrMiss(r, 80, 30, 45))
		} else {
			switch roll := r.Intn(100); {
			case roll < 45:
				damage := hitOrMiss(r, 75, 5, 20)
				g.attack(p, pick(r, moves), damage)
				if damage == 0 && r.Intn(100) < 60 {
					g.block(rival)
				}
			case roll < 65:
				g.special(p, pick(r, g.fighterOf[p].specials), hitOrMiss(r, 70, 12, 20))
			case roll < 82:
				hits := 2 + r.Intn(4)
				g.combo(p, hits, 6*hits+r.Intn(6))
				if g.health[rival] > 0 && r.Intn(100) < 25 {
					g.comboBreaker(rival)
				}
			default:
				g.throw(p, 8+r.Intn(8))
				if r.Intn(100) < 40 {
					g.knockdown(p)
				}
			}
		}

		if g.health[rival] <= 0 {
			g.roundWon(p, "ko")
			return
		}
	}
	// Se acabó el tiempo: gana el que tiene más vida (en empate, p1).
	g.wait(20 * time.Second)
	if g.health["p2"] > g.health["p1"] {
		g.roundWon("p2", "timeout")
	} else {
		g.roundWon("p1", "timeout")
	}
}

// hitOrMiss devuelve 0 (falló) o un daño entre lo y hi, con hitPct % de acierto.
func hitOrMiss(r *rand.Rand, hitPct, lo, hi int) int {
	if r.Intn(100) >= hitPct {
		return 0
	}
	return lo + r.Intn(hi-lo+1)
}
