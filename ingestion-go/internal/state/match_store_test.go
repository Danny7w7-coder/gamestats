package state

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"gamestats/ingestion/internal/model"
)

func ev(matchID, typ, player string) model.Event {
	return model.Event{MatchID: matchID, GameID: "racing", Type: typ, PlayerID: player}
}

func TestRecord(t *testing.T) {
	s := NewMatchStore()
	events := []model.Event{
		ev("race-001", "MATCH_STARTED", ""),
		ev("race-001", "PLAYER_JOINED", "p1"),
		ev("race-001", "PLAYER_JOINED", "p2"),
		ev("race-001", "PLAYER_JOINED", "p3"),
		ev("race-001", "PLAYER_ACTION", "p1"),
		ev("race-001", "PLAYER_DISCONNECTED", "p2"),
		ev("race-001", "PLAYER_LEFT", "p3"),
	}
	for _, e := range events {
		s.Record(e)
	}

	want := []MatchInfo{{
		MatchID: "race-001", GameID: "racing", Status: StatusInProgress,
		EventCount: 7, ConnectedPlayers: []string{"p1"},
	}}
	if got := s.List(); !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %+v\nse esperaba    %+v", got, want)
	}

	// Reconexión y fin de partida.
	s.Record(ev("race-001", "PLAYER_RECONNECTED", "p2"))
	s.Record(ev("race-001", "MATCH_FINISHED", ""))

	got := s.List()[0]
	if got.Status != StatusFinished || got.EventCount != 9 ||
		!reflect.DeepEqual(got.ConnectedPlayers, []string{"p1", "p2"}) {
		t.Fatalf("después del fin: %+v", got)
	}
}

func TestListEmptyAndSorted(t *testing.T) {
	s := NewMatchStore()
	if got := s.List(); got == nil || len(got) != 0 {
		t.Fatalf("store vacío debe dar lista vacía (no nil), dio %#v", got)
	}

	s.Record(ev("b", "MATCH_STARTED", ""))
	s.Record(ev("a", "MATCH_STARTED", ""))
	got := s.List()
	if got[0].MatchID != "a" || got[1].MatchID != "b" {
		t.Fatalf("no está ordenado por matchId: %+v", got)
	}
	if got[0].ConnectedPlayers == nil {
		t.Fatal("ConnectedPlayers debe ser [] y no nil (en JSON: [] y no null)")
	}
}

// Varias goroutines escriben y leen a la vez. Con -race, este test detecta
// cualquier acceso al mapa sin el lock.
func TestConcurrentRecordAndList(t *testing.T) {
	s := NewMatchStore()
	const matches, perMatch = 10, 50

	var wg sync.WaitGroup
	for m := 0; m < matches; m++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for i := 0; i < perMatch; i++ {
				s.Record(ev(id, "PLAYER_JOINED", fmt.Sprintf("p%d", i)))
			}
		}(fmt.Sprintf("m%d", m))

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perMatch; i++ {
				s.List()
			}
		}()
	}
	wg.Wait()

	list := s.List()
	if len(list) != matches {
		t.Fatalf("partidas = %d, se esperaban %d", len(list), matches)
	}
	for _, m := range list {
		if m.EventCount != perMatch || len(m.ConnectedPlayers) != perMatch {
			t.Fatalf("partida %s: %+v", m.MatchID, m)
		}
	}
}

func evGame(matchID, gameID, typ string) model.Event {
	return model.Event{MatchID: matchID, GameID: gameID, Type: typ}
}

// Games cuenta solo las partidas en curso y lista también los juegos que
// ya no tienen ninguna (con 0), ordenados por gameId.
func TestGames(t *testing.T) {
	s := NewMatchStore()
	if got := s.Games(); got == nil || len(got) != 0 {
		t.Fatalf("store vacío debe dar lista vacía (no nil), dio %#v", got)
	}

	s.Record(evGame("c1", "connect4", "MATCH_STARTED"))
	s.Record(evGame("c2", "connect4", "MATCH_STARTED"))
	s.Record(evGame("c3", "connect4", "MATCH_STARTED"))
	s.Record(evGame("c3", "connect4", "MATCH_FINISHED"))
	s.Record(evGame("t1", "tetris", "MATCH_STARTED"))
	s.Record(evGame("t1", "tetris", "MATCH_FINISHED"))
	s.Record(evGame("x1", "chess", "MATCH_STARTED"))
	s.Record(evGame("x2", "chess", "MATCH_STARTED"))
	s.SetStatus("x2", StatusAbandoned)
	s.Record(evGame("r1", "racing", "MATCH_STARTED"))

	want := []GameInfo{
		{GameID: "chess", InProgress: 1},
		{GameID: "connect4", InProgress: 2},
		{GameID: "racing", InProgress: 1},
		{GameID: "tetris", InProgress: 0},
	}
	if got := s.Games(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Games() = %+v\nse esperaba  %+v", got, want)
	}

	// Una partida enviada o con error de envío ya no está en curso.
	s.SetStatus("r1", StatusSent)
	s.SetStatus("c1", StatusSendError)
	got := s.Games()
	if got[1].InProgress != 1 || got[2].InProgress != 0 {
		t.Fatalf("después de enviar: %+v", got)
	}
}

// Games se lee mientras otras goroutines escriben. Con -race, detecta
// cualquier acceso al mapa sin el lock.
func TestConcurrentRecordAndGames(t *testing.T) {
	s := NewMatchStore()
	const matches, perMatch = 10, 50

	var wg sync.WaitGroup
	for m := 0; m < matches; m++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			for i := 0; i < perMatch; i++ {
				s.Record(evGame(id, "connect4", "PLAYER_JOINED"))
			}
		}(fmt.Sprintf("m%d", m))

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < perMatch; i++ {
				s.Games()
			}
		}()
	}
	wg.Wait()

	want := []GameInfo{{GameID: "connect4", InProgress: matches}}
	if got := s.Games(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Games() = %+v, se esperaba %+v", got, want)
	}
}
