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

// generateFootball simula un partido de 90 minutos entre A y B, con 3 a 5
// jugadores por equipo. Cada "jugada" es un pase, un tiro (que a veces
// termina en gol) o una falta. Reglas de la sección 5.3:
//   - todo GOAL va precedido de un SHOT con onTarget del mismo jugador, al
//     menos 1 s antes;
//   - tras una tarjeta roja, ese jugador no genera más eventos.
func generateFootball(r *rand.Rand, id string, start time.Time) Match {
	b := newBuilder(id, "football")
	perTeam := 3 + r.Intn(3) // 3..5
	players := playerIDs(2 * perTeam)

	// active son los jugadores que siguen en cancha, por equipo. Son slices
	// (no maps) para que elegir al azar sea determinista.
	active := map[string][]string{
		homeTeam: append([]string{}, players[:perTeam]...),
		awayTeam: append([]string{}, players[perTeam:]...),
	}

	b.add(start, "MATCH_STARTED", "", "", map[string]any{"homeTeam": homeTeam, "awayTeam": awayTeam})
	for i, p := range players {
		team := homeTeam
		if i >= perTeam {
			team = awayTeam
		}
		b.add(start.Add(time.Duration(i+1)*time.Second), "PLAYER_JOINED", p, "", map[string]any{"teamId": team})
	}

	kickoff := start.Add(time.Minute)
	end := kickoff.Add(90 * time.Minute)

	// Una jugada cada 30 s a 3 min (en promedio ~50 por partido).
	for t := kickoff.Add(randSeconds(r, 30, 180)); t.Before(end); t = t.Add(randSeconds(r, 30, 180)) {
		minute := int(t.Sub(kickoff).Minutes())
		team := homeTeam
		if r.Intn(2) == 1 {
			team = awayTeam
		}
		player := pick(r, active[team])

		switch roll := r.Intn(100); {
		case roll < 60:
			addPass(r, b, t, team, player, active[team], minute)
		case roll < 80:
			addShot(r, b, t, team, player, active[team], minute)
		default:
			addFoul(r, b, t, team, player, active, minute)
		}
	}

	b.add(end.Add(randSeconds(r, 60, 240)), "MATCH_FINISHED", "", "", map[string]any{})
	return b.build()
}

func addPass(r *rand.Rand, b *builder, t time.Time, team, player string, teammates []string, minute int) {
	target := pickOther(r, teammates, player)
	distance := math.Round((5+r.Float64()*35)*10) / 10 // 5.0 a 40.0 m
	b.add(t, "PLAYER_ACTION", player, "PASS", map[string]any{
		"teamId": team, "targetId": target, "success": r.Intn(100) < 80,
		"distance": distance, "minute": minute,
	})
}

// addShot agrega un tiro y, a veces, el gol que lo sigue. Con estas
// probabilidades salen ~1.5 goles por partido (marcador bajo y realista).
func addShot(r *rand.Rand, b *builder, t time.Time, team, player string, teammates []string, minute int) {
	onTarget := r.Intn(100) < 40
	b.add(t, "PLAYER_ACTION", player, "SHOT", map[string]any{
		"teamId": team, "onTarget": onTarget, "minute": minute,
	})
	if !onTarget || r.Intn(100) >= 40 {
		return
	}

	goal := map[string]any{"teamId": team, "minute": minute}
	if r.Intn(100) < 60 { // 60 % de los goles tienen asistencia
		goal["assistId"] = pickOther(r, teammates, player)
	}
	// El gol va 2 s después del tiro (la regla pide al menos 1 s).
	b.add(t.Add(2*time.Second), "PLAYER_ACTION", player, "GOAL", goal)
}

// addFoul agrega una falta. Una roja saca al jugador de la cancha; solo se da
// si a su equipo le quedan más de 2 jugadores, para que siempre pueda haber
// pases.
func addFoul(r *rand.Rand, b *builder, t time.Time, team, player string, active map[string][]string, minute int) {
	rival := awayTeam
	if team == awayTeam {
		rival = homeTeam
	}

	card := "none"
	switch roll := r.Intn(100); {
	case roll >= 98 && len(active[team]) > 2:
		card = "red"
	case roll >= 75:
		card = "yellow"
	}

	foul := map[string]any{"teamId": team, "card": card, "minute": minute}
	if r.Intn(100) < 80 { // targetId es opcional
		foul["targetId"] = pick(r, active[rival])
	}
	b.add(t, "PLAYER_ACTION", player, "FOUL", foul)

	if card == "red" {
		active[team] = remove(active[team], player)
	}
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
