package simulator

import (
	"math/rand"
	"sort"
	"time"
)

var tracks = []string{"neon_city", "rainbow_road", "desert_dunes", "jungle_ruins", "frozen_peak"}

var penaltyReasons = []string{"corner_cutting", "track_limits", "dangerous_driving"}

var items = []string{"boost", "shield", "oil"}

var dnfReasons = []string{"crash", "mechanical", "disqualified"}

var pauseReasons = []string{"crash_cleanup", "weather", "technical"}

// racePlan describe una carrera antes de convertirla en eventos. La arman
// generateRacing (al azar) y los escenarios guionizados; buildRace la
// convierte en eventos.
type racePlan struct {
	track     string
	lapTimes  [][]int // lapTimes[vuelta][piloto], en ms
	penalties []racePenalty
	actions   []raceAction // objetos, derrapes, choques, trompos, respawns
	dnf       *raceDNF     // nil si nadie abandona
	pause     *racePause   // nil si no hay pausa
}

// racePenalty es una penalización: a qué piloto (índice), en qué vuelta,
// cuántos segundos y por qué. Suma tiempo, así que puede cambiar posiciones.
type racePenalty struct {
	driver  int
	lap     int
	seconds int
	reason  string
}

// raceAction es una acción de un piloto dentro de una vuelta. "at" es el
// momento de la vuelta, como fracción de su tiempo de vuelta (0.5 = a mitad).
type raceAction struct {
	driver int
	lap    int
	at     float64
	action string
	data   map[string]any
}

// raceDNF: un piloto abandona en una vuelta (al 60 % de ella).
type raceDNF struct {
	driver int
	lap    int
	reason string
}

// racePause: la carrera se detiene "seconds" segundos a los "atMs" ms de la
// largada.
type racePause struct {
	atMs    int
	seconds int
	reason  string
}

// Momento de la vuelta en que ocurre cada cosa (fracción del tiempo de vuelta).
// Las acciones van entre 0.1 y 0.47; la penalización a 0.5 y el DNF a 0.6, así
// un piloto nunca hace nada en la vuelta después de abandonar.
const (
	penaltyAt = 0.5
	dnfAt     = 0.6
)

// generateRacing simula una carrera arcade al azar: 3 a 6 pilotos, 3 a 5
// vueltas. Aquí solo se deciden los números y las acciones; buildRace arma
// los eventos (vueltas, posiciones, adelantamientos) y los ubica en el tiempo.
//
// Reglas de contexto que se cumplen por construcción:
//   - cada objeto se agarra y se usa en la misma vuelta: nunca se agarra otro
//     con la mano llena;
//   - RESPAWN solo justo después de un choque o trompo del mismo piloto;
//   - tras un DNF, buildRace no agrega más eventos de ese piloto.
func generateRacing(r *rand.Rand, id string, start time.Time) Match {
	drivers := 3 + r.Intn(4) // 3..6
	laps := 3 + r.Intn(3)    // 3..5
	plan := racePlan{track: pick(r, tracks)}

	// Ritmo base de cada piloto: entre 80 y 84 segundos por vuelta. En el 20 %
	// de las carreras hay un piloto rezagado (+30 a 40 s por vuelta), así el
	// líder le saca una vuelta (LAPPED).
	pace := make([]int, drivers)
	for i := range pace {
		pace[i] = 80000 + r.Intn(4000)
	}
	if r.Intn(100) < 20 {
		pace[r.Intn(drivers)] += 30000 + r.Intn(10001)
	}

	plan.lapTimes = make([][]int, laps)
	for lap := range plan.lapTimes {
		plan.lapTimes[lap] = make([]int, drivers)
		for i := range pace {
			plan.lapTimes[lap][i] = pace[i] + r.Intn(3001) - 1500 // ±1.5 s
		}
	}

	// Abandono (DNF) en el 15 % de las carreras. Se decide antes que los
	// choques para no elegir como "otherId" a alguien que ya abandonó.
	dnfDriver := -1
	if r.Intn(100) < 15 {
		dnfDriver = r.Intn(drivers)
		plan.dnf = &raceDNF{driver: dnfDriver, lap: 1 + r.Intn(laps), reason: pick(r, dnfReasons)}
	}

	for lap := 1; lap <= laps; lap++ {
		for d := 0; d < drivers; d++ {
			plan.actions = append(plan.actions, randomLapActions(r, &plan, d, lap, dnfDriver)...)
		}
	}

	// Penalización ocasional: 12 % de los pilotos recibe una, en una vuelta al azar.
	for i := 0; i < drivers; i++ {
		if r.Intn(100) < 12 {
			plan.penalties = append(plan.penalties, racePenalty{
				driver: i, lap: 1 + r.Intn(laps),
				seconds: 5 * (1 + r.Intn(2)), // 5 o 10 s
				reason:  pick(r, penaltyReasons),
			})
		}
	}

	// Pausa en el 20 % de las carreras, entre los 20 s y la mitad de la carrera.
	if r.Intn(100) < 20 {
		plan.pause = &racePause{
			atMs:    20000 + r.Intn(laps*40000-20000),
			seconds: 15 + r.Intn(31), // 15..45 s
			reason:  pick(r, pauseReasons),
		}
	}

	return buildRace(id, start, plan)
}

// randomLapActions decide qué hace el piloto d en una vuelta: derrapes,
// objetos y, a veces, un choque o trompo (que le cuesta tiempo).
func randomLapActions(r *rand.Rand, plan *racePlan, d, lap, dnfDriver int) []raceAction {
	var acts []raceAction
	if r.Intn(100) < 40 {
		acts = append(acts, raceAction{d, lap, 0.1, "DRIFT", map[string]any{
			"lap": lap, "corner": 1 + r.Intn(8), "durationMs": 1000 + r.Intn(1501),
		}})
	}
	if r.Intn(100) < 35 {
		item := pick(r, items)
		used := map[string]any{"lap": lap, "item": item}
		if item == "boost" { // el nitro dura un tiempo
			used["durationMs"] = 1500 + r.Intn(2001)
		}
		acts = append(acts,
			raceAction{d, lap, 0.2, "ITEM_PICKUP", map[string]any{"lap": lap, "item": item}},
			raceAction{d, lap, 0.35, "ITEM_USED", used})
	}
	if r.Intn(100) < 6 {
		// Choque o trompo: el piloto pierde 4 a 8 s en esa vuelta.
		plan.lapTimes[lap-1][d] += 4000 + r.Intn(4001)
		var others []int
		for o := range plan.lapTimes[0] {
			if o != d && o != dnfDriver {
				others = append(others, o)
			}
		}
		if len(others) > 0 && r.Intn(2) == 0 {
			acts = append(acts, raceAction{d, lap, 0.45, "COLLISION", map[string]any{
				"lap": lap, "otherId": playerIDs(len(plan.lapTimes[0]))[others[r.Intn(len(others))]],
				"damage": 10 + r.Intn(51),
			}})
		} else {
			acts = append(acts, raceAction{d, lap, 0.45, "SPIN", map[string]any{"lap": lap, "corner": 1 + r.Intn(8)}})
		}
		if r.Intn(100) < 70 { // casi siempre vuelve a la pista
			acts = append(acts, raceAction{d, lap, 0.47, "RESPAWN", nil}) // buildRace pone la posición
		}
	}
	return acts
}

// buildRace arma los eventos de una carrera a partir de su plan. La usan
// tanto las carreras al azar como las guionizadas. La posición se calcula
// con el tiempo acumulado más las penalizaciones, solo entre los pilotos que
// siguen en carrera.
func buildRace(id string, start time.Time, plan racePlan) Match {
	b := newBuilder(id, "racing")
	n := len(plan.lapTimes[0])
	laps := len(plan.lapTimes)
	drivers := playerIDs(n)

	b.add(start, "MATCH_STARTED", "", "", map[string]any{"track": plan.track, "laps": laps})
	for i, p := range drivers {
		b.add(start.Add(time.Duration(i+1)*time.Second), "PLAYER_JOINED", p, "", map[string]any{})
	}

	raceStart := start.Add(10 * time.Second)

	// clock convierte "ms desde la largada" en la hora del evento. Si hubo
	// pausa, todo lo que pasa desde ese momento se corre lo que duró, así
	// ninguna acción cae dentro de la pausa. Los tiempos de vuelta no cambian.
	clock := func(msFromStart int) time.Time {
		t := raceStart.Add(ms(msFromStart))
		if plan.pause != nil && msFromStart >= plan.pause.atMs {
			t = t.Add(time.Duration(plan.pause.seconds) * time.Second)
		}
		return t
	}
	if plan.pause != nil {
		at := raceStart.Add(ms(plan.pause.atMs))
		b.add(at, "MATCH_PAUSED", "", "", map[string]any{"reason": plan.pause.reason})
		b.add(at.Add(time.Duration(plan.pause.seconds)*time.Second), "MATCH_RESUMED", "", "", map[string]any{})
	}

	race := newRaceState(n)
	for lap := 1; lap <= laps; lap++ {
		times := plan.lapTimes[lap-1]
		// lapStart: ms desde la largada en que cada piloto empezó esta vuelta.
		lapStart := append([]int{}, race.driven...)
		atLap := func(driver int, frac float64) int {
			return lapStart[driver] + int(frac*float64(times[driver]))
		}

		for _, a := range plan.actions {
			if a.lap != lap || race.out[a.driver] {
				continue
			}
			data := a.data
			if a.action == "RESPAWN" { // vuelve a la pista en la posición que tenía
				data = map[string]any{"lap": lap, "position": race.pos[a.driver]}
			}
			b.add(clock(atLap(a.driver, a.at)), "PLAYER_ACTION", drivers[a.driver], a.action, data)
		}

		for _, pen := range plan.penalties {
			if pen.lap == lap && !race.out[pen.driver] {
				race.penalty[pen.driver] += pen.seconds * 1000
				b.add(clock(atLap(pen.driver, penaltyAt)), "PLAYER_ACTION", drivers[pen.driver], "PENALTY",
					map[string]any{"seconds": pen.seconds, "reason": pen.reason})
			}
		}

		if plan.dnf != nil && plan.dnf.lap == lap {
			d := plan.dnf.driver
			race.out[d] = true
			race.dnfMs[d] = atLap(d, dnfAt)
			b.add(clock(race.dnfMs[d]), "PLAYER_ACTION", drivers[d], "DNF",
				map[string]any{"lap": lap, "reason": plan.dnf.reason})
		}

		for i := range drivers {
			if !race.out[i] {
				race.driven[i] += times[i]
				race.crossed[i] = append(race.crossed[i], race.driven[i])
			}
		}
		addLapEvents(b, drivers, race, lap, times, clock)
	}
	addLappedEvents(b, drivers, race, clock)

	b.add(clock(race.maxDriven()).Add(5*time.Second), "MATCH_FINISHED", "", "",
		map[string]any{"ranking": race.ranking(drivers)})
	return b.build()
}

// raceState es el estado de la carrera (índice = piloto).
type raceState struct {
	driven  []int   // ms manejados
	penalty []int   // ms de penalización
	pos     []int   // posición actual (empieza en el orden de la grilla)
	out     []bool  // abandonó (DNF)
	dnfMs   []int   // ms desde la largada en que abandonó
	crossed [][]int // crossed[piloto][k] = ms en que completó la vuelta k+1
}

func newRaceState(n int) *raceState {
	s := &raceState{
		driven: make([]int, n), penalty: make([]int, n), pos: make([]int, n),
		out: make([]bool, n), dnfMs: make([]int, n), crossed: make([][]int, n),
	}
	for i := range s.pos {
		s.pos[i] = i + 1
	}
	return s
}

// positions calcula la posición de cada piloto en carrera por tiempo total
// (manejado + penalización). En empate gana el de menor índice. El que
// abandonó queda con posición 0.
func (s *raceState) positions() []int {
	var order []int
	for i := range s.driven {
		if !s.out[i] {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		return s.driven[order[a]]+s.penalty[order[a]] < s.driven[order[b]]+s.penalty[order[b]]
	})
	pos := make([]int, len(s.driven))
	for place, i := range order {
		pos[i] = place + 1
	}
	return pos
}

// ranking devuelve los pilotos que terminaron, ordenados por posición final.
// El que abandonó (DNF) no aparece.
func (s *raceState) ranking(drivers []string) []string {
	var ranking []string
	pos := s.positions()
	for place := 1; place <= len(drivers); place++ {
		for i, p := range pos {
			if p == place {
				ranking = append(ranking, drivers[i])
			}
		}
	}
	return ranking
}

func (s *raceState) maxDriven() int {
	m := 0
	for i, d := range s.driven {
		if !s.out[i] {
			m = max(m, d)
		}
	}
	return m
}

// addLapEvents agrega, para cada piloto en carrera, POSITION_CHANGED (si
// cambió de posición), un OVERTAKE por cada rival que iba delante y ahora va
// detrás, y LAP_COMPLETED en el momento en que cruza la meta.
func addLapEvents(b *builder, drivers []string, s *raceState, lap int, lapTimes []int, clock func(int) time.Time) {
	newPos := s.positions()
	for i, p := range drivers {
		if s.out[i] {
			continue
		}
		at := clock(s.driven[i])
		if newPos[i] != s.pos[i] {
			b.add(at, "PLAYER_ACTION", p, "POSITION_CHANGED",
				map[string]any{"from": s.pos[i], "to": newPos[i], "lap": lap})
		}
		for j := range drivers {
			if j != i && !s.out[j] && s.pos[j] < s.pos[i] && newPos[j] > newPos[i] {
				b.add(at, "PLAYER_ACTION", p, "OVERTAKE", map[string]any{"lap": lap, "overtakenId": drivers[j]})
			}
		}
		b.add(at, "PLAYER_ACTION", p, "LAP_COMPLETED",
			map[string]any{"lap": lap, "lapTimeMs": lapTimes[i], "position": newPos[i]})
	}
	s.pos = newPos
}

// addLappedEvents agrega un LAPPED cuando un piloto le saca una vuelta a
// otro: al cruzar la meta en la vuelta L, el otro todavía no completó la
// vuelta L-1. Se anota una sola vez por pareja.
func addLappedEvents(b *builder, drivers []string, s *raceState, clock func(int) time.Time) {
	for a := range drivers {
		lapped := map[int]bool{}
		for k, t := range s.crossed[a] {
			lap := k + 1
			for o := range drivers {
				if o == a || lapped[o] || (s.out[o] && s.dnfMs[o] <= t) {
					continue
				}
				done := 0 // vueltas que el otro había completado en ese momento
				for _, c := range s.crossed[o] {
					if c <= t {
						done++
					}
				}
				if done <= lap-2 {
					lapped[o] = true
					b.add(clock(t), "PLAYER_ACTION", drivers[a], "LAPPED", map[string]any{"lap": lap, "lappedId": drivers[o]})
				}
			}
		}
	}
}

func ms(n int) time.Duration {
	return time.Duration(n) * time.Millisecond
}
