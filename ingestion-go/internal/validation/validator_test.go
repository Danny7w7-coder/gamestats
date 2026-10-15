package validation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gamestats/ingestion/internal/model"
)

// validEvent devuelve un evento válido de racing. Cada caso de la tabla lo
// modifica para probar una regla.
func validEvent() model.Event {
	return model.Event{
		EventID:     "race-001-e005",
		Timestamp:   "2026-10-05T14:01:22Z",
		GameID:      "racing",
		GameVersion: "1.0",
		MatchID:     "race-001",
		PlayerID:    "p1",
		Type:        "PLAYER_ACTION",
		Action:      "LAP_COMPLETED",
		Data:        json.RawMessage(`{"lap":1,"lapTimeMs":82000,"position":1}`),
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(e *model.Event)
		wantErr string // "" significa que el evento debe ser válido
	}{
		// Regla 1: campos obligatorios.
		{"r1 válido: todos los campos", func(e *model.Event) {}, ""},
		{"r1 inválido: sin eventId", func(e *model.Event) { e.EventID = "" }, "falta el campo eventId"},
		{"r1 inválido: sin timestamp", func(e *model.Event) { e.Timestamp = "" }, "falta el campo timestamp"},
		{"r1 inválido: sin gameId", func(e *model.Event) { e.GameID = "" }, "falta el campo gameId"},
		{"r1 inválido: sin gameVersion", func(e *model.Event) { e.GameVersion = "" }, "falta el campo gameVersion"},
		{"r1 inválido: sin matchId", func(e *model.Event) { e.MatchID = "" }, "falta el campo matchId"},
		{"r1 inválido: sin type", func(e *model.Event) { e.Type = "" }, "falta el campo type"},

		// Regla 2: timestamp RFC 3339.
		{"r2 válido: con milisegundos", func(e *model.Event) { e.Timestamp = "2026-10-05T15:01:22.500Z" }, ""},
		{"r2 válido: con zona horaria", func(e *model.Event) { e.Timestamp = "2026-10-05T08:01:22-06:00" }, ""},
		{"r2 inválido: texto libre", func(e *model.Event) { e.Timestamp = "ayer a las 2" }, "RFC 3339"},
		{"r2 inválido: sin zona horaria", func(e *model.Event) { e.Timestamp = "2026-10-05T14:01:22" }, "RFC 3339"},

		// Regla 3: gameId en el catálogo.
		{"r3 válido: connect4", func(e *model.Event) { e.GameID = "connect4"; e.Action = "DISC_DROPPED" }, ""},
		{"r3 válido: fighting", func(e *model.Event) { e.GameID = "fighting"; e.Action = "COMBO" }, ""},
		{"r3 válido: tetris", func(e *model.Event) { e.GameID = "tetris"; e.Action = "T_SPIN" }, ""},
		{"r3 inválido: juego desconocido", func(e *model.Event) { e.GameID = "chess" }, "gameId desconocido"},

		// Regla 4: type válido.
		{"r4 válido: PLAYER_JOINED", func(e *model.Event) { e.Type = "PLAYER_JOINED"; e.Action = "" }, ""},
		{"r4 válido: MATCH_PAUSED", func(e *model.Event) { e.Type = "MATCH_PAUSED"; e.Action = "" }, ""},
		{"r4 válido: MATCH_RESUMED", func(e *model.Event) { e.Type = "MATCH_RESUMED"; e.Action = "" }, ""},
		{"r4 inválido: type desconocido", func(e *model.Event) { e.Type = "PLAYER_DANCED" }, "type desconocido"},

		// Regla 5: playerId obligatorio salvo en los eventos de partida.
		{"r5 válido: MATCH_STARTED sin playerId", func(e *model.Event) { e.Type = "MATCH_STARTED"; e.Action = ""; e.PlayerID = "" }, ""},
		{"r5 válido: MATCH_FINISHED sin playerId", func(e *model.Event) { e.Type = "MATCH_FINISHED"; e.Action = ""; e.PlayerID = "" }, ""},
		{"r5 válido: MATCH_PAUSED sin playerId", func(e *model.Event) { e.Type = "MATCH_PAUSED"; e.Action = ""; e.PlayerID = "" }, ""},
		{"r5 válido: MATCH_RESUMED sin playerId", func(e *model.Event) { e.Type = "MATCH_RESUMED"; e.Action = ""; e.PlayerID = "" }, ""},
		{"r5 inválido: PLAYER_ACTION sin playerId", func(e *model.Event) { e.PlayerID = "" }, "falta el campo playerId"},
		{"r5 inválido: PLAYER_LEFT sin playerId", func(e *model.Event) { e.Type = "PLAYER_LEFT"; e.Action = ""; e.PlayerID = "" }, "falta el campo playerId"},

		// Regla 6: action del catálogo de ESE juego.
		{"r6 válido: CONNECT_FOUR en connect4", func(e *model.Event) { e.GameID = "connect4"; e.Action = "CONNECT_FOUR" }, ""},
		{"r6 inválido: sin action", func(e *model.Event) { e.Action = "" }, "falta el campo action"},
		{"r6 inválido: CONNECT_FOUR en racing (es de connect4)", func(e *model.Event) { e.Action = "CONNECT_FOUR" }, `la acción "CONNECT_FOUR" no existe en el juego "racing" (es una acción de "connect4")`},
		{"r6 válido: UNDO en connect4", func(e *model.Event) { e.GameID = "connect4"; e.Action = "UNDO" }, ""},
		{"r6 válido: FATALITY en fighting", func(e *model.Event) { e.GameID = "fighting"; e.Action = "FATALITY" }, ""},
		{"r6 válido: TOP_OUT en tetris", func(e *model.Event) { e.GameID = "tetris"; e.Action = "TOP_OUT" }, ""},
		{"r6 inválido: DISC_DROPPED en fighting", func(e *model.Event) { e.GameID = "fighting"; e.Action = "DISC_DROPPED" }, `la acción "DISC_DROPPED" no existe en el juego "fighting"`},
		{"r6 inválido: LAP_COMPLETED en tetris", func(e *model.Event) { e.GameID = "tetris" }, `la acción "LAP_COMPLETED" no existe en el juego "tetris"`},
		{"r6 válido: acción que no existe en ningún juego (desconocida)", func(e *model.Event) { e.Action = "JUMP" }, ""},

		// Regla 7: action solo en PLAYER_ACTION.
		{"r7 válido: PLAYER_JOINED sin action", func(e *model.Event) { e.Type = "PLAYER_JOINED"; e.Action = "" }, ""},
		{"r7 inválido: MATCH_STARTED con action", func(e *model.Event) { e.Type = "MATCH_STARTED"; e.PlayerID = "" }, "action solo se permite en PLAYER_ACTION"},
		{"r7 inválido: MATCH_PAUSED con action", func(e *model.Event) { e.Type = "MATCH_PAUSED"; e.PlayerID = "" }, "action solo se permite en PLAYER_ACTION"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validEvent()
			tt.modify(&e)

			err := Validate(e)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("se esperaba válido, pero falló: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("se esperaba error con %q, pero fue válido", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, se esperaba que contuviera %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidateContractFixtures valida los eventos de los fixtures de Danny
// (contracts/fixtures/, solo lectura). Cada fixture es un lote: se validan sus
// "events". Si la carpeta no existe (por ejemplo, fuera del repo), se salta.
//
// Los *_invalid_events.json NO tienen rechazos en Go: sus eventos "bad-*"
// tienen data mal formada o rompen una regla de contexto, y eso lo valida
// Scala. Go solo revisa formato y catálogo.
func TestValidateContractFixtures(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "contracts", "fixtures")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("no se encontró %s: %v", dir, err)
	}

	tests := []struct {
		file        string
		wantInvalid []string // eventId que deben ser rechazados
	}{
		{"racing_match_ok.json", nil},
		{"comeback_case.json", nil},
		{"racing_full_ok.json", nil},
		{"fighting_match_ok.json", nil},
		{"fighting_comeback.json", nil},
		{"tetris_match_ok.json", nil},
		{"tetris_perfect_clear.json", nil},
		{"racing_invalid_events.json", nil},
		{"fighting_invalid_events.json", nil},
		{"tetris_invalid_events.json", nil},
		{"connect4_match_ok.json", nil},
		{"connect4_vs_bot.json", nil},
		{"connect4_draw.json", nil},
		{"connect4_invalid_events.json", nil},
		{"invalid_bad_timestamp.json", []string{"y-e02"}},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, tt.file))
			if err != nil {
				t.Fatal(err)
			}
			var batch struct {
				Events []model.Event `json:"events"`
			}
			if err := json.Unmarshal(raw, &batch); err != nil {
				t.Fatal(err)
			}

			var invalid []string
			for _, e := range batch.Events {
				if err := Validate(e); err != nil {
					invalid = append(invalid, e.EventID)
				}
			}
			if strings.Join(invalid, ",") != strings.Join(tt.wantInvalid, ",") {
				t.Fatalf("rechazados = %v, se esperaba %v", invalid, tt.wantInvalid)
			}
		})
	}
}

// UnknownAction solo marca acciones de PLAYER_ACTION que no están en el
// catálogo del juego. Las del catálogo y los eventos sin acción no son avisos.
func TestUnknownAction(t *testing.T) {
	tests := []struct {
		name   string
		modify func(e *model.Event)
		want   bool
	}{
		{"acción del catálogo", func(e *model.Event) {}, false},
		{"acción nueva", func(e *model.Event) { e.Action = "BOOST_JUMP" }, true},
		{"evento sin acción", func(e *model.Event) { e.Type = "PLAYER_JOINED"; e.Action = "" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validEvent()
			tt.modify(&e)
			if got := UnknownAction(e); got != tt.want {
				t.Fatalf("UnknownAction = %v, se esperaba %v", got, tt.want)
			}
		})
	}
}
