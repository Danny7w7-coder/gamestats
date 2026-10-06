package simulator

import (
	"math/rand"
	"sort"
	"time"
)

var tracks = []string{"monza", "interlagos", "silverstone", "spa", "suzuka"}

var penaltyReasons = []string{"track_limits", "unsafe_release", "collision"}

// generateRacing simula una carrera: 3 a 6 pilotos, 3 a 5 vueltas. Cada piloto
// tiene un ritmo base y cada vuelta varía un poco. La posición se calcula con
// el tiempo acumulado más las penalizaciones.
func generateRacing(r *rand.Rand, id string, start time.Time) Match {
	b := newBuilder(id, "racing")
	drivers := playerIDs(3 + r.Intn(4)) // 3..6
	laps := 3 + r.Intn(3)               // 3..5

	b.add(start, "MATCH_STARTED", "", "", map[string]any{"track": pick(r, tracks), "laps": laps})
	for i, p := range drivers {
		b.add(start.Add(time.Duration(i+1)*time.Second), "PLAYER_JOINED", p, "", map[string]any{})
	}

	// Ritmo base de cada piloto: entre 80 y 84 segundos por vuelta.
	pace := make([]int, len(drivers))
	for i := range pace {
		pace[i] = 80000 + r.Intn(4000)
	}

	race := newRaceState(len(drivers))
	raceStart := start.Add(10 * time.Second)
	penalized := make([]bool, len(drivers)) // máximo una penalización por piloto

	for lap := 1; lap <= laps; lap++ {
		lapTimes := make([]int, len(drivers))
		for i := range drivers {
			lapTimes[i] = pace[i] + r.Intn(3001) - 1500 // ±1.5 s
			race.driven[i] += lapTimes[i]
		}

		// Penalización ocasional (3 % por piloto y vuelta), a mitad de vuelta.
		for i, p := range drivers {
			if !penalized[i] && r.Intn(100) < 3 {
				penalized[i] = true
				seconds := 5 * (1 + r.Intn(2)) // 5 o 10 s
				race.penalty[i] += seconds * 1000
				at := raceStart.Add(ms(race.driven[i] - lapTimes[i]/2))
				b.add(at, "PLAYER_ACTION", p, "PENALTY",
					map[string]any{"seconds": seconds, "reason": pick(r, penaltyReasons)})
			}
		}

		addLapEvents(b, drivers, race, lap, lapTimes, raceStart)
	}

	b.add(raceStart.Add(ms(race.maxDriven())+5*time.Second), "MATCH_FINISHED", "", "",
		map[string]any{"ranking": race.ranking(drivers)})
	return b.build()
}

// raceState son los tiempos acumulados de cada piloto (índice = piloto).
type raceState struct {
	driven  []int // ms manejados
	penalty []int // ms de penalización
	pos     []int // posición actual (empieza en el orden de la grilla)
}

func newRaceState(n int) *raceState {
	s := &raceState{driven: make([]int, n), penalty: make([]int, n), pos: make([]int, n)}
	for i := range s.pos {
		s.pos[i] = i + 1
	}
	return s
}

// positions calcula la posición de cada piloto por tiempo total
// (manejado + penalización). En empate gana el de menor índice.
func (s *raceState) positions() []int {
	order := make([]int, len(s.driven))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return s.driven[order[a]]+s.penalty[order[a]] < s.driven[order[b]]+s.penalty[order[b]]
	})
	pos := make([]int, len(order))
	for place, i := range order {
		pos[i] = place + 1
	}
	return pos
}

// ranking devuelve los pilotos ordenados por posición final.
func (s *raceState) ranking(drivers []string) []string {
	ranking := make([]string, len(drivers))
	for i, p := range s.positions() {
		ranking[p-1] = drivers[i]
	}
	return ranking
}

func (s *raceState) maxDriven() int {
	m := 0
	for _, d := range s.driven {
		m = max(m, d)
	}
	return m
}

// addLapEvents agrega, para cada piloto, POSITION_CHANGED (si cambió de
// posición) y LAP_COMPLETED en el momento en que cruza la meta.
func addLapEvents(b *builder, drivers []string, s *raceState, lap int, lapTimes []int, raceStart time.Time) {
	newPos := s.positions()
	for i, p := range drivers {
		at := raceStart.Add(ms(s.driven[i]))
		if newPos[i] != s.pos[i] {
			b.add(at, "PLAYER_ACTION", p, "POSITION_CHANGED",
				map[string]any{"from": s.pos[i], "to": newPos[i], "lap": lap})
		}
		b.add(at, "PLAYER_ACTION", p, "LAP_COMPLETED",
			map[string]any{"lap": lap, "lapTimeMs": lapTimes[i], "position": newPos[i]})
	}
	s.pos = newPos
}

func ms(n int) time.Duration {
	return time.Duration(n) * time.Millisecond
}
