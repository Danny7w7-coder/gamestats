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

- El catálogo cubre todo lo que manda cada juego real (para la app del proyecto 2 no hace falta agregar eventos).
- El orden de los eventos se toma del `timestamp`.
- `playerId` es obligatorio salvo en `MATCH_STARTED`, `MATCH_FINISHED`, `MATCH_PAUSED` y `MATCH_RESUMED`; `action` solo existe en `PLAYER_ACTION`.
- Los nombres de acción son únicos entre todos los juegos (no hay dos juegos con una acción del mismo nombre).
- Racing es una carrera por vueltas y posiciones, sin objetos. Un jugador con `DNF` no aparece en `MATCH_FINISHED.data.ranking`.
- Fighting es una pelea simple 1 contra 1 por rounds (golpe, patada, bloqueo, combo y derribo). Un round termina con `ROUND_WON` (KO o tiempo) o con `ROUND_DRAW` (doble KO o tiempo con la misma vida), que no suma para nadie. Gana la pelea quien llega primero a la mayoría de los rounds; si se juegan todos sin llegar, es empate.
- Connect4 es el 4 en línea clásico: tablero de 7 columnas × 6 filas (`row` 1 = abajo), dos jugadores por turnos, se puede jugar contra la IA (`PLAYER_JOINED.data.bot`). `THREAT_CREATED` = el jugador tiene 3 en línea y esa casilla vacía completaría la cuarta; `THREAT_BLOCKED` = el rival soltó su ficha en esa casilla. Sin `CONNECT_FOUR` la partida es empate (tablero lleno).
- Tetris es de **un solo jugador por partida** (el primero que hace `PLAYER_JOINED`). Se juega hasta perder (`TOP_OUT`); el puntaje es la suma de `LINES_CLEARED.points`.
- `winner`: racing → `playerId` (`ranking[0]`); fighting → `playerId` con más `ROUND_WON`; connect4 → `playerId` del `CONNECT_FOUR`; tetris → siempre `null` (no hay rival); empate o sin datos → `null`.

## Catálogo de eventos

Hay 9 tipos de evento (`type`), iguales para los cuatro juegos: `MATCH_STARTED`, `MATCH_FINISHED`, `MATCH_PAUSED`, `MATCH_RESUMED`, `PLAYER_JOINED`, `PLAYER_LEFT`, `PLAYER_DISCONNECTED`, `PLAYER_RECONNECTED` y `PLAYER_ACTION`. Un `PLAYER_ACTION` dice en `action` qué hizo el jugador; cada juego tiene sus acciones: 8 en racing, 6 en fighting, 7 en connect4 y 13 en tetris. Juegos de referencia: racing → carrera arcade por posiciones, fighting → Street Fighter / Mortal Kombat básico, connect4 → Connect Four, tetris → Tetris clásico de un jugador. El `data` exacto de cada acción, con sus rangos y valores permitidos, está en `event.schema.json` (`$defs`).

| Juego | Datos de inicio | Acciones |
|---|---|---|
| `racing` | `MATCH_STARTED.data`: `track`, `laps` | `LAP_COMPLETED`, `POSITION_CHANGED`, `OVERTAKE`, `LAPPED`, `COLLISION`, `RESPAWN`, `PENALTY`, `DNF` |
| `fighting` | `MATCH_STARTED.data`: `stage`, `rounds` (1, 3 o 5); `PLAYER_JOINED.data.character` | `ATTACK` (`punch`/`kick`), `BLOCK`, `COMBO`, `KNOCKDOWN`, `ROUND_WON`, `ROUND_DRAW` |
| `connect4` | `PLAYER_JOINED.data`: `color` (`red`/`yellow`), `bot` y `difficulty` (`easy`/`medium`/`hard`) si es la IA | `DISC_DROPPED`, `THREAT_CREATED`, `THREAT_BLOCKED`, `CONNECT_FOUR`, `TURN_TIMEOUT`, `UNDO`, `HINT_USED` |
| `tetris` | `MATCH_STARTED.data.startLevel` (1–15) | `PIECE_SPAWNED`, `PIECE_MOVED`, `PIECE_ROTATED`, `SOFT_DROP`, `HARD_DROP`, `HOLD`, `PIECE_PLACED`, `LINES_CLEARED`, `T_SPIN`, `ALL_CLEAR`, `LEVEL_UP`, `STACK_HEIGHT`, `TOP_OUT` |

## Validaciones de contexto

El schema valida cada evento por separado (tipos, rangos, enums, campos obligatorios, y en fighting que un golpe fallado tenga `damage: 0`). Las reglas de abajo dependen de **otros eventos de la partida**, así que no caben en un schema y las aplica Scala recorriendo los eventos en orden. Un evento que rompe una regla se descarta y **no cambia el estado** (por ejemplo, un `DISC_DROPPED` descartado no pone la ficha en el tablero).

| Juego | Regla |
|---|---|
| Todos | `MATCH_RESUMED` solo después de un `MATCH_PAUSED`, y no se pausa dos veces seguidas. |
| Todos | No hay `PLAYER_ACTION` mientras la partida está en pausa. |
| Todos | Toda referencia a otro jugador (`targetId`, `overtakenId`, `otherId`, `lappedId`, `attackerId`) es un jugador de la partida (`players`) distinto del que hace la acción. |
| Racing | `lap` ≤ `MATCH_STARTED.data.laps`. |
| Racing | `POSITION_CHANGED`: `from` ≠ `to`. |
| Racing | `RESPAWN` solo después de un `COLLISION` en el que participó ese jugador (`playerId` u `otherId`) en esa vuelta, una vez por choque. |
| Racing | Después de un `DNF` el jugador no tiene más eventos. |
| Fighting | `round` ≤ `MATCH_STARTED.data.rounds`. |
| Fighting | Un solo resultado por round: un `ROUND_WON` o un `ROUND_DRAW`. |
| Fighting | Después del `ROUND_WON` que decide la pelea (el que llega a la mayoría de los rounds) no hay más acciones. |
| Connect4 | Juegan dos: todo evento de un jugador distinto de los dos primeros que entraron se descarta. |
| Connect4 | Turnos: empieza el primero que entró. `DISC_DROPPED`, `TURN_TIMEOUT` y `HINT_USED` solo los hace el jugador que tiene el turno; `DISC_DROPPED` y `TURN_TIMEOUT` pasan el turno al rival. |
| Connect4 | Gravedad: `row` es la primera fila libre de esa columna (una columna llena no acepta fichas). |
| Connect4 | `THREAT_CREATED` solo en una casilla vacía que no sea ya una amenaza abierta de ese jugador. |
| Connect4 | `THREAT_BLOCKED` solo si `attackerId` tiene una amenaza abierta en esa casilla y la casilla tiene una ficha del que bloquea; la amenaza queda cerrada. |
| Connect4 | `UNDO` solo en partidas contra la IA y solo lo hace el humano. `column`/`row` son la última ficha del tablero; el tablero, las amenazas y el turno vuelven a como estaban antes de esa ficha (para deshacer la jugada propia se mandan dos: la de la IA y la propia). |
| Connect4 | `CONNECT_FOUR` solo lo hace quien soltó la última ficha, y después no hay más acciones. |
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
| `racing_full_ok.json` | race-003 | Las 8 acciones de racing y una pausa. p2 cierra las 3 vueltas en 1°, sin choques ni penalizaciones (**WIRE_TO_WIRE** y **CLEAN_RACE**) y le saca una vuelta a p4 (el más lento). p1 choca con p3, vuelve a la pista (`RESPAWN`) y recibe 5 s por cortar camino. p3 choca con p1 y abandona (`DNF`, no está en el ranking). Ranking `[p2, p1, p4]`. |
| `fighting_match_ok.json` | fight-001 | Mejor de 3, Scorpion (p1) vs Sub-Zero (p2), las 6 acciones. Round 1 de p1 sin recibir daño (vida 100 → **FLAWLESS_VICTORY**); round 2 empatado por tiempo (`ROUND_DRAW`, los dos con vida 35); round 3 de p1 con vida 25 gracias a un combo de 6 golpes (**COMBO_MASTER**). p1 gana 2-0. |
| `fighting_comeback.json` | fight-002 | Mejor de 3, Liu Kang (p3) vs Kitana (p4). p3 gana el round 1 por tiempo (vida 60); p4 gana los rounds 2 y 3 → **COMEBACK**. p4 hace un combo de 5 golpes en el round 2 → **COMBO_MASTER**. |
| `connect4_match_ok.json` | c4-001 | Persona contra persona, con una pausa. p1 deja pasar un turno (`TURN_TIMEOUT`) y p2 pide una pista. p1 arma 2 amenazas y p2 las tapa las dos (**PERFECT_DEFENSE**); p2 gana en diagonal con su ficha 11. |
| `connect4_vs_bot.json` | c4-002 | p1 contra la IA (`cpu`, dificultad media). p1 se equivoca, deshace la ficha de la IA y la suya (dos `UNDO`), pide una pista y arma dos amenazas a la vez (**DOUBLE_THREAT**). La IA tapa una y p1 gana en horizontal con 4 fichas en el tablero (**QUICK_WIN**). |
| `connect4_draw.json` | c4-003 | p3 contra p4: tablero lleno (42 fichas) sin 4 en línea → empate (`winner: null`). p3 crea 2 amenazas y p4 las tapa las dos (**PERFECT_DEFENSE**). |
| `tetris_match_ok.json` | tetris-001 | p1 desde el nivel 1: dos tetris seguidos (back-to-back, usando `HOLD` para guardar una O), sube al nivel 2, hace un T-spin doble y pierde con la pila en 20 (`TOP_OUT`). 3200 puntos, 10 líneas. 12 de las 13 acciones (falta `ALL_CLEAR`). |
| `tetris_perfect_clear.json` | tetris-002 | p3 desde el nivel 5, con pausa. 3 T-spins (0, 1 y 2 líneas), un tetris que deja el tablero vacío (`ALL_CLEAR`), niveles 6 y 7, termina en `TOP_OUT`. |
| `racing_invalid_events.json` | race-997 | 8 eventos con `data` inválida + 10 que rompen validaciones de contexto. |
| `fighting_invalid_events.json` | fight-997 | 7 + 7. |
| `connect4_invalid_events.json` | c4-997 | 6 + 13 (incluye un tercer jugador, jugadas fuera de turno y una ficha flotando). |
| `tetris_invalid_events.json` | tetris-997 | 7 + 4. |
| `invalid_missing_matchid.json` | — | Lote sin `matchId` → `400`. |
| `invalid_bad_timestamp.json` | race-998 | Un `timestamp` que no parsea → `400`. |

En los `*_invalid_events.json`, el `eventId` dice qué se espera: todo evento `bad-data-*` falla el schema, todo evento `bad-ctx-*` rompe una validación de contexto, y **solo esos** se descartan. El resto de la partida es válido y se analiza normal. Un test puede comprobarlo así: los descartados de Scala son exactamente los `eventId` que empiezan con `bad-`.

Probar contra Scala:

```powershell
curl.exe -X POST http://localhost:8081/analyze -H "Content-Type: application/json" --data-binary "@contracts/fixtures/racing_match_ok.json"
```
