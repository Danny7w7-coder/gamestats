// Simulador de GameStats: genera partidas de racing y football y las envía al
// servidor de ingestión (POST {target}/events).
//
// Ejemplo:
//
//	go run ./cmd/simulator -racing 4 -football 4 -seed 42 -target http://localhost:8080
package main

import (
	"flag"
	"fmt"
	"os"

	"gamestats/ingestion/internal/simulator"
)

func main() {
	racing := flag.Int("racing", 4, "cuántas partidas aleatorias de racing generar")
	football := flag.Int("football", 4, "cuántas partidas aleatorias de football generar")
	seed := flag.Int64("seed", 42, "semilla: la misma semilla genera exactamente las mismas partidas")
	target := flag.String("target", "http://localhost:8080", "dirección del servidor de ingestión")
	flag.Parse()

	if *racing < 0 || *football < 0 {
		fmt.Fprintln(os.Stderr, "-racing y -football no pueden ser negativos")
		os.Exit(2)
	}

	matches := simulator.Generate(*seed, *racing, *football)
	total := 0
	for _, m := range matches {
		total += len(m.Events)
	}
	fmt.Printf("seed %d: %d partidas generadas (%d eventos). Enviando a %s...\n",
		*seed, len(matches), total, *target)

	results := simulator.Send(*target, matches)

	failed := false
	accepted, rejected := 0, 0
	for _, r := range results {
		if r.Err != nil {
			failed = true
			fmt.Printf("  %-16s ERROR: %v\n", r.MatchID, r.Err)
			continue
		}
		fmt.Printf("  %-16s %3d enviados, %3d aceptados, %d rechazados\n",
			r.MatchID, r.Received, r.Accepted, len(r.Rejected))
		for _, rj := range r.Rejected {
			fmt.Printf("      rechazado %s\n", rj)
		}
		accepted += r.Accepted
		rejected += len(r.Rejected)
	}
	fmt.Printf("Total: %d aceptados, %d rechazados\n", accepted, rejected)

	if failed {
		os.Exit(1)
	}
}
