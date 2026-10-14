package gamestats.stats

import gamestats.Fixtures
import gamestats.api.AnalyticsRoutes
import gamestats.json.Codecs
import gamestats.model.*
import gamestats.validation.Connect4Check

/** 4 en línea (c4-001, c4-002, c4-003 y c4-997). */
class Connect4Suite extends munit.FunSuite:

  def validated(name: String): MatchBatch =
    AnalyticsRoutes.parseBody(Fixtures.text(name)).fold(fail(_), identity)

  def analyzed(name: String): MatchAnalysis =
    Analyzer.analyze(validated(name)).fold(fail(_), identity)

  val human: MatchAnalysis = analyzed("connect4_match_ok.json")
  val vsBot: MatchAnalysis = analyzed("connect4_vs_bot.json")
  val draw: MatchAnalysis  = analyzed("connect4_draw.json")

  def metricsOf(a: MatchAnalysis, player: String): Map[String, Double] =
    a.playerStats.find(_.playerId == player).map(_.metrics).getOrElse(fail(s"no hay stats de $player"))

  // --- c4-001: persona contra persona ------------------------------------------------

  test("c4-001: sin descartes, gana p2"):
    assertEquals(validated("connect4_match_ok.json").skipped, Nil)
    assertEquals(human.matchStats.winner, Some("p2"))
    assertEquals(human.matchStats.totalEvents, 37)

  test("c4-001: métricas por jugador"):
    val p1 = metricsOf(human, "p1")
    assertEquals((p1("discs"), p1("threatsCreated"), p1("threatsBlocked"), p1("turnTimeouts")), (10.0, 2.0, 1.0, 1.0))
    assertEqualsDouble(p1("centerControl"), 0.2, 1e-9) // 2 de sus 10 fichas en la columna 4
    val p2 = metricsOf(human, "p2")
    assertEquals((p2("discs"), p2("threatsCreated"), p2("threatsBlocked"), p2("hintsUsed")), (11.0, 2.0, 2.0, 1.0))

  test("c4-001: p2 tapó las dos amenazas de p1"):
    assertEquals(human.patterns, List(Pattern("PERFECT_DEFENSE", Some("p2"), "p2 tapó las 2 amenazas de p1")))

  // --- c4-002: contra la IA, con UNDO ---------------------------------------------------

  test("c4-002: gana p1 con doble amenaza y 4 fichas"):
    assertEquals(vsBot.matchStats.winner, Some("p1"))
    assertEquals(vsBot.patterns, List(
      Pattern("DOUBLE_THREAT", Some("p1"), "p1 tuvo 2 amenazas abiertas a la vez"),
      Pattern("QUICK_WIN", Some("p1"), "p1 ganó con solo 4 fichas")
    ))

  test("c4-002: las fichas deshechas cuentan como soltadas, pero no quedan en el tablero"):
    assertEquals(metricsOf(vsBot, "p1")("discs"), 5.0)
    assertEquals(metricsOf(vsBot, "p1")("undos"), 2.0)
    val board = Connect4Check.timeline(validated("connect4_vs_bot.json")).last.board
    assertEquals(board.discs.values.count(_ == "p1"), 4)
    assertEquals(board.discs.values.count(_ == "cpu"), 3)

  test("UNDO vuelve el tablero, las amenazas y el turno a como estaban"):
    val b = validated("connect4_vs_bot.json")
    val states = Connect4Check.timeline(b)
    // Estado antes de la ficha (1, 1) de p1 y estado después de los dos UNDO.
    val beforeMistake = states(b.events.indexWhere(_.eventId == "c2-e08"))
    val afterUndos    = states(b.events.indexWhere(_.eventId == "c2-e13") + 1)
    assertEquals(afterUndos.board, beforeMistake.board)
    assertEquals(afterUndos.board.turn, Some("p1"))

  // --- c4-003: empate -------------------------------------------------------------------

  test("c4-003: tablero lleno sin 4 en línea -> empate"):
    assertEquals(draw.matchStats.winner, None)
    assertEquals(draw.matchStats.eventsByAction("DISC_DROPPED"), 42)
    assertEquals(Connect4Check.timeline(validated("connect4_draw.json")).last.board.discs.size, 42)
    assertEquals(draw.patterns, List(Pattern("PERFECT_DEFENSE", Some("p4"), "p4 tapó las 2 amenazas de p3")))

  // --- c4-997 ---------------------------------------------------------------------------

  test("connect4_invalid_events: se descartan exactamente los eventos bad-*"):
    val b = validated("connect4_invalid_events.json")
    val skippedIds = b.skipped.map(_.takeWhile(_ != ':')).toSet
    val expected = ujson.read(Fixtures.text("connect4_invalid_events.json"))("events").arr
      .map(_("eventId").str).filter(_.startsWith("bad-")).toSet
    assertEquals(skippedIds, expected)
    assertEquals(expected.size, 19)

  test("los motivos de descarte de connect4"):
    val reasons = validated("connect4_invalid_events.json").skipped
    assert(reasons.contains("bad-ctx-third-player: connect4 es de dos: ya juegan p1 y p2"), reasons)
    assert(reasons.contains("bad-ctx-out-of-turn: no es el turno de p2"), reasons)
    assert(reasons.contains("bad-ctx-floating-disc: la ficha cae en la fila 1 de la columna 4, no en la 3"), reasons)
    assert(reasons.contains("bad-ctx-undo-against-human: UNDO solo vale para el humano en una partida contra la IA"), reasons)
    assert(reasons.contains("bad-ctx-block-without-threat: p1 no tiene una amenaza abierta en (3, 1)"), reasons)
    assert(reasons.contains("bad-ctx-four-not-last-disc: p2 no soltó la última ficha, no puede hacer el 4 en línea"), reasons)
    assert(reasons.contains("bad-ctx-after-win: la partida ya terminó con el 4 en línea"), reasons)

  test("una columna llena no acepta más fichas"):
    // Seis fichas alternadas en la columna 1 (sin 4 en línea) y una séptima.
    val base = validated("connect4_match_ok.json")
    val joins = base.events.take(3) // MATCH_STARTED y los dos PLAYER_JOINED
    val drops = (1 to 7).toList.map { i =>
      val p = if i % 2 == 1 then "p1" else "p2"
      joins.last.copy(eventId = s"d$i", playerId = Some(p), eventType = EventType.PlayerAction,
        action = Some(DiscDropped(1, i.min(6))), timestamp = joins.last.timestamp.plusSeconds(i.toLong))
    }
    val b = gamestats.validation.Validation.run(base.copy(events = joins ++ drops), Connect4Check)
    assertEquals(b.skipped, List("d7: la columna 1 está llena"))

  test("lectura: columna y fila fuera del tablero"):
    assertEquals(
      Codecs.parseAction("DISC_DROPPED", ujson.Obj("column" -> 8, "row" -> 1)),
      Left("DISC_DROPPED.data: 'column' debe estar entre 1 y 7")
    )
    assertEquals(Codecs.parseAction("CONNECT_FOUR", ujson.Obj("direction" -> "diagonal")), Right(ConnectFour(Line.Diagonal)))

  test("el registry usa Connect4Analyzer para connect4"):
    assertEquals(Analyzer.registry.get("connect4"), Some(Connect4Analyzer))
