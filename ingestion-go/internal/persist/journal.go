// Package persist guarda en disco los eventos de las partidas en curso, para
// que un reinicio de ingestion no las pierda.
//
// Cada partida en curso tiene un archivo DATA_DIR/matches/<matchId>.jsonl
// (JSON Lines): una línea por evento aceptado, en orden de llegada, con la
// hora del servidor al recibirlo. Al arrancar, Load lee esos archivos para
// reconstruir el estado. Cuando la partida llega a un estado final (enviada,
// abandonada o error_envio), su archivo se borra con Remove.
package persist

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gamestats/ingestion/internal/model"
)

// ext es la extensión de los archivos de partida.
const ext = ".jsonl"

// discardedExt es la extensión que recibe un archivo del que no se pudo
// restaurar nada: deja de cargarse, pero queda para revisarlo a mano.
const discardedExt = ".descartado"

// safeName es el formato de un matchId que se puede usar tal cual como nombre
// de archivo en cualquier sistema: minúsculas (Windows y macOS no distinguen
// mayúsculas), dígitos, punto, guion y guion bajo, sin empezar con punto.
var safeName = regexp.MustCompile(`^[a-z0-9_-][a-z0-9._-]{0,99}$`)

// line es el formato de cada línea del archivo.
type line struct {
	ReceivedAt time.Time   `json:"receivedAt"` // hora del servidor al aceptar el evento
	Event      model.Event `json:"event"`
}

// Journal escribe y lee los archivos de las partidas en curso. Un *Journal
// nil significa "persistencia desactivada": Append y Remove no hacen nada.
//
// Lo usan el router (Append, y Remove al abandonar) y los senders (Remove al
// enviar), cada uno en su goroutine; el mutex evita que se pisen.
type Journal struct {
	mu  sync.Mutex
	dir string // DATA_DIR/matches
}

// Saved es una partida leída del disco por Load.
type Saved struct {
	File     string        // ruta del archivo
	Events   []model.Event // eventos en orden de llegada (sin las líneas corruptas)
	LastSeen time.Time     // hora del servidor del último evento válido
	Skipped  int           // líneas corruptas que se saltaron
}

// Open prepara DATA_DIR/matches (la crea si no existe).
func Open(dataDir string) (*Journal, error) {
	dir := filepath.Join(dataDir, "matches")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("no se pudo crear %s: %w", dir, err)
	}
	return &Journal{dir: dir}, nil
}

// Dir devuelve la carpeta de los archivos de partida ("" si está desactivado).
func (j *Journal) Dir() string {
	if j == nil {
		return ""
	}
	return j.dir
}

// Path devuelve el archivo de la partida matchID. Un matchId que no es seguro
// como nombre de archivo (mayúsculas, "/", "..", muy largo...) se guarda con
// el hash SHA-256 del matchId; el "~" no está en safeName, así que ese nombre
// no choca con el de ningún matchId seguro. Load no depende del nombre: el
// matchId sale de los eventos.
func (j *Journal) Path(matchID string) string {
	name := matchID
	if !safeName.MatchString(matchID) {
		sum := sha256.Sum256([]byte(matchID))
		name = "~" + hex.EncodeToString(sum[:])
	}
	return filepath.Join(j.dir, name+ext)
}

// Append agrega el evento al final del archivo de su partida. Cuando devuelve
// nil, la línea ya está escrita en el archivo (un reinicio del proceso no la
// pierde).
func (j *Journal) Append(e model.Event, receivedAt time.Time) error {
	if j == nil {
		return nil
	}
	data, err := json.Marshal(line{ReceivedAt: receivedAt.UTC(), Event: e})
	if err != nil {
		return err
	}
	data = append(data, '\n')

	j.mu.Lock()
	defer j.mu.Unlock()
	f, err := os.OpenFile(j.Path(e.MatchID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	// Una sola llamada a Write: la línea queda entera o, si el proceso muere
	// a la mitad, queda cortada y Load la salta como corrupta.
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Remove borra el archivo de la partida (si existe). Un error se avisa en el
// log y no se devuelve: la partida ya terminó y no hay nada que deshacer.
func (j *Journal) Remove(matchID string) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := os.Remove(j.Path(matchID)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("no se pudo borrar el archivo de la partida %s: %v", matchID, err)
	}
}

// Discard renombra un archivo que no sirvió para restaurar nada (por ejemplo,
// porque su MATCH_STARTED estaba corrupto) para que no se vuelva a cargar en
// el próximo arranque. No se borra: queda para revisarlo.
func (j *Journal) Discard(file string) {
	if j == nil {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := os.Rename(file, file+discardedExt); err != nil {
		log.Printf("no se pudo apartar %s: %v", file, err)
	}
}

// Load lee todos los archivos de partida, ordenados por nombre para que el
// arranque sea siempre igual. Una línea que no se puede leer (por ejemplo,
// cortada por un apagado brusco) se salta con un aviso en el log; las demás
// se usan. Si el archivo no termina en salto de línea, se le agrega uno, así
// el próximo Append no queda pegado a la línea cortada.
func (j *Journal) Load() ([]Saved, error) {
	if j == nil {
		return nil, nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()

	entries, err := os.ReadDir(j.dir)
	if err != nil {
		return nil, fmt.Errorf("no se pudo leer %s: %w", j.dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	saved := make([]Saved, 0, len(names))
	for _, name := range names {
		s, err := loadFile(filepath.Join(j.dir, name))
		if err != nil {
			return nil, err
		}
		saved = append(saved, s)
	}
	return saved, nil
}

// loadFile lee un archivo de partida.
func loadFile(path string) (Saved, error) {
	s := Saved{File: path}
	data, err := os.ReadFile(path)
	if err != nil {
		return s, fmt.Errorf("no se pudo leer %s: %w", path, err)
	}

	if len(data) > 0 && data[len(data)-1] != '\n' {
		if err := appendNewline(path); err != nil {
			return s, err
		}
	}

	for i, raw := range bytes.Split(data, []byte{'\n'}) {
		text := bytes.TrimSpace(raw)
		if len(text) == 0 {
			continue
		}
		var l line
		if err := json.Unmarshal(text, &l); err != nil || l.Event.MatchID == "" || l.ReceivedAt.IsZero() {
			s.Skipped++
			log.Printf("aviso: %s línea %d corrupta, se salta", filepath.Base(path), i+1)
			continue
		}
		s.Events = append(s.Events, l.Event)
		s.LastSeen = l.ReceivedAt
	}
	return s, nil
}

// appendNewline agrega un salto de línea al final del archivo.
func appendNewline(path string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if _, err := f.Write([]byte{'\n'}); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
