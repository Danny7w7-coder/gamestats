package validation

import (
	"fmt"
	"regexp"
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

// gameIDPattern es el formato de cualquier gameId: minúsculas, dígitos y
// guiones, de 1 a 40 caracteres. Los juegos fuera del catálogo se aceptan
// mientras cumplan este formato.
var gameIDPattern = regexp.MustCompile(`^[a-z0-9-]{1,40}$`)

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

	// Regla 3: el gameId tiene formato válido. Cualquier juego con ese
	// formato se acepta; solo los del catálogo (GameActions) tienen además
	// validación estricta de acciones.
	if !gameIDPattern.MatchString(e.GameID) {
		return fmt.Errorf("gameId con formato inválido: %q (se espera %s)", e.GameID, gameIDPattern)
	}
	actions, known := GameActions[e.GameID]

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
		// Regla 6: PLAYER_ACTION necesita una acción. En un juego del catálogo,
		// si la acción es de OTRO juego (por ejemplo CONNECT_FOUR en racing),
		// casi seguro es un error y se rechaza. Si no existe en ningún juego,
		// es una acción nueva: se acepta (el enunciado pide poder sumar
		// eventos sin tocar el sistema) y el handler la anota como aviso (ver
		// UnknownAction). En un juego desconocido solo rige la regla genérica:
		// la acción no puede venir vacía.
		if e.Action == "" {
			return fmt.Errorf("falta el campo action (obligatorio en PLAYER_ACTION)")
		}
		if known && !actions[e.Action] {
			if other := gameOf(e.Action); other != "" {
				return fmt.Errorf("la acción %q no existe en el juego %q (es una acción de %q)", e.Action, e.GameID, other)
			}
		}
	} else if e.Action != "" {
		// Regla 7 (del contrato de Danny): action solo se permite en PLAYER_ACTION.
		return fmt.Errorf("action solo se permite en PLAYER_ACTION (llegó en %s)", e.Type)
	}

	return nil
}

// UnknownAction dice si un evento (que ya pasó Validate) trae una acción que
// no está en el catálogo de un juego conocido. Esos eventos se aceptan, pero
// el handler los anota como aviso para que se sepa que llegó algo nuevo. En un
// juego fuera del catálogo no hay catálogo contra qué comparar, así que nunca
// es aviso (si no, cada acción de un juego nuevo llenaría el log).
func UnknownAction(e model.Event) bool {
	actions, known := GameActions[e.GameID]
	return known && e.Type == TypePlayerAction && !actions[e.Action]
}

// gameOf devuelve el juego al que pertenece una acción, o "" si no es de
// ninguno. Como los nombres de acción no se repiten entre juegos, hay a lo
// sumo un resultado (por eso no importa el orden en que se recorre el map).
func gameOf(action string) string {
	for game, actions := range GameActions {
		if actions[action] {
			return game
		}
	}
	return ""
}
