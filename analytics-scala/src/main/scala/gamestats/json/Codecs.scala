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

  /** Lote Go -> Scala. Falla si el encabezado está mal, si `events` está vacío,
    * si cualquier evento no parsea o si algún evento es de otra partida/juego.
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
      events      <- sequence(rawEvents.zipWithIndex.map((e, i) => parseEvent(e).left.map(msg => s"events[$i]: $msg")))
      _           <- sameAsBatch(events, "matchId", _.matchId, matchId)
      _           <- sameAsBatch(events, "gameId", _.gameId, gameId)
    yield MatchBatch.fromEvents(matchId, gameId, gameVersion, startedAt, finishedAt, players, events)

  /** Un evento individual. */
  def parseEvent(json: ujson.Value): Result[Event] =
    for
      f       <- asObject(json, "el evento")
      eventId <- nonEmptyStr(f, "eventId")
      // Desde aquí, cada error lleva el eventId para ubicarlo.
      event   <- parseEventFields(eventId, f).left.map(msg => s"$eventId: $msg")
    yield event

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
      action      <- parseActionField(eventType, f, data)
    yield Event(eventId, timestamp, gameId, gameVersion, matchId, playerId, eventType, action, data)

  /** Nombre de acción + `data` -> GameAction. Para agregar un juego nuevo,
    * se agregan sus casos aquí; cualquier otro nombre queda como UnknownAction.
    */
  def parseAction(name: String, data: ujson.Value): Result[GameAction] =
    val f   = data.objOpt.map(_.toMap).getOrElse(Map.empty[String, ujson.Value])
    val ctx = (r: Result[GameAction]) => r.left.map(msg => s"$name.data: $msg")
    name match
      // Racing
      case "LAP_COMPLETED" =>
        ctx(for lap <- int(f, "lap"); t <- long(f, "lapTimeMs"); pos <- int(f, "position")
            yield LapCompleted(lap, t, pos))
      case "POSITION_CHANGED" =>
        ctx(for from <- int(f, "from"); to <- int(f, "to"); lap <- int(f, "lap")
            yield PositionChanged(from, to, lap))
      case "PENALTY" =>
        ctx(for s <- int(f, "seconds"); reason <- str(f, "reason")
            yield Penalty(s, reason))
      // Football
      case "PASS" =>
        ctx(for team <- str(f, "teamId"); target <- str(f, "targetId"); ok <- bool(f, "success")
                dist <- double(f, "distance"); min <- int(f, "minute")
            yield Pass(team, target, ok, dist, min))
      case "SHOT" =>
        ctx(for team <- str(f, "teamId"); onTarget <- bool(f, "onTarget"); min <- int(f, "minute")
            yield Shot(team, onTarget, min))
      case "GOAL" =>
        ctx(for team <- str(f, "teamId"); assist <- optStr(f, "assistId"); min <- int(f, "minute")
            yield Goal(team, assist, min))
      case "FOUL" =>
        ctx(for team <- str(f, "teamId"); card <- str(f, "card").flatMap(parseCard)
                min <- int(f, "minute"); target <- optStr(f, "targetId")
            yield Foul(team, card, min, target))
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

  private def encodePattern(p: Pattern): ujson.Value =
    ujson.Obj(
      "rule"        -> p.rule,
      "playerId"    -> p.playerId.fold[ujson.Value](ujson.Null)(ujson.Str(_)),
      "description" -> p.description
    )

  private def entity(idKey: String, id: String, metrics: Map[String, Double]): ujson.Value =
    ujson.Obj(idKey -> id, "metrics" -> sortedObj(metrics)(ujson.Num(_)))

  /** Map -> objeto JSON con las claves ordenadas, para que la salida sea estable. */
  private def sortedObj[A](m: Map[String, A])(f: A => ujson.Value): ujson.Value =
    ujson.Obj.from(m.toList.sortBy(_._1).map((k, v) => k -> f(v)))

  // ---------------------------------------------------------------------------
  // Auxiliares de entrada
  // ---------------------------------------------------------------------------

  private def parseEventType(s: String): Result[EventType] = s match
    case "MATCH_STARTED"       => Right(EventType.MatchStarted)
    case "MATCH_FINISHED"      => Right(EventType.MatchFinished)
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

  private def array(f: Fields, key: String): Result[List[ujson.Value]] =
    get(f, key).flatMap(_.arrOpt.map(_.toList).toRight(s"'$key' debe ser un arreglo"))

  private def strList(f: Fields, key: String): Result[List[String]] =
    array(f, key).flatMap(items => sequence(items.map(_.strOpt.toRight(s"'$key' debe tener solo strings"))))

  /** RFC3339 acepta `Z` o un offset (`-06:00`); OffsetDateTime cubre ambos. */
  private def instant(f: Fields, key: String): Result[Instant] =
    str(f, key).flatMap(s =>
      Try(OffsetDateTime.parse(s).toInstant).toEither.left.map(_ => s"'$key' no es RFC3339: '$s'")
    )
