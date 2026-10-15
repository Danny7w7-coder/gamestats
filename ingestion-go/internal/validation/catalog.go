// Package validation decide si un evento es válido antes de procesarlo.
package validation

// GameActions es el ÚNICO lugar donde se declaran los juegos y sus acciones.
// Para agregar un juego nuevo basta con agregar una entrada aquí: el resto del
// sistema (validación, pipeline, envío a Scala) no cambia.
//
// Las acciones salen del contrato compartido con Scala (contracts/README.md).
// Los nombres no se repiten entre juegos.
var GameActions = map[string]map[string]bool{
	// Carrera arcade por posiciones (8 acciones).
	"racing": {
		"LAP_COMPLETED": true, "POSITION_CHANGED": true, "OVERTAKE": true, "LAPPED": true,
		"COLLISION": true, "RESPAWN": true, "PENALTY": true, "DNF": true,
	},
	// Fútbol arcade retro 6 vs 6 (11 acciones).
	"football": {
		"PASS": true, "SHOT": true, "POWER_SHOT": true, "GOAL": true, "OWN_GOAL": true,
		"SAVE": true, "TACKLE": true, "INTERCEPTION": true, "DRIBBLE": true,
		"FOUL": true, "CORNER": true,
	},
	// Pelea 1 vs 1 básica, por rounds (6 acciones).
	"fighting": {
		"ATTACK": true, "BLOCK": true, "COMBO": true, "KNOCKDOWN": true,
		"ROUND_WON": true, "ROUND_DRAW": true,
	},
	// Tetris clásico de un jugador (13 acciones).
	"tetris": {
		"PIECE_SPAWNED": true, "PIECE_MOVED": true, "PIECE_ROTATED": true, "SOFT_DROP": true,
		"HARD_DROP": true, "HOLD": true, "PIECE_PLACED": true, "LINES_CLEARED": true,
		"T_SPIN": true, "ALL_CLEAR": true, "LEVEL_UP": true, "STACK_HEIGHT": true,
		"TOP_OUT": true,
	},
}
