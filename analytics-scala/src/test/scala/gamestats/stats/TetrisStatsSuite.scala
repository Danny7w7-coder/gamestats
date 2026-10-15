package gamestats.stats

import gamestats.Fixtures
import gamestats.api.AnalyticsRoutes
import gamestats.json.Codecs
import gamestats.model.*

/** Tetris de un solo jugador (tetris-001, tetris-002 y tetris-997). */
class TetrisStatsSuite extends munit.FunSuite:

  def validated(name: String): MatchBatch =
    AnalyticsRoutes.parseBody(Fixtures.text(name)).fold(fail(_), identity)

  def analyzed(name: String): MatchAnalysis =
    Analyzer.analyze(validated(name)).fold(fail(_), identity)

  val match1: MatchAnalysis = analyzed("tetris_match_ok.json")
  val match2: MatchAnalysis = analyzed("tetris_perfect_clear.json")

  def metricsOf(a: MatchAnalysis, player: String): Map[String, Double] =
    a.playerStats.find(_.playerId == player).map(_.metrics).getOrElse(fail(s"no hay stats de $player"))

  // --- tetris-001 --------------------------------------------------------------------

  test("tetris-001: un jugador, sin descartes y sin ganador (no hay rival)"):
    assertEquals(validated("tetris_match_ok.json").skipped, Nil)
    assertEquals(match1.playerStats.map(_.playerId), List("p1"))
    assertEquals(match1.matchStats.winner, None)

  test("p1: 3200 puntos, 10 líneas, 2 tetris, 5 piezas en 40 s"):
    val p1 = metricsOf(match1, "p1")
    assertEquals((p1("score"), p1("lines"), p1("tetrises"), p1("piecesPlaced")), (3200.0, 10.0, 2.0, 5.0))
    assertEquals(p1("piecesPerMinute"), 7.5) // 5 piezas / (40 s / 60)
    assertEquals((p1("maxLevel"), p1("maxCombo"), p1("tSpins"), p1("hardDrops")), (2.0, 2.0, 1.0, 2.0))

  test("tetris-001: dos tetris seguidos -> BACK_TO_BACK"):
    assertEquals(match1.patterns, List(Pattern("BACK_TO_BACK", Some("p1"), "p1 hizo dos tetris seguidos (back-to-back)")))

  // --- tetris-002 --------------------------------------------------------------------

  test("tetris-002: tablero vacío y 3 T-spins; el nivel parte de startLevel 5"):
    val p3 = metricsOf(match2, "p3")
    assertEquals((p3("score"), p3("lines"), p3("tSpins"), p3("maxLevel")), (16000.0, 7.0, 3.0, 7.0))
    assertEquals(match2.patterns, List(
      Pattern("PERFECT_CLEAR", Some("p3"), "p3 dejó el tablero vacío (perfect clear)"),
      Pattern("T_SPIN_MASTER", Some("p3"), "p3 hizo 3 T-spins")
    ))

  test("un tetris, otra limpieza y otro tetris no es back-to-back"):
    // tetris-002 limpia 1, 4 y 2 líneas: hay un tetris, pero no dos seguidos.
    assertEquals(match2.patterns.map(_.rule).contains("BACK_TO_BACK"), false)

  test("sin subir de nivel, maxLevel es el nivel inicial"):
    assertEquals(TetrisAnalyzer.maxLevel(Nil, 5), 5.0)

  // --- tetris-997 --------------------------------------------------------------------

  test("tetris_invalid_events: se descartan exactamente los eventos bad-*"):
    val b = validated("tetris_invalid_events.json")
    val skippedIds = b.skipped.map(_.takeWhile(_ != ':')).toSet
    val expected = ujson.read(Fixtures.text("tetris_invalid_events.json"))("events").arr
      .map(_("eventId").str).filter(_.startsWith("bad-")).toSet
    assertEquals(skippedIds, expected)
    assertEquals(expected.size, 11)

  test("los motivos de descarte de tetris"):
    val reasons = validated("tetris_invalid_events.json").skipped
    assert(reasons.contains("bad-ctx-second-player-joined: tetris es de un jugador: la partida es de p1, no de p2"), reasons)
    assert(reasons.contains("bad-ctx-level-not-higher: el nivel 3 no es mayor que el actual (3)"), reasons)
    assert(reasons.contains("bad-ctx-after-top-out: p1 ya perdió (TOP_OUT)"), reasons)
    assert(reasons.contains("bad-data-piece-type: PIECE_SPAWNED.data: 'piece' debe ser I|J|L|O|S|T|Z, llegó 'X'"), reasons)

  test("TOP_OUT se lee sin datos"):
    assertEquals(Codecs.parseAction("TOP_OUT", ujson.Obj()), Right(TopOut))

  test("el registry tiene los 4 juegos"):
    assertEquals(Analyzer.registry.keySet, Set("racing", "fighting", "connect4", "tetris"))
