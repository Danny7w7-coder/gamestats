package gamestats.stats

import gamestats.Fixtures
import gamestats.json.Codecs
import gamestats.model.*

class StatsSuite extends munit.FunSuite:

  def load(name: String): MatchBatch =
    Codecs.parseBatch(Fixtures.json(name)).fold(fail(_), identity)

  test("eventsByAction de racing suma exactamente los eventos PLAYER_ACTION"):
    val b = load("racing_match_ok.json")
    val stats = GenericStats.matchStats(b)
    val playerActions = b.events.count(_.eventType == EventType.PlayerAction)
    assertEquals(stats.eventsByAction.values.sum, playerActions)
    assertEquals(stats.eventsByAction, Map("LAP_COMPLETED" -> 9, "POSITION_CHANGED" -> 2, "PENALTY" -> 1))

  test("matchStats genéricas de racing"):
    val stats = GenericStats.matchStats(load("racing_match_ok.json"))
    assertEquals(stats.durationSeconds, 250L)
    assertEquals(stats.totalEvents, 17)
    assertEquals(stats.playerCount, 3)
    assertEquals(stats.winner, None)

  test("matchStats genéricas de connect4"):
    val stats = GenericStats.matchStats(load("connect4_draw.json"))
    assertEquals(stats.durationSeconds, 176L)
    assertEquals(stats.totalEvents, 50)
    assertEquals(stats.playerCount, 2)
    assertEquals(stats.eventsByAction, Map("DISC_DROPPED" -> 42, "THREAT_CREATED" -> 2, "THREAT_BLOCKED" -> 2))

  test("una acción desconocida se cuenta con su propio nombre"):
    val b = load("racing_match_ok.json")
    val dance = b.events.last.copy(eventId = "x", eventType = EventType.PlayerAction,
      playerId = Some("p1"), action = Some(UnknownAction("DANCE", ujson.Obj())))
    assertEquals(GenericStats.eventsByAction(dance :: b.events).get("DANCE"), Some(1))

  test("gameId desconocido devuelve Left"):
    val b = load("racing_match_ok.json").copy(gameId = "chess")
    assertEquals(Analyzer.analyze(b), Left("no hay analyzer para gameId 'chess'"))

  test("los cuatro juegos del contrato tienen analyzer"):
    for name <- List("racing_match_ok.json", "fighting_match_ok.json", "connect4_match_ok.json", "tetris_match_ok.json") do
      assert(Analyzer.analyze(load(name)).isRight, name)

  test("encode: winner null y solo los campos del contrato"):
    val a = BaseAnalyzer("racing").analyze(load("racing_match_ok.json"))
    val json = Codecs.encode(a)
    assertEquals(json("matchStats")("winner"), ujson.Null)
    assertEquals(json.obj.keySet.toSet, Set("matchId", "gameId", "matchStats", "playerStats", "patterns"))
    assertEquals(json("playerStats"), ujson.Arr())

  test("encode: escribe el ganador, las métricas y un patrón sin jugador con playerId null"):
    val a = MatchAnalysis(
      matchId = "c4-001",
      gameId = "connect4",
      matchStats = MatchStats(173, 37, 2, Map("DISC_DROPPED" -> 21), Some("p2")),
      playerStats = List(PlayerStats("p2", Map("discs" -> 11.0))),
      patterns = List(Pattern("TEST", None, "sin jugador"))
    )
    val json = Codecs.encode(a)
    assertEquals(json("matchStats")("winner"), ujson.Str("p2"))
    assertEquals(json("playerStats")(0)("metrics")("discs").num, 11.0)
    assertEquals(json("patterns")(0)("playerId"), ujson.Null)
