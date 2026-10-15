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
      case "COLLISION" =>
        ctx(for lap <- intMin(f, "lap", 1); other <- nonEmptyStr(f, "otherId"); dmg <- intIn(f, "damage", 0, 100)
            yield Collision(lap, other, dmg))
      case "RESPAWN" =>
        ctx(for lap <- intMin(f, "lap", 1); pos <- intMin(f, "position", 1)
            yield Respawn(lap, pos))
      case "DNF" =>
        ctx(for lap <- intMin(f, "lap", 1); reason <- oneOf(f, "reason", DnfReason.values.map(r => r.key -> r).toMap)
            yield Dnf(lap, reason))
      // Fighting (round: 1..5; damage: 0..100, y 0 si el golpe falla)
      case "ATTACK" =>
        ctx(for target <- nonEmptyStr(f, "targetId"); move <- oneOf(f, "move", Move.values.map(m => m.key -> m).toMap)
                hit <- bool(f, "hit"); dmg <- strike(f, hit); r <- round(f)
            yield Attack(target, move, hit, dmg, r))
      case "COMBO" =>
        ctx(for target <- nonEmptyStr(f, "targetId"); hits <- intMin(f, "hits", 2)
                dmg <- intIn(f, "damage", 1, 100); r <- round(f)
            yield Combo(target, hits, dmg, r))
      case "BLOCK" =>
        ctx(for attacker <- nonEmptyStr(f, "attackerId"); r <- round(f) yield Block(attacker, r))
      case "KNOCKDOWN" =>
        ctx(for target <- nonEmptyStr(f, "targetId"); r <- round(f) yield Knockdown(target, r))
      case "ROUND_WON" =>
        ctx(for r <- round(f); health <- intIn(f, "remainingHealth", 1, 100)
                method <- oneOf(f, "method", WinMethod.values.map(m => m.key -> m).toMap)
            yield RoundWon(r, health, method))
      case "ROUND_DRAW" =>
        ctx(for r <- round(f); method <- oneOf(f, "method", DrawMethod.values.map(m => m.key -> m).toMap)
                other <- nonEmptyStr(f, "otherId")
            yield RoundDraw(r, method, other))
      // Connect4 (column: 1..7; row: 1..6)
      case "DISC_DROPPED"   => ctx(for c <- column(f); r <- row(f) yield DiscDropped(c, r))
      case "THREAT_CREATED" => ctx(for c <- column(f); r <- row(f) yield ThreatCreated(c, r))
      case "THREAT_BLOCKED" =>
        ctx(for c <- column(f); r <- row(f); attacker <- nonEmptyStr(f, "attackerId") yield ThreatBlocked(c, r, attacker))
      case "CONNECT_FOUR" =>
        ctx(oneOf(f, "direction", Line.values.map(l => l.key -> l).toMap).map(ConnectFour(_)))
      case "TURN_TIMEOUT" => ctx(intMin(f, "seconds", 1).map(TurnTimeout(_)))
      case "UNDO"         => ctx(for c <- column(f); r <- row(f) yield Undo(c, r))
      case "HINT_USED"    => ctx(column(f).map(HintUsed(_)))
      // Tetris
      case "PIECE_SPAWNED" => ctx(piece(f, "piece").map(PieceSpawned(_)))
      case "PIECE_MOVED" =>
        ctx(oneOf(f, "direction", Side.values.map(s => s.key -> s).toMap).map(PieceMoved(_)))
      case "PIECE_ROTATED" =>
        ctx(oneOf(f, "direction", Rotation.values.map(r => r.key -> r).toMap).map(PieceRotated(_)))
      case "SOFT_DROP" => ctx(intIn(f, "cells", 1, 20).map(SoftDrop(_)))
      case "HARD_DROP" => ctx(intIn(f, "cells", 0, 20).map(HardDrop(_)))
      case "HOLD"      => ctx(piece(f, "piece").map(Hold(_)))
      case "PIECE_PLACED" =>
        ctx(for p <- piece(f, "piece"); hard <- bool(f, "hardDrop") yield PiecePlaced(p, hard))
      case "LINES_CLEARED" =>
        ctx(for lines <- intIn(f, "lines", 1, 4); points <- intMin(f, "points", 0); combo <- intMin(f, "combo", 0)
            yield LinesCleared(lines, points, combo))
      case "T_SPIN"       => ctx(intIn(f, "lines", 0, 3).map(TSpin(_)))
      case "ALL_CLEAR"    => ctx(intIn(f, "lines", 1, 4).map(AllClear(_)))
      case "LEVEL_UP"     => ctx(intIn(f, "level", 2, 30).map(LevelUp(_)))
      case "STACK_HEIGHT" => ctx(intIn(f, "rows", 0, 20).map(StackHeight(_)))
      case "TOP_OUT"      => Right(TopOut)
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
    ujson.Obj(
      "matchId"     -> a.matchId,
      "gameId"      -> a.gameId,
      "matchStats"  -> encodeMatchStats(a.matchStats),
      "playerStats" -> ujson.Arr.from(a.playerStats.map(p => ujson.Obj("playerId" -> p.playerId, "metrics" -> encodeMetrics(p.metrics)))),
      "patterns"    -> ujson.Arr.from(a.patterns.map(encodePattern))
    )

  private def encodeMatchStats(m: MatchStats): ujson.Value =
    ujson.Obj(
      "durationSeconds" -> ujson.Num(m.durationSeconds.toDouble),
      "totalEvents"     -> ujson.Num(m.totalEvents),
      "playerCount"     -> ujson.Num(m.playerCount),
      "eventsByAction"  -> sortedObj(m.eventsByAction)(n => ujson.Num(n)),
      // winner siempre se escribe; None -> null (empate o sin datos).
      "winner"          -> m.winner.fold[ujson.Value](ujson.Null)(ujson.Str(_))
    )

  def encodePattern(p: Pattern): ujson.Value =
    ujson.Obj(
      "rule"        -> p.rule,
      "playerId"    -> p.playerId.fold[ujson.Value](ujson.Null)(ujson.Str(_)),
      "description" -> p.description
    )

  def encodeMetrics(metrics: Map[String, Double]): ujson.Value =
    sortedObj(metrics)(ujson.Num(_))

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
      playerStats <- get(f, "playerStats").flatMap(decodeList(_, "playerStats")(decodePlayerStats))
      patterns    <- get(f, "patterns").flatMap(decodeList(_, "patterns")(decodePattern))
    yield MatchAnalysis(matchId, gameId, matchStats, playerStats, patterns)

  private def decodeMatchStats(json: ujson.Value): Result[MatchStats] =
    for
      f          <- asObject(json, "matchStats")
      duration   <- long(f, "durationSeconds")
      total      <- int(f, "totalEvents")
      players    <- int(f, "playerCount")
      byAction   <- get(f, "eventsByAction").flatMap(asObject(_, "'eventsByAction'")).flatMap(intValues)
      winner     <- optStr(f, "winner")
    yield MatchStats(duration, total, players, byAction, winner)

  private def decodePlayerStats(json: ujson.Value): Result[PlayerStats] =
    for
      f       <- asObject(json, "cada elemento de 'playerStats'")
      id      <- nonEmptyStr(f, "playerId")
      metrics <- get(f, "metrics").flatMap(asObject(_, "'metrics'")).flatMap(doubleValues)
    yield PlayerStats(id, metrics)

  private def decodePattern(json: ujson.Value): Result[Pattern] =
    for
      f           <- asObject(json, "cada patrón")
      rule        <- nonEmptyStr(f, "rule")
      playerId    <- optStr(f, "playerId")
      description <- str(f, "description")
    yield Pattern(rule, playerId, description)

  private def decodeList[A](json: ujson.Value, what: String)(decode: ujson.Value => Result[A]): Result[List[A]] =
    json.arrOpt.toRight(s"'$what' debe ser un arreglo").flatMap(items => sequence(items.toList.map(decode)))

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

  /** Casilla de connect4: columna 1..7 y fila 1..6 (la gravedad la revisa Connect4Check). */
  private def column(f: Fields): Result[Int] = intIn(f, "column", 1, 7)
  private def row(f: Fields): Result[Int]    = intIn(f, "row", 1, 6)

  /** Round de fighting (1 a 5; contra el máximo de la pelea lo revisa FightingCheck). */
  private def round(f: Fields): Result[Int] = intIn(f, "round", 1, 5)

  /** Daño de un golpe que puede fallar: 0..100, y exactamente 0 si no conectó. */
  private def strike(f: Fields, hit: Boolean): Result[Int] =
    intIn(f, "damage", 0, 100).filterOrElse(d => hit || d == 0, "un golpe que falla no hace daño ('damage' debe ser 0)")

  private def piece(f: Fields, key: String): Result[Piece] =
    oneOf(f, key, Piece.values.map(p => p.key -> p).toMap)

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
