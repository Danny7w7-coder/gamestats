package validation

import (
	"fmt"
	"time"

	"gamestats/ingestion/internal/model"
)

// Tipos de evento.
const (
	TypeMatchStarted       = "MATCH_STARTED"
	TypeMatchFinished      = "MATCH_FINISHED"
	TypeMatchPaused        = "MATCH_PAUSED"
	TypeMatchResumed       = "MATCH_RESUMED"
	TypePlayerJoined       = "PLAYER_JOINED"
	TypePlayerLeft         = "PLAYER_LEFT"
	TypePlayerDisconnected = "PLAYER_DISCONNECTED"
	TypePlayerReconnected  = "PLAYER_RECONNECTED"
	TypePlayerAction       = "PLAYER_ACTION"
)

// eventTypes son los valores válidos de "type". Son comunes a todos los juegos.
var eventTypes = map[string]bool{
	TypeMatchStarted:       true,
	TypeMatchFinished:      true,
	TypeMatchPaused:        true,
	TypeMatchResumed:       true,
	TypePlayerJoined:       true,
	TypePlayerLeft:         true,
	TypePlayerDisconnected: true,
	TypePlayerReconnected:  true,
	TypePlayerAction:       true,
}

// matchTypes son los eventos de la partida completa (no de un jugador): son
// los únicos que pueden venir sin playerId.
var matchTypes = map[string]bool{
	TypeMatchStarted:  true,
	TypeMatchFinished: true,
	TypeMatchPaused:   true,
	TypeMatchResumed:  true,
}

// Validate devuelve nil si el evento es válido, o un error cuyo mensaje es el
// motivo del rechazo. Las reglas se revisan en orden y se devuelve la primera
// que falla. No se revisa el contenido de "data": eso le corresponde a Scala.
func Validate(e model.Event) error {
	// Regla 1: campos obligatorios no vacíos.
	required := []struct {
		name  string
		value string
	}{
		{"eventId", e.EventID},
		{"timestamp", e.Timestamp},
		{"gameId", e.GameID},
		{"gameVersion", e.GameVersion},
		{"matchId", e.MatchID},
		{"type", e.Type},
	}
	for _, f := range required {
		if f.value == "" {
			return fmt.Errorf("falta el campo %s", f.name)
		}
	}

	// Regla 2: timestamp en RFC 3339 (acepta fracciones de segundo, ej. ".500Z").
	if _, err := time.Parse(time.RFC3339, e.Timestamp); err != nil {
		return fmt.Errorf("timestamp no está en formato RFC 3339: %q", e.Timestamp)
	}

	// Regla 3: el juego existe en el catálogo.
	actions, ok := GameActions[e.GameID]
	if !ok {
		return fmt.Errorf("gameId desconocido: %q", e.GameID)
	}

	// Regla 4: el tipo de evento existe.
	if !eventTypes[e.Type] {
		return fmt.Errorf("type desconocido: %q", e.Type)
	}

	// Regla 5: playerId obligatorio salvo en los eventos de partida
	// (MATCH_STARTED, MATCH_FINISHED, MATCH_PAUSED y MATCH_RESUMED).
	if e.PlayerID == "" && !matchTypes[e.Type] {
		return fmt.Errorf("falta el campo playerId (obligatorio en %s)", e.Type)
	}

	if e.Type == TypePlayerAction {
		// Regla 6: PLAYER_ACTION necesita una acción del catálogo de ESE juego.
		if e.Action == "" {
			return fmt.Errorf("falta el campo action (obligatorio en PLAYER_ACTION)")
		}
		if !actions[e.Action] {
			return fmt.Errorf("la acción %q no existe en el juego %q", e.Action, e.GameID)
		}
	} else if e.Action != "" {
		// Regla 7 (del contrato de Danny): action solo se permite en PLAYER_ACTION.
		return fmt.Errorf("action solo se permite en PLAYER_ACTION (llegó en %s)", e.Type)
	}

	return nil
}
