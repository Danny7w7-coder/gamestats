package pipeline

import (
	"fmt"
	"sort"
	"time"
)

// IdleTimeouts dice cuánto tiempo sin eventos tolera cada juego antes de que
// su partida se abandone. Un juego que no aparece en PerGame usa Default.
// Default debe ser mayor que 0, igual que cada valor de PerGame.
type IdleTimeouts struct {
	Default time.Duration            // para los juegos que no están en PerGame
	PerGame map[string]time.Duration // gameId -> inactividad tolerada
}

// Uniform devuelve unos timeouts donde todos los juegos esperan lo mismo.
func Uniform(d time.Duration) IdleTimeouts {
	return IdleTimeouts{Default: d}
}

// For devuelve el timeout del juego gameID.
func (t IdleTimeouts) For(gameID string) time.Duration {
	if d, ok := t.PerGame[gameID]; ok {
		return d
	}
	return t.Default
}

// min devuelve el timeout más corto de todos: el reloj del router debe
// revisar al menos tan seguido como el juego más impaciente.
func (t IdleTimeouts) min() time.Duration {
	m := t.Default
	for _, d := range t.PerGame {
		if d < m {
			m = d
		}
	}
	return m
}

// Table devuelve una línea por juego (ordenadas por gameId) y una última para
// "el resto", para mostrarlas en el log de arranque.
func (t IdleTimeouts) Table() []string {
	games := make([]string, 0, len(t.PerGame))
	width := len("(resto)")
	for g := range t.PerGame {
		games = append(games, g)
		if len(g) > width {
			width = len(g)
		}
	}
	sort.Strings(games)

	lines := make([]string, 0, len(games)+1)
	for _, g := range games {
		lines = append(lines, fmt.Sprintf("%-*s %s", width, g, t.PerGame[g]))
	}
	lines = append(lines, fmt.Sprintf("%-*s %s", width, "(resto)", t.Default))
	return lines
}
