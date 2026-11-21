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

  test("p1: 265 de daño, conectó 5 de 6 golpes sueltos y los 13 de sus combos, combo de 6"):
    val p1 = metricsOf(fight1, "p1")
    assertEquals(p1("damageDealt"), 265.0)
    assertEqualsDouble(p1("accuracy"), 18.0 / 19, 1e-9)
    assertEquals(p1("maxCombo"), 6.0)
    assertEquals((p1("blocks"), p1("knockdowns"), p1("roundsWon")), (2.0, 2.0, 2.0))

  test("p2: falló 1 de sus 4 golpes sueltos y conectó los 3 de su combo -> precisión 6/7"):
    val p2 = metricsOf(fight1, "p2")
    assertEqualsDouble(p2("accuracy"), 6.0 / 7, 1e-9)
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

  test("pelea cortada a la mitad (se cerró ganando 1 de 3) -> sin ganador"):
    val b = validated("fighting_comeback.json")
    // Hasta el final del round 1 (lo gana p3) y después el MATCH_FINISHED: la pelea quedó
    // a la mitad (el ganador no depende de los PLAYER_LEFT, por eso no hacen falta aquí).
    val upToRound1 = b.events.takeWhile(e => !e.action.exists { case RoundWon(1, _, _) => true; case _ => false })
    val round1 = b.events.find(_.action.exists { case RoundWon(1, _, _) => true; case _ => false }).toList
    val closed = b.copy(events = upToRound1 ++ round1 :+ b.events.last)
    assertEquals(FightingAnalyzer.roundWinners(closed), List(1 -> "p3"))
    assertEquals(FightingAnalyzer.winner(closed), None)

  test("se jugaron todos los rounds sin llegar a la mayoría (1 ganado y 2 empates) -> gana el que más ganó"):
    val b = validated("fighting_comeback.json")
    val draws = b.events.map { e =>
      e.action match
        case Some(RoundWon(r, _, _)) if r >= 2 => e.copy(action = Some(RoundDraw(r, DrawMethod.DoubleKo, "p3")))
        case _                                 => e
    }
    assertEquals(FightingAnalyzer.winner(b.copy(events = draws)), Some("p3"))

  test("precisión: un combo cuenta sus golpes, aunque no haya ATTACK sueltos"):
    val solo = FightingAnalyzer.accuracy(List(
      Event("e1", java.time.Instant.EPOCH, "fighting", "1.0", "m", Some("p1"), EventType.PlayerAction,
        Some(Combo("p2", 4, 40, 1)), ujson.Obj())
    ))
    assertEquals(solo, 1.0)

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
