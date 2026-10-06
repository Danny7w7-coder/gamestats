// Package validation decide si un evento es válido antes de procesarlo.
package validation

// GameActions es el ÚNICO lugar donde se declaran los juegos y sus acciones.
// Para agregar un juego nuevo basta con agregar una entrada aquí: el resto del
// sistema (validación, pipeline, envío a Scala) no cambia.
var GameActions = map[string]map[string]bool{
	"racing":   {"LAP_COMPLETED": true, "POSITION_CHANGED": true, "PENALTY": true},
	"football": {"PASS": true, "SHOT": true, "GOAL": true, "FOUL": true},
}
