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
    assertEquals(stats.score, None)

  test("matchStats genéricas de football"):
    val stats = GenericStats.matchStats(load("football_match_ok.json"))
    assertEquals(stats.durationSeconds, 5400L)
    assertEquals(stats.totalEvents, 26)
    assertEquals(stats.playerCount, 4)
    assertEquals(stats.eventsByAction, Map("PASS" -> 7, "SHOT" -> 5, "GOAL" -> 3, "FOUL" -> 3))

  test("una acción desconocida se cuenta con su propio nombre"):
    val b = load("racing_match_ok.json")
    val dance = b.events.last.copy(eventId = "x", eventType = EventType.PlayerAction,
      playerId = Some("p1"), action = Some(UnknownAction("DANCE", ujson.Obj())))
    assertEquals(GenericStats.eventsByAction(dance :: b.events).get("DANCE"), Some(1))

  test("gameId desconocido devuelve Left"):
    val b = load("racing_match_ok.json").copy(gameId = "chess")
    assertEquals(Analyzer.analyze(b), Left("no hay analyzer para gameId 'chess'"))

  test("los dos juegos del contrato tienen analyzer"):
    assert(Analyzer.analyze(load("racing_match_ok.json")).isRight)
    assert(Analyzer.analyze(load("football_match_ok.json")).isRight)

  test("encode: winner null y sin score/teamStats cuando no existen"):
    val a = Analyzer.analyze(load("racing_match_ok.json")).fold(fail(_), identity)
    val json = Codecs.encode(a)
    assertEquals(json("matchStats")("winner"), ujson.Null)
    assert(!json("matchStats").obj.contains("score"))
    assert(!json.obj.contains("teamStats"))
    assertEquals(json("playerStats"), ujson.Arr())

  test("encode: escribe score, teamStats y patrones de equipo con playerId null"):
    val a = MatchAnalysis(
      matchId = "foot-001",
      gameId = "football",
      matchStats = MatchStats(5400, 26, 4, Map("GOAL" -> 3), Some("A"), Some(Map("A" -> 2, "B" -> 1))),
      teamStats = Some(List(TeamStats("A", Map("goals" -> 2.0)))),
      playerStats = List(PlayerStats("p1", Map("goals" -> 2.0))),
      patterns = List(Pattern("COMEBACK", None, "remontada"))
    )
    val json = Codecs.encode(a)
    assertEquals(json("matchStats")("winner"), ujson.Str("A"))
    assertEquals(json("matchStats")("score"), ujson.Obj("A" -> 2, "B" -> 1))
    assertEquals(json("teamStats")(0)("metrics")("goals").num, 2.0)
    assertEquals(json("patterns")(0)("playerId"), ujson.Null)
