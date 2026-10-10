package gamestats.stats

import gamestats.Fixtures
import gamestats.api.AnalyticsRoutes
import gamestats.json.Codecs
import gamestats.model.*
import gamestats.rules.{CleanSheetRule, FootballComebackRule}

/** Football 6 vs 6 arcade: tiro potente, autogol, atajadas, roja y equipos llenos. */
class FootballArcadeSuite extends munit.FunSuite:

  def validated(name: String): MatchBatch =
    AnalyticsRoutes.parseBody(Fixtures.text(name)).fold(fail(_), identity)

  val full: MatchBatch        = validated("football_full_ok.json")
  val analysis: MatchAnalysis = Analyzer.analyze(full).fold(fail(_), identity)

  def metricsOf(player: String): Map[String, Double] =
    analysis.playerStats.find(_.playerId == player).map(_.metrics).getOrElse(fail(s"no hay stats de $player"))

  def teamOf(team: String): Map[String, Double] =
    analysis.teamStats.getOrElse(Nil).find(_.teamId == team).map(_.metrics).getOrElse(fail(s"no hay stats de $team"))

  // --- foot-003 -------------------------------------------------------------------

  test("football_full_ok: 6 vs 6 sin descartes, A gana 3-0 (el autogol de B suma para A)"):
    assertEquals(full.skipped, Nil)
    assertEquals(full.players.size, 12)
    assertEquals(analysis.matchStats.score, Some(Map("A" -> 3, "B" -> 0)))
    assertEquals(analysis.matchStats.winner, Some("A"))

  test("tiro potente: cuenta como tiro y su gol entra en la efectividad"):
    val p4 = metricsOf("p4")
    assertEquals((p4("powerShots"), p4("shots"), p4("goals"), p4("shotEffectiveness")), (1.0, 1.0, 1.0, 1.0))
    // p9 tiró 2 normales y 1 potente, sin goles.
    assertEquals((metricsOf("p9")("shots"), metricsOf("p9")("shotEffectiveness")), (3.0, 0.0))

  test("atajadas, recuperaciones y regates por jugador"):
    assertEquals(metricsOf("p1")("saves"), 2.0)       // portero de A
    assertEquals(metricsOf("p7")("saves"), 1.0)       // portero de B
    assertEquals(metricsOf("p5")("recoveries"), 1.0)  // intercepción
    assertEquals(metricsOf("p6")("recoveries"), 1.0)  // entrada exitosa
    assertEquals(metricsOf("p3")("dribblesWon"), 1.0)
    assertEquals(metricsOf("p8")("dribblesWon"), 0.0) // su regate falló

  test("teamStats: goles, faltas y córners"):
    assertEquals(teamOf("A"), Map("goals" -> 3.0, "fouls" -> 0.0, "corners" -> 1.0))
    assertEquals(teamOf("B"), Map("goals" -> 0.0, "fouls" -> 2.0, "corners" -> 1.0))

  test("CLEAN_SHEET: A gana sin recibir goles"):
    assertEquals(analysis.patterns, List(Pattern("CLEAN_SHEET", None, "El equipo A ganó 3-0 sin recibir goles")))

  test("CLEAN_SHEET: con un gol del rival o con empate no hay patrón"):
    val foot001 = Codecs.parseBatch(Fixtures.json("football_match_ok.json")).fold(fail(_), identity) // 2-1
    assertEquals(CleanSheetRule.detect(foot001), Nil)
    val goalless = full.copy(events = full.events.filterNot(_.action.exists {
      case _: Goal | _: OwnGoal => true
      case _                    => false
    }))
    assertEquals(CleanSheetRule.detect(goalless), Nil) // 0-0

  test("un autogol puede dar vuelta el partido: cuenta en la remontada"):
    // foot-002 (goles B, A, A) con el primer gol de A reemplazado por un autogol de B: igual remonta A.
    val comeback = Codecs.parseBatch(Fixtures.json("football_comeback.json")).fold(fail(_), identity)
    val withOwnGoal = comeback.copy(events = comeback.events.map {
      case e if e.eventId == "f2-e11" => e.copy(action = Some(OwnGoal("B", 44)))
      case e                          => e
    })
    assertEquals(FootballAnalyzer.goalsInOrder(withOwnGoal), List("B", "A", "A"))
    assertEquals(FootballComebackRule.detect(withOwnGoal).map(_.rule), List("COMEBACK"))

  // --- foot-997 ---------------------------------------------------------------------

  test("football_invalid_events: se descartan exactamente los eventos bad-*"):
    val b = validated("football_invalid_events.json")
    val skippedIds = b.skipped.map(_.takeWhile(_ != ':')).toSet
    val expected = ujson.read(Fixtures.text("football_invalid_events.json"))("events").arr
      .map(_("eventId").str).filter(_.startsWith("bad-")).toSet
    assertEquals(skippedIds, expected)
    assertEquals(expected.size, 11)

  test("los motivos de descarte de football"):
    val reasons = validated("football_invalid_events.json").skipped
    assert(reasons.contains("bad-ctx-seventh-player-joined: el equipo A ya tiene 6 jugadores"), reasons)
    assert(reasons.contains("bad-ctx-seventh-player-action: p7 no está en ningún equipo"), reasons)
    assert(reasons.contains("bad-ctx-after-red-card: p9 fue expulsado con roja"), reasons)
    assert(reasons.contains("bad-data-minute-range: CORNER.data: 'minute' debe estar entre 0 y 130"), reasons)
