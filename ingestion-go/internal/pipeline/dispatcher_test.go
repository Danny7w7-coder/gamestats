package pipeline

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"gamestats/ingestion/internal/model"
	"gamestats/ingestion/internal/state"
)

func ev(id, matchID, ts, typ, player string) model.Event {
	return model.Event{
		EventID: id, Timestamp: ts, GameID: "racing", GameVersion: "1.0",
		MatchID: matchID, PlayerID: player, Type: typ,
	}
}

// collectBatches lee de Out() hasta que se cierra. Si tarda demasiado, el
// test falla (señal de una goroutine colgada).
func collectBatches(t *testing.T, d *Dispatcher) []model.Batch {
	t.Helper()
	done := make(chan []model.Batch)
	go func() {
		var batches []model.Batch
		for b := range d.Out() {
			batches = append(batches, b)
		}
		done <- batches
	}()

	d.Close()
	select {
	case batches := <-done:
		return batches
	case <-time.After(2 * time.Second):
		t.Fatal("Out() no se cerró: hay una goroutine colgada")
		return nil
	}
}

func eventIDs(events []model.Event) []string {
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.EventID
	}
	return ids
}

// Dos partidas mezcladas, con eventos fuera de orden: deben salir dos lotes
// separados y cada uno ordenado por timestamp.
func TestTwoInterleavedMatches(t *testing.T) {
	d := New(state.NewMatchStore(), Uniform(time.Hour))

	arrivals := []model.Event{
		ev("a1", "A", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""),
		ev("b1", "B", "2026-10-05T15:00:00Z", "MATCH_STARTED", ""),
		ev("a3", "A", "2026-10-05T14:00:02Z", "PLAYER_JOINED", "p2"),
		ev("b2", "B", "2026-10-05T15:00:01.500Z", "PLAYER_JOINED", "p9"),
		ev("a2", "A", "2026-10-05T14:00:01Z", "PLAYER_JOINED", "p1"), // llega tarde pero va antes
		ev("b3", "B", "2026-10-05T15:00:01Z", "PLAYER_JOINED", "p8"), // .000 < .500
		ev("b4", "B", "2026-10-05T15:05:00Z", "MATCH_FINISHED", ""),
		ev("a4", "A", "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""),
	}
	for _, e := range arrivals {
		if err := d.Submit(e); err != nil {
			t.Fatalf("Submit(%s) = %v", e.EventID, err)
		}
	}

	batches := collectBatches(t, d)
	if len(batches) != 2 {
		t.Fatalf("lotes = %d, se esperaban 2", len(batches))
	}

	byMatch := map[string]model.Batch{}
	for _, b := range batches {
		byMatch[b.MatchID] = b
	}

	a := byMatch["A"]
	if got, want := eventIDs(a.Events), []string{"a1", "a2", "a3", "a4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("orden de A = %v, se esperaba %v", got, want)
	}
	if !reflect.DeepEqual(a.Players, []string{"p1", "p2"}) ||
		a.StartedAt != "2026-10-05T14:00:00Z" || a.FinishedAt != "2026-10-05T14:05:00Z" {
		t.Errorf("lote A = %+v", a)
	}

	b := byMatch["B"]
	if got, want := eventIDs(b.Events), []string{"b1", "b3", "b2", "b4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("orden de B = %v, se esperaba %v", got, want)
	}
	if !reflect.DeepEqual(b.Players, []string{"p8", "p9"}) {
		t.Errorf("jugadores de B = %v", b.Players)
	}
}

// Un evento que llega después de MATCH_FINISHED se rechaza, no entra al lote
// ni al store, y no crea una partida nueva.
func TestLateEventIsRejected(t *testing.T) {
	store := state.NewMatchStore()
	d := New(store, Uniform(time.Hour))

	d.Submit(ev("e1", "M", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
	d.Submit(ev("e2", "M", "2026-10-05T14:00:01Z", "PLAYER_JOINED", "p1"))
	d.Submit(ev("e3", "M", "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))

	err := d.Submit(ev("late", "M", "2026-10-05T14:01:00Z", "PLAYER_LEFT", "p1"))
	if !errors.Is(err, ErrMatchFinished) {
		t.Fatalf("Submit(tardío) = %v, se esperaba ErrMatchFinished", err)
	}
	// Un segundo MATCH_FINISHED también es tardío.
	if err := d.Submit(ev("e4", "M", "2026-10-05T14:06:00Z", "MATCH_FINISHED", "")); !errors.Is(err, ErrMatchFinished) {
		t.Fatalf("segundo MATCH_FINISHED = %v, se esperaba ErrMatchFinished", err)
	}

	m := store.List()[0]
	if m.EventCount != 3 || m.Status != state.StatusFinished {
		t.Fatalf("store = %+v, se esperaban 3 eventos y estado terminada", m)
	}

	batches := collectBatches(t, d)
	if len(batches) != 1 {
		t.Fatalf("lotes = %d, se esperaba 1", len(batches))
	}
	if got := eventIDs(batches[0].Events); !reflect.DeepEqual(got, []string{"e1", "e2", "e3"}) {
		t.Fatalf("eventos del lote = %v", got)
	}
}

// Con el mismo timestamp, se respeta el orden de llegada (sort.SliceStable).
func TestEqualTimestampsKeepArrivalOrder(t *testing.T) {
	d := New(state.NewMatchStore(), Uniform(time.Hour))
	d.Submit(ev("e1", "M", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
	d.Submit(ev("x", "M", "2026-10-05T14:02:20Z", "PLAYER_JOINED", "p3"))
	d.Submit(ev("y", "M", "2026-10-05T14:02:20Z", "PLAYER_JOINED", "p2"))
	d.Submit(ev("z", "M", "2026-10-05T14:02:20.000Z", "PLAYER_JOINED", "p1"))
	d.Submit(ev("e2", "M", "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))

	batches := collectBatches(t, d)
	if got, want := eventIDs(batches[0].Events), []string{"e1", "x", "y", "z", "e2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("orden = %v, se esperaba %v", got, want)
	}
}

// Sin MATCH_STARTED, startedAt es el timestamp del primer evento.
func TestStartedAtWithoutMatchStarted(t *testing.T) {
	d := New(state.NewMatchStore(), Uniform(time.Hour))
	d.Submit(ev("e2", "M", "2026-10-05T14:00:05Z", "PLAYER_JOINED", "p1"))
	d.Submit(ev("e1", "M", "2026-10-05T14:00:03Z", "PLAYER_JOINED", "p2"))
	d.Submit(ev("e3", "M", "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))

	b := collectBatches(t, d)[0]
	if b.StartedAt != "2026-10-05T14:00:03Z" {
		t.Fatalf("startedAt = %s", b.StartedAt)
	}
}

// Al cerrar, una partida sin MATCH_FINISHED no produce lote, su goroutine
// termina y Out() se cierra (collectBatches falla si algo queda colgado).
func TestCloseWithUnfinishedMatch(t *testing.T) {
	d := New(state.NewMatchStore(), Uniform(time.Hour))
	d.Submit(ev("e1", "M", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
	d.Submit(ev("e2", "M", "2026-10-05T14:00:01Z", "PLAYER_JOINED", "p1"))

	if batches := collectBatches(t, d); len(batches) != 0 {
		t.Fatalf("lotes = %d, se esperaban 0", len(batches))
	}
}

// Muchas goroutines (como muchos handlers HTTP) envían eventos a la vez.
// Con -race, este test detecta accesos sin sincronizar.
func TestConcurrentSubmit(t *testing.T) {
	store := state.NewMatchStore()
	d := New(store, Uniform(time.Hour))
	const matches, players = 20, 10

	var wg sync.WaitGroup
	for m := 0; m < matches; m++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			d.Submit(ev(id+"-s", id, "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
			for p := 0; p < players; p++ {
				d.Submit(ev(fmt.Sprintf("%s-%d", id, p), id, "2026-10-05T14:01:00Z", "PLAYER_JOINED", fmt.Sprintf("p%d", p)))
			}
			d.Submit(ev(id+"-f", id, "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))
		}(fmt.Sprintf("m%02d", m))
	}

	// Mientras tanto, alguien consulta el store (como GET /matches).
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				store.List()
			}
		}
	}()

	wg.Wait()
	close(stop)

	batches := collectBatches(t, d)
	if len(batches) != matches {
		t.Fatalf("lotes = %d, se esperaban %d", len(batches), matches)
	}
	for _, b := range batches {
		if len(b.Events) != players+2 || len(b.Players) != players {
			t.Fatalf("lote %s: %d eventos, %d jugadores", b.MatchID, len(b.Events), len(b.Players))
		}
	}
}

// waitStatus espera (hasta 2 s) a que la partida tenga el estado want.
func waitStatus(t *testing.T, store *state.MatchStore, matchID, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if status, _ := store.Status(matchID); status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	status, _ := store.Status(matchID)
	t.Fatalf("estado de %s = %q, se esperaba %q", matchID, status, want)
}

// Una partida sin eventos durante IDLE_TIMEOUT queda abandonada: no produce
// lote, su goroutine termina y un evento posterior se rechaza.
func TestIdleMatchIsAbandoned(t *testing.T) {
	store := state.NewMatchStore()
	d := New(store, Uniform(50*time.Millisecond))

	d.Submit(ev("e1", "M", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
	d.Submit(ev("e2", "M", "2026-10-05T14:00:01Z", "PLAYER_JOINED", "p1"))

	waitStatus(t, store, "M", state.StatusAbandoned)

	err := d.Submit(ev("e3", "M", "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))
	if !errors.Is(err, ErrMatchAbandoned) {
		t.Fatalf("Submit tras abandono = %v, se esperaba ErrMatchAbandoned", err)
	}
	if batches := collectBatches(t, d); len(batches) != 0 {
		t.Fatalf("lotes = %d, se esperaban 0 (una partida abandonada no se envía)", len(batches))
	}
}

// Una partida que recibe eventos seguido no se abandona, aunque en total
// dure más que IDLE_TIMEOUT: lo que cuenta es el tiempo SIN eventos.
func TestActiveMatchIsNotAbandoned(t *testing.T) {
	store := state.NewMatchStore()
	d := New(store, Uniform(200*time.Millisecond))

	d.Submit(ev("e0", "M", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
	for i := 1; i <= 15; i++ { // 15 × 20 ms = 300 ms en total, más que el timeout
		time.Sleep(20 * time.Millisecond)
		ts := fmt.Sprintf("2026-10-05T14:00:%02dZ", i)
		if err := d.Submit(ev(fmt.Sprintf("e%d", i), "M", ts, "PLAYER_JOINED", fmt.Sprintf("p%d", i))); err != nil {
			t.Fatalf("evento %d rechazado: %v", i, err)
		}
	}
	d.Submit(ev("end", "M", "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))

	if status, _ := store.Status("M"); status != state.StatusFinished {
		t.Fatalf("estado = %q, se esperaba terminada", status)
	}
	if batches := collectBatches(t, d); len(batches) != 1 {
		t.Fatalf("lotes = %d, se esperaba 1", len(batches))
	}
}

// evGame es como ev, pero de un juego dado.
func evGame(id, game, matchID, ts, typ, player string) model.Event {
	e := ev(id, matchID, ts, typ, player)
	e.GameID = game
	return e
}

// Cada juego tiene su propio timeout: con el mismo tiempo sin eventos, la
// partida del juego impaciente se abandona y la del paciente sigue en curso,
// igual que la de un juego sin timeout propio (que usa el valor por defecto).
func TestIdleTimeoutPerGame(t *testing.T) {
	store := state.NewMatchStore()
	d := New(store, IdleTimeouts{
		Default: time.Hour,
		PerGame: map[string]time.Duration{"tetris": 50 * time.Millisecond, "connect4": 2 * time.Hour},
	})

	d.Submit(evGame("t1", "tetris", "T", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
	d.Submit(evGame("c1", "connect4", "C", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
	d.Submit(evGame("r1", "racing", "R", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))

	waitStatus(t, store, "T", state.StatusAbandoned)
	// Se deja pasar bastante más que el timeout de tetris (3 veces) para
	// confirmar que las otras dos NO se abandonan.
	time.Sleep(150 * time.Millisecond)
	for _, m := range []string{"C", "R"} {
		if status, _ := store.Status(m); status != state.StatusInProgress {
			t.Fatalf("partida %s: estado %q, se esperaba en_curso", m, status)
		}
	}

	if err := d.Submit(evGame("t2", "tetris", "T", "2026-10-05T14:05:00Z", "MATCH_FINISHED", "")); !errors.Is(err, ErrMatchAbandoned) {
		t.Fatalf("Submit tras abandono = %v, se esperaba ErrMatchAbandoned", err)
	}
	// Las que siguen vivas aceptan eventos y terminan con su lote.
	d.Submit(evGame("c2", "connect4", "C", "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))
	d.Submit(evGame("r2", "racing", "R", "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))
	if batches := collectBatches(t, d); len(batches) != 2 {
		t.Fatalf("lotes = %d, se esperaban 2 (connect4 y racing)", len(batches))
	}
}

// Un juego sin timeout propio usa el valor por defecto, aunque otro juego
// tenga uno mucho más largo.
func TestIdleTimeoutDefaultForUnlistedGame(t *testing.T) {
	store := state.NewMatchStore()
	d := New(store, IdleTimeouts{
		Default: 50 * time.Millisecond,
		PerGame: map[string]time.Duration{"connect4": time.Hour},
	})

	d.Submit(evGame("x1", "chess", "X", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
	d.Submit(evGame("c1", "connect4", "C", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))

	waitStatus(t, store, "X", state.StatusAbandoned)
	if status, _ := store.Status("C"); status != state.StatusInProgress {
		t.Fatalf("connect4: estado %q, se esperaba en_curso", status)
	}
	if batches := collectBatches(t, d); len(batches) != 0 {
		t.Fatalf("lotes = %d, se esperaban 0", len(batches))
	}
}

// El timeout que cuenta es el del juego de la partida: un juego largo no se
// abandona cuando se cumple el timeout (corto) de otro, aunque el reloj del
// router corra al ritmo del más corto. Y una partida con eventos seguidos no
// se abandona aunque dure más que el timeout de su juego.
func TestIdleTimeoutLongGameKeepsAliveWithEvents(t *testing.T) {
	store := state.NewMatchStore()
	d := New(store, IdleTimeouts{
		Default: time.Hour,
		PerGame: map[string]time.Duration{"tetris": 100 * time.Millisecond},
	})

	d.Submit(evGame("t0", "tetris", "T", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
	for i := 1; i <= 15; i++ { // 15 × 20 ms = 300 ms, más que el timeout de tetris
		time.Sleep(20 * time.Millisecond)
		ts := fmt.Sprintf("2026-10-05T14:00:%02dZ", i)
		if err := d.Submit(evGame(fmt.Sprintf("t%d", i), "tetris", "T", ts, "PLAYER_JOINED", "p1")); err != nil {
			t.Fatalf("evento %d rechazado: %v", i, err)
		}
	}
	d.Submit(evGame("tend", "tetris", "T", "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))

	if status, _ := store.Status("T"); status != state.StatusFinished {
		t.Fatalf("estado = %q, se esperaba terminada", status)
	}
	if batches := collectBatches(t, d); len(batches) != 1 {
		t.Fatalf("lotes = %d, se esperaba 1", len(batches))
	}
}
