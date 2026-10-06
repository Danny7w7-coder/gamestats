package gamestats.rules

import gamestats.Fixtures
import gamestats.json.Codecs
import gamestats.model.*
import gamestats.stats.Analyzer

import java.time.Instant

class RulesSuite extends munit.FunSuite:

  def load(name: String): MatchBatch = Codecs.parseBatch(Fixtures.json(name)).fold(fail(_), identity)

  def patternsOf(batch: MatchBatch): List[Pattern] =
    Analyzer.analyze(batch).fold(fail(_), _.patterns)

  val raceOk: MatchBatch       = load("racing_match_ok.json")
  val raceComeback: MatchBatch = load("comeback_case.json")
  val footOk: MatchBatch       = load("football_match_ok.json")
  val footComeback: MatchBatch = load("football_comeback.json")

  // --- Fixtures: los de remontada dan el patrón, los *_ok no dan ninguno -------

  test("comeback_case: p3 va 3° -> 2° -> 1° y gana => COMEBACK"):
    assertEquals(
      patternsOf(raceComeback),
      List(Pattern("COMEBACK", Some("p3"), "p3 remontó desde el 3° lugar y ganó la carrera"))
    )

  test("football_comeback: goles B, A, A => COMEBACK del equipo A, sin playerId"):
    assertEquals(
      patternsOf(footComeback),
      List(Pattern("COMEBACK", None, "El equipo A remontó y ganó 2-1"))
    )

  test("los fixtures *_ok no devuelven patrones"):
    assertEquals(patternsOf(raceOk), Nil)
    assertEquals(patternsOf(footOk), Nil)

  // --- Racing -------------------------------------------------------------------

  test("racing: el ganador nunca bajó del 2° -> sin COMEBACK"):
    assertEquals(RacingComebackRule.positionTimeline(raceOk)("p1").max, 2)
    assertEquals(RacingComebackRule.detect(raceOk), Nil)

  test("racing: ir 3° sin ganar no es COMEBACK"):
    assertEquals(RacingComebackRule.positionTimeline(raceOk)("p3").max, 3)
    assertEquals(RacingComebackRule.detect(raceOk).flatMap(_.playerId), Nil)

  test("racing: POSITION_CHANGED.from también cuenta como posición ocupada"):
    // Se quita la vuelta 1 de p3 (position 3): el 3° solo queda en from=3 del adelantamiento.
    val noLap1 = raceComeback.copy(events = raceComeback.events.filterNot(_.eventId == "r2-e07"))
    assertEquals(RacingComebackRule.detect(noLap1).map(_.playerId), List(Some("p3")))

  test("racing: sin MATCH_FINISHED no hay ganador ni COMEBACK"):
    val unfinished = raceComeback.copy(events = raceComeback.events.filterNot(_.eventType == EventType.MatchFinished))
    assertEquals(RacingComebackRule.detect(unfinished), Nil)

  // --- Football -----------------------------------------------------------------

  test("football: goles A, A, B => sin COMEBACK (A nunca fue perdiendo)"):
    val end = FootballComebackRule.replayGoals(footOk)
    assertEquals(end.score, Map("A" -> 2, "B" -> 1))
    assertEquals(end.everBehind, Set("B"))
    assertEquals(FootballComebackRule.detect(footOk), Nil)

  test("football: goles B, A, A => A estuvo perdiendo y ganó"):
    assertEquals(FootballComebackRule.replayGoals(footComeback).everBehind, Set("A", "B"))

  test("football: empate => sin COMEBACK aunque alguien haya ido perdiendo"):
    // foot-002 sin el último gol de A: B 1-0, luego 1-1.
    val draw = footComeback.copy(events = footComeback.events.filterNot(_.eventId == "f2-e15"))
    assertEquals(FootballComebackRule.replayGoals(draw).everBehind, Set("A"))
    assertEquals(FootballComebackRule.detect(draw), Nil)

  // --- HAT_TRICK ------------------------------------------------------------------

  /** foot-001 con un gol más de p1 al final (3 en total). */
  val hatTrick: MatchBatch =
    val goal = footOk.events.find(_.eventId == "f1-e18").getOrElse(fail("falta f1-e18"))
    val extra = goal.copy(eventId = "f1-extra", timestamp = Instant.parse("2026-10-05T17:28:00Z"))
    footOk.copy(events = (footOk.events :+ extra).sortBy(_.timestamp))

  test("HAT_TRICK: 3 goles del mismo jugador"):
    assertEquals(HatTrickRule.detect(hatTrick), List(Pattern("HAT_TRICK", Some("p1"), "p1 anotó 3 goles")))
    assert(patternsOf(hatTrick).exists(_.rule == "HAT_TRICK"))

  test("HAT_TRICK: con 2 goles no hay patrón"):
    assertEquals(HatTrickRule.detect(footOk), Nil)

  // --- Integración ------------------------------------------------------------------

  test("cada juego corre solo sus reglas"):
    assertEquals(gamestats.stats.RacingAnalyzer.rules.map(_.name), List("COMEBACK"))
    assertEquals(gamestats.stats.FootballAnalyzer.rules.map(_.name), List("COMEBACK", "HAT_TRICK"))
