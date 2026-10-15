package gamestats.stats

import gamestats.Fixtures
import gamestats.api.AnalyticsRoutes
import gamestats.json.Codecs
import gamestats.model.*

/** Fighting básico 1 contra 1 por rounds (fight-001, fight-002 y fight-997). */
class FightingStatsSuite extends munit.FunSuite:

  def validated(name: String): MatchBatch =
    AnalyticsRoutes.parseBody(Fixtures.text(name)).fold(fail(_), identity)

  def analyzed(name: String): MatchAnalysis =
    Analyzer.analyze(validated(name)).fold(fail(_), identity)

  val fight1: MatchAnalysis = analyzed("fighting_match_ok.json")
  val fight2: MatchAnalysis = analyzed("fighting_comeback.json")

  def metricsOf(a: MatchAnalysis, player: String): Map[String, Double] =
    a.playerStats.find(_.playerId == player).map(_.metrics).getOrElse(fail(s"no hay stats de $player"))

  // --- fight-001: Scorpion 2-0 Sub-Zero, con un round empatado -----------------------

  test("fight-001: sin descartes, gana p1 (2 rounds; el round 2 fue empate)"):
    assertEquals(validated("fighting_match_ok.json").skipped, Nil)
    assertEquals(fight1.matchStats.winner, Some("p1"))

  test("ROUND_DRAW no suma para nadie"):
    assertEquals(FightingAnalyzer.roundWinners(validated("fighting_match_ok.json")), List(1 -> "p1", 3 -> "p1"))

  test("p1: 265 de daño, 5 de 6 golpes conectaron, combo de 6"):
    val p1 = metricsOf(fight1, "p1")
    assertEquals(p1("damageDealt"), 265.0)
    assertEqualsDouble(p1("accuracy"), 5.0 / 6, 1e-9)
    assertEquals(p1("maxCombo"), 6.0)
    assertEquals((p1("blocks"), p1("knockdowns"), p1("roundsWon")), (2.0, 2.0, 2.0))

  test("p2: falló 1 de sus 4 golpes -> precisión 0.75"):
    val p2 = metricsOf(fight1, "p2")
    assertEquals(p2("accuracy"), 0.75)
    assertEquals(p2("damageDealt"), 140.0)
    assertEquals(p2("maxCombo"), 3.0)
    assertEquals(p2("roundsWon"), 0.0)

  test("patrones de fight-001: round 1 sin daño y combo de 6"):
    assertEquals(fight1.patterns, List(
      Pattern("FLAWLESS_VICTORY", Some("p1"), "p1 ganó el round 1 sin recibir daño"),
      Pattern("COMBO_MASTER", Some("p1"), "p1 hizo un combo de 6 golpes")
    ))

  // --- fight-002: remontada de Kitana ---------------------------------------------------

  test("fight-002: p3 gana el round 1, p4 los otros dos -> COMEBACK de p4 (y su combo de 5)"):
    assertEquals(fight2.matchStats.winner, Some("p4"))
    assertEquals(fight2.patterns, List(
      Pattern("COMEBACK", Some("p4"), "p4 perdió el primer round y ganó la pelea"),
      Pattern("COMBO_MASTER", Some("p4"), "p4 hizo un combo de 5 golpes")
    ))

  test("ganar el primer round y la pelea no es remontada"):
    assertEquals(fight1.patterns.map(_.rule).contains("COMEBACK"), false)

  test("empate en rounds -> sin ganador"):
    val b = validated("fighting_comeback.json")
    val oneEach = b.copy(events = b.events.filterNot(_.action.exists { case RoundWon(3, _, _) => true; case _ => false }))
    assertEquals(FightingAnalyzer.winner(oneEach), None)

  test("COMBO_MASTER: un combo de 4 no alcanza"):
    assertEquals(gamestats.rules.ComboMasterRule.detect(validated("fighting_invalid_events.json")), Nil)

  // --- fight-997 -------------------------------------------------------------------------

  test("fighting_invalid_events: se descartan exactamente los eventos bad-*"):
    val b = validated("fighting_invalid_events.json")
    val skippedIds = b.skipped.map(_.takeWhile(_ != ':')).toSet
    val expected = ujson.read(Fixtures.text("fighting_invalid_events.json"))("events").arr
      .map(_("eventId").str).filter(_.startsWith("bad-")).toSet
    assertEquals(skippedIds, expected)
    assertEquals(expected.size, 14)

  test("los motivos de descarte de fighting"):
    val reasons = validated("fighting_invalid_events.json").skipped
    assert(reasons.contains("bad-ctx-round-won-twice: el round 1 ya tiene resultado"), reasons)
    assert(reasons.contains("bad-ctx-draw-after-won: el round 1 ya tiene resultado"), reasons)
    assert(reasons.contains("bad-ctx-draw-self: 'otherId' es el mismo jugador que hace la acción"), reasons)
    assert(reasons.contains("bad-ctx-after-fight-won: la pelea ya terminó: la ganó p1"), reasons)
    assert(reasons.contains("bad-ctx-round-over-max: round 4, pero la pelea es a 3"), reasons)

  test("lectura: golpe fallado con daño, uppercut y empate con método inválido"):
    assertEquals(
      Codecs.parseAction("ATTACK", ujson.Obj("targetId" -> "p2", "move" -> "punch", "hit" -> false, "damage" -> 10, "round" -> 1)),
      Left("ATTACK.data: un golpe que falla no hace daño ('damage' debe ser 0)")
    )
    assertEquals(
      Codecs.parseAction("ATTACK", ujson.Obj("targetId" -> "p2", "move" -> "uppercut", "hit" -> true, "damage" -> 10, "round" -> 1)),
      Left("ATTACK.data: 'move' debe ser kick|punch, llegó 'uppercut'")
    )
    assertEquals(
      Codecs.parseAction("ROUND_DRAW", ujson.Obj("round" -> 2, "method" -> "double_ko", "otherId" -> "p2")),
      Right(RoundDraw(2, DrawMethod.DoubleKo, "p2"))
    )

  test("el registry usa FightingAnalyzer para fighting"):
    assertEquals(Analyzer.registry.get("fighting"), Some(FightingAnalyzer))
