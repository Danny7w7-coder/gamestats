package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{"PORT", "SCALA_URL", "WORKERS", "IDLE_TIMEOUT"} {
		t.Setenv(k, "")
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Port: "8080", ScalaURL: "http://localhost:8081", Workers: 4, IdleTimeout: 2 * time.Minute}
	if cfg != want {
		t.Fatalf("Load() = %+v, se esperaba %+v", cfg, want)
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("SCALA_URL", "http://analytics:8081")
	t.Setenv("WORKERS", "8")
	t.Setenv("IDLE_TIMEOUT", "30s")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := Config{Port: "9090", ScalaURL: "http://analytics:8081", Workers: 8, IdleTimeout: 30 * time.Second}
	if cfg != want {
		t.Fatalf("Load() = %+v, se esperaba %+v", cfg, want)
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := []struct{ key, value string }{
		{"WORKERS", "abc"},
		{"WORKERS", "0"},
		{"IDLE_TIMEOUT", "dos minutos"},
		{"IDLE_TIMEOUT", "-1m"},
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
