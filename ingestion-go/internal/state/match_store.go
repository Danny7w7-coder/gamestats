// Package state guarda el estado actual de cada partida para consultarlo con
// GET /matches.
package state

import (
	"sort"
	"sync"

	"gamestats/ingestion/internal/model"
	"gamestats/ingestion/internal/validation"
)

// Estados posibles de una partida.
const (
	StatusInProgress = "en_curso"
	StatusFinished   = "terminada"
	StatusSent       = "enviada"
	StatusSendError  = "error_envio"
	StatusAbandoned  = "abandonada"
)

// MatchInfo es una foto (copia) del estado de una partida, lista para
// convertirse a JSON. Como es una copia, se puede leer sin tener el lock.
type MatchInfo struct {
	MatchID          string   `json:"matchId"`
	GameID           string   `json:"gameId"`
	Status           string   `json:"status"`
	EventCount       int      `json:"eventCount"`
	ConnectedPlayers []string `json:"connectedPlayers"`
}

// GameInfo resume un juego: cuántas de sus partidas están en curso ahora.
type GameInfo struct {
	GameID     string `json:"gameId"`
	InProgress int    `json:"inProgress"`
}

// match es el estado interno de una partida. Solo se toca con el lock tomado.
type match struct {
	gameID     string
	status     string
	eventCount int
	connected  map[string]bool // conjunto de playerId conectados
}

// MatchStore es estado COMPARTIDO: lo escriben quienes procesan eventos y lo
// leen los handlers de GET /matches, cada uno en su propia goroutine.
// Se usa sync.RWMutex porque hay muchas lecturas: varias lecturas pueden ir
// a la vez (RLock), pero una escritura (Lock) espera a quedar sola.
type MatchStore struct {
	mu      sync.RWMutex
	matches map[string]*match // matchId -> estado
}

// NewMatchStore crea un store vacío.
func NewMatchStore() *MatchStore {
	return &MatchStore{matches: make(map[string]*match)}
}

// Record actualiza la partida con un evento ya validado. Si la partida no
// existía, la crea en estado en_curso.
func (s *MatchStore) Record(e model.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := s.matches[e.MatchID]
	if !ok {
		m = &match{gameID: e.GameID, status: StatusInProgress, connected: make(map[string]bool)}
		s.matches[e.MatchID] = m
	}

	m.eventCount++

	switch e.Type {
	case validation.TypePlayerJoined, validation.TypePlayerReconnected:
		m.connected[e.PlayerID] = true
	case validation.TypePlayerLeft, validation.TypePlayerDisconnected:
		delete(m.connected, e.PlayerID)
	case validation.TypeMatchFinished:
		m.status = StatusFinished
	}
}

// SetStatus cambia el estado de una partida que ya existe (por ejemplo, a
// enviada o error_envio después de intentar mandarla a Scala).
func (s *MatchStore) SetStatus(matchID, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if m, ok := s.matches[matchID]; ok {
		m.status = status
	}
}

// Status devuelve el estado de una partida. ok es false si no existe.
func (s *MatchStore) Status(matchID string) (status string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	m, ok := s.matches[matchID]
	if !ok {
		return "", false
	}
	return m.status, true
}

// List devuelve una copia de todas las partidas, ordenadas por matchId.
// Se devuelven copias (no punteros a match) para que nadie lea el estado
// interno fuera del lock.
func (s *MatchStore) List() []MatchInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]MatchInfo, 0, len(s.matches))
	for id, m := range s.matches {
		players := make([]string, 0, len(m.connected))
		for p := range m.connected {
			players = append(players, p)
		}
		sort.Strings(players)

		list = append(list, MatchInfo{
			MatchID:          id,
			GameID:           m.gameID,
			Status:           m.status,
			EventCount:       m.eventCount,
			ConnectedPlayers: players,
		})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].MatchID < list[j].MatchID })
	return list
}

// Games devuelve, por cada gameId que aparece en el store, cuántas partidas
// están en curso (0 si todas terminaron o se abandonaron). Va ordenado por
// gameId para que la salida sea estable. La lectura se hace con RLock, igual
// que List.
func (s *MatchStore) Games() []GameInfo {
	s.mu.RLock()
	counts := make(map[string]int)
	for _, m := range s.matches {
		if _, ok := counts[m.gameID]; !ok {
			counts[m.gameID] = 0
		}
		if m.status == StatusInProgress {
			counts[m.gameID]++
		}
	}
	s.mu.RUnlock()

	list := make([]GameInfo, 0, len(counts))
	for id, n := range counts {
		list = append(list, GameInfo{GameID: id, InProgress: n})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].GameID < list[j].GameID })
	return list
}
