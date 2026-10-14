package gamestats.stats

import gamestats.Fixtures
import gamestats.api.AnalyticsRoutes
import gamestats.json.Codecs
import gamestats.model.*

/** Racing por posiciones: choques, respawn, abandono y vueltas lideradas (race-003 y race-997). */
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
    assertEquals(analysis.matchStats.totalEvents, 32)
    assertEquals(analysis.matchStats.winner, Some("p2"))

  test("p1: 2 adelantamientos, 5 s de penalización, ninguna vuelta liderada"):
    val p1 = metricsOf("p1")
    assertEquals(p1("overtakes"), 2.0)
    assertEquals(p1("penaltySeconds"), 5.0)
    assertEquals(p1("bestLapMs"), 83000.0)
    assertEquals(p1("lapsLed"), 0.0)

  test("p2 cerró sus 3 vueltas en 1°"):
    assertEquals(metricsOf("p2")("lapsLed"), 3.0)

  test("el choque cuenta para quien lo provoca"):
    // p1 choca a p3 en la vuelta 1 y p3 choca a p1 en la vuelta 2.
    assertEquals(metricsOf("p1")("collisions"), 1.0)
    assertEquals(metricsOf("p3")("collisions"), 1.0)
    assertEquals(metricsOf("p2")("collisions"), 0.0)

  test("patrones: p2 gana limpio y liderando de principio a fin"):
    assertEquals(analysis.patterns, List(
      Pattern("CLEAN_RACE", Some("p2"), "p2 ganó sin choques ni penalizaciones"),
      Pattern("WIRE_TO_WIRE", Some("p2"), "p2 lideró la carrera de principio a fin")
    ))

  test("CLEAN_RACE: si el ganador estuvo en un choque ya no es carrera limpia"):
    assert(!gamestats.rules.CleanRaceRule.clean(full, "p1"))
    assert(gamestats.rules.CleanRaceRule.clean(full, "p2"))

  // --- race-997: exactamente los bad-* se descartan -------------------------------------

  test("racing_invalid_events: se descartan exactamente los eventos bad-*"):
    val b = validated("racing_invalid_events.json")
    val skippedIds = b.skipped.map(_.takeWhile(_ != ':')).toSet
    val expected = Codecs.readJson(Fixtures.text("racing_invalid_events.json")).toOption.get("events").arr
      .map(_("eventId").str).filter(_.startsWith("bad-")).toSet
    assertEquals(skippedIds, expected)
    assertEquals(expected.size, 18)

  test("los motivos de descarte dicen qué regla se rompió"):
    val reasons = validated("racing_invalid_events.json").skipped
    assert(reasons.contains("bad-ctx-respawn-without-collision: RESPAWN sin un choque antes en esa vuelta"), reasons)
    assert(reasons.contains("bad-ctx-respawn-twice: RESPAWN sin un choque antes en esa vuelta"), reasons)
    assert(reasons.contains("bad-ctx-after-dnf: p3 ya abandonó la carrera (DNF)"), reasons)
    assert(reasons.contains("bad-ctx-lap-over-max: vuelta 3, pero la carrera tiene 2"), reasons)

  test("el que recibe el choque también puede volver a la pista"):
    // En race-997, p1 choca a p2 (ri-e05) y el RESPAWN de p2 (ri-e06) es válido.
    assert(validated("racing_invalid_events.json").events.exists(_.eventId == "ri-e06"))

  // --- Lectura (rangos y valores del schema) --------------------------------------------

  test("valores fuera del schema se rechazan con un mensaje claro"):
    assertEquals(
      Codecs.parseAction("DNF", ujson.Obj("lap" -> 1, "reason" -> "tired")),
      Left("DNF.data: 'reason' debe ser crash|disqualified|mechanical, llegó 'tired'")
    )
    assertEquals(
      Codecs.parseAction("COLLISION", ujson.Obj("lap" -> 1, "otherId" -> "p2", "damage" -> 150)),
      Left("COLLISION.data: 'damage' debe estar entre 0 y 100")
    )
    assertEquals(
      Codecs.parseAction("LAP_COMPLETED", ujson.Obj("lap" -> 0, "lapTimeMs" -> 1, "position" -> 1)),
      Left("LAP_COMPLETED.data: 'lap' debe ser ≥ 1")
    )
