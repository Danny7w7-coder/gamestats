package gamestats.stats

import gamestats.Fixtures
import gamestats.api.AnalyticsRoutes
import gamestats.json.Codecs
import gamestats.model.*

class SummariesSuite extends munit.FunSuite:

  /** Partida armada a mano: solo lo que usan los resúmenes. */
  def m(id: String, game: String, secs: Long, events: Int, winner: Option[String],
        players: List[(String, Map[String, Double])] = Nil, rules: List[String] = Nil,
        actions: Map[String, Int] = Map.empty): MatchAnalysis =
    MatchAnalysis(id, game, MatchStats(secs, events, players.size, actions, winner),
      players.map((p, ms) => PlayerStats(p, ms)), rules.map(Pattern(_, None, "")))

  val all = List(
    m("c4-1", "connect4", 100, 20, Some("ana"), List("ana" -> Map("discs" -> 10.0), "cpu" -> Map("discs" -> 9.0)),
      actions = Map("DISC_DROPPED" -> 19)),
    m("c4-2", "connect4", 200, 30, Some("cpu"), List("ana" -> Map("discs" -> 15.0), "cpu" -> Map("discs" -> 16.0)),
      actions = Map("DISC_DROPPED" -> 31, "UNDO" -> 1)),
    m("c4-3", "connect4", 300, 40, Some("cpu"), List("beto" -> Map("discs" -> 4.0), "cpu" -> Map("discs" -> 4.0)),
      rules = List("QUICK_WIN")),
    m("t-1", "tetris", 60, 50, None, List("ana" -> Map("score" -> 1200.0)), rules = List("TETRIS_MASTER", "QUICK_WIN")),
    m("x-1", "snake", 30, 5, None)
  )

  test("game: totales, promedios y partidas sin ganador"):
    val g = Summaries.game("connect4", all)
    assertEquals(g.matches, 3)
    assertEquals(g.players, 3) // ana, cpu, beto
    assertEquals(g.totalEvents, 90)
    assertEquals(g.avgDurationSeconds, 200.0)
    assertEquals(g.avgEventsPerMatch, 30.0)
    assertEquals(g.matchesWithoutWinner, 0)

  test("game: ganadores de más a menos"):
    assertEquals(Summaries.game("connect4", all).winners, List(Ranked("cpu", 2), Ranked("ana", 1)))

  test("game: métricas con promedio, mínimo y máximo de todos los jugadores"):
    // discs: 10, 9, 15, 16, 4, 4 -> promedio 58/6 = 9.67
    assertEquals(Summaries.game("connect4", all).metrics, Map("discs" -> MetricSummary(9.67, 4.0, 16.0)))

  test("game: acciones sumadas entre partidas y patrones contados"):
    val g = Summaries.game("connect4", all)
    assertEquals(g.actions, Map("DISC_DROPPED" -> 50, "UNDO" -> 1))
    assertEquals(g.patterns, List(Ranked("QUICK_WIN", 1)))

  test("game: un juego sin partidas da todo en cero (no falla)"):
    val g = Summaries.game("fighting", all)
    assertEquals((g.matches, g.players, g.avgDurationSeconds, g.winners, g.metrics), (0, 0, 0.0, Nil, Map.empty))

  test("game: tetris (un jugador) cuenta sus partidas sin ganador"):
    assertEquals(Summaries.game("tetris", all).matchesWithoutWinner, 1)

  test("overview: totales y un resumen por juego, incluido un gameId nuevo"):
    val o = Summaries.overview(all)
    assertEquals(o.matches, 5)
    assertEquals(o.players, 3)
    assertEquals(o.totalEvents, 145)
    assertEquals(o.avgDurationSeconds, 138.0)
    assertEquals(o.games.map(g => g.gameId -> g.matches), List("connect4" -> 3, "snake" -> 1, "tetris" -> 1))
    assertEquals(o.topWinners, List(Ranked("cpu", 2), Ranked("ana", 1)))
    assertEquals(o.topPatterns, List(Ranked("QUICK_WIN", 2), Ranked("TETRIS_MASTER", 1)))

  test("overview sin partidas"):
    val o = Summaries.overview(Nil)
    assertEquals((o.matches, o.games, o.avgDurationSeconds), (0, Nil, 0.0))

  test("standings: partidas, ganadas, perdidas y porcentaje de cada jugador del juego"):
    val s = Summaries.game("connect4", all).standings
    // cpu: 3 partidas, ganó 2 (c4-2, c4-3) y perdió 1 (c4-1)
    assertEquals(s.map(_.playerId), List("cpu", "ana", "beto"))
    val cpu = s.head
    assertEquals((cpu.played, cpu.wins, cpu.losses, cpu.noResult, cpu.winRate), (3, 2, 1, 0, 0.67))
    assertEquals(cpu.metrics("discs"), MetricSummary(9.67, 4.0, 16.0))
    val beto = s.last
    assertEquals((beto.played, beto.wins, beto.losses, beto.winRate), (1, 0, 1, 0.0))

  test("standings general: suma juegos, cuenta las partidas sin ganador y no mezcla métricas"):
    val ana = Summaries.overview(all).standings.find(_.playerId == "ana").get
    // ana: c4-1 ganó, c4-2 perdió, t-1 (tetris) sin ganador
    assertEquals((ana.played, ana.wins, ana.losses, ana.noResult, ana.winRate), (3, 1, 1, 1, 0.33))
    assertEquals(ana.metrics, Map.empty)

  test("standings: con las mismas victorias, va primero el de mejor porcentaje"):
    val ms = List(
      m("a", "connect4", 10, 5, Some("x"), List("x" -> Map(), "y" -> Map())),
      m("b", "connect4", 10, 5, Some("y"), List("x" -> Map(), "y" -> Map())),
      m("c", "connect4", 10, 5, Some("z"), List("y" -> Map(), "z" -> Map())))
    // x: 1/2, y: 1/3, z: 1/1
    assertEquals(Summaries.standings(ms, withMetrics = false).map(_.playerId), List("z", "x", "y"))

  test("rank desempata por nombre"):
    assertEquals(Summaries.rank(List("b", "a", "b", "c", "a")), List(Ranked("a", 2), Ranked("b", 2), Ranked("c", 1)))

  test("con los fixtures reales: un resumen por cada juego analizado"):
    val names = List("racing_match_ok.json", "fighting_match_ok.json", "connect4_match_ok.json",
      "connect4_vs_bot.json", "connect4_draw.json", "tetris_match_ok.json")
    val real = names.map(n => AnalyticsRoutes.analyzeBody(Fixtures.text(n)).fold(fail(_), identity))
    val o = Summaries.overview(real)
    assertEquals(o.matches, 6)
    assertEquals(o.games.map(_.gameId), List("connect4", "fighting", "racing", "tetris"))
    val c4 = o.games.find(_.gameId == "connect4").get
    assertEquals(c4.matches, 3)
    assertEquals(c4.matchesWithoutWinner, real.count(a => a.gameId == "connect4" && a.matchStats.winner.isEmpty))
    assert(c4.metrics.contains("discs"))

  test("encode: forma del JSON de /stats/games/{id}"):
    val json = Codecs.encodeGameSummary(Summaries.game("connect4", all))
    assertEquals(json("winners")(0)("playerId").str, "cpu")
    assertEquals(json("winners")(0)("wins").num, 2.0)
    assertEquals(json("metrics")("discs")("max").num, 16.0)
    assertEquals(json("patterns")(0)("rule").str, "QUICK_WIN")
    val first = json("standings")(0)
    assertEquals((first("playerId").str, first("played").num, first("wins").num, first("losses").num), ("cpu", 3.0, 2.0, 1.0))
    assertEquals(first("winRate").num, 0.67)
    assertEquals(first("metrics")("discs")("avg").num, 9.67)

  test("encode: forma del JSON de /stats/overview"):
    val json = Codecs.encodeOverview(Summaries.overview(all))
    assertEquals(json("matches").num, 5.0)
    assertEquals(json("games").arr.map(_("gameId").str).toList, List("connect4", "snake", "tetris"))
    assertEquals(json("topPatterns")(0)("count").num, 2.0)
