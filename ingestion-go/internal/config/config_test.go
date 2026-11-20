package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// defaultsByGame son los timeouts por juego cuando no se pide otra cosa.
func defaultsByGame() map[string]time.Duration {
	return map[string]time.Duration{
		"tetris": time.Minute, "fighting": 2 * time.Minute, "connect4": 15 * time.Minute,
	}
}

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{"PORT", "SCALA_URL", "WORKERS", "IDLE_TIMEOUT", "IDLE_TIMEOUT_BY_GAME", "DATA_DIR"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Port: "8080", ScalaURL: "http://localhost:8081", Workers: 4,
		IdleTimeout: 2 * time.Minute, GameIdleTimeouts: defaultsByGame()}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("Load() = %+v, se esperaba %+v", cfg, want)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("SCALA_URL", "http://analytics:8081")
	t.Setenv("WORKERS", "8")
	t.Setenv("IDLE_TIMEOUT", "30s")
	t.Setenv("IDLE_TIMEOUT_BY_GAME", "")
	t.Setenv("DATA_DIR", " /app/data ")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// IDLE_TIMEOUT solo cambia "el resto": los juegos con valor propio no se mueven.
	want := Config{Port: "9090", ScalaURL: "http://analytics:8081", Workers: 8,
		IdleTimeout: 30 * time.Second, GameIdleTimeouts: defaultsByGame(), DataDir: "/app/data"}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("Load() = %+v, se esperaba %+v", cfg, want)
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := []struct{ key, value string }{
		{"WORKERS", "abc"},
		{"WORKERS", "0"},
		{"IDLE_TIMEOUT", "dos minutos"},
		{"IDLE_TIMEOUT", "-1m"},
		{"IDLE_TIMEOUT_BY_GAME", "tetris"},              // sin '='
		{"IDLE_TIMEOUT_BY_GAME", "tetris="},             // sin duración
		{"IDLE_TIMEOUT_BY_GAME", "=1m"},                 // sin juego
		{"IDLE_TIMEOUT_BY_GAME", "tetris=un minuto"},    // duración ilegible
		{"IDLE_TIMEOUT_BY_GAME", "tetris=-1m"},          // negativa
		{"IDLE_TIMEOUT_BY_GAME", "tetris=0s"},           // cero
		{"IDLE_TIMEOUT_BY_GAME", "Tetris=1m"},           // gameId con mayúscula
		{"IDLE_TIMEOUT_BY_GAME", "my_game=1m"},          // gameId con guion bajo
		{"IDLE_TIMEOUT_BY_GAME", "tetris=1m,tetris=2m"}, // repetido
		{"IDLE_TIMEOUT_BY_GAME", "tetris=1m,"},          // coma de más
		{"IDLE_TIMEOUT_BY_GAME", "tetris=1m,,chess=2m"},
		{"IDLE_TIMEOUT_BY_GAME", "tetris=1m=2m"},
	}
	for _, tt := range tests {
		t.Run(tt.key+"="+tt.value, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			if _, err := Load(); err == nil {
				t.Fatal("se esperaba error")
			}
		})
	}
}

// Los timeouts por juego salen de los valores por defecto, reemplazados o
// ampliados por IDLE_TIMEOUT_BY_GAME. Se toleran espacios alrededor.
func TestLoadIdleTimeoutByGame(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want map[string]time.Duration
	}{
		{"vacío: solo los valores por defecto", "", defaultsByGame()},
		{"solo espacios: igual que vacío", "   ", defaultsByGame()},
		{"reemplaza uno conocido", "tetris=30s", map[string]time.Duration{
			"tetris": 30 * time.Second, "fighting": 2 * time.Minute, "connect4": 15 * time.Minute}},
		{"agrega un juego desconocido", "chess=5m", map[string]time.Duration{
			"tetris": time.Minute, "fighting": 2 * time.Minute, "connect4": 15 * time.Minute, "chess": 5 * time.Minute}},
		{"varios, con espacios y gameId con guion", " tetris = 45s , my-game-2=1h30m ,racing=10s", map[string]time.Duration{
			"tetris": 45 * time.Second, "fighting": 2 * time.Minute, "connect4": 15 * time.Minute,
			"my-game-2": 90 * time.Minute, "racing": 10 * time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("IDLE_TIMEOUT", "")
			t.Setenv("IDLE_TIMEOUT_BY_GAME", tt.env)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(cfg.GameIdleTimeouts, tt.want) {
				t.Fatalf("GameIdleTimeouts = %v, se esperaba %v", cfg.GameIdleTimeouts, tt.want)
			}
			if cfg.IdleTimeout != 2*time.Minute {
				t.Fatalf("IdleTimeout = %v, se esperaba 2m (el resto)", cfg.IdleTimeout)
			}
		})
	}
}

// Una variable con formato inválido hace fallar Load con un mensaje que
// nombra la variable y la entrada que falló.
func TestLoadIdleTimeoutByGameErrorMessages(t *testing.T) {
	tests := []struct{ env, contains string }{
		{"tetris", `la entrada "tetris" no tiene '='`},
		{"tetris=abc", `la duración "abc" de "tetris"`},
		{"tetris=-5s", `la duración "-5s" de "tetris"`},
		{"Tetris=1m", `gameId inválido "Tetris"`},
		{"tetris=1m,tetris=2m", `el juego "tetris" aparece más de una vez`},
		{"tetris=1m,", "entrada vacía"},
	}
	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			t.Setenv("IDLE_TIMEOUT_BY_GAME", tt.env)
			_, err := Load()
			if err == nil {
				t.Fatal("se esperaba error")
			}
			if !strings.Contains(err.Error(), "IDLE_TIMEOUT_BY_GAME") || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("error = %q, se esperaba que nombrara IDLE_TIMEOUT_BY_GAME y contuviera %q", err, tt.contains)
			}
		})
	}
}

// IDLE_TIMEOUT cambia el valor de "el resto" sin tocar los juegos con valor
// propio, y IDLE_TIMEOUT_BY_GAME puede mover cualquiera de ellos.
func TestIdleTimeoutIsOnlyForTheRest(t *testing.T) {
	t.Setenv("IDLE_TIMEOUT", "10s")
	t.Setenv("IDLE_TIMEOUT_BY_GAME", "fighting=7s")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]time.Duration{"tetris": time.Minute, "fighting": 7 * time.Second, "connect4": 15 * time.Minute}
	if cfg.IdleTimeout != 10*time.Second || !reflect.DeepEqual(cfg.GameIdleTimeouts, want) {
		t.Fatalf("cfg = %+v", cfg)
	}
}
