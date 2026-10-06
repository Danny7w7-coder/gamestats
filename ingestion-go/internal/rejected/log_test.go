package rejected

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestLogWritesOneLinePerEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "rejected.jsonl")
	l, err := New(path)
	if err != nil {
		t.Fatal(err)
	}

	// Evento con saltos de línea: debe quedar en una sola línea.
	ev := json.RawMessage("{\n  \"eventId\": \"e1\"\n}")
	if err := l.Log(ev, "falta el campo matchId"); err != nil {
		t.Fatal(err)
	}
	l.Close()

	lines := readLines(t, path)
	if len(lines) != 1 {
		t.Fatalf("líneas = %d, se esperaba 1", len(lines))
	}
	var got entry
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("la línea no es JSON válido: %v", err)
	}
	if got.Reason != "falta el campo matchId" || string(got.Event) != `{"eventId":"e1"}` || got.Time == "" {
		t.Fatalf("línea inesperada: %s", lines[0])
	}
}

// Muchas goroutines escriben a la vez: no se deben perder ni mezclar líneas.
func TestLogConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rejected.jsonl")
	l, err := New(path)
	if err != nil {
		t.Fatal(err)
	}

	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Log(json.RawMessage(`{"eventId":"x"}`), "motivo")
		}()
	}
	wg.Wait()
	l.Close()

	lines := readLines(t, path)
	if len(lines) != n {
		t.Fatalf("líneas = %d, se esperaban %d", len(lines), n)
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatalf("línea corrupta: %q", line)
		}
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}
