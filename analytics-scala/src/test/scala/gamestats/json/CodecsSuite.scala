package gamestats.json

import gamestats.Fixtures
import gamestats.model.*

import java.time.Instant

class CodecsSuite extends munit.FunSuite:

  /** Evento mínimo válido; `extra` reemplaza o agrega campos. */
  def event(extra: (String, ujson.Value)*): ujson.Obj =
    val base = ujson.Obj(
      "eventId"     -> "e1",
      "timestamp"   -> "2026-10-05T14:00:00Z",
      "gameId"      -> "racing",
      "gameVersion" -> "1.0",
      "matchId"     -> "race-001",
      "playerId"    -> "p1",
      "type"        -> "PLAYER_JOINED",
      "data"        -> ujson.Obj()
    )
    extra.foreach((k, v) => base(k) = v)
    base

  def batch(events: ujson.Value*): ujson.Obj = ujson.Obj(
    "matchId"     -> "race-001",
    "gameId"      -> "racing",
    "gameVersion" -> "1.0",
    "startedAt"   -> "2026-10-05T14:00:00Z",
    "finishedAt"  -> "2026-10-05T14:04:10Z",
    "players"     -> ujson.Arr("p1"),
    "events"      -> ujson.Arr(events*)
  )

  // --- Fixtures del contrato ------------------------------------------------

  test("parsea racing_match_ok.json sin error"):
    val b = Codecs.parseBatch(Fixtures.json("racing_match_ok.json")).fold(fail(_), identity)
    assertEquals(b.matchId, "race-001")
    assertEquals(b.events.size, 17)
    assertEquals(b.players, List("p1", "p2", "p3"))
    assertEquals(b.events.head.eventType, EventType.MatchStarted)
    assertEquals(b.events.head.playerId, None)
    val p1Laps = b.events.filter(_.playerId.contains("p1")).flatMap(_.action).collect { case l: LapCompleted => l.lapTimeMs }
    assertEquals(p1Laps, List(82000L, 81500L, 80900L))
    assert(b.events.flatMap(_.action).contains(Penalty(5, "track_limits")))

  test("parsea football_match_ok.json sin error"):
    val b = Codecs.parseBatch(Fixtures.json("football_match_ok.json")).fold(fail(_), identity)
    assertEquals(b.matchId, "foot-001")
    assertEquals(b.events.size, 26)
    assertEquals(b.players, List("p1", "p2", "p3", "p4"))
    val actions = b.events.flatMap(_.action)
    assert(actions.contains(Goal("A", Some("p2"), 5)))
    assert(actions.contains(Goal("A", None, 61)), "gol sin asistencia -> assistId = None")
    assert(actions.contains(Foul("B", Card.Yellow, 20, Some("p1"))))
    assert(actions.contains(Foul("A", Card.None, 52, Some("p3"))))

  test("parsea los fixtures de remontada"):
    assert(Codecs.parseBatch(Fixtures.json("comeback_case.json")).isRight)
    assert(Codecs.parseBatch(Fixtures.json("football_comeback.json")).isRight)

  // --- Genericidad -----------------------------------------------------------

  test("una acción inventada produce UnknownAction y no falla"):
    val data = ujson.Obj("style" -> "moonwalk")
    val e = event("type" -> "PLAYER_ACTION", "action" -> "DANCE", "data" -> data)
    val parsed = Codecs.parseEvent(e).fold(fail(_), identity)
    assertEquals(parsed.action, Some(UnknownAction("DANCE", data)))
    // Y el lote completo también pasa.
    assert(Codecs.parseBatch(batch(e)).isRight)

  // --- Errores ----------------------------------------------------------------

  test("un evento sin eventId produce Left"):
    val e = event()
    e.value.remove("eventId")
    assertEquals(Codecs.parseEvent(e), Left("falta 'eventId'"))

  test("un timestamp malo produce Left con la posición del evento"):
    val r = Codecs.parseBatch(Fixtures.json("invalid_bad_timestamp.json"))
    assert(r.isLeft)
    assert(r.left.exists(_.startsWith("events[1]: y-e02: 'timestamp' no es RFC3339")), r)

  test("un lote sin matchId produce Left"):
    assertEquals(Codecs.parseBatch(Fixtures.json("invalid_missing_matchid.json")), Left("falta 'matchId'"))

  test("events vacío produce Left"):
    assertEquals(Codecs.parseBatch(batch()), Left("'events' está vacío"))

  test("un evento de otra partida produce Left"):
    val r = Codecs.parseBatch(batch(event("matchId" -> "race-999")))
    assert(r.left.exists(_.contains("es de otro matchId")), r)

  test("una acción conocida con data incompleta produce Left"):
    val e = event("type" -> "PLAYER_ACTION", "action" -> "LAP_COMPLETED", "data" -> ujson.Obj("lap" -> 1, "position" -> 2))
    assertEquals(Codecs.parseEvent(e), Left("e1: LAP_COMPLETED.data: falta 'lapTimeMs'"))

  test("PLAYER_ACTION sin playerId produce Left"):
    val e = event("type" -> "PLAYER_ACTION", "action" -> "PENALTY", "data" -> ujson.Obj("seconds" -> 5, "reason" -> "x"))
    e.value.remove("playerId")
    assertEquals(Codecs.parseEvent(e), Left("e1: falta 'playerId'"))

  test("un timestamp con offset (como lo puede mandar Go) se acepta"):
    val parsed = Codecs.parseEvent(event("timestamp" -> "2026-10-05T08:00:00-06:00")).fold(fail(_), identity)
    assertEquals(parsed.timestamp, Instant.parse("2026-10-05T14:00:00Z"))

  // --- sequence y fromEvents --------------------------------------------------

  test("sequence devuelve el primer error o la lista completa"):
    assertEquals(Codecs.sequence(List(Right(1), Right(2), Right(3))), Right(List(1, 2, 3)))
    assertEquals(Codecs.sequence(List(Right(1), Left("a"), Left("b"))), Left("a"))
    assertEquals(Codecs.sequence(List.empty[Codecs.Result[Int]]), Right(Nil))

  test("fromEvents ordena por timestamp y deriva los jugadores"):
    val late  = event("eventId" -> "tarde", "timestamp" -> "2026-10-05T14:02:00Z", "playerId" -> "p2")
    val early = event("eventId" -> "temprano", "timestamp" -> "2026-10-05T14:01:00Z", "playerId" -> "p9")
    val b = Codecs.parseBatch(batch(late, early)).fold(fail(_), identity)
    assertEquals(b.events.map(_.eventId), List("temprano", "tarde"))
    // p1 viene declarado en `players`; p9 y p2 solo aparecen en los eventos.
    assertEquals(b.players, List("p1", "p9", "p2"))
