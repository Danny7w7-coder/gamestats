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
	// 4 en línea (Connect Four), contra persona o contra la IA (7 acciones).
	"connect4": {
		"DISC_DROPPED": true, "THREAT_CREATED": true, "THREAT_BLOCKED": true,
		"CONNECT_FOUR": true, "TURN_TIMEOUT": true, "UNDO": true, "HINT_USED": true,
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
