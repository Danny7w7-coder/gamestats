package gamestats.stats

import gamestats.Fixtures
import gamestats.api.AnalyticsRoutes
import gamestats.json.Codecs
import gamestats.model.*

/** Racing arcade: objetos, choques, respawn y abandono (race-003 y race-997). */
class RacingArcadeSuite extends munit.FunSuite:

  def validated(name: String): MatchBatch =
    AnalyticsRoutes.parseBody(Fixtures.text(name)).fold(fail(_), identity)

  val full: MatchBatch        = validated("racing_full_ok.json")
  val analysis: MatchAnalysis = Analyzer.analyze(full).fold(fail(_), identity)

  def metricsOf(player: String): Map[String, Double] =
    analysis.playerStats.find(_.playerId == player).map(_.metrics).getOrElse(fail(s"no hay stats de $player"))

  // --- race-003 -------------------------------------------------------------------

  test("racing_full_ok: ningún evento se descarta y gana p2"):
    assertEquals(full.skipped, Nil)
    assertEquals(analysis.matchStats.totalEvents, 47)
    assertEquals(analysis.matchStats.winner, Some("p2"))

  test("p1: 2 adelantamientos, 4 objetos agarrados y usados, 2 nitros"):
    val p1 = metricsOf("p1")
    assertEquals(p1("overtakes"), 2.0)
    assertEquals(p1("itemsPicked"), 4.0)
    assertEquals(p1("itemsUsed"), 4.0)
    assertEquals(p1("boostsUsed"), 2.0)
    assertEquals(p1("penaltySeconds"), 5.0)
    assertEquals(p1("bestLapMs"), 83000.0)

  test("el choque cuenta para quien lo provoca (p3), no para quien lo recibe (p1)"):
    assertEquals(metricsOf("p3")("collisions"), 1.0)
    assertEquals(metricsOf("p1")("collisions"), 0.0)

  test("patrones: p2 gana limpio y p1 usa los tres objetos"):
    assertEquals(analysis.patterns, List(
      Pattern("CLEAN_RACE", Some("p2"), "p2 ganó sin choques ni penalizaciones"),
      Pattern("ITEM_MASTER", Some("p1"), "p1 usó los tres objetos (boost, shield y oil)")
    ))

  test("CLEAN_RACE: si el ganador recibió un choque ya no es carrera limpia"):
    // p3 choca con p1 en race-003; si p1 hubiera ganado, no sería limpia.
    assert(!gamestats.rules.CleanRaceRule.clean(full, "p1"))
    assert(gamestats.rules.CleanRaceRule.clean(full, "p2"))

  // --- race-997: exactamente los bad-* se descartan -------------------------------------

  test("racing_invalid_events: se descartan exactamente los eventos bad-*"):
    val b = validated("racing_invalid_events.json")
    val skippedIds = b.skipped.map(_.takeWhile(_ != ':')).toSet
    val expected = Codecs.readJson(Fixtures.text("racing_invalid_events.json")).toOption.get("events").arr
      .map(_("eventId").str).filter(_.startsWith("bad-")).toSet
    assertEquals(skippedIds, expected)
    assertEquals(expected.size, 20)

  test("los motivos de descarte dicen qué regla se rompió"):
    val reasons = validated("racing_invalid_events.json").skipped
    assert(reasons.contains("bad-ctx-pickup-hand-full: agarra oil con boost en la mano"), reasons)
    assert(reasons.contains("bad-ctx-use-item-twice: usa boost sin tenerlo en la mano"), reasons)
    assert(reasons.contains("bad-ctx-respawn-twice: RESPAWN sin un choque o trompo antes en esa vuelta"), reasons)
    assert(reasons.contains("bad-ctx-after-dnf: p3 ya abandonó la carrera (DNF)"), reasons)
    assert(reasons.contains("bad-ctx-lap-over-max: vuelta 3, pero la carrera tiene 2"), reasons)

  // --- Lectura de las acciones nuevas (rangos y valores del schema) --------------------

  test("ITEM_USED: el boost necesita durationMs; el escudo no"):
    assertEquals(
      Codecs.parseAction("ITEM_USED", ujson.Obj("lap" -> 1, "item" -> "boost")),
      Left("ITEM_USED.data: el boost necesita 'durationMs'")
    )
    assertEquals(Codecs.parseAction("ITEM_USED", ujson.Obj("lap" -> 1, "item" -> "shield")), Right(ItemUsed(1, Item.Shield, None)))

  test("valores fuera del schema se rechazan con un mensaje claro"):
    assertEquals(
      Codecs.parseAction("ITEM_PICKUP", ujson.Obj("lap" -> 1, "item" -> "rocket")),
      Left("ITEM_PICKUP.data: 'item' debe ser boost|oil|shield, llegó 'rocket'")
    )
    assertEquals(
      Codecs.parseAction("COLLISION", ujson.Obj("lap" -> 1, "otherId" -> "p2", "damage" -> 150)),
      Left("COLLISION.data: 'damage' debe estar entre 0 y 100")
    )
    assertEquals(
      Codecs.parseAction("LAP_COMPLETED", ujson.Obj("lap" -> 0, "lapTimeMs" -> 1, "position" -> 1)),
      Left("LAP_COMPLETED.data: 'lap' debe ser ≥ 1")
    )
