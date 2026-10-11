// Simulador de GameStats: genera partidas de racing, football y fighting y
// las envía al servidor de ingestión (POST {target}/events).
//
// Ejemplo:
//
//	go run ./cmd/simulator -racing 4 -football 4 -fighting 4 -seed 42 -target http://localhost:8080
package main

import (
	"flag"
	"fmt"
	"os"

	"gamestats/ingestion/internal/simulator"
)

func main() {
	racing := flag.Int("racing", 4, "cuántas partidas aleatorias de racing generar (además de las guionizadas)")
	football := flag.Int("football", 4, "cuántas partidas aleatorias de football generar (además de las guionizadas)")
	fighting := flag.Int("fighting", 4, "cuántas partidas aleatorias de fighting generar (además de las guionizadas)")
	seed := flag.Int64("seed", 42, "semilla: la misma semilla genera exactamente las mismas partidas")
	target := flag.String("target", "http://localhost:8080", "dirección del servidor de ingestión")
	corrupt := flag.Int("corrupt", 3, "porcentaje (0 a 100) de eventos a los que se agrega una copia dañada a propósito")
	flag.Parse()

	if *racing < 0 || *football < 0 || *fighting < 0 {
		fmt.Fprintln(os.Stderr, "-racing, -football y -fighting no pueden ser negativos")
		os.Exit(2)
	}
	if *corrupt < 0 || *corrupt > 100 {
		fmt.Fprintln(os.Stderr, "-corrupt debe estar entre 0 y 100")
		os.Exit(2)
	}

	matches := simulator.Generate(*seed, simulator.Counts{Racing: *racing, Football: *football, Fighting: *fighting})
	damaged := simulator.Corrupt(matches, *seed, *corrupt)
	total := 0
	for _, m := range matches {
		total += len(m.Events)
	}
	fmt.Printf("seed %d: %d partidas generadas (%d eventos, %d dañados a propósito). Enviando a %s...\n",
		*seed, len(matches), total, damaged, *target)
	fmt.Println("Escenarios guionizados:")
	for _, m := range matches {
		if m.Script != "" {
			fmt.Printf("  %-18s %s\n", m.ID, m.Script)
		}
	}

	results := simulator.Send(*target, matches)

	failed := false
	accepted, rejected := 0, 0
	for _, r := range results {
		if r.Err != nil {
			failed = true
			fmt.Printf("  %-18s ERROR: %v\n", r.MatchID, r.Err)
			continue
		}
		fmt.Printf("  %-18s %3d enviados, %3d aceptados, %d rechazados\n",
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
