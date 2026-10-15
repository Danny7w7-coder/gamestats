# Imperativo vs. funcional en GameStats

GameStats usa dos paradigmas a propósito: la ingestión está escrita en **Go** con un estilo imperativo y concurrente, y el análisis en **Scala** con un estilo funcional. Este documento recoge, para cada lado, las ventajas y limitaciones que aparecieron al implementarlo, con ejemplos del código del proyecto.

- [Go — ingestión (imperativo)](#go--ingestión-imperativo)
- [Scala — análisis (funcional)](#scala--análisis-funcional)

---

## Go — ingestión (imperativo)

El módulo de ingestión (`ingestion-go/`, unas 1700 líneas más unas 1400 de tests) recibe los eventos, los valida, los agrupa por partida y envía cada partida terminada a Scala. Solo usa la librería estándar de Go. Se escribió con estas reglas:

- **Estado mutable, pero con un dueño claro**: cada dato lo modifica una sola goroutine, o está protegido por un mutex.
- **Channels para pasar datos y coordinar el flujo; mutex para el estado compartido que se consulta.**
- **Errores como valores** (`error`), revisados en el momento con `if err != nil`, sin excepciones.
- **Código directo**: ciclos, `switch` y `return` temprano, sin abstracciones que no hagan falta.

El estado mutable quedó en cuatro lugares: el mapa del router (`pipeline/dispatcher.go`), el slice de eventos de cada partida (`collect`, en el mismo archivo), el store de partidas (`state/match_store.go`) y el log de rechazados (`rejected/log.go`, que también escribe el de acciones desconocidas). Los dos primeros los toca una sola goroutine; los dos últimos están protegidos con un mutex.

### Ventajas

#### 1. La ingestión es una secuencia de pasos con efectos, y el estilo imperativo la describe tal cual

Recibir un evento implica leer el cuerpo, validar, registrar el estado, reenviar, escribir en el log de rechazados y responder. Casi todo es un efecto. El handler de `POST /events` (`api/handler.go`) se lee como esa lista de pasos:

```go
for _, raw := range raws {
    var e model.Event
    if err := json.Unmarshal(raw, &e); err != nil {
        h.reject(&resp, raw, e.EventID, "el evento no tiene el formato esperado: "+err.Error())
        continue
    }
    if err := validation.Validate(e); err != nil {
        h.reject(&resp, raw, e.EventID, err.Error())
        continue
    }
    if err := h.Pipeline.Submit(e); err != nil {
        h.reject(&resp, raw, e.EventID, err.Error())
        continue
    }
    resp.Accepted++
}
```

Un evento malo se salta con `continue` y los demás siguen. Es justamente el caso que en Scala obligó a usar un `Either` dentro de otro (limitación 2 de la sección de Scala): aquí es una línea.

`Validate` (`validation/validator.go`) sigue el mismo estilo. Son siete reglas en orden y cada una devuelve su motivo apenas falla:

```go
actions, ok := GameActions[e.GameID]
if !ok {
    return fmt.Errorf("gameId desconocido: %q", e.GameID)
}
if !eventTypes[e.Type] {
    return fmt.Errorf("type desconocido: %q", e.Type)
}
```

Modificar en el lugar también simplificó el armado del lote. La goroutine de cada partida hace `events = append(events, e)` y, al terminar, ordena ese mismo slice con `sort.SliceStable`, sin crear copias.

#### 2. Las goroutines hacen barato el diseño "una por partida"

Cada partida tiene su propia goroutine, que acumula sus eventos y arma el lote al recibir `MATCH_FINISHED`. Una goroutine arranca con unos pocos KB de pila, así que crear una por partida no cuesta casi nada. Con hilos del sistema operativo este diseño sería caro, y con callbacks habría que guardar a mano el estado de cada partida entre llamadas.

En total hay cuatro lugares que lanzan goroutines: el router (`go d.route()`), una por partida (`go d.collect(...)`), `WORKERS` senders que envían a Scala (`pipeline/sender.go`) y, en el simulador, una por partida para que los eventos lleguen mezclados (`simulator/simulator.go`).

#### 3. Los channels reemplazan a los locks en el flujo de datos

Los eventos pasan de una goroutine a otra: handler → router → partida → sender. Como cada dato tiene un solo dueño a la vez, no hay nada que proteger. El router mantiene un mapa `matchId → canal de la partida` **sin mutex**, porque es la única goroutine que lo usa:

```go
for sub := range d.in {
    e := sub.event
    if status, ok := d.store.Status(e.MatchID); ok && status != state.StatusInProgress {
        sub.reply <- ErrMatchFinished // evento tardío
        continue
    }
    d.store.Record(e)

    ch, ok := matches[e.MatchID]
    if !ok {
        ch = make(chan model.Event)
        matches[e.MatchID] = ch
        wg.Add(1)
        go d.collect(ch, &wg)
    }
    ch <- e

    if e.Type == validation.TypeMatchFinished {
        close(ch)
        delete(matches, e.MatchID)
    }
    sub.reply <- nil
}
```

Cerrar un channel también sirve como señal. Cuando el router cierra el canal de una partida, el `for e := range in` de su goroutine termina y ella sabe que ya puede armar el lote. Cuando se cierra `out`, los senders salen de su `range` y terminan. No hace falta ningún flag compartido.

Los senders comparten un solo canal de salida, y Go garantiza que cada lote lo recibe uno solo de ellos. Repartir el trabajo entre `WORKERS` goroutines no necesitó código extra: si una está esperando un reintento, las demás siguen enviando.

#### 4. El mutex queda para el estado que otros consultan

El estado de las partidas lo escriben el router y los senders, y lo lee cualquier petición `GET /matches`, cada una en su goroutine. Ese estado no fluye: se consulta. Por eso no va por un channel, sino detrás de un `sync.RWMutex` (`state/match_store.go`):

```go
func (s *MatchStore) List() []MatchInfo {
    s.mu.RLock()
    defer s.mu.RUnlock()
    // ...arma y devuelve COPIAS de cada partida
}
```

`RLock` deja que varias lecturas vayan a la vez, y `Lock` hace que una escritura espere a quedar sola. `List` devuelve copias para que nadie lea el estado interno después de soltar el lock. El log de rechazados usa un `sync.Mutex` simple, porque cada línea tiene que escribirse completa sin mezclarse con otra.

La división dio una regla fácil de aplicar en cada decisión: si el dato se *mueve*, va por un channel; si se *consulta*, va detrás de un mutex.

#### 5. La librería estándar alcanzó para todo

No se agregó ni una dependencia: `go.mod` no tiene `require` y no existe `go.sum`.

- `net/http` trae el servidor, con rutas por método (`"POST /events"`, desde Go 1.22), `http.MaxBytesReader` para limitar el cuerpo y un `http.Client` con `Timeout`.
- `encoding/json` con `json.RawMessage` permite reenviar `data` a Scala sin interpretarlo. Gracias a eso, Go no necesita saber nada de cada juego: agregar uno es una línea en `GameActions`.
- `net/http/httptest` simula a Scala en los tests del cliente: éxito, fallo transitorio que se recupera, fallo total, `4xx` sin reintento, timeout y Scala caído.
- `go test -race` detecta data races sin instalar nada (en Linux; en Windows necesita gcc).

Para Docker y el CI esto simplificó todo. El Dockerfile no descarga dependencias y la imagen final es un Alpine con dos binarios estáticos (`CGO_ENABLED=0`).

#### 6. Los errores como valores permiten decidir qué hacer con cada uno

Un error en Go es un valor que se devuelve y se revisa en el momento. El cliente de Scala (`client/scala_client.go`) devuelve el error y además dice si vale la pena reintentar:

```go
func (c *ScalaClient) post(body []byte) (retry bool, err error)
```

Un `5xx` o un timeout se reintentan hasta 3 veces (esperando 1, 2 y 4 s). Un `4xx` no, porque el lote está mal y mandarlo otra vez no lo arregla. La idea se parece al `Either` de Scala, pero el compilador no obliga a revisar el error: la convención `if err != nil` sí.

### Limitaciones

#### 1. El compilador no detecta las data races

En Scala, la inmutabilidad evita el problema desde el diseño. En Go, que dos goroutines escriban el mismo dato sin protección compila sin advertencias. La única defensa es correr `go test -race`, y solo detecta las races que *ocurren* durante los tests. Por eso hay tests que fuerzan la concurrencia, como `TestConcurrentSubmit`, que manda eventos desde muchas goroutines a la vez, y por eso el CI corre siempre con `-race`.

Además, en Windows `-race` necesita gcc. Durante el desarrollo se corrió dentro de un contenedor de Docker.

#### 2. El orden de cierre de los channels es responsabilidad del programador

Enviar a un channel cerrado provoca `panic` en ejecución, y el compilador no lo previene. El canal de salida `out` lo escriben todas las goroutines de partida, así que solo se puede cerrar cuando **todas** terminaron. El router las cuenta con un `sync.WaitGroup` y recién después cierra:

```go
for _, ch := range matches {
    close(ch)
}
wg.Wait()
close(d.out)
```

Esta regla (cada channel lo cierra quien envía en él, y una sola vez) tuvo que documentarse en un comentario al inicio de `dispatcher.go`. Que "solo el router toca el mapa" también es una convención: si mañana alguien lo usara desde un handler, compilaría igual.

#### 3. Más código repetitivo

Muchas operaciones que en Scala son una línea aquí son un ciclo. Los jugadores distintos y ordenados de una partida (`buildBatch`) se arman así:

```go
seen := make(map[string]bool)
for _, e := range events {
    if e.PlayerID != "" && !seen[e.PlayerID] {
        seen[e.PlayerID] = true
        b.Players = append(b.Players, e.PlayerID)
    }
}
sort.Strings(b.Players)
```

En Scala sería algo como `events.flatMap(_.playerId).distinct.sorted`. Lo mismo pasa con `if err != nil { return ... }`, que se repite en cada llamada que puede fallar.

#### 4. Detalles que costaron tiempo

- **Timestamps.** `"…:22Z"` y `"…:22.000Z"` son el mismo instante, pero como texto no son iguales. Para ordenar hubo que comparar valores `time.Time`. Además, en el struct `Timestamp` es un `string` y no un `time.Time`: así un timestamp mal escrito no hace fallar el decode de todo el lote, y la validación rechaza solo ese evento.
- **El evento tardío.** Decidir dónde detectar un evento que llega después de `MATCH_FINISHED` llevó a que `Submit` espere una respuesta del router por un canal `reply` con buffer de 1. Solo el router conoce el orden real de llegada, y así la respuesta HTTP puede informar el rechazo con exactitud.
- **Determinismo del simulador.** En Go, el orden en que se recorre un `map` es aleatorio a propósito. Para que la misma semilla genere siempre las mismas partidas, el simulador elige los jugadores de slices y no de maps, y no usa `time.Now()` ni el generador aleatorio global. Cuando sí hace falta recorrer un map, como el catálogo `GameActions` para elegir una acción de otro juego, primero se pasa a un slice y se ordena.
- **PowerShell 5.1** agrega una marca BOM al inicio de los archivos UTF-8 que genera, y `encoding/json` no la acepta. El handler la quita antes de decodificar.

#### 5. Todo vive en memoria

El estado de las partidas en curso está solo en la memoria del proceso. Si el servidor se reinicia, las partidas que no habían terminado se pierden. Para este proyecto es aceptable, pero un sistema real necesitaría guardarlas en disco o en una cola persistente. El apagado ordenado (Ctrl+C o `docker stop`) cubre el caso normal: antes de cerrar, el servidor termina de enviar las partidas que ya habían terminado. Lo que no puede evitar es perder lo que estaba en memoria si el proceso muere de golpe.

### ¿Por qué HTTP entre Go y Scala?

- **Respuesta inmediata.** Cada envío devuelve un código (`2xx`, `4xx` o `5xx`), y con eso Go decide si la partida quedó `enviada`, si reintenta o si la marca como `error_envio`.
- **Sin dependencias.** Go trae servidor y cliente HTTP en la librería estándar. Se prueba con `curl.exe` a mano y con `httptest` en los tests.
- **Simple en Docker.** Cada servicio llama al otro por su nombre (`http://analytics:8081`), sin volúmenes compartidos.

Con **archivos** compartidos, Go no sabría cuándo Scala procesó una partida: habría que vigilar carpetas, evitar leer archivos a medio escribir y compartir un volumen entre contenedores. Con **mensajería** (Kafka o RabbitMQ) se ganaría persistencia y desacople, porque los lotes esperarían en la cola si Scala se cae. El costo sería otro servicio en el compose y librerías externas en ambos lenguajes. Los reintentos con espera creciente cubren en parte ese caso, que es suficiente para el alcance del proyecto.

### Resumen

| | Ventaja | Limitación |
|---|---|---|
| Estilo imperativo (ciclos, `continue`, `return` temprano) | El código sigue los pasos de la ingestión tal cual; saltar un evento malo es una línea | Más código repetitivo que `flatMap`/`distinct` en Scala |
| Goroutines | Una por partida, casi sin costo | Hay que asegurar que todas terminen (`WaitGroup`) |
| Channels | Pasan los datos sin locks; cerrarlos sirve de señal | Cerrar en el orden incorrecto da `panic` en ejecución |
| `sync.RWMutex` en el store | Lecturas en paralelo para `GET /matches` | El compilador no avisa si se olvida el lock: solo `-race` lo detecta |
| Librería estándar | Cero dependencias; Docker y CI simples | — |
| Errores como valores | Cada error se decide en el momento (reintentar o no) | Nada obliga a revisarlos |

---

## Scala — análisis (funcional)

El módulo de análisis (`analytics-scala/src/main`, unas 850 líneas) recibe el lote completo de una partida y devuelve sus estadísticas y patrones. Se escribió con estas reglas:

- **Sin `var`**: todo es `val`, `case class` y colecciones inmutables.
- **Funciones puras**: el análisis no hace I/O ni guarda estado.
- **`Option` y `Either`** en lugar de `null` y excepciones.
- **Pattern matching exhaustivo** sobre tipos cerrados (`sealed trait`, `enum`).

El resultado es que el I/O y el estado mutable quedaron en solo tres lugares: `Main.scala`, `storage/ResultStore.scala` y los handlers HTTP de `api/Routes.scala`. Todo lo demás son funciones puras.

### Ventajas

#### 1. Las operaciones sobre colecciones dicen *qué* se calcula, no *cómo*

Contar cuántas veces ocurrió cada acción en la partida (`eventsByAction`) es una cadena de tres operaciones (`stats/GenericStats.scala`):

```scala
def eventsByAction(events: List[Event]): Map[String, Int] =
  events
    .flatMap(_.action)     // solo los eventos que traen acción
    .groupBy(_.actionName) // una lista de acciones por nombre
    .view
    .mapValues(_.size)     // de cada lista solo interesa cuántas hay
    .toMap
```

La versión imperativa necesitaría un mapa mutable, un ciclo, un `if` para saltar los eventos sin acción y otro para inicializar cada contador. Acá cada línea corresponde a un paso de la idea: quedarse con las acciones, agruparlas y contarlas. El mismo patrón (`groupBy` + `mapValues`) se repite para los goles por equipo, las faltas por equipo y las asistencias por jugador, y siempre se lee igual.

`collect` resultó especialmente útil con el `sealed trait GameAction`, porque filtra por tipo y extrae el dato en un solo paso:

```scala
val lapTimes: List[Event] => List[Long] =
  _.flatMap(_.action).collect { case LapCompleted(_, lapTimeMs, _) => lapTimeMs }
```

#### 2. `foldLeft` hace explícito el estado de un recorrido

La regla COMEBACK de fútbol tiene que recorrer los goles en orden y recordar dos cosas: el marcador y qué equipos estuvieron perdiendo alguna vez. En lugar de dos variables que se modifican dentro de un ciclo, ese estado es un valor (`Progress`) y cada gol produce uno nuevo (`rules/ComebackRule.scala`):

```scala
case class Progress(score: Map[String, Int], everBehind: Set[String])

goals.foldLeft(start) { (acc, team) =>
  val score  = acc.score.updated(team, acc.score.getOrElse(team, 0) + 1)
  val behind = score.keySet.filter(t => score.exists((other, g) => other != t && g > score(t)))
  Progress(score, acc.everBehind ++ behind)
}
```

La ventaja concreta fue al probarlo. `replayGoals` es una función pura que devuelve el estado final, así que los tests revisan directamente el marcador y `everBehind` para las secuencias B-A-A (remontada), A-A-B (sin remontada) y 1-1 (empate, sin remontada), sin levantar nada ni inspeccionar variables intermedias. Lo mismo pasa con la desviación estándar de las vueltas, que suma los cuadrados con `foldLeft` sin un acumulador mutable.

#### 3. La composición arma métricas a partir de piezas chicas

Las métricas de racing son funciones `List[Event] => valor`, y varias se construyen componiendo otras (`stats/RacingStats.scala`):

```scala
val avgLapMs: List[Event] => Double    = lapTimes andThen average
val consistency: List[Event] => Double = lapTimes andThen stdDev
val bestLapMs: List[Event] => Double   = lapTimes andThen (_.minOption.fold(0.0)(_.toDouble))
```

`lapTimes`, `average` y `stdDev` se escriben y se prueban una sola vez. Agregar una métrica nueva sobre las vueltas es una línea más. En fútbol, `passAccuracy` se armó igual (`passes andThen (ps => ratio(...))`).

#### 4. `Option` y `Either` obligan a manejar los casos borde

Los casos borde del enunciado quedaron resueltos por los tipos, no por recordar poner un `if`:

- Un jugador sin vueltas: `minOption` devuelve `None` y se convierte en `0.0`.
- Sin tiros o sin pases: toda división pasa por `ratio(hits, attempts)`, que devuelve `0.0` si `attempts == 0`.
- Empate: `winner` es `Option[String]`, y en empate es `None`, que en el JSON sale `null`. La regla COMEBACK filtra sobre ese `Option`, así que con empate no hay patrón sin código extra.
- Una carrera sin `MATCH_FINISHED`: el `ranking` es un `Option` y no hay ganador.

En el parseo, cada paso devuelve `Either[String, A]` y se encadenan con `for`. El primer error corta la cadena y vuelve con contexto, sin que ninguna función lance excepciones:

```scala
for
  f         <- asObject(json, "el lote")
  matchId   <- nonEmptyStr(f, "matchId")
  startedAt <- instant(f, "startedAt")
  ...
  read      <- sequence(rawEvents.zipWithIndex.map((e, i) => readEvent(e).left.map(msg => s"events[$i]: $msg")))
```

Eso dio mensajes de error útiles durante la integración con Go, como `events[1]: y-e02: 'timestamp' no es RFC3339: 'ayer a las 2'`. `sequence` (de `List[Either]` a `Either[List]`) se implementó con `foldRight`.

#### 5. El compilador revisa que no falte ningún caso

`GameAction` es un `sealed trait`, así que cada `match` sobre él tiene que cubrir las 8 variantes (7 acciones más `UnknownAction`). `GameAction.actionName` y `EventType.requiresPlayer` están escritos sin comodín (`case _`) a propósito: si se agrega una acción o un tipo de evento, el compilador avisa dónde falta tratarlo.

#### 6. Lo puro se prueba sin montar nada

De los 76 tests del módulo, 69 prueban funciones puras: reciben un lote y comparan el resultado. No levantan el servidor ni comparten estado entre tests. Solo los 7 de `ResultStoreSuite` tocan el disco, y lo hacen en carpetas temporales.

Como `Analyzer.analyze` es puro, el mismo lote siempre da el mismo resultado. Durante la integración eso permitió reproducir exactamente lo que llegaba desde Go con los mismos escenarios del simulador (semilla 42) y comparar.

#### 7. La genericidad sale de tratar funciones y reglas como datos

Los juegos están en un `Map[String, GameAnalyzer]` y cada analizador declara su lista de reglas. Aplicar todas las reglas es un `flatMap`:

```scala
analysis.copy(patterns = analysis.patterns ++ analyzer.rules.flatMap(_.detect(batch)))
```

Agregar un juego o una regla no modifica esta línea. Y una acción desconocida no rompe nada: se guarda como `UnknownAction` y solo se cuenta en `eventsByAction`.

### Limitaciones

#### 1. El I/O obliga a salir de lo puro

Un servicio que guarda resultados no puede ser 100 % puro. `ResultStore` tiene el único estado mutable del módulo (un `TrieMap`, que es seguro con varios requests en paralelo) y escribe un archivo JSON por partida. `Main` termina el programa con `sys.exit(1)` si no puede abrir la carpeta de resultados. La ruta `/analyze` imprime en consola los eventos descartados.

La solución fue separar: un núcleo puro (parseo, análisis, reglas) y una capa delgada con efectos alrededor (`storage/` y `api/`). Aun así, esa capa necesita su propio cuidado, que lo funcional no resuelve:

- Escribir a un archivo temporal y moverlo, para no dejar un JSON a medio escribir.
- Seguir arrancando si un archivo guardado está corrupto.
- Devolver los errores de disco como `Either`, para que la ruta responda `500` en vez de caerse.

#### 2. `Either` corta en el primer error

`Either` se detiene en el primer `Left`. Si un lote tiene tres eventos mal formados, el mensaje solo menciona el primero. Para reportar todos habría que acumular errores con `Validated`, de la librería Cats, o escribir esa lógica a mano. No se hizo, para no sumar dependencias.

Un caso relacionado apareció al integrar con Go. Como Go no revisa el `data` de las acciones y no reintenta los errores `4xx`, rechazar el lote entero por un evento malo hacía perder la partida completa. El cambio fue descartar solo ese evento, y el tipo de `readEvent` quedó así:

```scala
private def readEvent(json: ujson.Value): Result[Either[Skipped, Event]]
```

Es un `Either` dentro de otro: el de afuera significa "el evento está roto, se rechaza el lote", y el de adentro "solo se descarta este evento". Es correcto y el compilador obliga a tratar los tres casos, pero se lee peor que la versión imperativa, que sería un `continue` dentro del ciclo y un `append` a una lista de descartados.

#### 3. Se recorren las mismas listas varias veces

Cada métrica es una función independiente y vuelve a recorrer los eventos del jugador. En racing, `lapTimes` se calcula de nuevo para el promedio, la consistencia, la mejor vuelta y el tiempo efectivo. En fútbol, `goals` se calcula una vez para la métrica de goles y otra dentro de `shotEffectiveness`. `teams(batch)` lo recalculan `score`, `teamStats` y la regla COMEBACK. Además, cada paso del `foldLeft` del COMEBACK crea un `Map` nuevo con `updated`.

Un ciclo imperativo con contadores haría todo en una sola pasada. Con partidas de 20 a 80 eventos no se nota, pero es un intercambio consciente: legibilidad y funciones que se prueban por separado a cambio de recorridos extra. Con volúmenes grandes habría que juntar varias métricas en un solo `foldLeft`, y se perdería parte de la claridad.

#### 4. La frontera con JSON es verbosa

`json/Codecs.scala` es el archivo más grande del módulo (unas 270 líneas de 850). Leer y escribir JSON a mano de forma segura, con un `Either` por cada campo, es repetitivo. La librería `ujson` guarda todos los números como `Double`, así que hubo que escribir funciones auxiliares (`int`, `long`) que verifican que el número sea entero y que quepa en el tipo. Además, `ujson.Value` es mutable: el código lo trata como si no lo fuera, pero el lenguaje no lo garantiza.

#### 5. Detalles del lenguaje que costaron tiempo

- El método que da el nombre de una acción se llamó primero `name` en el `sealed trait`. No compiló, porque `UnknownAction(name, raw)` ya tiene un campo `name`, y hubo que renombrarlo a `actionName`.
- `enum Card { case None, Yellow, Red }` sigue el enunciado, pero `None` choca con el `None` de `Option`. Dentro del código siempre hay que escribir `Card.None` para que no se confundan.

### Resumen

| | Ventaja | Limitación |
|---|---|---|
| Colecciones (`groupBy`, `collect`, `flatMap`) | Cálculos cortos que se leen como la definición de la estadística | Varias pasadas sobre las mismas listas |
| `foldLeft` | Estado del recorrido explícito y fácil de probar (`replayGoals`) | Crea estructuras nuevas en cada paso |
| `Option` / `Either` | Casos borde y errores manejados por el tipo, sin `null` ni excepciones | `Either` reporta solo el primer error; anidado se lee peor |
| Funciones puras | 69 de 76 tests sin servidor ni estado; resultados reproducibles | El I/O (disco, consola, salida del programa) tuvo que aislarse a mano en `storage/` y `api/` |
| Tipos cerrados (`sealed`, `enum`) | El compilador avisa si falta un caso | Choques de nombres (`name`, `Card.None`) |
