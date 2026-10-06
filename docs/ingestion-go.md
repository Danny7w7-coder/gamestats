# Módulo de ingestión (Go)

*A cargo de Dylan Mendez Zamora.*

El módulo `ingestion-go/` es la puerta de entrada de GameStats. Recibe los eventos de las partidas por HTTP, valida cada uno, guarda el estado de cada partida y, cuando una partida termina, envía el lote completo al servicio de análisis en Scala (`POST /analyze`).

Go **no calcula estadísticas**: esa parte es de Scala. Tampoco interpreta el campo `data` de los eventos, que se reenvía intacto.

- [Qué hace](#qué-hace)
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

Además incluye un **simulador** (`cmd/simulator`) que genera partidas realistas de racing y football y las manda al servidor, para probar el sistema completo.

Solo usa la librería estándar de Go (`net/http`, `encoding/json`, `sync`, `sort`, `time`, `math/rand`, `flag`). No tiene dependencias externas.

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
| `IDLE_TIMEOUT` | `2m` | Inactividad tras la cual una partida se marca como `abandonada` *(en desarrollo)* |

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

Siempre responde `200` con el detalle de lo aceptado y lo rechazado, aunque se rechacen todos los eventos:

```powershell
curl.exe -X POST http://localhost:8080/events -H "Content-Type: application/json" --data-binary "@eventos.json"
```

```json
{"received":3,"accepted":2,"rejected":[{"eventId":"x-e3","reason":"la acción \"GOAL\" no existe en el juego \"racing\""}]}
```

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
                                  └──────fallan los reintentos──▶ error_envio
```

Los jugadores conectados se actualizan con `PLAYER_JOINED` y `PLAYER_RECONNECTED` (entran) y con `PLAYER_LEFT` y `PLAYER_DISCONNECTED` (salen).

## Validación

`validation.Validate(event)` (`internal/validation/validator.go`) revisa las reglas en orden y devuelve la **primera** que falla. Ese mensaje es el motivo del rechazo.

| # | Regla | Ejemplo de motivo |
|---|---|---|
| 1 | Tiene `eventId`, `timestamp`, `gameId`, `gameVersion`, `matchId` y `type` | `falta el campo matchId` |
| 2 | `timestamp` en RFC 3339 | `timestamp no está en formato RFC 3339: "ayer a las 2"` |
| 3 | `gameId` existe en el catálogo | `gameId desconocido: "chess"` |
| 4 | `type` es uno de los 7 tipos conocidos | `type desconocido: "PLAYER_DANCED"` |
| 5 | Tiene `playerId`, salvo en `MATCH_STARTED` y `MATCH_FINISHED` | `falta el campo playerId (obligatorio en PLAYER_LEFT)` |
| 6 | En `PLAYER_ACTION`, `action` existe en el catálogo **de ese juego** | `la acción "LAP_COMPLETED" no existe en el juego "football"` |
| 7 | `action` solo aparece en `PLAYER_ACTION` (acordado con Danny en el contrato) | `action solo se permite en PLAYER_ACTION (llegó en PLAYER_JOINED)` |

Hay dos motivos de rechazo más, que no vienen de `Validate`:

- **Formato:** el evento no se puede decodificar, por ejemplo `"timestamp": 5`. Se rechaza ese evento y los demás siguen.
- **Partida ya finalizada:** llega un evento de una partida que ya recibió `MATCH_FINISHED`. Su lote ya se armó, así que el evento se rechaza (ver [Concurrencia](#concurrencia)).

Go **no** revisa el contenido de `data`. Esa división de responsabilidades está acordada con Scala en [`contracts/README.md` → "Quién valida qué"](../contracts/README.md#quién-valida-qué):

> **Go** valida cada evento al recibirlo y rechaza solo ese evento [...] Go **no** revisa `data`. [...] **Scala** rechaza el lote completo (`400`) solo por errores del lote [...] Si una acción conocida trae `data` mal formada, Scala **descarta ese evento**.

### Log de rechazados

Cada rechazo agrega una línea a `logs/rejected/rejected.jsonl` (formato JSON Lines) con la hora, el motivo y el evento tal como llegó:

```json
{"time":"2026-10-06T21:52:25Z","reason":"la acción \"GOAL\" no existe en el juego \"racing\"","event":{"eventId":"x-e3","timestamp":"2026-10-05T14:01:22Z","gameId":"racing","gameVersion":"1.0","matchId":"x","playerId":"p1","type":"PLAYER_ACTION","action":"GOAL","data":{}}}
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
| **Router** (una goroutine, `dispatcher.go`) | Lee de `in`. Si la partida ya terminó, rechaza el evento. Si no, lo registra en el store y lo pasa al canal de su partida, creando la goroutine de la partida si es la primera vez que la ve. |
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
| `in` | `Dispatcher.Close()` | Al apagar el pipeline. Hoy lo usan los tests; el apagado ordenado con Ctrl+C está *en desarrollo* |
| Canal de cada partida | El router, que es el único que les envía | Al reenviar `MATCH_FINISHED`, o al apagar |
| `out` | El router | Después de que **todas** las goroutines de partida terminaron (las espera con un `sync.WaitGroup`), porque ellas son las que envían a `out` |
| `reply` de cada `Submit` | Nadie: tiene buffer de 1 y el recolector de basura lo libera | — |

Como cerrar `out` hace terminar el `for b := range batches` de cada sender, el apagado se propaga en cadena y no quedan goroutines colgadas.

### Eventos tardíos

Si llega un evento de una partida que ya recibió `MATCH_FINISHED`, el router lo detecta consultando el store (el estado ya no es `en_curso`). Responde `ErrMatchFinished` y **no** crea una goroutine nueva para esa partida:

```json
{"received":1,"accepted":0,"rejected":[{"eventId":"x-e10","reason":"partida ya finalizada"}]}
```

`Submit` espera la respuesta del router porque solo él conoce el orden real de llegada. Así `POST /events` puede decir con exactitud qué eventos se rechazaron.

### Cómo se comprueba

- `go test -race ./...` corre todos los tests con el detector de data races. También corre en el CI (`.github/workflows/ci-go.yml`).
- `pipeline/dispatcher_test.go` mezcla los eventos de dos partidas y comprueba que salen **dos** lotes separados y ordenados por `timestamp` (`TestTwoInterleavedMatches`). También prueba el evento tardío, los empates de `timestamp`, el cierre con una partida sin terminar y muchos `Submit` en paralelo (`TestConcurrentSubmit`).
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
go run ./cmd/simulator -racing 4 -football 4 -seed 42 -corrupt 3 -target http://localhost:8080
```

| Opción | Por defecto | Qué hace |
|---|---|---|
| `-racing N` | `4` | Partidas aleatorias de racing (además de las guionizadas) |
| `-football N` | `4` | Partidas aleatorias de football (además de las guionizadas) |
| `-seed S` | `42` | Semilla. La misma semilla genera exactamente las mismas partidas |
| `-corrupt P` | `3` | Porcentaje de eventos a los que se les agrega una copia dañada a propósito |
| `-target URL` | `http://localhost:8080` | Servidor de ingestión (en Docker: `http://ingestion:8080`) |

- **Determinista:** todo sale de un solo `rand.New(rand.NewSource(seed))` y de una fecha base fija. No se usa `time.Now()` ni el `rand` global. Los `matchId` llevan la semilla (`race-s42-001`) para poder correr varias semillas contra el mismo servidor.
- **Concurrente:** cada partida se envía en su propia goroutine, en grupos de 5 eventos, para que los eventos de distintas partidas lleguen mezclados.
- **Racing:** de 3 a 6 pilotos y de 3 a 5 vueltas. Cada piloto tiene un ritmo base con variación por vuelta, la posición se calcula con el tiempo acumulado y hay penalizaciones ocasionales.
- **Football:** equipos `A` y `B`, con pases, tiros, goles y faltas con tarjetas. Respeta las reglas acordadas con Danny: todo `GOAL` va precedido de un `SHOT` a puerta del mismo jugador, no hay autogoles y un jugador con tarjeta roja no genera más eventos.
- **Eventos dañados:** se agregan como **copias** con `eventId` terminado en `-bad`, cada una rompiendo una regla de validación. Se usan copias para que la partida original siga completa: si se dañara un `SHOT`, Scala recibiría un `GOAL` sin su tiro.

### Escenarios guionizados

Se generan siempre y no dependen del azar. Sirven para probar las reglas de Scala y para la demo:

| Escenario (seed 42) | Qué prueba |
|---|---|
| `race-s42-comeback` | Remontada: p4 sale último y gana |
| `race-s42-penalty` | p1 es el más rápido en pista, pero una penalización de 5 s le hace perder |
| `foot-s42-comeback` | B gana 1-0 y A da vuelta 2-1; tarjeta roja a p6; gol de p2 sin asistencia |
| `foot-s42-draw` | Empate 1-1; p3 y p6 no tiran al arco |
| `foot-s42-hattrick` | Hat-trick de p1; A gana 3-1 |

Con la configuración por defecto se generan 13 partidas: 5 guionizadas, 4 de racing y 4 de football.

## Agregar un juego nuevo

Para la ingestión basta con **editar un solo lugar**, el catálogo de `internal/validation/catalog.go`:

```go
var GameActions = map[string]map[string]bool{
    "racing":   {"LAP_COMPLETED": true, "POSITION_CHANGED": true, "PENALTY": true},
    "football": {"PASS": true, "SHOT": true, "GOAL": true, "FOUL": true},
    "tennis":   {"SERVE": true, "POINT_WON": true}, // juego nuevo
}
```

Nada más cambia en Go: la validación lee el catálogo, el pipeline agrupa por `matchId` sin importar el juego, y `data` se reenvía sin interpretarlo. Para generar partidas del juego nuevo habría que agregar su generador al simulador. Del lado de Scala, el juego necesita su propio analizador (ver el [README principal](../README.md#agregar-un-juego-nuevo)).

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
│   ├── rejected/log.go          rejected.jsonl (sync.Mutex)
│   └── simulator/               generadores de racing y football, escenarios, eventos dañados
├── testdata/invalid_events.json un evento inválido por regla, para los tests
└── Dockerfile                   imagen con los dos programas (/app/ingestion y /app/simulator)
```

Cada paquete de `internal/` tiene sus tests junto al código (`*_test.go`).
