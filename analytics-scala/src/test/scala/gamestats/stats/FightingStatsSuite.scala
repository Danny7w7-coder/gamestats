package gamestats.stats

import gamestats.Fixtures
import gamestats.api.AnalyticsRoutes
import gamestats.json.Codecs
import gamestats.model.*

/** Fighting estilo Mortal Kombat (fight-001, fight-002 y fight-997). */
class FightingStatsSuite extends munit.FunSuite:

  def validated(name: String): MatchBatch =
    AnalyticsRoutes.parseBody(Fixtures.text(name)).fold(fail(_), identity)

  def analyzed(name: String): MatchAnalysis =
    Analyzer.analyze(validated(name)).fold(fail(_), identity)

  val fight1: MatchAnalysis = analyzed("fighting_match_ok.json")
  val fight2: MatchAnalysis = analyzed("fighting_comeback.json")

  def metricsOf(a: MatchAnalysis, player: String): Map[String, Double] =
    a.playerStats.find(_.playerId == player).map(_.metrics).getOrElse(fail(s"no hay stats de $player"))

  // --- fight-001: Scorpion 2-0 Sub-Zero ----------------------------------------------

  test("fight-001: sin descartes, gana p1 (2 rounds)"):
    assertEquals(validated("fighting_match_ok.json").skipped, Nil)
    assertEquals(fight1.matchStats.winner, Some("p1"))
    assertEquals(fight1.teamStats, None)

  test("p1: 200 de daño (100 por round), todos sus golpes conectaron"):
    val p1 = metricsOf(fight1, "p1")
    assertEquals(p1("damageDealt"), 200.0)
    assertEquals(p1("accuracy"), 1.0)
    assertEquals(p1("maxCombo"), 4.0)
    assertEquals((p1("specials"), p1("blocks"), p1("comboBreakers"), p1("knockdowns"), p1("roundsWon")), (1.0, 1.0, 1.0, 1.0, 2.0))

  test("p2: falló 2 de sus 4 golpes -> precisión 0.5"):
    val p2 = metricsOf(fight1, "p2")
    assertEquals(p2("accuracy"), 0.5)
    assertEquals(p2("damageDealt"), 75.0)
    assertEquals(p2("maxCombo"), 5.0)
    assertEquals(p2("roundsWon"), 0.0)

  test("patrones de fight-001: round 1 sin daño y fatality"):
    assertEquals(fight1.patterns, List(
      Pattern("FLAWLESS_VICTORY", Some("p1"), "p1 ganó el round 1 sin recibir daño"),
      Pattern("FATALITY", Some("p1"), "p1 remató a p2 con una fatality (toasty)")
    ))

  // --- fight-002: remontada de Kitana ---------------------------------------------------

  test("fight-002: p3 gana el round 1, p4 los otros dos -> COMEBACK de p4"):
    assertEquals(fight2.matchStats.winner, Some("p4"))
    assertEquals(fight2.patterns, List(Pattern("COMEBACK", Some("p4"), "p4 perdió el primer round y ganó la pelea")))

  test("ganar el primer round y la pelea no es remontada"):
    assertEquals(fight1.patterns.map(_.rule).contains("COMEBACK"), false)

  test("empate en rounds -> sin ganador"):
    val b = validated("fighting_comeback.json")
    val oneEach = b.copy(events = b.events.filterNot(_.action.exists { case RoundWon(3, _, _) => true; case _ => false }))
    assertEquals(FightingAnalyzer.winner(oneEach), None)

  // --- fight-997 -------------------------------------------------------------------------

  test("fighting_invalid_events: se descartan exactamente los eventos bad-*"):
    val b = validated("fighting_invalid_events.json")
    val skippedIds = b.skipped.map(_.takeWhile(_ != ':')).toSet
    val expected = ujson.read(Fixtures.text("fighting_invalid_events.json"))("events").arr
      .map(_("eventId").str).filter(_.startsWith("bad-")).toSet
    assertEquals(skippedIds, expected)
    assertEquals(expected.size, 16)

  test("los motivos de descarte de fighting"):
    val reasons = validated("fighting_invalid_events.json").skipped
    assert(reasons.contains("bad-ctx-breaker-without-combo: no hay un combo de p1 que cortar en el round 1"), reasons)
    assert(reasons.contains("bad-ctx-fatal-blow-twice: p1 ya usó su FATAL_BLOW"), reasons)
    assert(reasons.contains("bad-ctx-fatality-by-loser: p2 no ganó la pelea, no puede hacer la FATALITY"), reasons)
    assert(reasons.contains("bad-ctx-after-fatality: la pelea ya terminó con la FATALITY"), reasons)
    assert(reasons.contains("bad-ctx-round-over-max: round 2, pero la pelea es a 1"), reasons)

  test("un golpe fallado con daño se rechaza al leerlo"):
    assertEquals(
      Codecs.parseAction("ATTACK", ujson.Obj("targetId" -> "p2", "move" -> "punch", "hit" -> false, "damage" -> 10, "round" -> 1)),
      Left("ATTACK.data: un golpe que falla no hace daño ('damage' debe ser 0)")
    )
    assertEquals(
      Codecs.parseAction("FATAL_BLOW", ujson.Obj("targetId" -> "p2", "hit" -> true, "damage" -> 40, "health" -> 80, "round" -> 1)),
      Left("FATAL_BLOW.data: 'health' debe estar entre 1 y 30")
    )

  test("el registry usa FightingAnalyzer para fighting"):
    assertEquals(Analyzer.registry.get("fighting"), Some(FightingAnalyzer))
