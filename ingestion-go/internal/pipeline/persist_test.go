package pipeline

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gamestats/ingestion/internal/client"
	"gamestats/ingestion/internal/model"
	"gamestats/ingestion/internal/persist"
	"gamestats/ingestion/internal/state"
)

// startPersistent crea store + dispatcher con persistencia en dataDir, como
// lo hace main al arrancar.
func startPersistent(t *testing.T, dataDir string, timeouts IdleTimeouts) (*state.MatchStore, *Dispatcher, *persist.Journal, Restored) {
	t.Helper()
	j, err := persist.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	store := state.NewMatchStore()
	d, r, err := NewPersistent(store, timeouts, j)
	if err != nil {
		t.Fatal(err)
	}
	return store, d, j, r
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// waitGone espera hasta `limit` a que el archivo deje de existir.
func waitGone(path string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for fileExists(path) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	return true
}

// writeLines escribe a mano un archivo de partida (líneas JSON Lines).
func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func line(t *testing.T, receivedAt time.Time, e model.Event) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"receivedAt": receivedAt, "event": e})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Reinicio a mitad de partida: la partida queda en disco al apagar, vuelve al
// arrancar (en el store, con sus jugadores y en_curso), acepta los eventos
// que faltan y sale COMPLETA hacia Scala. Al enviarse, su archivo se borra.
func TestRestartMidMatch(t *testing.T) {
	dir := t.TempDir()

	// Primera ejecución: la partida empieza y se apaga el servidor.
	_, d1, j, _ := startPersistent(t, dir, Uniform(time.Hour))
	for _, e := range []model.Event{
		ev("e1", "M", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""),
		ev("e2", "M", "2026-10-05T14:00:01Z", "PLAYER_JOINED", "p1"),
		ev("e3", "M", "2026-10-05T14:00:02Z", "PLAYER_JOINED", "p2"),
		ev("e4", "M", "2026-10-05T14:00:30Z", "PLAYER_LEFT", "p2"),
	} {
		if err := d1.Submit(e); err != nil {
			t.Fatalf("Submit(%s) = %v", e.EventID, err)
		}
	}
	// Un evento rechazado no se guarda.
	if err := d1.Submit(ev("x1", "X", "2026-10-05T14:00:00Z", "PLAYER_JOINED", "p1")); !errors.Is(err, ErrUnknownMatch) {
		t.Fatalf("Submit(x1) = %v", err)
	}
	if batches := collectBatches(t, d1); len(batches) != 0 {
		t.Fatalf("lotes al apagar = %d, se esperaban 0", len(batches))
	}
	if !fileExists(j.Path("M")) {
		t.Fatal("el apagado ordenado no debe borrar la partida en curso")
	}
	if fileExists(j.Path("X")) {
		t.Fatal("se guardó un evento rechazado")
	}

	// Segunda ejecución: se restaura.
	sent := make(chan model.Batch, 1)
	scala := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b model.Batch
		json.NewDecoder(r.Body).Decode(&b)
		sent <- b
	}))
	defer scala.Close()

	store, d2, _, r := startPersistent(t, dir, Uniform(time.Hour))
	if want := (Restored{InProgress: 1, Events: 4}); r != want {
		t.Fatalf("Restored = %+v, se esperaba %+v", r, want)
	}
	if got := store.List(); len(got) != 1 || got[0].Status != state.StatusInProgress ||
		got[0].EventCount != 4 || !reflect.DeepEqual(got[0].ConnectedPlayers, []string{"p1"}) {
		t.Fatalf("store restaurado = %+v", got)
	}
	if got := store.Games(); !reflect.DeepEqual(got, []state.GameInfo{{GameID: "racing", InProgress: 1}}) {
		t.Fatalf("Games = %+v", got)
	}

	done := make(chan struct{})
	go func() {
		RunSenders(1, d2.Out(), client.New(scala.URL), store, time.Hour)
		close(done)
	}()

	if err := d2.Submit(ev("e5", "M", "2026-10-05T14:01:00Z", "PLAYER_JOINED", "p3")); err != nil {
		t.Fatalf("evento tras el reinicio = %v", err)
	}
	if err := d2.Submit(ev("e6", "M", "2026-10-05T14:05:00Z", "MATCH_FINISHED", "")); err != nil {
		t.Fatalf("MATCH_FINISHED tras el reinicio = %v", err)
	}

	b := <-sent
	if got := eventIDs(b.Events); !reflect.DeepEqual(got, []string{"e1", "e2", "e3", "e4", "e5", "e6"}) {
		t.Fatalf("lote enviado = %v, se esperaba la partida completa", got)
	}
	if b.StartedAt != "2026-10-05T14:00:00Z" || !reflect.DeepEqual(b.Players, []string{"p1", "p2", "p3"}) {
		t.Fatalf("lote = %+v", b)
	}
	waitStatus(t, store, "M", state.StatusSent)
	d2.Close()
	<-done
	if !waitGone(persistPath(t, dir, "M"), 2*time.Second) {
		t.Fatal("el archivo de una partida enviada debe borrarse")
	}
}

func persistPath(t *testing.T, dir, matchID string) string {
	t.Helper()
	j, err := persist.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return j.Path(matchID)
}

// Una línea corrupta se salta con un aviso y el resto de la partida se
// restaura igual.
func TestRestoreSkipsCorruptLine(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeLines(t, persistPath(t, dir, "M"),
		line(t, now, ev("e1", "M", "2026-10-05T14:00:00Z", "MATCH_STARTED", "")),
		`{"receivedAt":"2026-11-19T10:00:00Z","event":{"eventId":`,
		line(t, now, ev("e3", "M", "2026-10-05T14:00:02Z", "PLAYER_JOINED", "p1")),
	)

	store, d, _, r := startPersistent(t, dir, Uniform(time.Hour))
	if want := (Restored{InProgress: 1, Events: 2, Skipped: 1}); r != want {
		t.Fatalf("Restored = %+v, se esperaba %+v", r, want)
	}
	if got := store.List(); len(got) != 1 || got[0].EventCount != 2 {
		t.Fatalf("store = %+v", got)
	}
	d.Submit(ev("e4", "M", "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))
	batches := collectBatches(t, d)
	if len(batches) != 1 || !reflect.DeepEqual(eventIDs(batches[0].Events), []string{"e1", "e3", "e4"}) {
		t.Fatalf("lotes = %+v", batches)
	}
}

// Si lo que queda de un archivo no empieza con MATCH_STARTED (por ejemplo,
// porque esa línea se corrompió), la partida no se restaura: se aplica la
// misma regla que a un evento nuevo, y el archivo se aparta.
func TestRestoreWithoutMatchStarted(t *testing.T) {
	dir := t.TempDir()
	path := persistPath(t, dir, "M")
	writeLines(t, path,
		`basura`,
		line(t, time.Now(), ev("e2", "M", "2026-10-05T14:00:01Z", "PLAYER_JOINED", "p1")),
	)

	store, d, _, r := startPersistent(t, dir, Uniform(time.Hour))
	if want := (Restored{Skipped: 2, Discarded: 1}); r != want {
		t.Fatalf("Restored = %+v, se esperaba %+v", r, want)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("store = %+v, no debía restaurarse", got)
	}
	if fileExists(path) || !fileExists(path+".descartado") {
		t.Fatal("el archivo debía apartarse como .descartado")
	}
	if err := d.Submit(ev("e3", "M", "2026-10-05T14:00:02Z", "PLAYER_JOINED", "p2")); !errors.Is(err, ErrUnknownMatch) {
		t.Fatalf("Submit = %v, se esperaba ErrUnknownMatch", err)
	}
	collectBatches(t, d)
}

// El timeout de abandono cuenta desde la hora del último evento guardado: una
// partida que quedó sin eventos más que el timeout de su juego (mientras el
// servidor estaba apagado) se abandona enseguida y su archivo se borra; la de
// otro juego, más paciente, sigue en curso.
func TestRestoredIdleMatchIsAbandoned(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-10 * time.Minute)
	tetris := persistPath(t, dir, "T")
	writeLines(t, tetris, line(t, old, evGame("t1", "tetris", "T", "2026-10-05T14:00:00Z", "MATCH_STARTED", "")))
	writeLines(t, persistPath(t, dir, "C"), line(t, old, evGame("c1", "connect4", "C", "2026-10-05T14:00:00Z", "MATCH_STARTED", "")))

	store, d, _, r := startPersistent(t, dir, IdleTimeouts{
		Default: time.Hour,
		PerGame: map[string]time.Duration{"tetris": 50 * time.Millisecond, "connect4": time.Hour},
	})
	if r.InProgress != 2 {
		t.Fatalf("Restored = %+v", r)
	}
	waitStatus(t, store, "T", state.StatusAbandoned)
	// El estado cambia a abandonada justo antes de borrar el archivo: en una
	// máquina lenta (el CI) la prueba puede mirar entre las dos cosas, así que
	// se espera un poco a que el archivo desaparezca.
	if !waitGone(tetris, 2*time.Second) {
		t.Fatal("el archivo de una partida abandonada debe borrarse")
	}
	if status, _ := store.Status("C"); status != state.StatusInProgress {
		t.Fatalf("connect4: %q, se esperaba en_curso", status)
	}
	if err := d.Submit(evGame("t2", "tetris", "T", "2026-10-05T14:05:00Z", "MATCH_FINISHED", "")); !errors.Is(err, ErrMatchAbandoned) {
		t.Fatalf("Submit = %v, se esperaba ErrMatchAbandoned", err)
	}
	collectBatches(t, d)
}

// Si el servidor se cayó después de guardar MATCH_FINISHED pero antes de
// enviar el lote, al arrancar la partida sale hacia Scala. Si Scala la
// rechaza (4xx), queda error_envio y el archivo también se borra.
func TestRestoredFinishedMatchIsSent(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	path := persistPath(t, dir, "M")
	writeLines(t, path,
		line(t, now, ev("e1", "M", "2026-10-05T14:00:00Z", "MATCH_STARTED", "")),
		line(t, now, ev("e2", "M", "2026-10-05T14:05:00Z", "MATCH_FINISHED", "")),
	)
	scala := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer scala.Close()

	store, d, _, r := startPersistent(t, dir, Uniform(time.Hour))
	if want := (Restored{Finished: 1, Events: 2}); r != want {
		t.Fatalf("Restored = %+v, se esperaba %+v", r, want)
	}
	done := make(chan struct{})
	go func() {
		RunSenders(1, d.Out(), client.New(scala.URL), store, time.Hour)
		close(done)
	}()
	waitStatus(t, store, "M", state.StatusSendError)
	d.Close()
	<-done
	if !waitGone(path, 2*time.Second) {
		t.Fatal("el archivo de una partida con error_envio debe borrarse")
	}
}

// Scala caído cuando termina una partida: queda reintentando y su archivo NO
// se borra. Si Go se apaga antes de que Scala vuelva, al arrancar otra vez la
// partida se restaura y se envía; recién ahí el archivo se borra.
func TestRetryingMatchSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	path := persistPath(t, dir, "M")

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()

	store, d, _, _ := startPersistent(t, dir, Uniform(time.Hour))
	done := make(chan struct{})
	go func() {
		RunSenders(1, d.Out(), fastClient(down.URL), store, time.Hour)
		close(done)
	}()
	d.Submit(ev("e1", "M", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
	d.Submit(ev("e2", "M", "2026-10-05T14:05:00Z", "MATCH_FINISHED", ""))
	waitStatus(t, store, "M", state.StatusRetrying)
	d.Close()
	<-done
	if !fileExists(path) {
		t.Fatal("el archivo de una partida en reintentando no debe borrarse")
	}

	// Segunda ejecución, con Scala de vuelta.
	sent := make(chan model.Batch, 1)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b model.Batch
		json.NewDecoder(r.Body).Decode(&b)
		sent <- b
	}))
	defer up.Close()

	store2, d2, _, r := startPersistent(t, dir, Uniform(time.Hour))
	if want := (Restored{Finished: 1, Events: 2}); r != want {
		t.Fatalf("Restored = %+v, se esperaba %+v", r, want)
	}
	done2 := make(chan struct{})
	go func() {
		RunSenders(1, d2.Out(), fastClient(up.URL), store2, time.Hour)
		close(done2)
	}()
	if b := <-sent; !reflect.DeepEqual(eventIDs(b.Events), []string{"e1", "e2"}) {
		t.Fatalf("lote reenviado = %v", eventIDs(b.Events))
	}
	waitStatus(t, store2, "M", state.StatusSent)
	d2.Close()
	<-done2
	if !waitGone(path, 2*time.Second) {
		t.Fatal("el archivo debe borrarse cuando la partida por fin llega a Scala")
	}
}

// Si el evento no se puede guardar, no se acepta (y no entra al store).
func TestPersistFailureRejectsEvent(t *testing.T) {
	dir := t.TempDir()
	store, d, j, _ := startPersistent(t, dir, Uniform(time.Hour))
	// Una carpeta con el nombre del archivo hace fallar la escritura.
	if err := os.Mkdir(j.Path("M"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := d.Submit(ev("e1", "M", "2026-10-05T14:00:00Z", "MATCH_STARTED", ""))
	if !errors.Is(err, ErrPersist) || !strings.HasPrefix(err.Error(), "no se pudo guardar el evento en disco") {
		t.Fatalf("Submit = %v, se esperaba ErrPersist", err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("store = %+v", got)
	}
	collectBatches(t, d)
}
