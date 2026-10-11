package simulator

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"gamestats/ingestion/internal/validation"
)

func TestCorrupt(t *testing.T) {
	clean := Generate(42, Counts{Racing: 3, Football: 3, Fighting: 3, Tetris: 3})
	matches := Generate(42, Counts{Racing: 3, Football: 3, Fighting: 3, Tetris: 3})
	n := Corrupt(matches, 42, 20)
	if n == 0 {
		t.Fatal("con 20 % se esperaban eventos dañados")
	}

	added := 0
	for i, m := range matches {
		var originals []string
		for _, e := range m.Events {
			if strings.HasSuffix(e.EventID, "-bad") {
				added++
				if m.Script != "" {
					t.Fatalf("se dañó el escenario guionizado %s", m.ID)
				}
				if err := validation.Validate(e); err == nil {
					t.Fatalf("%s: el evento dañado %s es válido", m.ID, e.EventID)
				}
				continue
			}
			originals = append(originals, e.EventID)
		}

		// Los originales siguen todos, en el mismo orden y sin cambios.
		var want []string
		for _, e := range clean[i].Events {
			want = append(want, e.EventID)
		}
		if !reflect.DeepEqual(originals, want) {
			t.Fatalf("%s: los eventos originales cambiaron", m.ID)
		}
	}
	if added != n {
		t.Fatalf("Corrupt devolvió %d pero hay %d eventos dañados", n, added)
	}
}

func TestCorruptDeterministic(t *testing.T) {
	a, b := Generate(42, Counts{Racing: 3, Football: 3, Fighting: 3, Tetris: 3}), Generate(42, Counts{Racing: 3, Football: 3, Fighting: 3, Tetris: 3})
	Corrupt(a, 42, 10)
	Corrupt(b, 42, 10)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("la misma semilla dañó eventos distintos")
	}
}

func TestCorruptPercentLimits(t *testing.T) {
	if n := Corrupt(Generate(42, Counts{Racing: 2, Football: 2, Fighting: 2, Tetris: 2}), 42, 0); n != 0 {
		t.Fatalf("con 0 %% se dañaron %d eventos", n)
	}

	matches := Generate(42, Counts{Racing: 2, Football: 2, Fighting: 2, Tetris: 2})
	eligible := 0
	for _, m := range matches {
		if m.Script == "" {
			eligible += len(m.Events)
		}
	}
	if n := Corrupt(matches, 42, 100); n != eligible {
		t.Fatalf("con 100 %% se dañaron %d de %d eventos", n, eligible)
	}
}

// Todas las formas de dañar producen un evento inválido, en todos los juegos y
// partiendo de cualquier tipo de evento.
func TestDamageAlwaysInvalid(t *testing.T) {
	for _, m := range Generate(1, Counts{Racing: 3, Football: 3, Fighting: 3, Tetris: 3}) {
		for _, e := range m.Events {
			for k := 0; k < 20; k++ {
				r := rand.New(rand.NewSource(int64(k)))
				if bad := damage(r, e); validation.Validate(bad) == nil {
					t.Fatalf("damage dejó válido a %s: %+v", e.EventID, bad)
				}
			}
		}
	}
}
