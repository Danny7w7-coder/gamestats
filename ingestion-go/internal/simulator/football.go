package simulator

import (
	"math"
	"math/rand"
	"time"
)

// Equipos, iguales a los de los fixtures de Danny.
const (
	homeTeam = "A"
	awayTeam = "B"
)

// perTeam: fútbol arcade 6 vs 6 (5 de campo y portero), sin suplentes.
const perTeam = 6

// halfTime es lo que dura el entretiempo (la pausa del minuto 45).
const halfTime = 15 * time.Minute

// footballGame escribe un partido evento por evento. Lo usan tanto el
// generador al azar como los escenarios guionizados.
// Equipo A: p1 (portero) a p6. Equipo B: p7 (portero) a p12.
type footballGame struct {
	b       *builder
	kickoff time.Time
	teamOf  map[string]string // jugador -> equipo
	keeper  map[string]string // equipo -> portero
	field   map[string][]string
}

func newFootballGame(id string, start time.Time) *footballGame {
	g := &footballGame{
		b:       newBuilder(id, "football"),
		kickoff: start.Add(time.Minute),
		teamOf:  map[string]string{},
		keeper:  map[string]string{},
		field:   map[string][]string{},
	}
	g.b.add(start, "MATCH_STARTED", "", "", map[string]any{"homeTeam": homeTeam, "awayTeam": awayTeam})
	for i, p := range playerIDs(2 * perTeam) {
		team := homeTeam
		if i >= perTeam {
			team = awayTeam
		}
		g.teamOf[p] = team
		if i%perTeam == 0 {
			g.keeper[team] = p
		} else {
			g.field[team] = append(g.field[team], p)
		}
		g.b.add(start.Add(time.Duration(i+1)*time.Second), "PLAYER_JOINED", p, "", map[string]any{"teamId": team})
	}

	// Entretiempo: al minuto 45 el partido se pausa 15 minutos.
	g.b.add(g.kickoff.Add(45*time.Minute), "MATCH_PAUSED", "", "", map[string]any{"reason": "half_time"})
	g.b.add(g.kickoff.Add(45*time.Minute+halfTime), "MATCH_RESUMED", "", "", map[string]any{})
	return g
}

// at devuelve la hora real de un momento del juego. Desde el minuto 45 se
// suma el entretiempo, así ninguna jugada cae dentro de la pausa. second
// puede pasar de 59 (por ejemplo, un gol 2 s después de un tiro en el 44:59).
func (g *footballGame) at(minute, second int) time.Time {
	played := time.Duration(minute)*time.Minute + time.Duration(second)*time.Second
	if played >= 45*time.Minute {
		played += halfTime
	}
	return g.kickoff.Add(played)
}

// act agrega una acción del jugador; teamId y minute se completan solos.
func (g *footballGame) act(minute, second int, player, action string, data map[string]any) {
	data["teamId"] = g.teamOf[player]
	data["minute"] = minute
	g.b.add(g.at(minute, second), "PLAYER_ACTION", player, action, data)
}

func (g *footballGame) pass(minute, second int, from, to string, success bool, distance float64) {
	g.act(minute, second, from, "PASS", map[string]any{"targetId": to, "success": success, "distance": distance})
}

// shot es un tiro normal (power = false) o un tiro potente (POWER_SHOT).
func (g *footballGame) shot(minute, second int, player string, power, onTarget bool) {
	action := "SHOT"
	if power {
		action = "POWER_SHOT"
	}
	g.act(minute, second, player, action, map[string]any{"onTarget": onTarget})
}

// goal agrega el tiro al arco y, 2 s después, el GOAL del mismo jugador
// (todo gol va precedido de su tiro). assist vacío = gol sin asistencia.
func (g *footballGame) goal(minute, second int, player, assist string, power bool) {
	g.shot(minute, second, player, power, true)
	data := map[string]any{}
	if assist != "" {
		data["assistId"] = assist
	}
	g.act(minute, second+2, player, "GOAL", data)
}

// save: el portero del equipo rival ataja un tiro al arco de shooter.
func (g *footballGame) save(minute, second int, shooter string, power bool) {
	g.shot(minute, second, shooter, power, true)
	g.act(minute, second+1, g.keeper[g.rival(g.teamOf[shooter])], "SAVE", map[string]any{})
}

// ownGoal: player mete la pelota en su propio arco. Lleva el teamId de quien
// lo hace y el gol suma para el rival (lo calcula Scala).
func (g *footballGame) ownGoal(minute, second int, player string) {
	g.act(minute, second, player, "OWN_GOAL", map[string]any{})
}

// foul: target vacío = falta sin jugador afectado (es opcional). Una roja
// saca al jugador de la cancha: no vuelve a aparecer.
func (g *footballGame) foul(minute, second int, player, target, card string) {
	data := map[string]any{"card": card}
	if target != "" {
		data["targetId"] = target
	}
	g.act(minute, second, player, "FOUL", data)
	if card == "red" {
		team := g.teamOf[player]
		g.field[team] = remove(g.field[team], player)
	}
}

func (g *footballGame) tackle(minute, second int, player, target string, success bool) {
	g.act(minute, second, player, "TACKLE", map[string]any{"targetId": target, "success": success})
}

func (g *footballGame) dribble(minute, second int, player string, success bool) {
	g.act(minute, second, player, "DRIBBLE", map[string]any{"success": success})
}

func (g *footballGame) interception(minute, second int, player string) {
	g.act(minute, second, player, "INTERCEPTION", map[string]any{})
}

func (g *footballGame) corner(minute, second int, player string) {
	g.act(minute, second, player, "CORNER", map[string]any{})
}

func (g *footballGame) rival(team string) string {
	if team == homeTeam {
		return awayTeam
	}
	return homeTeam
}

// finish cierra el partido (con unos minutos de descuento).
func (g *footballGame) finish(minute int) Match {
	g.b.add(g.at(minute, 30), "MATCH_FINISHED", "", "", map[string]any{})
	return g.b.build()
}

// generateFootball simula un partido 6 vs 6 de 90 minutos. Cada "jugada"
// (una cada 30 s a 3 min, ~50 por partido) es un pase, una gambeta, un tiro,
// un córner, una falta, una barrida, una intercepción o, rara vez, un autogol.
// Reglas que se cumplen por construcción:
//   - todo GOAL va 2 s después de un tiro al arco del mismo jugador;
//   - tras una roja, el jugador sale de g.field y no vuelve a aparecer;
//   - los porteros solo atajan, así que nunca los expulsan y siempre hay
//     quien ataje.
func generateFootball(r *rand.Rand, id string, start time.Time) Match {
	g := newFootballGame(id, start)

	for t := randSeconds(r, 30, 180); t < 90*time.Minute; t += randSeconds(r, 30, 180) {
		minute, second := int(t.Minutes()), int(t.Seconds())%60
		team := homeTeam
		if r.Intn(2) == 1 {
			team = awayTeam
		}
		rival := g.rival(team)
		player := pick(r, g.field[team])

		switch roll := r.Intn(1000); {
		case roll < 350:
			success := r.Intn(100) < 80
			g.pass(minute, second, player, pickOther(r, g.field[team], player), success, randDistance(r))
			if !success && r.Intn(2) == 0 { // el pase fallido lo corta un rival
				g.interception(minute, second+3, pick(r, g.field[rival]))
			}
		case roll < 470:
			success := r.Intn(100) < 60
			g.dribble(minute, second, player, success)
			if !success { // le quitan la pelota con una barrida
				g.tackle(minute, second+2, pick(r, g.field[rival]), player, true)
			}
		case roll < 650:
			addShot(r, g, minute, second, player)
		case roll < 720:
			// Córner y, a veces, un remate de un compañero.
			g.corner(minute, second, player)
			if r.Intn(100) < 40 {
				addShot(r, g, minute, second+20, pickOther(r, g.field[team], player))
			}
		case roll < 820:
			addFoul(r, g, minute, second, player)
		case roll < 920:
			g.tackle(minute, second, pick(r, g.field[rival]), player, r.Intn(100) < 60)
		case roll < 998:
			g.interception(minute, second, pick(r, g.field[rival]))
		default: // 0.2 %: un defensor la mete en su propio arco
			g.ownGoal(minute, second, pick(r, g.field[rival]))
		}
	}

	return g.finish(90 + r.Intn(5))
}

// addShot agrega un tiro (30 % potentes). Si va al arco, 35 % termina en gol
// y el resto lo ataja el portero rival. Con estas probabilidades salen ~1.5
// goles por partido (marcador bajo y realista).
func addShot(r *rand.Rand, g *footballGame, minute, second int, player string) {
	power := r.Intn(100) < 30
	if r.Intn(100) >= 45 { // afuera
		g.shot(minute, second, player, power, false)
		return
	}
	if r.Intn(100) >= 35 {
		g.save(minute, second, player, power)
		return
	}
	assist := ""
	if r.Intn(100) < 60 { // 60 % de los goles tienen asistencia
		assist = pickOther(r, g.field[g.teamOf[player]], player)
	}
	g.goal(minute, second, player, assist, power)
}

// addFoul agrega una falta. La roja solo se da si a su equipo le quedan más
// de 3 jugadores de campo, para que siempre pueda haber pases.
func addFoul(r *rand.Rand, g *footballGame, minute, second int, player string) {
	team := g.teamOf[player]
	card := "none"
	switch roll := r.Intn(100); {
	case roll >= 98 && len(g.field[team]) > 3:
		card = "red"
	case roll >= 75:
		card = "yellow"
	}
	target := ""
	if r.Intn(100) < 80 { // targetId es opcional
		target = pick(r, g.field[g.rival(team)])
	}
	g.foul(minute, second, player, target, card)
}

// randDistance: distancia de un pase, de 5.0 a 40.0 m.
func randDistance(r *rand.Rand) float64 {
	return math.Round((5+r.Float64()*35)*10) / 10
}

// pickOther elige a alguien de list que no sea except.
func pickOther(r *rand.Rand, list []string, except string) string {
	return pick(r, remove(list, except))
}

// remove devuelve una copia de list sin el elemento x.
func remove(list []string, x string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

func randSeconds(r *rand.Rand, minSec, maxSec int) time.Duration {
	return time.Duration(minSec+r.Intn(maxSec-minSec+1)) * time.Second
}
