// Package model define las estructuras de datos que comparten los demás paquetes.
package model

import "encoding/json"

// Event es un evento de una partida, tal como llega por POST /events.
//
// Timestamp se guarda como string (y no como time.Time) para que un timestamp
// mal formado no haga fallar el decode de todo el lote: la validación lo
// revisa después y rechaza solo ese evento con su motivo.
//
// Data es json.RawMessage: Go no interpreta su contenido. Así el sistema es
// genérico (sirve para cualquier juego) y Scala recibe exactamente lo que llegó.
type Event struct {
	EventID     string          `json:"eventId"`
	Timestamp   string          `json:"timestamp"`
	GameID      string          `json:"gameId"`
	GameVersion string          `json:"gameVersion"`
	MatchID     string          `json:"matchId"`
	PlayerID    string          `json:"playerId,omitempty"`
	Type        string          `json:"type"`
	Action      string          `json:"action,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}

// Batch es una partida terminada, lista para enviarse a Scala (POST /analyze).
// Events va ordenado por timestamp.
type Batch struct {
	MatchID     string   `json:"matchId"`
	GameID      string   `json:"gameId"`
	GameVersion string   `json:"gameVersion"`
	StartedAt   string   `json:"startedAt"`
	FinishedAt  string   `json:"finishedAt"`
	Players     []string `json:"players"`
	Events      []Event  `json:"events"`
}
