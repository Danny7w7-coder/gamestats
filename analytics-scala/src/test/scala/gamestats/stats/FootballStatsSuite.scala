package gamestats.stats

import gamestats.Fixtures
import gamestats.json.Codecs
import gamestats.model.*

class FootballStatsSuite extends munit.FunSuite:

  def load(name: String): MatchBatch = Codecs.parseBatch(Fixtures.json(name)).fold(fail(_), identity)

  val batch: MatchBatch       = load("football_match_ok.json")
  val analysis: MatchAnalysis = FootballAnalyzer.analyze(batch)

  def metricsOf(a: MatchAnalysis, player: String): Map[String, Double] =
    a.playerStats.find(_.playerId == player).map(_.metrics).getOrElse(fail(s"no hay stats de $player"))

  def teamOf(a: MatchAnalysis, team: String): Map[String, Double] =
    a.teamStats.getOrElse(Nil).find(_.teamId == team).map(_.metrics).getOrElse(fail(s"no hay stats de $team"))

  /** foot-001 sin un evento concreto (para armar variantes del marcador). */
  def without(eventIds: String*): MatchBatch =
    batch.copy(events = batch.events.filterNot(e => eventIds.contains(e.eventId)))

  test("marcador A 2 - B 1 y gana A"):
    assertEquals(analysis.matchStats.score, Some(Map("A" -> 2, "B" -> 1)))
    assertEquals(analysis.matchStats.winner, Some("A"))

  test("jugador con 3 pases (2 exitosos) -> passAccuracy ≈ 0.667"):
    assertEqualsDouble(metricsOf(analysis, "p2")("passAccuracy"), 0.667, 0.001)

  test("jugador con 0 tiros -> shotEffectiveness = 0.0"):
    val p4 = metricsOf(analysis, "p4")
    assertEquals(p4("shots"), 0.0)
    assertEquals(p4("shotEffectiveness"), 0.0)

  test("jugador sin pases -> passAccuracy = 0.0"):
    // En foot-002, p1 tira y anota pero nunca pasa.
    val p1 = metricsOf(FootballAnalyzer.analyze(load("football_comeback.json")), "p1")
    assertEquals(p1("passAccuracy"), 0.0)

  test("goles, asistencias y efectividad por jugador"):
    val p1 = metricsOf(analysis, "p1")
    assertEquals((p1("goals"), p1("assists"), p1("shots"), p1("shotEffectiveness")), (2.0, 0.0, 2.0, 1.0))
    val p3 = metricsOf(analysis, "p3")
    assertEquals((p3("goals"), p3("shots"), p3("shotEffectiveness")), (1.0, 2.0, 0.5))
    // Las asistencias vienen del assistId de los GOAL de otros.
    assertEquals(metricsOf(analysis, "p2")("assists"), 1.0)
    assertEquals(metricsOf(analysis, "p4")("assists"), 1.0)

  test("teamStats: goles y faltas por equipo"):
    assertEquals(teamOf(analysis, "A"), Map("goals" -> 2.0, "fouls" -> 2.0))
    assertEquals(teamOf(analysis, "B"), Map("goals" -> 1.0, "fouls" -> 1.0))
    assertEquals(analysis.matchStats.eventsByAction.get("FOUL"), Some(3)) // total de faltas

  test("partida 1-1 -> winner = None"):
    val draw = FootballAnalyzer.analyze(without("f1-e18")) // se quita el 2do gol de A
    assertEquals(draw.matchStats.score, Some(Map("A" -> 1, "B" -> 1)))
    assertEquals(draw.matchStats.winner, None)

  test("un equipo sin goles aparece en el marcador con 0"):
    val shutout = FootballAnalyzer.analyze(without("f1-e22")) // se quita el gol de B
    assertEquals(shutout.matchStats.score, Some(Map("A" -> 2, "B" -> 0)))
    assertEquals(teamOf(shutout, "B")("goals"), 0.0)

  test("0-0 -> winner = None"):
    val noGoals = batch.copy(events = batch.events.filterNot(_.action.exists(_.isInstanceOf[Goal])))
    assertEquals(FootballAnalyzer.analyze(noGoals).matchStats.winner, None)

  test("los equipos salen de MATCH_STARTED aunque nadie de ese equipo juegue"):
    val onlyStart = batch.copy(events = batch.events.take(1)) // solo MATCH_STARTED
    assertEquals(FootballAnalyzer.teams(onlyStart), List("A", "B"))

  test("ratio nunca divide entre 0"):
    assertEquals(FootballAnalyzer.ratio(0, 0), 0.0)
    assertEquals(FootballAnalyzer.ratio(3, 0), 0.0)

  test("el registry usa FootballAnalyzer para football"):
    assertEquals(Analyzer.registry.get("football"), Some(FootballAnalyzer))
