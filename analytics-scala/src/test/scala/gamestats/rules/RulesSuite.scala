package gamestats.rules

import gamestats.Fixtures
import gamestats.json.Codecs
import gamestats.model.*
import gamestats.stats.Analyzer

class RulesSuite extends munit.FunSuite:

  def load(name: String): MatchBatch = Codecs.parseBatch(Fixtures.json(name)).fold(fail(_), identity)

  def patternsOf(batch: MatchBatch): List[Pattern] =
    Analyzer.analyze(batch).fold(fail(_), _.patterns)

  val raceOk: MatchBatch       = load("racing_match_ok.json")
  val raceComeback: MatchBatch = load("comeback_case.json")

  // --- Fixtures: los de remontada dan el patrón, los *_ok no dan ninguno -------

  test("comeback_case: p3 va 3° -> 2° -> 1° y gana => COMEBACK (y además sin choques: CLEAN_RACE)"):
    assertEquals(
      patternsOf(raceComeback),
      List(
        Pattern("COMEBACK", Some("p3"), "p3 remontó desde el 3° lugar y ganó la carrera"),
        Pattern("CLEAN_RACE", Some("p3"), "p3 ganó sin choques ni penalizaciones")
      )
    )

  test("racing_match_ok no tiene remontada; p1 gana limpio"):
    // p3 tiene la penalización, no p1: la carrera del ganador es limpia.
    assertEquals(patternsOf(raceOk), List(Pattern("CLEAN_RACE", Some("p1"), "p1 ganó sin choques ni penalizaciones")))

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

  // --- WIRE_TO_WIRE ---------------------------------------------------------------

  test("WIRE_TO_WIRE: el ganador que estuvo 2° en algún momento no cuenta"):
    assertEquals(WireToWireRule.detect(raceOk), Nil)
    assertEquals(WireToWireRule.detect(raceComeback), Nil)

  test("WIRE_TO_WIRE: el ganador que solo tiene posiciones 1 sí cuenta"):
    // race-001 sin los eventos donde p1 aparece fuera del 1°.
    val onlyFirst = raceOk.copy(events = raceOk.events.filterNot {
      case Event(_, _, _, _, _, Some("p1"), _, Some(LapCompleted(_, _, pos)), _) => pos != 1
      case Event(_, _, _, _, _, Some("p1"), _, Some(_: PositionChanged), _)      => true
      case _                                                                      => false
    })
    assertEquals(WireToWireRule.detect(onlyFirst), List(Pattern("WIRE_TO_WIRE", Some("p1"), "p1 lideró la carrera de principio a fin")))

  // --- Integración ------------------------------------------------------------------

  test("cada juego corre solo sus reglas"):
    assertEquals(gamestats.stats.RacingAnalyzer.rules.map(_.name), List("COMEBACK", "CLEAN_RACE", "WIRE_TO_WIRE"))
    assertEquals(gamestats.stats.FightingAnalyzer.rules.map(_.name), List("COMEBACK", "FLAWLESS_VICTORY", "COMBO_MASTER"))
    assertEquals(gamestats.stats.Connect4Analyzer.rules.map(_.name), List("DOUBLE_THREAT", "QUICK_WIN", "PERFECT_DEFENSE"))
