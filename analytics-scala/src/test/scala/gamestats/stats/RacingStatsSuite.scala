package gamestats.stats

import gamestats.Fixtures
import gamestats.json.Codecs
import gamestats.model.*

class RacingStatsSuite extends munit.FunSuite:

  val batch: MatchBatch = Codecs.parseBatch(Fixtures.json("racing_match_ok.json")).fold(fail(_), identity)
  val analysis: MatchAnalysis = RacingAnalyzer.analyze(batch)

  def metricsOf(a: MatchAnalysis, player: String): Map[String, Double] =
    a.playerStats.find(_.playerId == player).map(_.metrics).getOrElse(fail(s"no hay stats de $player"))

  test("p1: mejor vuelta 80900 y promedio ≈ 81466.67"):
    val p1 = metricsOf(analysis, "p1")
    assertEquals(p1("bestLapMs"), 80900.0)
    assertEqualsDouble(p1("avgLapMs"), 81466.67, 0.01)

  test("p1 gana: winner = ranking.head"):
    assertEquals(analysis.matchStats.winner, Some("p1"))

  test("p3 tiene 5 s de penalización y su tiempo efectivo la suma"):
    val p3 = metricsOf(analysis, "p3")
    assertEquals(p3("penaltySeconds"), 5.0)
    // 83000 + 82800 + 82500 + 5 * 1000
    assertEquals(p3("effectiveTimeMs"), 253300.0)

  test("consistencia = desviación estándar de las vueltas"):
    // p1: promedio 81466.67; desviaciones 533.33, 33.33, -566.67
    assertEqualsDouble(metricsOf(analysis, "p1")("consistency"), 449.69, 0.01)
    assertEquals(RacingAnalyzer.stdDev(List(80000L, 80000L, 80000L)), 0.0)

  test("un jugador sin vueltas no revienta: métricas en 0.0"):
    val withGhost = batch.copy(players = batch.players :+ "p9")
    val p9 = metricsOf(RacingAnalyzer.analyze(withGhost), "p9")
    assertEquals(p9.values.toSet, Set(0.0))
    assertEquals(p9.keySet, Set("bestLapMs", "avgLapMs", "consistency", "penaltySeconds", "effectiveTimeMs",
      "overtakes", "itemsPicked", "itemsUsed", "boostsUsed", "collisions"))

  test("sin MATCH_FINISHED no hay ganador"):
    val unfinished = batch.copy(events = batch.events.filterNot(_.eventType == EventType.MatchFinished))
    assertEquals(RacingAnalyzer.analyze(unfinished).matchStats.winner, None)

  test("avgLapMs es la composición lapTimes andThen average"):
    val events = batch.events.filter(_.playerId.contains("p2"))
    assertEquals(RacingAnalyzer.avgLapMs(events), RacingAnalyzer.average(RacingAnalyzer.lapTimes(events)))
    assertEquals(RacingAnalyzer.average(Nil), 0.0)

  test("racing no tiene score ni teamStats y mantiene las estadísticas genéricas"):
    assertEquals(analysis.matchStats.score, None)
    assertEquals(analysis.teamStats, None)
    assertEquals(analysis.matchStats.totalEvents, 17)
    assertEquals(analysis.playerStats.map(_.playerId), List("p1", "p2", "p3"))

  test("el registry usa RacingAnalyzer para racing"):
    assertEquals(Analyzer.registry.get("racing"), Some(RacingAnalyzer))
