package simulator

import (
	"math/rand"
	"sort"

	"gamestats/ingestion/internal/model"
	"gamestats/ingestion/internal/validation"
)

// Corrupt agrega eventos dañados a propósito para demostrar la validación.
// Por cada evento, con probabilidad percent %, inserta justo después una
// COPIA dañada (con eventId "<original>-bad"). Devuelve cuántas agregó.
//
// Se agregan copias en vez de dañar los originales para que las partidas
// sigan completas: si se dañara un DISC_DROPPED, el tablero de connect4 quedaría
// distinto y las amenazas y el 4 en línea de después no tendrían sentido;
// si se dañara MATCH_FINISHED, la partida nunca terminaría.
//
// No se tocan los escenarios guionizados. El daño usa su propio generador
// con la misma semilla: es determinista y no cambia las partidas generadas.
func Corrupt(matches []Match, seed int64, percent int) int {
	r := rand.New(rand.NewSource(seed))
	count := 0
	for i := range matches {
		if matches[i].Script != "" {
			continue
		}
		var events []model.Event
		for _, e := range matches[i].Events {
			events = append(events, e)
			if r.Intn(100) < percent {
				events = append(events, damage(r, e))
				count++
			}
		}
		matches[i].Events = events
	}
	return count
}

// damage devuelve una copia de e que rompe una regla de validación al azar.
func damage(r *rand.Rand, e model.Event) model.Event {
	bad := e
	bad.EventID = e.EventID + "-bad"

	switch r.Intn(7) {
	case 0: // regla 1: falta un campo obligatorio
		bad.MatchID = ""
	case 1: // regla 2: timestamp que no es RFC 3339
		bad.Timestamp = "ayer a las 2"
	case 2: // regla 3: juego desconocido
		bad.GameID = "chess"
	case 3: // regla 4: tipo desconocido
		bad.Type = "PLAYER_DANCED"
	case 4: // regla 5: falta playerId (se fuerza un tipo que lo exige)
		bad.Type, bad.Action, bad.PlayerID = "PLAYER_LEFT", "", ""
	case 5: // regla 6: acción que existe, pero en otro juego
		bad.Type = "PLAYER_ACTION"
		if bad.PlayerID == "" {
			bad.PlayerID = "p1"
		}
		bad.Action = actionOfAnotherGame(r, e.GameID)
	case 6: // regla 7: action en un evento que no es PLAYER_ACTION
		bad.Type, bad.Action = "MATCH_STARTED", "LAP_COMPLETED"
	}
	return bad
}

// actionOfAnotherGame elige al azar una acción del catálogo que NO es del
// juego gameID. Se ordena la lista porque recorrer un map en Go no tiene un
// orden fijo, y el simulador tiene que ser determinista.
func actionOfAnotherGame(r *rand.Rand, gameID string) string {
	var others []string
	for game, actions := range validation.GameActions {
		if game == gameID {
			continue
		}
		for action := range actions {
			others = append(others, action)
		}
	}
	sort.Strings(others)
	return others[r.Intn(len(others))]
}
