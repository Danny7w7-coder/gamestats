# contracts

Contrato compartido entre `ingestion-go` (Go) y `analytics-scala` (Scala).
Si algo de aquí cambia, cambia en `main` y ambos lo traen con `git pull origin main`.

## Schemas (JSON Schema 2020-12)

| Archivo | Qué describe |
|---|---|
| `event.schema.json` | Un evento. Valida el `data` de cada `type`/`action` conocido. Una `action` desconocida se acepta (Scala la guarda como `UnknownAction`). |
| `batch.schema.json` | Cuerpo de `POST /analyze` (Go → Scala). |
| `results.schema.json` | `MatchAnalysis` que devuelve Scala (`POST /analyze`, `GET /results`). |

Decisiones:

- El orden de los eventos se toma del `timestamp`; `minute` (football) es solo informativo.
- `playerId` es obligatorio salvo en `MATCH_STARTED`, `MATCH_FINISHED`, `MATCH_PAUSED` y `MATCH_RESUMED`; `action` solo existe en `PLAYER_ACTION`.
- Los nombres de acción son únicos entre todos los juegos (no hay dos juegos con una acción del mismo nombre).
- Racing es una carrera arcade por vueltas y posiciones. Un jugador con `DNF` no aparece en `MATCH_FINISHED.data.ranking`.
- Racing, objetos: cada jugador lleva **un objeto a la vez**. Lo agarra con `ITEM_PICKUP` y al usarlo (`ITEM_USED`) queda con la mano vacía. `boost` = nitro (lleva `durationMs`), `shield` = escudo, `oil` = mancha de aceite.
- Football es un 6 vs 6 estilo arcade retro (máximo 6 por equipo: 5 de campo y portero), sin fuera de juego, cambios ni penales. Todo `GOAL` va precedido de un `SHOT` o `POWER_SHOT` (tiro potente) con `onTarget: true` del mismo jugador. `OWN_GOAL` lleva el `teamId` de quien lo hace y suma para el **otro** equipo. El expulsado con roja no genera más eventos. Empate → `winner: null`.
- Fighting es estilo Mortal Kombat, 1 contra 1 por rounds. `FATAL_BLOW` es un golpe muy fuerte que se puede usar una vez por pelea y solo con la vida propia en 30 o menos. `FATALITY` es el remate final: la hace el ganador de la pelea después de su último `ROUND_WON` y después ya no hay más acciones.
- Tetris es de **un solo jugador por partida** (el primero que hace `PLAYER_JOINED`). Se juega hasta perder (`TOP_OUT`); el puntaje es la suma de `LINES_CLEARED.points`.
- `winner`: racing → `playerId` (`ranking[0]`); football → `teamId`; fighting → `playerId` con más `ROUND_WON`; tetris → siempre `null` (no hay rival); empate o sin datos → `null`.
- `score` y `teamStats` solo aparecen en football. `patterns[].playerId` es `null` en patrones de equipo.

## Catálogo de eventos

Los 8 tipos generales sirven para los cuatro juegos: `MATCH_STARTED`, `MATCH_FINISHED`, `MATCH_PAUSED`, `MATCH_RESUMED`, `PLAYER_JOINED`, `PLAYER_LEFT`, `PLAYER_DISCONNECTED`, `PLAYER_RECONNECTED`. Además, cada juego tiene sus acciones (`type: "PLAYER_ACTION"`): 12 en racing, 11 en football, 10 en fighting y 13 en tetris. Juegos de referencia: racing → Mario Kart, football → arcade retro 6 vs 6 (Nintendo World Cup, Super Sidekicks) con tiro potente, fighting → Mortal Kombat (versión simple), tetris → Tetris clásico de un jugador. El `data` exacto de cada una, con sus rangos y valores permitidos, está en `event.schema.json` (`$defs`).

| Juego | `MATCH_STARTED.data` | Acciones |
|---|---|---|
| `racing` | `track`, `laps` | `LAP_COMPLETED`, `POSITION_CHANGED`, `OVERTAKE`, `LAPPED`, `ITEM_PICKUP`, `ITEM_USED`, `DRIFT`, `COLLISION`, `SPIN`, `RESPAWN`, `PENALTY`, `DNF` |
| `football` | `homeTeam`, `awayTeam` (y `PLAYER_JOINED.data.teamId`) | `PASS`, `SHOT`, `POWER_SHOT`, `GOAL`, `OWN_GOAL`, `SAVE`, `TACKLE`, `INTERCEPTION`, `DRIBBLE`, `FOUL`, `CORNER` |
| `fighting` | `stage`, `rounds` (1, 3 o 5) (y `PLAYER_JOINED.data.character`) | `ATTACK`, `SPECIAL_MOVE`, `COMBO`, `THROW`, `BLOCK`, `COMBO_BREAKER`, `KNOCKDOWN`, `FATAL_BLOW`, `ROUND_WON`, `FATALITY` |
| `tetris` | `startLevel` (1–15) | `PIECE_SPAWNED`, `PIECE_MOVED`, `PIECE_ROTATED`, `SOFT_DROP`, `HARD_DROP`, `HOLD`, `PIECE_PLACED`, `LINES_CLEARED`, `T_SPIN`, `ALL_CLEAR`, `LEVEL_UP`, `STACK_HEIGHT`, `TOP_OUT` |

## Validaciones de contexto

El schema valida cada evento por separado (tipos, rangos, enums, campos obligatorios, y en fighting que un golpe fallado tenga `damage: 0`). Las reglas de abajo dependen de **otros eventos de la partida**, así que no caben en un schema y las aplica Scala recorriendo los eventos en orden. Un evento que rompe una regla se descarta y **no cambia el estado** (por ejemplo, un `ITEM_PICKUP` descartado no le da el objeto al jugador).

| Juego | Regla |
|---|---|
| Todos | `MATCH_RESUMED` solo después de un `MATCH_PAUSED`, y no se pausa dos veces seguidas. |
| Todos | No hay `PLAYER_ACTION` mientras la partida está en pausa. |
| Todos | Toda referencia a otro jugador (`targetId`, `overtakenId`, `otherId`, `lappedId`, `attackerId`, `assistId`) es un jugador de la partida (`players`) distinto del que hace la acción. |
| Racing | `lap` ≤ `MATCH_STARTED.data.laps`. |
| Racing | `POSITION_CHANGED`: `from` ≠ `to`. |
| Racing | `ITEM_PICKUP` solo con la mano vacía. |
| Racing | `ITEM_USED` solo si el jugador tiene en la mano ese mismo objeto. |
| Racing | `RESPAWN` solo después de un `COLLISION` o `SPIN` de ese jugador en esa vuelta, una vez por incidente. |
| Racing | Después de un `DNF` el jugador no tiene más eventos. |
| Football | Máximo 6 jugadores por equipo: un `PLAYER_JOINED` que haría el séptimo se descarta, y también todos los eventos de ese jugador. |
| Football | El que recibe roja no tiene más eventos. |
| Fighting | `round` ≤ `MATCH_STARTED.data.rounds`. |
| Fighting | `COMBO_BREAKER` solo después de un `COMBO` del rival (`attackerId`) contra ese jugador en ese round, una vez por combo. |
| Fighting | `FATAL_BLOW` una sola vez por pelea por jugador. |
| Fighting | Un solo `ROUND_WON` por round. |
| Fighting | `FATALITY` solo la hace quien ya ganó la pelea (la mayoría de los rounds), y después de ella no hay más acciones. |
| Tetris | Un solo jugador: todo evento de otro `playerId` distinto del primero que entró se descarta. |
| Tetris | `LEVEL_UP`: el nivel nuevo es mayor que el actual del jugador (al inicio, `startLevel`). |
| Tetris | Después de un `TOP_OUT` el jugador no tiene más eventos. |

## Quién valida qué

- **Go** valida cada evento al recibirlo y rechaza solo ese evento: campos obligatorios, `timestamp` RFC3339, `type` conocido, `action` solo en `PLAYER_ACTION`, y `action` dentro del catálogo del juego (`ingestion-go/internal/validation/catalog.go`). Go **no** revisa `data`. Por eso, aunque `event.schema.json` acepta cualquier `action`, en la práctica a Scala solo llegan acciones del catálogo.
- **Scala** rechaza el lote completo (`400`) solo por errores del lote: encabezado incompleto, `events` vacío, un evento sin `eventId`/`timestamp`/`type` válidos o de otra partida/juego, o `gameId` sin analyzer. Si una acción conocida trae `data` mal formada o rompe una validación de contexto, Scala **descarta ese evento**, lo registra en su log y analiza la partida igual. Una acción desconocida se cuenta en `eventsByAction` y no afecta el resto.
- Ante un `4xx` Go no reintenta (la partida queda como `error_envio`); ante `5xx` o timeout reintenta 3 veces (1, 2 y 4 s). Scala solo responde `500` si no puede guardar el resultado.

## Fixtures

| Archivo | Partida | Para qué sirve |
|---|---|---|
| `racing_match_ok.json` | race-001 | p1 gana (vueltas 82000/81500/80900 ms, peor posición 2 → **sin** COMEBACK). p3 tiene un `PENALTY` de 5 s. |
| `comeback_case.json` | race-002 | p3 va 3° → 2° → 1° y gana → **COMEBACK**. |
| `racing_full_ok.json` | race-003 | Las 12 acciones de racing y una pausa. p2 gana sin choques ni penalizaciones y le saca una vuelta a p4 (el más lento). p1 usa los 3 objetos (dos nitros, escudo y aceite), hace un trompo por el aceite de p3, vuelve a la pista (`RESPAWN`) y recibe 5 s por cortar camino. p3 choca con p1, hace un trompo y abandona (`DNF`, no está en el ranking). Ranking `[p2, p1, p4]`. |
| `football_match_ok.json` | foot-001 | A gana 2-1 (goles A, A, B → **sin** COMEBACK). p2: 3 pases, 2 exitosos. p4: 0 tiros. Un gol sin asistencia. |
| `football_comeback.json` | foot-002 | A gana 2-1 remontando (goles B, A, A → **COMEBACK**). p4 recibe roja y no vuelve a aparecer. |
| `football_full_ok.json` | foot-003 | 6 vs 6 (A: p1–p6, B: p7–p12; porteros p1 y p7) con las 11 acciones y el entretiempo como pausa. A gana 3-0: autogol de p10 (B), gol de tiro potente de p4 y gol de p3 con asistencia de p2. p12 (B) ve la roja en el 62'. p1 hace 2 atajadas → A con la valla invicta. |
| `fighting_match_ok.json` | fight-001 | Mejor de 3, Scorpion (p1) vs Sub-Zero (p2). p1 gana 2-0: round 1 sin recibir daño (vida 100 → *flawless victory*) y round 2 por KO con vida 25, después de cortar un combo y usar su `FATAL_BLOW`. Termina con una `FATALITY`. Usa las 10 acciones. |
| `fighting_comeback.json` | fight-002 | Mejor de 3, Liu Kang (p3) vs Kitana (p4). p3 gana el round 1 por tiempo (vida 60); p4 gana los rounds 2 y 3 → **remontada**. En el round 3 p4 queda con vida 25 y da vuelta la pelea con su `FATAL_BLOW`. Sin fatality. |
| `tetris_match_ok.json` | tetris-001 | p1 desde el nivel 1: dos tetris seguidos (back-to-back, usando `HOLD` para guardar una O), sube al nivel 2, hace un T-spin doble y pierde con la pila en 20 (`TOP_OUT`). 3200 puntos, 10 líneas. 12 de las 13 acciones (falta `ALL_CLEAR`). |
| `tetris_perfect_clear.json` | tetris-002 | p3 desde el nivel 5, con pausa. 3 T-spins (0, 1 y 2 líneas), un tetris que deja el tablero vacío (`ALL_CLEAR`), niveles 6 y 7, termina en `TOP_OUT`. |
| `racing_invalid_events.json` | race-997 | 8 eventos con `data` inválida + 12 que rompen validaciones de contexto. |
| `football_invalid_events.json` | foot-997 | 5 + 6 (incluye un séptimo jugador en el equipo A). |
| `fighting_invalid_events.json` | fight-997 | 6 + 10. |
| `tetris_invalid_events.json` | tetris-997 | 7 + 4. |
| `invalid_missing_matchid.json` | — | Lote sin `matchId` → `400`. |
| `invalid_bad_timestamp.json` | race-998 | Un `timestamp` que no parsea → `400`. |

En los `*_invalid_events.json`, el `eventId` dice qué se espera: todo evento `bad-data-*` falla el schema, todo evento `bad-ctx-*` rompe una validación de contexto, y **solo esos** se descartan. El resto de la partida es válido y se analiza normal. Un test puede comprobarlo así: los descartados de Scala son exactamente los `eventId` que empiezan con `bad-`.

Probar contra Scala:

```powershell
curl.exe -X POST http://localhost:8081/analyze -H "Content-Type: application/json" --data-binary "@contracts/fixtures/racing_match_ok.json"
```
