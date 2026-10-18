# Módulo de ingestión (Go)

*A cargo de Dylan Mendez Zamora.*

El módulo `ingestion-go/` es la puerta de entrada de GameStats. Recibe los eventos de las partidas por HTTP, valida cada uno, guarda el estado de cada partida y, cuando una partida termina, envía el lote completo al servicio de análisis en Scala (`POST /analyze`).

Go **no calcula estadísticas**: esa parte es de Scala. Tampoco interpreta el campo `data` de los eventos, que se reenvía intacto.

- [Qué hace](#qué-hace)
- [Juegos](#juegos)
- [Cómo correrlo](#cómo-correrlo)
- [Endpoints](#endpoints)
- [Validación](#validación)
- [Concurrencia](#concurrencia)
- [Envío a Scala](#envío-a-scala)
- [Simulador](#simulador)
- [Agregar un juego nuevo](#agregar-un-juego-nuevo)
- [Estructura del código](#estructura-del-código)

---

## Qué hace

1. **Recibe** eventos en `POST /events`, de uno en uno (`{...}`) o en lote (`[...]`).
2. **Valida** cada evento por separado. Un evento inválido se rechaza con su motivo y se anota en `logs/rejected/rejected.jsonl`, sin afectar a los demás eventos de la misma petición.
3. **Reparte** los eventos válidos por partida. Cada partida tiene su propia goroutine que acumula sus eventos.
4. **Guarda el estado** de cada partida (estado, cantidad de eventos, jugadores conectados) para consultarlo en `GET /matches`.
5. **Arma el lote** cuando llega `MATCH_FINISHED`: ordena los eventos por `timestamp` y calcula `startedAt`, `finishedAt` y `players`.
6. **Envía el lote a Scala** con reintentos. La partida queda como `enviada` o `error_envio`.

Además incluye un **simulador** (`cmd/simulator`) que genera partidas realistas de los 4 juegos (racing, fighting, connect4 y tetris) y las manda al servidor, para probar el sistema completo.

Solo usa la librería estándar de Go (`net/http`, `encoding/json`, `sync`, `sort`, `time`, `math/rand`, `flag`). No tiene dependencias externas.

## Juegos

GameStats trabaja con 4 juegos:

| `gameId` | Qué es | Acciones (`PLAYER_ACTION`) | Eventos en total |
|---|---|---|---|
| `racing` | Carrera arcade por vueltas y posiciones | `LAP_COMPLETED`, `POSITION_CHANGED`, `OVERTAKE`, `LAPPED`, `COLLISION`, `RESPAWN`, `PENALTY`, `DNF` (8) | 16 |
| `fighting` | Pelea 1 vs 1 por rounds (estilo Street Fighter / Mortal Kombat básico) | `ATTACK`, `BLOCK`, `COMBO`, `KNOCKDOWN`, `ROUND_WON`, `ROUND_DRAW` (6) | 14 |
| `connect4` | 4 en línea, entre dos personas o contra la IA | `DISC_DROPPED`, `THREAT_CREATED`, `THREAT_BLOCKED`, `CONNECT_FOUR`, `TURN_TIMEOUT`, `UNDO`, `HINT_USED` (7) | 15 |
| `tetris` | Tetris clásico de un jugador | `PIECE_SPAWNED`, `PIECE_MOVED`, `PIECE_ROTATED`, `SOFT_DROP`, `HARD_DROP`, `HOLD`, `PIECE_PLACED`, `LINES_CLEARED`, `T_SPIN`, `ALL_CLEAR`, `LEVEL_UP`, `STACK_HEIGHT`, `TOP_OUT` (13) | 21 |

Además de sus acciones, los 4 juegos comparten 8 tipos generales: `MATCH_STARTED`, `MATCH_FINISHED`, `MATCH_PAUSED`, `MATCH_RESUMED`, `PLAYER_JOINED`, `PLAYER_LEFT`, `PLAYER_DISCONNECTED` y `PLAYER_RECONNECTED`. En total son 42 eventos distintos (8 generales y 34 acciones), y ninguna acción se repite entre juegos.

El catálogo lo define el contrato compartido con Scala ([`contracts/README.md`](../contracts/README.md)). El `data` exacto de cada acción está en `contracts/event.schema.json`. En Go, los juegos y sus acciones se declaran en un solo lugar: `GameActions`, en `internal/validation/catalog.go`.

## Cómo correrlo

Requisitos: Go 1.24. Los comandos se corren dentro de `ingestion-go/`.

```powershell
# Servidor (puerto 8080 por defecto)
go run ./cmd/ingestion

# En otra terminal: el simulador
go run ./cmd/simulator -target http://localhost:8080

# Pruebas
go vet ./...
go test ./...
go test -race ./...   # necesita CGO (gcc); en Windows sin gcc se puede correr en Docker
```

Para levantar todo el sistema (Go + Scala) con Docker, ver el [README principal](../README.md#cómo-correrlo).

### Variables de entorno

Se leen al arrancar (`internal/config/config.go`). Si una variable viene vacía se usa su valor por defecto. Si trae un valor inválido, como `WORKERS=abc`, el servidor no arranca y explica por qué.

| Variable | Por defecto | Para qué |
|---|---|---|
| `PORT` | `8080` | Puerto HTTP del servidor |
| `SCALA_URL` | `http://localhost:8081` | Dirección del servicio de Scala (en Docker: `http://analytics:8081`) |
| `WORKERS` | `4` | Cuántas goroutines envían lotes a Scala en paralelo |
| `IDLE_TIMEOUT` | `2m` | Tiempo sin eventos tras el cual una partida se marca como `abandonada` (formato de Go: `30s`, `2m`, `1h`) |

Ejemplo en PowerShell:

```powershell
$env:PORT="9090"; $env:WORKERS="2"; go run ./cmd/ingestion
```

## Endpoints

| Método y ruta | Qué hace |
|---|---|
| `GET /health` | Responde `{"status":"ok"}` |
| `POST /events` | Recibe uno o varios eventos, valida cada uno y manda los válidos al pipeline |
| `GET /matches` | Estado de todas las partidas |

Las rutas se registran con método (`"POST /events"`, disponible desde Go 1.22), así que otro método recibe `405` automáticamente.

### `POST /events`

Siempre responde `200` con el detalle de lo aceptado, lo rechazado y los avisos, aunque se rechacen todos los eventos:

```powershell
curl.exe -X POST http://localhost:8080/events -H "Content-Type: application/json" --data-binary "@eventos.json"
```

```json
{"received":3,"accepted":2,
 "rejected":[{"eventId":"x-e3","reason":"la acción \"TOP_OUT\" no existe en el juego \"racing\" (es una acción de \"tetris\")"}],
 "warnings":[{"eventId":"x-e2","reason":"acción desconocida \"BOOST_JUMP\" en el juego \"racing\" (se acepta)"}]}
```

`warnings` solo aparece cuando algún evento aceptado trae una acción que no está en el catálogo (ver [Acciones desconocidas](#acciones-desconocidas)).

Solo responde con error si el cuerpo completo no sirve:

| Caso | Respuesta |
|---|---|
| El cuerpo no es JSON válido | `400` `{"error":"JSON inválido: ..."}` |
| El cuerpo pasa de 1 MB (`http.MaxBytesReader`) | `413` |

Los fixtures de `contracts/fixtures/` son lotes completos (el formato que Go le manda a Scala). Para enviar sus eventos a Go, primero se extrae el arreglo `events`:

```powershell
$batch = Get-Content ..\contracts\fixtures\racing_match_ok.json -Raw | ConvertFrom-Json
ConvertTo-Json -InputObject $batch.events -Depth 10 | Out-File $env:TEMP\events.json -Encoding utf8
curl.exe -X POST http://localhost:8080/events -H "Content-Type: application/json" --data-binary "@$env:TEMP\events.json"
```

(PowerShell 5.1 agrega una marca BOM al inicio del archivo. El servidor la quita antes de leer el JSON.)

### `GET /matches`

```json
[{"matchId":"x","gameId":"racing","status":"en_curso","eventCount":2,"connectedPlayers":["p1"]}]
```

Estados de una partida:

```
en_curso ──MATCH_FINISHED──▶ terminada ──Scala responde 2xx──▶ enviada
   │                              └──────fallan los reintentos──▶ error_envio
   └──IDLE_TIMEOUT sin eventos──▶ abandonada (no se envía a Scala)
```

Los jugadores conectados se actualizan con `PLAYER_JOINED` y `PLAYER_RECONNECTED` (entran) y con `PLAYER_LEFT` y `PLAYER_DISCONNECTED` (salen).

## Validación

`validation.Validate(event)` (`internal/validation/validator.go`) revisa las reglas en orden y devuelve la **primera** que falla. Ese mensaje es el motivo del rechazo.

| # | Regla | Ejemplo de motivo |
|---|---|---|
| 1 | Tiene `eventId`, `timestamp`, `gameId`, `gameVersion`, `matchId` y `type` | `falta el campo matchId` |
| 2 | `timestamp` en RFC 3339 | `timestamp no está en formato RFC 3339: "ayer a las 2"` |
| 3 | `gameId` existe en el catálogo | `gameId desconocido: "chess"` |
| 4 | `type` es uno de los 9 tipos conocidos (los 8 generales y `PLAYER_ACTION`) | `type desconocido: "PLAYER_DANCED"` |
| 5 | Tiene `playerId`, salvo en los eventos de la partida completa: `MATCH_STARTED`, `MATCH_FINISHED`, `MATCH_PAUSED` y `MATCH_RESUMED` | `falta el campo playerId (obligatorio en PLAYER_LEFT)` |
| 6 | En `PLAYER_ACTION` hay una `action`, y no es una acción de **otro** juego del catálogo | `la acción "CONNECT_FOUR" no existe en el juego "racing" (es una acción de "connect4")` |
| 7 | `action` solo aparece en `PLAYER_ACTION` (acordado con Danny en el contrato) | `action solo se permite en PLAYER_ACTION (llegó en PLAYER_JOINED)` |

Hay tres motivos de rechazo más, que no vienen de `Validate`:

- **Formato:** el evento no se puede decodificar, por ejemplo `"timestamp": 5`. Se rechaza ese evento y los demás siguen.
- **Partida ya finalizada:** llega un evento de una partida que ya recibió `MATCH_FINISHED`. Su lote ya se armó, así que el evento se rechaza (ver [Concurrencia](#concurrencia)).
- **Partida abandonada por inactividad:** llega un evento de una partida que se abandonó por `IDLE_TIMEOUT`.

### Acciones desconocidas

El enunciado pide que un evento nuevo se pueda incorporar sin modificar todo el sistema. Por eso la regla 6 distingue dos casos:

| La acción... | Qué hace Go | Por qué |
|---|---|---|
| es de **otro** juego (por ejemplo `TOP_OUT` en racing) | La **rechaza** | Se sabe que está mal: casi seguro es un error de la fuente de eventos |
| no existe en **ningún** juego (por ejemplo `BOOST_JUMP`) | La **acepta**, la anota como aviso en `logs/unknown/unknown_actions.jsonl` y la devuelve en `warnings` | Puede ser un evento nuevo. Llega a Scala, que la cuenta como acción desconocida (`UnknownAction`) sin afectar el resto |

Un `gameId` desconocido se sigue rechazando: un juego nuevo sí necesita su entrada en el catálogo y su analizador en Scala. `validation.UnknownAction(e)` le dice al handler cuándo avisar. El log de avisos usa el mismo `rejected.Logger` (con mutex) que el de rechazados, en otro archivo.

### Quién valida qué

Go **no** revisa el contenido de `data`. Esa división de responsabilidades está acordada con Scala en [`contracts/README.md` → "Quién valida qué"](../contracts/README.md#quién-valida-qué):

> **Go** valida cada evento al recibirlo y rechaza solo ese evento [...] Go **no** revisa `data`. [...] **Scala** rechaza el lote completo (`400`) solo por errores del lote [...] Si una acción conocida trae `data` mal formada o rompe una validación de contexto, Scala **descarta ese evento**, lo registra en su log y analiza la partida igual.

Las validaciones de contexto son reglas que dependen de otros eventos de la partida. Por ejemplo: en connect4 cada ficha cae en la primera fila libre de su columna y solo juega quien tiene el turno, en fighting cada round tiene un solo resultado, y en ningún juego hay acciones durante una pausa. Go no las revisa porque para eso tendría que entender cada juego. Por eso los fixtures `*_invalid_events.json` de Danny pasan completos la validación de Go: sus eventos `bad-*` los descarta Scala.

### Log de rechazados

Cada rechazo agrega una línea a `logs/rejected/rejected.jsonl` (formato JSON Lines) con la hora, el motivo y el evento tal como llegó:

```json
{"time":"2026-10-15T02:21:27Z","reason":"la acción \"TOP_OUT\" no existe en el juego \"racing\" (es una acción de \"tetris\")","event":{"eventId":"u2","timestamp":"2026-10-14T10:00:01Z","gameId":"racing","gameVersion":"1.0","matchId":"r-x","playerId":"p1","type":"PLAYER_ACTION","action":"TOP_OUT","data":{}}}
```

`net/http` atiende cada petición en su propia goroutine, así que varios handlers pueden escribir a la vez. La escritura está protegida con un `sync.Mutex` (`internal/rejected/log.go`) para que las líneas no se mezclen.

## Concurrencia

Es la parte central del módulo (`internal/pipeline/`). Funciona como una oficina de correos: una ventanilla recibe todo, un clasificador reparte por destino, cada destino junta lo suyo y unos carteros lo entregan.

```
POST /events ──▶ [in] ──▶ router ──┬──▶ goroutine partida 1 ──┐
  (handler)                        ├──▶ goroutine partida 2 ──┤
                                   └──▶ goroutine partida N ──┤
                                                              ▼
                                                     [out: Batch] (buffer 16)
                                                              ▼
                                                  WORKERS senders ──▶ Scala
```

| Pieza | Qué hace |
|---|---|
| **Handler** (`api/handler.go`) | Valida el evento y llama a `Submit`, que lo pone en el canal `in` y espera la respuesta del router. |
| **Router** (una goroutine, `dispatcher.go`) | Lee de `in`. Si la partida ya terminó o se abandonó, rechaza el evento. Si no, lo registra en el store y lo pasa al canal de su partida, creando la goroutine de la partida si es la primera vez que la ve. Además revisa periódicamente las partidas inactivas (ver [Abandono por inactividad](#abandono-por-inactividad)). |
| **Goroutine por partida** (`collect`) | Acumula los eventos de **una** partida. Cuando su canal se cierra después de `MATCH_FINISHED`, ordena los eventos con `sort.SliceStable` por `timestamp`, arma el `Batch` y lo envía a `out`. Después termina. |
| **Senders** (`WORKERS` goroutines, `sender.go`) | Leen de `out` y envían cada lote a Scala. Todos leen del mismo canal, y Go garantiza que cada lote lo recibe un solo sender. |

### Por qué channels en unas partes y mutex en otras

La regla que se siguió: **channels para pasar datos y coordinar el flujo; mutex para el estado compartido que se consulta.**

- **Channels:** los eventos *se mueven* de una goroutine a otra (handler → router → partida → sender). Cada dato tiene un solo dueño a la vez, así que no hay nada que proteger.
- **Mapa `matchId → canal`:** lo usa **solo el router**, así que no necesita mutex.
- **Store de partidas** (`state/match_store.go`): lo escriben el router y los senders y lo leen los handlers de `GET /matches`, cada uno en su propia goroutine. Es estado compartido, así que usa `sync.RWMutex`: varias lecturas pueden ir a la vez (`RLock`) y una escritura (`Lock`) espera a quedar sola. `List()` devuelve **copias**, para que nadie lea el estado interno fuera del lock.
- **Log de rechazados:** un archivo que escriben varios handlers a la vez, así que usa `sync.Mutex`.

### Quién cierra cada channel

Todo channel tiene un dueño que lo cierra, y es siempre quien envía en él. Así nunca se envía a un channel cerrado (eso haría `panic`).

| Channel | Quién lo cierra | Cuándo |
|---|---|---|
| `in` | `Dispatcher.Close()`, llamado por `main` | Al apagar, **después** de que el servidor HTTP dejó de atender peticiones (ver [Apagado ordenado](#apagado-ordenado)) |
| Canal de cada partida | El router, que es el único que les envía | Al reenviar `MATCH_FINISHED`, al abandonar la partida por inactividad o al apagar |
| `out` | El router | Después de que **todas** las goroutines de partida terminaron (las espera con un `sync.WaitGroup`), porque ellas son las que envían a `out` |
| `reply` de cada `Submit` | Nadie: tiene buffer de 1 y el recolector de basura lo libera | — |

Como cerrar `out` hace terminar el `for b := range batches` de cada sender, el apagado se propaga en cadena y no quedan goroutines colgadas.

### Eventos tardíos

Si llega un evento de una partida que ya recibió `MATCH_FINISHED`, el router lo detecta consultando el store (el estado ya no es `en_curso`). Responde `ErrMatchFinished` y **no** crea una goroutine nueva para esa partida:

```json
{"received":1,"accepted":0,"rejected":[{"eventId":"x-e10","reason":"partida ya finalizada"}]}
```

`Submit` espera la respuesta del router porque solo él conoce el orden real de llegada. Así `POST /events` puede decir con exactitud qué eventos se rechazaron.

### Abandono por inactividad

Si una partida pasa `IDLE_TIMEOUT` (por defecto 2 minutos) sin recibir eventos, se marca como `abandonada` y **no se envía a Scala**, porque está incompleta. Se mide con la hora real del servidor al recibir cada evento, no con el `timestamp` del evento.

El router lo resuelve sin goroutines ni locks extra. Guarda la hora del último evento de cada partida y usa un `select` que espera dos cosas a la vez: un evento nuevo o el tic de un `time.Ticker`. En cada tic recorre sus partidas abiertas y, a las inactivas, les cierra el canal y las marca como abandonadas. Como el canal se cerró sin `MATCH_FINISHED`, la goroutine de la partida termina sin armar lote. El router sigue siendo el único dueño del mapa y de los canales.

El tic ocurre cada medio `IDLE_TIMEOUT`, así que una partida se abandona entre 1 y 1,5 veces `IDLE_TIMEOUT` después de su último evento. Un evento que llegue después se rechaza:

```json
{"received":1,"accepted":0,"rejected":[{"eventId":"y-e3","reason":"partida abandonada por inactividad"}]}
```

### Apagado ordenado

Con Ctrl+C, o con `docker stop` (que manda la señal SIGTERM), el servidor no se corta de golpe. `main.go` usa `signal.NotifyContext` para enterarse de la señal y apaga en este orden:

1. **`srv.Shutdown`**: deja de aceptar peticiones y espera a que terminen las que están en curso. No tiene límite de tiempo a propósito: si el pipeline se cerrara mientras un handler todavía puede llamar a `Submit`, ese handler enviaría a un channel cerrado (`panic`).
2. **`dispatcher.Close()`**: cierra `in`. El router cierra las partidas abiertas (las que no terminaron no se envían), espera a sus goroutines y cierra `out`.
3. **`senders.Wait()`**: un `sync.WaitGroup` espera a que los senders envíen a Scala los lotes que quedaban pendientes y terminen.

En el log se ve así:

```
apagando: no se aceptan más eventos
apagado: 1 partidas sin terminar no se envían a Scala
apagado completo
```

Un segundo Ctrl+C cierra el programa de inmediato. Si Scala está caído al apagar, los reintentos pueden alargar el apagado: `docker stop` espera 10 s y después termina el proceso a la fuerza.

### Cómo se comprueba

- `go test -race ./...` corre todos los tests con el detector de data races. También corre en el CI (`.github/workflows/ci-go.yml`).
- `pipeline/dispatcher_test.go` mezcla los eventos de dos partidas y comprueba que salen **dos** lotes separados y ordenados por `timestamp` (`TestTwoInterleavedMatches`). También prueba el evento tardío, los empates de `timestamp`, el cierre con una partida sin terminar, muchos `Submit` en paralelo (`TestConcurrentSubmit`), el abandono por inactividad (`TestIdleMatchIsAbandoned`) y que una partida activa no se abandone (`TestActiveMatchIsNotAbandoned`).
- `pipeline/sender_test.go` comprueba que, al cerrar el pipeline, los lotes pendientes se envían igual a un Scala lento antes de que los senders terminen (`TestCloseSendsPendingBatches`).
- El simulador manda cada partida desde su propia goroutine, así que los eventos llegan intercalados al servidor.

## Envío a Scala

`internal/client/scala_client.go` hace `POST {SCALA_URL}/analyze` con el lote:

```json
{
  "matchId": "race-001",
  "gameId": "racing",
  "gameVersion": "1.0",
  "startedAt": "2026-10-05T14:00:00Z",
  "finishedAt": "2026-10-05T14:04:10Z",
  "players": ["p1", "p2", "p3"],
  "events": [ "...todos los eventos, ordenados por timestamp..." ]
}
```

- `startedAt` es el timestamp de `MATCH_STARTED`, o el del primer evento si nunca llegó. `finishedAt` es el de `MATCH_FINISHED`.
- `players` son los `playerId` distintos, ordenados.
- El `http.Client` tiene un timeout de 10 s, para que un Scala colgado no deje al sender esperando para siempre.

| Respuesta de Scala | Qué hace Go |
|---|---|
| `2xx` | La partida queda `enviada` |
| `4xx` | No reintenta (el lote está mal y mandarlo otra vez no lo arregla). Queda `error_envio` |
| `5xx`, timeout o Scala caído | Reintenta hasta 3 veces, esperando 1 s, 2 s y 4 s. Si fallan todos, queda `error_envio` |

Ejemplo real del log con Scala apagado:

```
sender 2: enviando x (3 eventos) a Scala
envío de x falló (... connection refused ...); reintento 1 de 3 en 1s
envío de x falló (... connection refused ...); reintento 2 de 3 en 2s
envío de x falló (... connection refused ...); reintento 3 de 3 en 4s
sender 2: ERROR al enviar x: ...
```

Mientras un sender espera un reintento, los otros `WORKERS - 1` siguen enviando las demás partidas.

## Simulador

`cmd/simulator` simula la fuente externa de eventos.

```powershell
go run ./cmd/simulator -racing 4 -fighting 4 -connect4 4 -tetris 4 -seed 42 -corrupt 3 -target http://localhost:8080
```

| Opción | Por defecto | Qué hace |
|---|---|---|
| `-racing N` | `4` | Carreras aleatorias (además de las guionizadas) |
| `-fighting N` | `4` | Peleas aleatorias |
| `-connect4 N` | `4` | Partidas aleatorias de connect4 |
| `-tetris N` | `4` | Partidas aleatorias de tetris |
| `-seed S` | `42` | Semilla. La misma semilla genera exactamente las mismas partidas |
| `-corrupt P` | `3` | Porcentaje de eventos a los que se les agrega una copia dañada a propósito |
| `-target URL` | `http://localhost:8080` | Servidor de ingestión (en Docker: `http://ingestion:8080`) |

- **Determinista:** todo sale de un solo `rand.New(rand.NewSource(seed))` y de una fecha base fija. No se usa `time.Now()` ni el `rand` global, y los jugadores se eligen de slices y no de maps, porque en Go el orden en que se recorre un map es aleatorio. Los `matchId` llevan la semilla (`race-s42-001`) para poder correr varias semillas contra el mismo servidor.
- **Concurrente:** cada partida se envía en su propia goroutine, en grupos de 5 eventos, para que los eventos de distintas partidas lleguen mezclados.
- **Eventos dañados:** se agregan como **copias** con `eventId` terminado en `-bad`, cada una rompiendo una regla de validación. Se usan copias para que la partida original siga completa: si se dañara un `DISC_DROPPED`, el tablero de connect4 quedaría distinto y lo que viene después no tendría sentido. Para la regla 6 se elige una acción del catálogo que pertenece a **otro** juego (por ejemplo `DISC_DROPPED` en tetris).

### Cómo se genera cada juego

Cada juego tiene un tipo que escribe la partida evento por evento y lleva el estado necesario para que los datos sean coherentes. Lo usan tanto las partidas aleatorias como los escenarios guionizados.

| Juego | Archivo | Cómo funciona |
|---|---|---|
| Racing | `racing.go` | Una carrera se describe con un `racePlan` (tiempos de vuelta, choques, penalizaciones, abandono, pausa) y `buildRace` la convierte en eventos. Las posiciones se calculan con el tiempo acumulado más las penalizaciones. Los adelantamientos (`OVERTAKE`) y las vueltas de ventaja (`LAPPED`) salen de comparar tiempos. De 3 a 6 pilotos y de 3 a 5 vueltas. |
| Fighting | `fighting.go` | `fightGame` lleva la vida de cada luchador, así `remainingHealth` coincide con el daño recibido. Al mejor de 1, 3 o 5 rounds, con `punch`/`kick`, combos y derribos. Un round termina por KO, por tiempo o empatado (`ROUND_DRAW` por doble KO o por tiempo con la misma vida); con empates, una pelea puede terminar sin ganador. |
| Connect4 | `connect4.go` | `c4Game` lleva un tablero real de 7×6. Después de cada ficha calcula el 4 en línea, las amenazas nuevas (casillas vacías que le completarían 4 al jugador) y los bloqueos, así esos eventos siempre son ciertos. Guarda una foto del estado antes de cada ficha para poder deshacerla (`UNDO`). Los jugadores eligen con una IA simple (si puede ganar, gana; si no, tapa al rival; si no, juega al centro) con un porcentaje de errores según la dificultad. La mitad de las partidas son humano contra la IA (`cpu`) y la otra mitad dos humanos. |
| Tetris | `tetris.go` | `tetrisGame` no simula el tablero celda por celda: lleva la altura de la pila, las líneas, el nivel, el combo y el back-to-back. Con eso salen solos los puntos, `LEVEL_UP` (cada 10 líneas), `ALL_CLEAR` (la pila queda en 0) y `TOP_OUT` (la pila llega a 20). Un solo jugador. |

### Reglas que respeta

Scala descarta los eventos que rompen una validación de contexto. El simulador las cumple **por construcción**, sin generar eventos para después filtrarlos:

- **Todos los juegos:** ninguna acción cae dentro de una pausa y toda referencia a otro jugador (`targetId`, `otherId`, `attackerId`, ...) es a otro jugador de la partida.
- **Racing:**
  - `RESPAWN` va 1 o 2 s después de un `COLLISION` en el que participó el piloto (el que choca o el chocado), uno por choque.
  - Después de un `DNF` no hay más eventos de ese piloto, y no aparece en el `ranking`.
- **Fighting:**
  - Un golpe fallado lleva `damage: 0` y solo hay `punch` o `kick`.
  - Cada round tiene un solo resultado (`ROUND_WON` o `ROUND_DRAW`) y después del round que decide la pelea no hay más acciones.
- **Connect4:**
  - Juegan dos, por turnos, empezando el primero que entró. Cada ficha cae en la primera fila libre.
  - `THREAT_BLOCKED` va justo después de la ficha que tapa la amenaza, y `CONNECT_FOUR` justo después de la ficha ganadora, como última acción.
  - `UNDO` solo lo hace el humano contra la IA, y deshace la última ficha del tablero.
- **Tetris:**
  - Un solo jugador.
  - `LEVEL_UP` solo cuando el nivel sube.
  - Nada después de `TOP_OUT`.

Los tests del simulador (`simulator_test.go`) vuelven a revisar todas estas reglas en 50 semillas distintas. En connect4, el test reconstruye el tablero con su propio código de 4 en línea, independiente de `c4Game`. También comprueban que las partidas aleatorias de cada juego usan **todas** las acciones de su catálogo.

Contra el Scala real, `go run ./cmd/simulator -seed 42` manda 30 partidas y 2538 eventos: Go rechaza los 57 dañados a propósito y Scala no descarta ninguno de los demás.

### Escenarios guionizados

Se generan siempre y no dependen del azar. Sirven para probar las reglas de Scala y para la demo. Hay al menos un escenario por cada patrón que detecta Scala:

| Escenario (seed 42) | Qué muestra | Patrones en Scala |
|---|---|---|
| `race-s42-comeback` | p4 sale último y gana | COMEBACK, CLEAN_RACE |
| `race-s42-penalty` | p1 es el más rápido en pista, pero una penalización de 5 s le hace perder | CLEAN_RACE (de p2) |
| `race-s42-clean` | p1 gana sin choques ni penalizaciones; los demás sí los tienen | CLEAN_RACE, WIRE_TO_WIRE |
| `race-s42-wire` | p1 termina primero todas las vueltas, sin ningún cambio de posición | WIRE_TO_WIRE, CLEAN_RACE |
| `race-s42-dnf` | p3 choca contra p1 y abandona (`DNF`); p1, el chocado, vuelve a la pista; la carrera se pausa; p2 gana desde el 3° lugar | COMEBACK, CLEAN_RACE |
| `fight-s42-flawless` | p1 gana el round 1 sin recibir daño y la pelea 2-0 | FLAWLESS_VICTORY |
| `fight-s42-comeback` | p1 gana el round 1 por tiempo y p2 gana los rounds 2 y 3, con una pausa | COMEBACK |
| `fight-s42-combo` | Round 1 en doble KO (`ROUND_DRAW`), un combo de 6 golpes y p1 gana 2-0; usa las 6 acciones | COMBO_MASTER |
| `c4-s42-quick` | p1 gana con 4 fichas (vertical) | QUICK_WIN |
| `c4-s42-double` | p1 contra la IA: deshace una mala jugada (`UNDO`), pide una pista y arma dos amenazas a la vez | DOUBLE_THREAT, QUICK_WIN |
| `c4-s42-defense` | p2 tapa las dos amenazas de p1; p1 pierde un turno por tiempo y p2 gana | PERFECT_DEFENSE, QUICK_WIN |
| `c4-s42-draw` | El tablero se llena (42 fichas) sin 4 en línea | sin ganador |
| `tetris-s42-b2b` | Dos tetris seguidos (con `HOLD`) y subida al nivel 2 | BACK_TO_BACK |
| `tetris-s42-perfect` | 3 T-spins y un tetris que deja el tablero vacío, con pausa | PERFECT_CLEAR, T_SPIN_MASTER |

La columna de patrones es lo que devolvió el Scala real para cada escenario. En `c4-s42-draw` Scala también marca DOUBLE_THREAT y PERFECT_DEFENSE para los dos jugadores, porque en 42 fichas ambos arman y tapan amenazas.

Con la configuración por defecto se generan 30 partidas: 14 guionizadas y 4 aleatorias de cada juego.

## Agregar un juego nuevo

Para la ingestión basta con **editar un solo lugar**, el catálogo de `internal/validation/catalog.go`:

```go
var GameActions = map[string]map[string]bool{
    "racing":   {"LAP_COMPLETED": true, "POSITION_CHANGED": true, /* ... */},
    "fighting": {"ATTACK": true, "COMBO": true, /* ... */},
    "connect4": {"DISC_DROPPED": true, "CONNECT_FOUR": true, /* ... */},
    "tetris":   {"PIECE_SPAWNED": true, "LINES_CLEARED": true, /* ... */},
    "tennis":   {"SERVE": true, "POINT_WON": true}, // juego nuevo
}
```

Nada más cambia en Go: la validación lee el catálogo, el pipeline agrupa por `matchId` sin importar el juego, y `data` se reenvía sin interpretarlo. Así se fue football y entró connect4, y así se achicaron racing y fighting: en el servidor solo cambió `catalog.go`; el handler, el pipeline, el store y el cliente de Scala quedaron igual.

Para una acción nueva de un juego que ya existe no hace falta ni eso: se acepta como acción desconocida (con un aviso) y llega a Scala. Agregarla al catálogo solo hace que deje de avisarse.

Para que el simulador genere partidas del juego nuevo hay que agregar su generador (un archivo como `connect4.go`), un campo en `simulator.Counts` y una opción en `cmd/simulator/main.go`. Del lado de Scala, el juego necesita su propio analizador (ver el [README principal](../README.md)).

## Estructura del código

```
ingestion-go/
├── cmd/
│   ├── ingestion/main.go        arranca el servidor y conecta las piezas
│   └── simulator/main.go        opciones del simulador y resumen del envío
├── internal/
│   ├── model/event.go           Event y Batch
│   ├── api/handler.go           /health, /events, /matches
│   ├── validation/              catálogo de juegos (catalog.go) y Validate
│   ├── state/match_store.go     estado de las partidas (sync.RWMutex)
│   ├── pipeline/                router y goroutines de partida (dispatcher.go), senders (sender.go)
│   ├── client/scala_client.go   POST /analyze con reintentos
│   ├── config/config.go         variables de entorno
│   ├── rejected/log.go          rejected.jsonl y unknown_actions.jsonl (sync.Mutex)
│   └── simulator/               generadores de los 4 juegos, escenarios, eventos dañados
├── testdata/invalid_events.json un evento inválido por regla, para los tests
└── Dockerfile                   imagen con los dos programas (/app/ingestion y /app/simulator)
```

Cada paquete de `internal/` tiene sus tests junto al código (`*_test.go`).
