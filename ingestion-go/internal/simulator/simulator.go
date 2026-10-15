// Package simulator genera partidas falsas (pero realistas) y las envía al
// servidor de ingestión, como si fueran una fuente externa de eventos.
//
// Determinismo: todo sale de UN generador rand.New(rand.NewSource(seed)) y de
// una fecha base fija. No se usa time.Now() ni el rand global, así que con la
// misma semilla se generan exactamente las mismas partidas.
package simulator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"gamestats/ingestion/internal/model"
)

// baseTime es la fecha fija desde la que empiezan todas las partidas.
var baseTime = time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)

// tsLayout es RFC 3339 con milisegundos, por ejemplo 2026-10-05T14:01:22.500Z.
const tsLayout = "2006-01-02T15:04:05.000Z07:00"

// Match es una partida generada, con sus eventos en orden de timestamp.
type Match struct {
	ID     string
	GameID string
	Events []model.Event
	Script string // descripción si es un escenario guionizado; vacío si es aleatoria
}

// Counts dice cuántas partidas aleatorias generar de cada juego.
type Counts struct {
	Racing, Fighting, Connect4, Tetris int
}

// Generate crea los escenarios guionizados (siempre) y las partidas
// aleatorias de cada juego a partir de la semilla.
func Generate(seed int64, n Counts) []Match {
	r := rand.New(rand.NewSource(seed))
	matches := scriptedMatches(seed)
	for i := 1; i <= n.Racing; i++ {
		id := fmt.Sprintf("race-s%d-%03d", seed, i)
		start := baseTime.Add(time.Duration(i) * 15 * time.Minute)
		matches = append(matches, generateRacing(r, id, start))
	}
	for i := 1; i <= n.Connect4; i++ {
		id := fmt.Sprintf("c4-s%d-%03d", seed, i)
		start := baseTime.Add(time.Duration(i) * 2 * time.Hour)
		matches = append(matches, generateConnect4(r, id, start))
	}
	for i := 1; i <= n.Fighting; i++ {
		id := fmt.Sprintf("fight-s%d-%03d", seed, i)
		start := baseTime.Add(time.Duration(i) * 10 * time.Minute)
		matches = append(matches, generateFighting(r, id, start))
	}
	for i := 1; i <= n.Tetris; i++ {
		id := fmt.Sprintf("tetris-s%d-%03d", seed, i)
		start := baseTime.Add(time.Duration(i) * 20 * time.Minute)
		matches = append(matches, generateTetris(r, id, start))
	}
	return matches
}

// builder acumula los eventos de una partida mientras se generan.
type builder struct {
	matchID string
	gameID  string
	events  []timedEvent
}

type timedEvent struct {
	at    time.Time
	event model.Event
}

func newBuilder(matchID, gameID string) *builder {
	return &builder{matchID: matchID, gameID: gameID}
}

// add agrega un evento. data puede ser nil (sin "data").
func (b *builder) add(at time.Time, typ, player, action string, data any) {
	e := model.Event{
		Timestamp:   at.Format(tsLayout),
		GameID:      b.gameID,
		GameVersion: "1.0",
		MatchID:     b.matchID,
		PlayerID:    player,
		Type:        typ,
		Action:      action,
	}
	if data != nil {
		// json.Marshal ordena las claves de un map, así el resultado es
		// siempre el mismo.
		raw, err := json.Marshal(data)
		if err != nil {
			panic(err) // solo pasaría por un error de programación
		}
		e.Data = raw
	}
	b.events = append(b.events, timedEvent{at: at, event: e})
}

// build ordena los eventos por tiempo (estable: los empates conservan el
// orden en que se agregaron) y les asigna los eventId.
func (b *builder) build() Match {
	sort.SliceStable(b.events, func(i, j int) bool {
		return b.events[i].at.Before(b.events[j].at)
	})
	events := make([]model.Event, len(b.events))
	for i, te := range b.events {
		events[i] = te.event
		events[i].EventID = fmt.Sprintf("%s-e%03d", b.matchID, i+1)
	}
	return Match{ID: b.matchID, GameID: b.gameID, Events: events}
}

// playerIDs devuelve p1, p2, ..., pn.
func playerIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("p%d", i+1)
	}
	return ids
}

// pick elige un elemento al azar.
func pick(r *rand.Rand, list []string) string {
	return list[r.Intn(len(list))]
}

// randSeconds devuelve una duración al azar entre minSec y maxSec segundos.
func randSeconds(r *rand.Rand, minSec, maxSec int) time.Duration {
	return time.Duration(minSec+r.Intn(maxSec-minSec+1)) * time.Second
}

// ---------------------------------------------------------------------------
// Envío

// chunkSize es cuántos eventos van en cada POST. Mandar en grupos chicos (y
// no la partida completa de una vez) hace que los eventos de distintas
// partidas lleguen intercalados al servidor.
const chunkSize = 5

// chunkPause es la pausa entre grupos de una misma partida.
const chunkPause = 10 * time.Millisecond

// Result resume el envío de una partida.
type Result struct {
	MatchID  string
	Received int
	Accepted int
	Rejected []string // "eventId: motivo"
	Err      error
}

// eventsResponse es la respuesta de POST /events del servidor.
type eventsResponse struct {
	Received int `json:"received"`
	Accepted int `json:"accepted"`
	Rejected []struct {
		EventID string `json:"eventId"`
		Reason  string `json:"reason"`
	} `json:"rejected"`
}

// Send envía cada partida en su propia goroutine y espera a que terminen
// todas. Cada goroutine escribe solo en su posición de results, así que no
// hace falta mutex.
func Send(target string, matches []Match) []Result {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	url := strings.TrimRight(target, "/") + "/events"

	results := make([]Result, len(matches))
	var wg sync.WaitGroup
	for i, m := range matches {
		wg.Add(1)
		go func(i int, m Match) {
			defer wg.Done()
			results[i] = sendMatch(httpClient, url, m)
		}(i, m)
	}
	wg.Wait()
	return results
}

// sendMatch manda los eventos de una partida en grupos de chunkSize.
func sendMatch(c *http.Client, url string, m Match) Result {
	res := Result{MatchID: m.ID}
	for start := 0; start < len(m.Events); start += chunkSize {
		end := min(start+chunkSize, len(m.Events))
		body, err := json.Marshal(m.Events[start:end])
		if err != nil {
			res.Err = err
			return res
		}

		resp, err := c.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			res.Err = err
			return res
		}
		var er eventsResponse
		err = json.NewDecoder(resp.Body).Decode(&er)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || err != nil {
			res.Err = fmt.Errorf("el servidor respondió %s", resp.Status)
			return res
		}

		res.Received += er.Received
		res.Accepted += er.Accepted
		for _, rj := range er.Rejected {
			res.Rejected = append(res.Rejected, rj.EventID+": "+rj.Reason)
		}
		time.Sleep(chunkPause)
	}
	return res
}
