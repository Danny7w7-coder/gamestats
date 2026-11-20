// Package config lee la configuración del servidor desde variables de entorno.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gamestats/ingestion/internal/validation"
)

// defaultGameTimeouts son los timeouts de inactividad de los juegos conocidos
// cuando IDLE_TIMEOUT_BY_GAME no dice otra cosa: tetris es rápido, connect4
// tiene turnos largos. Los juegos que no aparecen usan IDLE_TIMEOUT.
var defaultGameTimeouts = map[string]time.Duration{
	"tetris":   1 * time.Minute,
	"fighting": 2 * time.Minute,
	"connect4": 15 * time.Minute,
}

// Config es la configuración del servidor de ingestión.
type Config struct {
	Port        string        // PORT: puerto HTTP
	ScalaURL    string        // SCALA_URL: dirección del servicio de Scala
	Workers     int           // WORKERS: cuántos senders envían a Scala
	IdleTimeout time.Duration // IDLE_TIMEOUT: inactividad para abandonar una partida de un juego sin timeout propio
	DataDir     string        // DATA_DIR: carpeta donde se guardan las partidas en curso; vacía = sin persistencia

	// GameIdleTimeouts es el timeout de cada juego con valor propio: los de
	// defaultGameTimeouts, reemplazados o ampliados por IDLE_TIMEOUT_BY_GAME.
	GameIdleTimeouts map[string]time.Duration
}

// Load lee las variables de entorno. Si una viene vacía, usa su valor por
// defecto. Si trae un valor inválido, devuelve un error: es mejor que el
// servidor no arranque a que arranque con una configuración que nadie pidió.
func Load() (Config, error) {
	cfg := Config{
		Port:     getEnv("PORT", "8080"),
		ScalaURL: getEnv("SCALA_URL", "http://localhost:8081"),
		DataDir:  strings.TrimSpace(os.Getenv("DATA_DIR")),
	}

	workers, err := strconv.Atoi(getEnv("WORKERS", "4"))
	if err != nil || workers < 1 {
		return Config{}, fmt.Errorf("WORKERS debe ser un entero mayor que 0, llegó %q", os.Getenv("WORKERS"))
	}
	cfg.Workers = workers

	idle, err := time.ParseDuration(getEnv("IDLE_TIMEOUT", "2m"))
	if err != nil || idle <= 0 {
		return Config{}, fmt.Errorf("IDLE_TIMEOUT debe ser una duración positiva (ej. 2m, 30s), llegó %q", os.Getenv("IDLE_TIMEOUT"))
	}
	cfg.IdleTimeout = idle

	byGame, err := parseGameTimeouts(os.Getenv("IDLE_TIMEOUT_BY_GAME"))
	if err != nil {
		return Config{}, err
	}
	cfg.GameIdleTimeouts = make(map[string]time.Duration, len(defaultGameTimeouts)+len(byGame))
	for game, d := range defaultGameTimeouts {
		cfg.GameIdleTimeouts[game] = d
	}
	for game, d := range byGame { // lo que pide el usuario gana sobre el valor por defecto
		cfg.GameIdleTimeouts[game] = d
	}

	return cfg, nil
}

// parseGameTimeouts lee IDLE_TIMEOUT_BY_GAME: pares juego=duración separados
// por comas, por ejemplo "tetris=30s,connect4=20m,chess=5m". Vacío significa
// "sin cambios". Cualquier error (par sin "=", gameId con formato inválido,
// duración que no es positiva, juego repetido, coma de más) se devuelve con
// un mensaje que dice qué entrada falló, para que el servidor no arranque con
// una configuración que nadie pidió.
func parseGameTimeouts(raw string) (map[string]time.Duration, error) {
	const format = `formato: juego=duración,juego=duración (ej. "tetris=30s,connect4=20m")`
	result := make(map[string]time.Duration)
	if strings.TrimSpace(raw) == "" {
		return result, nil
	}

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			return nil, fmt.Errorf("IDLE_TIMEOUT_BY_GAME tiene una entrada vacía (¿coma de más?) en %q; %s", raw, format)
		}
		game, value, found := strings.Cut(entry, "=")
		game, value = strings.TrimSpace(game), strings.TrimSpace(value)
		if !found {
			return nil, fmt.Errorf("IDLE_TIMEOUT_BY_GAME: la entrada %q no tiene '='; %s", entry, format)
		}
		if !validation.ValidGameID(game) {
			return nil, fmt.Errorf("IDLE_TIMEOUT_BY_GAME: gameId inválido %q en la entrada %q (se espera ^[a-z0-9-]{1,40}$)", game, entry)
		}
		d, err := time.ParseDuration(value)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("IDLE_TIMEOUT_BY_GAME: la duración %q de %q debe ser positiva (ej. 30s, 2m, 1h)", value, game)
		}
		if _, dup := result[game]; dup {
			return nil, fmt.Errorf("IDLE_TIMEOUT_BY_GAME: el juego %q aparece más de una vez", game)
		}
		result[game] = d
	}
	return result, nil
}

// getEnv devuelve la variable de entorno key, o def si está vacía.
func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
