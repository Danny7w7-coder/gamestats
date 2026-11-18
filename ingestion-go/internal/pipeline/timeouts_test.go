package pipeline

import (
	"reflect"
	"testing"
	"time"
)

func TestIdleTimeoutsFor(t *testing.T) {
	ts := IdleTimeouts{
		Default: 2 * time.Minute,
		PerGame: map[string]time.Duration{"tetris": time.Minute, "connect4": 15 * time.Minute},
	}
	tests := []struct {
		game string
		want time.Duration
	}{
		{"tetris", time.Minute},
		{"connect4", 15 * time.Minute},
		{"racing", 2 * time.Minute},
		{"chess", 2 * time.Minute},
		{"", 2 * time.Minute},
	}
	for _, tt := range tests {
		if got := ts.For(tt.game); got != tt.want {
			t.Errorf("For(%q) = %v, se esperaba %v", tt.game, got, tt.want)
		}
	}
}

func TestUniform(t *testing.T) {
	ts := Uniform(3 * time.Second)
	if ts.For("tetris") != 3*time.Second || ts.For("x") != 3*time.Second || ts.min() != 3*time.Second {
		t.Fatalf("Uniform no es uniforme: %+v", ts)
	}
}

// min es el timeout más corto, venga de un juego o del valor por defecto.
func TestIdleTimeoutsMin(t *testing.T) {
	tests := []struct {
		name string
		ts   IdleTimeouts
		want time.Duration
	}{
		{"solo el valor por defecto", Uniform(time.Minute), time.Minute},
		{"un juego más corto", IdleTimeouts{Default: 2 * time.Minute, PerGame: map[string]time.Duration{"a": 10 * time.Second}}, 10 * time.Second},
		{"el valor por defecto es el más corto", IdleTimeouts{Default: time.Second, PerGame: map[string]time.Duration{"a": time.Hour}}, time.Second},
	}
	for _, tt := range tests {
		if got := tt.ts.min(); got != tt.want {
			t.Errorf("%s: min() = %v, se esperaba %v", tt.name, got, tt.want)
		}
	}
}

// Table lista los juegos ordenados por gameId y "(resto)" al final.
func TestIdleTimeoutsTable(t *testing.T) {
	ts := IdleTimeouts{
		Default: 2 * time.Minute,
		PerGame: map[string]time.Duration{"tetris": time.Minute, "connect4": 15 * time.Minute, "fighting": 2 * time.Minute},
	}
	want := []string{
		"connect4 15m0s",
		"fighting 2m0s",
		"tetris   1m0s",
		"(resto)  2m0s",
	}
	if got := ts.Table(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Table() = %q\nse esperaba   %q", got, want)
	}
	if got := Uniform(time.Minute).Table(); !reflect.DeepEqual(got, []string{"(resto) 1m0s"}) {
		t.Fatalf("Table() sin juegos = %q", got)
	}
}
