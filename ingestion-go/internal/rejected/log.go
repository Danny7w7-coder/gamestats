// Package rejected registra los eventos rechazados en un archivo JSON Lines.
package rejected

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger escribe una línea por evento rechazado.
//
// net/http atiende cada petición en su propia goroutine, así que varios
// handlers pueden llamar a Log al mismo tiempo. El mutex garantiza que solo
// uno escriba a la vez y que las líneas no se mezclen en el archivo.
type Logger struct {
	mu   sync.Mutex
	file *os.File
}

// entry es el formato de cada línea del archivo.
type entry struct {
	Time   string          `json:"time"`
	Reason string          `json:"reason"`
	Event  json.RawMessage `json:"event"`
}

// New abre (o crea) el archivo en modo "agregar al final", creando las
// carpetas que falten.
func New(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("no se pudo crear la carpeta del log: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir el log: %w", err)
	}
	return &Logger{file: f}, nil
}

// Log anota el evento original y el motivo del rechazo.
func (l *Logger) Log(event json.RawMessage, reason string) error {
	// json.Marshal compacta el RawMessage, así cada evento ocupa una sola
	// línea aunque haya llegado con saltos de línea.
	line, err := json.Marshal(entry{
		Time:   time.Now().UTC().Format(time.RFC3339),
		Reason: reason,
		Event:  event,
	})
	if err != nil {
		return err
	}
	line = append(line, '\n')

	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.file.Write(line)
	return err
}

// Close cierra el archivo.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.file.Close()
}
