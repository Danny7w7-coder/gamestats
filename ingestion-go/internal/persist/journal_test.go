package persist

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gamestats/ingestion/internal/model"
)

func ev(id, matchID, typ string) model.Event {
	return model.Event{
		EventID: id, Timestamp: "2026-10-05T14:00:00Z", GameID: "racing", GameVersion: "1.0",
		MatchID: matchID, Type: typ, Data: json.RawMessage(`{"lap":1}`),
	}
}

func open(t *testing.T) *Journal {
	t.Helper()
	j, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return j
}

// Lo que se guarda con Append vuelve igual con Load, en orden de llegada y
// con la hora del último evento.
func TestAppendAndLoad(t *testing.T) {
	j := open(t)
	t0 := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	events := []model.Event{ev("a1", "a", "MATCH_STARTED"), ev("a2", "a", "PLAYER_JOINED")}
	events[1].PlayerID = "p1"
	for i, e := range events {
		if err := j.Append(e, t0.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Append(ev("b1", "b", "MATCH_STARTED"), t0); err != nil {
		t.Fatal(err)
	}

	saved, err := j.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 {
		t.Fatalf("Load = %d partidas, se esperaban 2", len(saved))
	}
	a := saved[0]
	if a.File != filepath.Join(j.Dir(), "a.jsonl") || !reflect.DeepEqual(a.Events, events) ||
		!a.LastSeen.Equal(t0.Add(time.Second)) || a.Skipped != 0 {
		t.Fatalf("partida a = %+v", a)
	}
}

// Una línea corrupta en el medio y una cortada al final se saltan; las demás
// se usan. Después, un Append nuevo no queda pegado a la línea cortada.
func TestLoadSkipsCorruptLines(t *testing.T) {
	j := open(t)
	t0 := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	j.Append(ev("e1", "m", "MATCH_STARTED"), t0)
	path := j.Path("m")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("esto no es JSON\n")
	f.Close()
	j.Append(ev("e2", "m", "MATCH_PAUSED"), t0.Add(time.Second))
	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"receivedAt":"2026-11-19T10:00:05Z","event":{"eventId":"e3","matc`) // cortada, sin \n
	f.Close()

	saved, err := j.Load()
	if err != nil {
		t.Fatal(err)
	}
	s := saved[0]
	if got := []string{s.Events[0].EventID, s.Events[1].EventID}; len(s.Events) != 2 || got[0] != "e1" || got[1] != "e2" {
		t.Fatalf("eventos = %+v", s.Events)
	}
	if s.Skipped != 2 || !s.LastSeen.Equal(t0.Add(time.Second)) {
		t.Fatalf("skipped = %d, lastSeen = %s", s.Skipped, s.LastSeen)
	}

	// Load agregó el \n que faltaba: el próximo evento queda en su propia línea.
	j.Append(ev("e4", "m", "MATCH_RESUMED"), t0.Add(2*time.Second))
	saved, _ = j.Load()
	if s := saved[0]; len(s.Events) != 3 || s.Events[2].EventID != "e4" || s.Skipped != 2 {
		t.Fatalf("tras el Append: %d eventos, %d saltados", len(s.Events), s.Skipped)
	}
}

// Remove borra el archivo; borrar uno que no existe no es un error.
func TestRemove(t *testing.T) {
	j := open(t)
	j.Append(ev("e1", "m", "MATCH_STARTED"), time.Now())
	j.Remove("m")
	if _, err := os.Stat(j.Path("m")); !os.IsNotExist(err) {
		t.Fatalf("el archivo sigue: %v", err)
	}
	j.Remove("m")
	if saved, _ := j.Load(); len(saved) != 0 {
		t.Fatalf("Load = %+v", saved)
	}
}

// Discard aparta el archivo: deja de cargarse pero no se pierde.
func TestDiscard(t *testing.T) {
	j := open(t)
	j.Append(ev("e1", "m", "MATCH_STARTED"), time.Now())
	j.Discard(j.Path("m"))
	if saved, _ := j.Load(); len(saved) != 0 {
		t.Fatalf("Load = %+v", saved)
	}
	if _, err := os.Stat(j.Path("m") + ".descartado"); err != nil {
		t.Fatal(err)
	}
}

// Un matchId que no sirve como nombre de archivo se guarda con su hash, dentro
// de la carpeta, y sigue volviendo con su matchId original.
func TestUnsafeMatchID(t *testing.T) {
	j := open(t)
	for _, id := range []string{"../../escape", "Race-01", "a/b", ".oculto", strings.Repeat("x", 200)} {
		p := j.Path(id)
		if filepath.Dir(p) != j.Dir() || !strings.HasPrefix(filepath.Base(p), "~") {
			t.Fatalf("Path(%q) = %s", id, p)
		}
		if err := j.Append(ev("e-"+id[:1], id, "MATCH_STARTED"), time.Now()); err != nil {
			t.Fatalf("Append(%q): %v", id, err)
		}
	}
	if p := j.Path("race-s42-001"); filepath.Base(p) != "race-s42-001.jsonl" {
		t.Fatalf("un matchId seguro debe usarse tal cual: %s", p)
	}
	saved, _ := j.Load()
	got := map[string]bool{}
	for _, s := range saved {
		got[s.Events[0].MatchID] = true
	}
	if len(got) != 5 || !got["../../escape"] || !got["Race-01"] {
		t.Fatalf("matchIds restaurados = %v", got)
	}
}

// Un *Journal nil es "persistencia desactivada": nada falla y nada se guarda.
func TestNilJournal(t *testing.T) {
	var j *Journal
	if err := j.Append(ev("e1", "m", "MATCH_STARTED"), time.Now()); err != nil {
		t.Fatal(err)
	}
	j.Remove("m")
	j.Discard("x")
	if saved, err := j.Load(); saved != nil || err != nil || j.Dir() != "" {
		t.Fatalf("Load = %v, %v", saved, err)
	}
}
