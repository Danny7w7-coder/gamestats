package gamestats.json

import gamestats.model.*

import java.time.{Instant, OffsetDateTime}
import scala.util.Try

/** Lectura de JSON a mano con ujson. Ninguna función lanza excepciones:
  * todo error vuelve como `Left(mensaje)`.
  */
object Codecs:

  type Result[A] = Either[String, A]
  private type Fields = Map[String, ujson.Value]

  // ---------------------------------------------------------------------------
  // Entrada
  // ---------------------------------------------------------------------------

  /** Texto -> JSON. `ujson.read` lanza con JSON inválido; Try lo convierte en Left. */
  def readJson(body: String): Result[ujson.Value] =
    Try(ujson.read(body)).toEither.left.map(e => s"JSON inválido: ${e.getMessage}")

  /** Lote Go -> Scala. Hay dos niveles de error:
    *  - del LOTE (encabezado mal, `events` vacío, un evento sin eventId/timestamp/
    *    type válidos, o de otra partida/juego): Left, el lote se rechaza (400).
    *  - de la ACCIÓN de un evento (`data` mal formada en una acción conocida): ese
    *    evento se descarta, queda anotado en `skipped` y la partida se analiza igual.
    *    Go no revisa `data`, así que rechazar todo el lote haría perder la partida
    *    entera por un solo evento.
    */
  def parseBatch(json: ujson.Value): Result[MatchBatch] =
    for
      f           <- asObject(json, "el lote")
      matchId     <- nonEmptyStr(f, "matchId")
      gameId      <- nonEmptyStr(f, "gameId")
      gameVersion <- nonEmptyStr(f, "gameVersion")
      startedAt   <- instant(f, "startedAt")
      finishedAt  <- instant(f, "finishedAt")
      _           <- check(!finishedAt.isBefore(startedAt), "'finishedAt' es anterior a 'startedAt'")
      players     <- strList(f, "players")
      rawEvents   <- array(f, "events")
      _           <- check(rawEvents.nonEmpty, "'events' está vacío")
      // zipWithIndex + map: cada error dice en qué posición del arreglo ocurrió.
      read        <- sequence(rawEvents.zipWithIndex.map((e, i) => readEvent(e).left.map(msg => s"events[$i]: $msg")))
      // collect separa los eventos que quedaron de los descartados.
      events       = read.collect { case Right(event) => event }
      skipped      = read.collect { case Left(Skipped(id, reason)) => s"$id: $reason" }
      _           <- check(events.nonEmpty, "todos los eventos se descartaron")
      _           <- sameAsBatch(events, "matchId", _.matchId, matchId)
      _           <- sameAsBatch(events, "gameId", _.gameId, gameId)
    yield MatchBatch.fromEvents(matchId, gameId, gameVersion, startedAt, finishedAt, players, events, skipped)

  /** Un evento individual, estricto: cualquier problema (incluida la `data` de
    * su acción) es Left. Dentro de un lote se usa `readEvent`, que es más tolerante.
    */
  def parseEvent(json: ujson.Value): Result[Event] =
    readEvent(json).flatMap(_.left.map(s => s"${s.eventId}: ${s.reason}"))

  /** Evento que se descarta del lote sin rechazarlo, y por qué. */
  case class Skipped(eventId: String, reason: String)

  /** Left (afuera) = evento roto, rechaza el lote.
    * Right(Left(...)) = la acción no se pudo leer, se descarta solo este evento.
    * Right(Right(evento)) = todo bien.
    */
  private def readEvent(json: ujson.Value): Result[Either[Skipped, Event]] =
    for
      f       <- asObject(json, "el evento")
      eventId <- nonEmptyStr(f, "eventId")
      // Desde aquí, cada error lleva el eventId para ubicarlo.
      base    <- parseEventFields(eventId, f).left.map(msg => s"$eventId: $msg")
    yield parseActionField(base.eventType, f, base.data) match
      case Right(action) => Right(base.copy(action = action))
      case Left(reason)  => Left(Skipped(eventId, reason))

  /** Todo menos la acción (que se lee aparte para poder descartarla sola). */
  private def parseEventFields(eventId: String, f: Fields): Result[Event] =
    for
      timestamp   <- instant(f, "timestamp")
      gameId      <- nonEmptyStr(f, "gameId")
      gameVersion <- nonEmptyStr(f, "gameVersion")
      matchId     <- nonEmptyStr(f, "matchId")
      eventType   <- str(f, "type").flatMap(parseEventType)
      playerId    <- optStr(f, "playerId")
      _           <- check(playerId.isDefined || !eventType.requiresPlayer, "falta 'playerId'")
      // `data` es opcional: si falta o es null se usa un objeto vacío.
      data         = f.get("data").filterNot(_.isNull).getOrElse(ujson.Obj())
    yield Event(eventId, timestamp, gameId, gameVersion, matchId, playerId, eventType, None, data)

  /** Nombre de acción + `data` -> GameAction. Para agregar un juego nuevo,
    * se agregan sus casos aquí; cualquier otro nombre queda como UnknownAction.
    * Además de los tipos se revisan los rangos y valores de event.schema.json.
    */
  def parseAction(name: String, data: ujson.Value): Result[GameAction] =
    val f   = data.objOpt.map(_.toMap).getOrElse(Map.empty[String, ujson.Value])
    val ctx = (r: Result[GameAction]) => r.left.map(msg => s"$name.data: $msg")
    name match
      // Racing
      case "LAP_COMPLETED" =>
        ctx(for lap <- intMin(f, "lap", 1); t <- longMin(f, "lapTimeMs", 1); pos <- intMin(f, "position", 1)
            yield LapCompleted(lap, t, pos))
      case "POSITION_CHANGED" =>
        ctx(for from <- intMin(f, "from", 1); to <- intMin(f, "to", 1); lap <- intMin(f, "lap", 1)
            yield PositionChanged(from, to, lap))
      case "PENALTY" =>
        ctx(for s <- intMin(f, "seconds", 0); reason <- str(f, "reason")
            yield Penalty(s, reason))
      case "OVERTAKE" =>
        ctx(for lap <- intMin(f, "lap", 1); other <- nonEmptyStr(f, "overtakenId")
            yield Overtake(lap, other))
      case "LAPPED" =>
        ctx(for lap <- intMin(f, "lap", 1); other <- nonEmptyStr(f, "lappedId")
            yield Lapped(lap, other))
      case "ITEM_PICKUP" =>
        ctx(for lap <- intMin(f, "lap", 1); item <- oneOf(f, "item", Item.values.map(i => i.key -> i).toMap)
            yield ItemPickup(lap, item))
      case "ITEM_USED" =>
        ctx(for lap <- intMin(f, "lap", 1); item <- oneOf(f, "item", Item.values.map(i => i.key -> i).toMap)
                ms  <- optionalField(f, "durationMs")(_ => longMin(f, "durationMs", 1))
                _   <- check(item != Item.Boost || ms.isDefined, "el boost necesita 'durationMs'")
            yield ItemUsed(lap, item, ms))
      case "DRIFT" =>
        ctx(for lap <- intMin(f, "lap", 1); corner <- intMin(f, "corner", 1); ms <- longMin(f, "durationMs", 1)
            yield Drift(lap, corner, ms))
      case "COLLISION" =>
        ctx(for lap <- intMin(f, "lap", 1); other <- nonEmptyStr(f, "otherId"); dmg <- intIn(f, "damage", 0, 100)
            yield Collision(lap, other, dmg))
      case "SPIN" =>
        ctx(for lap <- intMin(f, "lap", 1); corner <- intMin(f, "corner", 1)
            yield Spin(lap, corner))
      case "RESPAWN" =>
        ctx(for lap <- intMin(f, "lap", 1); pos <- intMin(f, "position", 1)
            yield Respawn(lap, pos))
      case "DNF" =>
        ctx(for lap <- intMin(f, "lap", 1); reason <- oneOf(f, "reason", DnfReason.values.map(r => r.key -> r).toMap)
            yield Dnf(lap, reason))
      // Football (minute: 0..130 en todas las acciones)
      case "PASS" =>
        ctx(for team <- str(f, "teamId"); target <- str(f, "targetId"); ok <- bool(f, "success")
                dist <- double(f, "distance").filterOrElse(_ >= 0, "'distance' debe ser ≥ 0"); min <- minute(f)
            yield Pass(team, target, ok, dist, min))
      case "SHOT" =>
        ctx(for team <- str(f, "teamId"); onTarget <- bool(f, "onTarget"); min <- minute(f)
            yield Shot(team, onTarget, min))
      case "POWER_SHOT" =>
        ctx(for team <- str(f, "teamId"); onTarget <- bool(f, "onTarget"); min <- minute(f)
            yield PowerShot(team, onTarget, min))
      case "GOAL" =>
        ctx(for team <- str(f, "teamId"); assist <- optStr(f, "assistId"); min <- minute(f)
            yield Goal(team, assist, min))
      case "OWN_GOAL" =>
        ctx(for team <- str(f, "teamId"); min <- minute(f) yield OwnGoal(team, min))
      case "FOUL" =>
        ctx(for team <- str(f, "teamId"); card <- str(f, "card").flatMap(parseCard)
                min <- minute(f); target <- optStr(f, "targetId")
            yield Foul(team, card, min, target))
      case "SAVE" =>
        ctx(for team <- str(f, "teamId"); min <- minute(f) yield Save(team, min))
      case "TACKLE" =>
        ctx(for team <- str(f, "teamId"); target <- nonEmptyStr(f, "targetId"); ok <- bool(f, "success"); min <- minute(f)
            yield Tackle(team, target, ok, min))
      case "INTERCEPTION" =>
        ctx(for team <- str(f, "teamId"); min <- minute(f) yield Interception(team, min))
      case "DRIBBLE" =>
        ctx(for team <- str(f, "teamId"); ok <- bool(f, "success"); min <- minute(f) yield Dribble(team, ok, min))
      case "CORNER" =>
        ctx(for team <- str(f, "teamId"); min <- minute(f) yield Corner(team, min))
      // Acción que ningún analyzer conoce: no es error, se conserva.
      case other =>
        Right(UnknownAction(other, data))

  /** Convierte List[Either] en Either[List]: el primer Left gana.
    * foldRight arma la lista de derecha a izquierda, así el resultado queda en el
    * orden original sin tener que invertirla.
    */
  def sequence[A](results: List[Result[A]]): Result[List[A]] =
    results.foldRight[Result[List[A]]](Right(Nil)) { (r, acc) =>
      for a <- r; as <- acc yield a :: as
    }

  // ---------------------------------------------------------------------------
  // Salida (formato de contracts/results.schema.json)
  // ---------------------------------------------------------------------------

  def encode(a: MatchAnalysis): ujson.Value =
    // Los campos opcionales de football solo se escriben si existen:
    // `lista ++ option` agrega el par si es Some y nada si es None.
    val base = List(
      "matchId"     -> ujson.Str(a.matchId),
      "gameId"      -> ujson.Str(a.gameId),
      "matchStats"  -> encodeMatchStats(a.matchStats)
    )
    val teams = a.teamStats.map(ts => "teamStats" -> ujson.Arr.from(ts.map(t => entity("teamId", t.teamId, t.metrics))))
    val rest = List(
      "playerStats" -> ujson.Arr.from(a.playerStats.map(p => entity("playerId", p.playerId, p.metrics))),
      "patterns"    -> ujson.Arr.from(a.patterns.map(encodePattern))
    )
    ujson.Obj.from(base ++ teams ++ rest)

  private def encodeMatchStats(m: MatchStats): ujson.Value =
    val fields = List(
      "durationSeconds" -> ujson.Num(m.durationSeconds.toDouble),
      "totalEvents"     -> ujson.Num(m.totalEvents),
      "playerCount"     -> ujson.Num(m.playerCount),
      "eventsByAction"  -> sortedObj(m.eventsByAction)(n => ujson.Num(n)),
      // winner siempre se escribe; None -> null (empate o sin datos).
      "winner"          -> m.winner.fold[ujson.Value](ujson.Null)(ujson.Str(_))
    )
    ujson.Obj.from(fields ++ m.score.map(s => "score" -> sortedObj(s)(n => ujson.Num(n))))

  def encodePattern(p: Pattern): ujson.Value =
    ujson.Obj(
      "rule"        -> p.rule,
      "playerId"    -> p.playerId.fold[ujson.Value](ujson.Null)(ujson.Str(_)),
      "description" -> p.description
    )

  def encodeMetrics(metrics: Map[String, Double]): ujson.Value =
    sortedObj(metrics)(ujson.Num(_))

  private def entity(idKey: String, id: String, metrics: Map[String, Double]): ujson.Value =
    ujson.Obj(idKey -> id, "metrics" -> encodeMetrics(metrics))

  /** Map -> objeto JSON con las claves ordenadas, para que la salida sea estable. */
  private def sortedObj[A](m: Map[String, A])(f: A => ujson.Value): ujson.Value =
    ujson.Obj.from(m.toList.sortBy(_._1).map((k, v) => k -> f(v)))

  // ---------------------------------------------------------------------------
  // Lectura de resultados guardados (inverso de `encode`, para ResultStore)
  // ---------------------------------------------------------------------------

  def decodeAnalysis(json: ujson.Value): Result[MatchAnalysis] =
    for
      f           <- asObject(json, "el resultado")
      matchId     <- nonEmptyStr(f, "matchId")
      gameId      <- nonEmptyStr(f, "gameId")
      matchStats  <- get(f, "matchStats").flatMap(decodeMatchStats).left.map(msg => s"matchStats: $msg")
      teamStats   <- optionalField(f, "teamStats")(v => decodeList(v, "teamStats")(decodeEntity("teamId", TeamStats(_, _))))
      playerStats <- get(f, "playerStats").flatMap(decodeList(_, "playerStats")(decodeEntity("playerId", PlayerStats(_, _))))
      patterns    <- get(f, "patterns").flatMap(decodeList(_, "patterns")(decodePattern))
    yield MatchAnalysis(matchId, gameId, matchStats, teamStats, playerStats, patterns)

  private def decodeMatchStats(json: ujson.Value): Result[MatchStats] =
    for
      f          <- asObject(json, "matchStats")
      duration   <- long(f, "durationSeconds")
      total      <- int(f, "totalEvents")
      players    <- int(f, "playerCount")
      byAction   <- get(f, "eventsByAction").flatMap(asObject(_, "'eventsByAction'")).flatMap(intValues)
      winner     <- optStr(f, "winner")
      score      <- optionalField(f, "score")(v => asObject(v, "'score'").flatMap(intValues))
    yield MatchStats(duration, total, players, byAction, winner, score)

  private def decodeEntity[A](idKey: String, build: (String, Map[String, Double]) => A)(json: ujson.Value): Result[A] =
    for
      f       <- asObject(json, s"cada elemento con '$idKey'")
      id      <- nonEmptyStr(f, idKey)
      metrics <- get(f, "metrics").flatMap(asObject(_, "'metrics'")).flatMap(doubleValues)
    yield build(id, metrics)

  private def decodePattern(json: ujson.Value): Result[Pattern] =
    for
      f           <- asObject(json, "cada patrón")
      rule        <- nonEmptyStr(f, "rule")
      playerId    <- optStr(f, "playerId")
      description <- str(f, "description")
    yield Pattern(rule, playerId, description)

  private def decodeList[A](json: ujson.Value, what: String)(decode: ujson.Value => Result[A]): Result[List[A]] =
    json.arrOpt.toRight(s"'$what' debe ser un arreglo").flatMap(items => sequence(items.toList.map(decode)))

  /** Campo opcional de cualquier tipo: ausente o null -> None. */
  private def optionalField[A](f: Fields, key: String)(decode: ujson.Value => Result[A]): Result[Option[A]] =
    f.get(key).filterNot(_.isNull) match
      case None    => Right(None)
      case Some(v) => decode(v).map(Some(_))

  private def doubleValues(f: Fields): Result[Map[String, Double]] =
    sequence(f.toList.map((k, v) => v.numOpt.map(k -> _).toRight(s"'$k' debe ser número"))).map(_.toMap)

  private def intValues(f: Fields): Result[Map[String, Int]] =
    sequence(f.toList.map((k, v) =>
      v.numOpt.filter(n => n.isWhole && n.isValidInt).map(n => k -> n.toInt).toRight(s"'$k' debe ser entero")
    )).map(_.toMap)

  // ---------------------------------------------------------------------------
  // Auxiliares de entrada
  // ---------------------------------------------------------------------------

  private def parseEventType(s: String): Result[EventType] = s match
    case "MATCH_STARTED"       => Right(EventType.MatchStarted)
    case "MATCH_FINISHED"      => Right(EventType.MatchFinished)
    case "MATCH_PAUSED"        => Right(EventType.MatchPaused)
    case "MATCH_RESUMED"       => Right(EventType.MatchResumed)
    case "PLAYER_JOINED"       => Right(EventType.PlayerJoined)
    case "PLAYER_LEFT"         => Right(EventType.PlayerLeft)
    case "PLAYER_DISCONNECTED" => Right(EventType.PlayerDisconnected)
    case "PLAYER_RECONNECTED"  => Right(EventType.PlayerReconnected)
    case "PLAYER_ACTION"       => Right(EventType.PlayerAction)
    case other                 => Left(s"'type' desconocido: '$other'")

  private def parseCard(s: String): Result[Card] = s match
    case "none"   => Right(Card.None)
    case "yellow" => Right(Card.Yellow)
    case "red"    => Right(Card.Red)
    case other    => Left(s"'card' debe ser none|yellow|red, llegó '$other'")

  /** Solo PLAYER_ACTION lleva acción; en los demás tipos se ignora. */
  private def parseActionField(eventType: EventType, f: Fields, data: ujson.Value): Result[Option[GameAction]] =
    eventType match
      case EventType.PlayerAction => str(f, "action").flatMap(parseAction(_, data)).map(Some(_))
      case _                      => Right(None)

  /** Todos los eventos deben coincidir con el encabezado del lote. */
  private def sameAsBatch(events: List[Event], name: String, get: Event => String, expected: String): Result[Unit] =
    // find devuelve el primer infractor (Option); toLeft lo vuelve error si existe.
    events
      .find(get(_) != expected)
      .map(e => s"el evento ${e.eventId} es de otro $name ('${get(e)}', se esperaba '$expected')")
      .toLeft(())

  private def check(cond: Boolean, error: => String): Result[Unit] =
    Either.cond(cond, (), error)

  private def asObject(json: ujson.Value, what: String): Result[Fields] =
    json.objOpt.map(_.toMap).toRight(s"$what debe ser un objeto JSON")

  private def get(f: Fields, key: String): Result[ujson.Value] =
    f.get(key).filterNot(_.isNull).toRight(s"falta '$key'")

  private def str(f: Fields, key: String): Result[String] =
    get(f, key).flatMap(_.strOpt.toRight(s"'$key' debe ser string"))

  private def nonEmptyStr(f: Fields, key: String): Result[String] =
    str(f, key).filterOrElse(_.nonEmpty, s"'$key' está vacío")

  /** Campo opcional: ausente o null -> None; presente pero de otro tipo -> error. */
  private def optStr(f: Fields, key: String): Result[Option[String]] =
    f.get(key).filterNot(_.isNull) match
      case None    => Right(None)
      case Some(v) => v.strOpt.map(Some(_)).toRight(s"'$key' debe ser string")

  private def bool(f: Fields, key: String): Result[Boolean] =
    get(f, key).flatMap(_.boolOpt.toRight(s"'$key' debe ser booleano"))

  private def double(f: Fields, key: String): Result[Double] =
    get(f, key).flatMap(_.numOpt.toRight(s"'$key' debe ser número"))

  // ujson guarda todo número como Double: se exige que sea entero y que quepa.
  private def long(f: Fields, key: String): Result[Long] =
    double(f, key).filterOrElse(_.isWhole, s"'$key' debe ser entero").map(_.toLong)

  private def int(f: Fields, key: String): Result[Int] =
    long(f, key).filterOrElse(_.isValidInt, s"'$key' fuera de rango").map(_.toInt)

  // Rangos del schema (minimum / maximum). filterOrElse convierte un valor fuera
  // de rango en Left sin cambiar el tipo del resultado.
  private def intMin(f: Fields, key: String, min: Int): Result[Int] =
    int(f, key).filterOrElse(_ >= min, s"'$key' debe ser ≥ $min")

  private def intIn(f: Fields, key: String, min: Int, max: Int): Result[Int] =
    int(f, key).filterOrElse(n => n >= min && n <= max, s"'$key' debe estar entre $min y $max")

  private def longMin(f: Fields, key: String, min: Long): Result[Long] =
    long(f, key).filterOrElse(_ >= min, s"'$key' debe ser ≥ $min")

  /** Minuto de football: informativo, pero dentro del rango del schema. */
  private def minute(f: Fields): Result[Int] = intIn(f, "minute", 0, 130)

  /** Valor dentro de una lista cerrada (los `enum` del schema). */
  private def oneOf[A](f: Fields, key: String, options: Map[String, A]): Result[A] =
    str(f, key).flatMap(s =>
      options.get(s).toRight(s"'$key' debe ser ${options.keys.toList.sorted.mkString("|")}, llegó '$s'")
    )

  private def array(f: Fields, key: String): Result[List[ujson.Value]] =
    get(f, key).flatMap(_.arrOpt.map(_.toList).toRight(s"'$key' debe ser un arreglo"))

  private def strList(f: Fields, key: String): Result[List[String]] =
    array(f, key).flatMap(items => sequence(items.map(_.strOpt.toRight(s"'$key' debe tener solo strings"))))

  /** RFC3339 acepta `Z` o un offset (`-06:00`); OffsetDateTime cubre ambos. */
  private def instant(f: Fields, key: String): Result[Instant] =
    str(f, key).flatMap(s =>
      Try(OffsetDateTime.parse(s).toInstant).toEither.left.map(_ => s"'$key' no es RFC3339: '$s'")
    )
