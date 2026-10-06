// Package config lee la configuración del servidor desde variables de entorno.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config es la configuración del servidor de ingestión.
type Config struct {
	Port        string        // PORT: puerto HTTP
	ScalaURL    string        // SCALA_URL: dirección del servicio de Scala
	Workers     int           // WORKERS: cuántos senders envían a Scala
	IdleTimeout time.Duration // IDLE_TIMEOUT: inactividad para abandonar una partida
}

// Load lee las variables de entorno. Si una viene vacía, usa su valor por
// defecto. Si trae un valor inválido, devuelve un error: es mejor que el
// servidor no arranque a que arranque con una configuración que nadie pidió.
func Load() (Config, error) {
	cfg := Config{
		Port:     getEnv("PORT", "8080"),
		ScalaURL: getEnv("SCALA_URL", "http://localhost:8081"),
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

	return cfg, nil
}

// getEnv devuelve la variable de entorno key, o def si está vacía.
func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
