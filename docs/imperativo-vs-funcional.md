# Imperativo vs. funcional en GameStats

GameStats usa dos paradigmas a propósito: la ingestión está escrita en **Go** con un estilo imperativo y concurrente, y el análisis en **Scala** con un estilo funcional. Este documento recoge, para cada lado, las ventajas y limitaciones que aparecieron al implementarlo, con ejemplos del código del proyecto.

- [Go — ingestión (imperativo)](#go--ingestión-imperativo)
- [Scala — análisis (funcional)](#scala--análisis-funcional)

---

## Go — ingestión (imperativo)

*Sección a cargo de Dylan.*

> **ESQUEMA DE TRABAJO: reemplazar por el texto final.** Abajo están las preguntas guía de cada apartado y datos concretos del código para responderlas. La sección de Scala usa la estructura Ventajas → Limitaciones → Resumen; conviene seguir la misma para que el documento quede parejo.

**Párrafo de apertura (ideas para escribirlo):**
- El módulo `ingestion-go/` tiene unas 1700 líneas de código (más unas 1400 de tests, 36 funciones `Test`) y usa solo la librería estándar.
- Ejemplo de estilo: con las reglas de Danny ("sin `var`", "funciones puras"...), ¿cuáles serían las reglas de Go? Algunas ideas: estado mutable, pero con un dueño claro; un channel para cada flujo de datos; los errores como valores (`error`) y no como excepciones; código directo, sin abstracciones.
- ¿Dónde vive el estado mutable? En el store (`state/match_store.go`), el log de rechazados (`rejected/log.go`), el mapa del router (`pipeline/dispatcher.go`) y el slice de eventos de cada partida (`collect`).

### Ventajas

#### 1. ¿Por qué la ingestión se beneficia de un estilo imperativo?

Preguntas guía:
- La ingestión es, por naturaleza, una secuencia de pasos con efectos: leer el cuerpo, validar, registrar, reenviar, escribir el log y responder. ¿Se leería mejor como una composición de funciones?
- ¿Qué ganas al poder modificar el estado en el lugar (agregar a un slice, incrementar un contador)?

Datos del código:
- `Validate` (`validation/validator.go`) son 7 reglas en orden, cada una con `return` temprano: el primer error corta. Se puede comparar con el `for` + `Either` de Scala, que también corta en el primer error, pero con otra sintaxis.
- `collect` acumula con `events = append(events, e)` y después ordena **en el mismo lugar** con `sort.SliceStable`. En Scala sería una lista nueva.
- `MatchStore.Record` actualiza el contador y el conjunto de jugadores conectados con un `switch` sobre el tipo de evento.
- El handler (`api/handler.go`) usa un `for` con `continue` para saltar los eventos rechazados. Danny menciona en su limitación 2 que justamente eso le costó en Scala (el `Either` anidado).

#### 2. ¿Qué aportaron las goroutines y los channels?

Preguntas guía:
- ¿Cómo sería el diseño de "una goroutine por partida" sin goroutines (con hilos del sistema operativo o con callbacks)?
- ¿Por qué el router puede usar un mapa sin mutex?
- ¿Qué significa "no comunicar compartiendo memoria; compartir memoria comunicando"?

Datos del código:
- Hay 4 lugares que lanzan goroutines: el router (`go d.route()`), una por partida (`go d.collect(...)`), `WORKERS` senders (`sender.go`) y una por partida en el simulador (`simulator.go`, función `Send`).
- Channels: `in` (sin buffer), un canal por partida (sin buffer), `out` (buffer de 16) y `reply` (buffer de 1, para que el router no se bloquee al responder).
- Quién cierra cada channel está documentado en el comentario de `dispatcher.go` y en `docs/ingestion-go.md`.
- Una goroutine cuesta unos pocos KB al empezar, así que crear una por partida es barato.

#### 3. Channels para el flujo, mutex para el estado consultado

Preguntas guía:
- ¿Por qué no se hizo todo con channels, o todo con mutex?
- ¿Por qué `RWMutex` y no `Mutex` en el store?

Datos del código:
- El store usa `sync.RWMutex`: `GET /matches` usa `RLock` y el router y los senders usan `Lock`. `List()` devuelve copias.
- El log de rechazados usa `sync.Mutex`, porque cada línea tiene que escribirse completa.
- Hay 3 `sync.WaitGroup`: las partidas vivas (en el router), los senders y los envíos del simulador.

#### 4. La librería estándar alcanzó para todo

Preguntas guía:
- ¿Qué habría hecho falta en otro lenguaje (un framework web, una librería de JSON, una de logs)?
- ¿Qué ventaja tiene para el CI y para Docker no tener dependencias?

Datos del código:
- `net/http` con patrones por método (`"POST /events"`, desde Go 1.22), `http.MaxBytesReader` para limitar el cuerpo y `http.Client` con `Timeout`.
- `encoding/json` con `json.RawMessage`: `data` pasa a Scala sin interpretarse, y por eso el sistema es genérico.
- `httptest.NewServer` simula a Scala en los tests del cliente (éxito, fallo que se recupera, fallo total, `4xx` sin reintento, timeout y Scala caído).
- `go.mod` no tiene `require`: no hay `go.sum` y el Dockerfile no necesita `go mod download`. La imagen final es Alpine con dos binarios estáticos (`CGO_ENABLED=0`).
- `go test -race` viene incluido y detecta data races sin instalar nada (en Linux, porque en Windows necesita gcc).

#### 5. Los errores como valores

Preguntas guía:
- ¿Qué tienen en común `if err != nil` y el `Either` de Scala, y en qué se diferencian?

Datos del código:
- `ScalaClient.post` devuelve `(retry bool, err error)`: el error dice qué pasó y `retry` dice si vale la pena reintentar (un `4xx` no se reintenta; un `5xx` o un timeout sí).
- `config.Load` devuelve un error si `WORKERS` o `IDLE_TIMEOUT` son inválidos, y el servidor no arranca.

### Limitaciones

#### 1. ¿Qué fue difícil?

Preguntas guía y datos para elegir (tomar 3 o 4 que se puedan contar bien):
- **Cerrar los channels en el orden correcto.** `out` solo se puede cerrar cuando todas las goroutines de partida terminaron, de lo contrario un envío a un channel cerrado hace `panic`. Por eso se usa el `WaitGroup` dentro del router. El compilador no ayuda: es un error que aparece en ejecución.
- **El compilador no detecta data races.** Hay que correr `go test -race`, y solo detecta las races que ocurren durante el test. En Scala, la inmutabilidad evita el problema desde el diseño.
- **Evento tardío.** Decidir dónde detectarlo: en el router, consultando el store, y que `Submit` espere una respuesta (canal `reply`) para poder informar el rechazo en la respuesta HTTP.
- **Timestamps.** `"…:22Z"` y `"…:22.000Z"` son el mismo instante pero distinto texto, así que hubo que ordenar comparando `time.Time`. `Timestamp` se guarda como `string` en el struct para que un timestamp malo no rompa el decode de todo el lote.
- **Determinismo del simulador.** El orden en que se recorre un `map` en Go es aleatorio, así que el simulador usa slices para elegir jugadores. Además, `time.Now()` y el `rand` global están prohibidos.
- **Detalles del entorno.** PowerShell 5.1 agrega un BOM al inicio de los archivos (el handler lo quita), y `go test -race` no corre en Windows sin gcc (se corrió en Docker y en el CI).

#### 2. Más código repetitivo

Preguntas guía:
- ¿Cuántas veces se escribe `if err != nil { return ... }`?
- ¿Cómo se vería `players` (ids distintos y ordenados) en Scala, con `.distinct.sorted`, contra lo que hace `buildBatch` (un `map` de vistos, un `append` y `sort.Strings`)?

#### 3. La disciplina reemplaza a las garantías

Preguntas guía:
- Que "solo el router toca el mapa" es una convención: ¿qué pasaría si alguien lo usara desde un handler?
- `MatchInfo` se copia para que nadie lea el estado fuera del lock. ¿Qué pasaría si se devolvieran punteros?

### ¿Por qué HTTP y no archivos o mensajería?

Preguntas guía:
- ¿Qué se gana con HTTP? La respuesta es inmediata (`2xx`, `4xx`, `5xx`), lo que permite decidir si reintentar. Además ambos lenguajes lo traen en su librería estándar (o casi), se prueba con `curl.exe` y con `httptest`, y en Docker basta con el nombre del servicio (`http://analytics:8081`).
- Con **archivos** compartidos, ¿cómo sabría Go que Scala procesó una partida? Habría que vigilar carpetas, manejar archivos a medio escribir y compartir un volumen entre contenedores.
- Con **mensajería** (Kafka, RabbitMQ), ¿qué ventaja habría (persistencia, desacople si Scala se cae)? ¿Y qué costo (otro servicio en el compose, dependencias externas)? Se puede mencionar que los reintentos con espera creciente (1, 2 y 4 s) cubren en parte el caso de que Scala esté caído.
- Una limitación honesta: si Go se reinicia, las partidas en curso se pierden, porque todo está en memoria.

### Resumen

Tabla con el mismo formato que la de Scala (Ventaja | Limitación). Filas sugeridas: goroutines por partida, channels, mutex en el store, librería estándar, errores como valores.

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
