package gamestats.api

import gamestats.Fixtures
import gamestats.model.MatchAnalysis

class RoutesSuite extends munit.FunSuite:

  def analyzed(name: String): MatchAnalysis =
    AnalyticsRoutes.analyzeBody(Fixtures.text(name)).fold(fail(_), identity)

  test("analyzeBody devuelve un MatchAnalysis con matchStats reales"):
    val a = analyzed("racing_match_ok.json")
    assertEquals(a.matchId, "race-001")
    assertEquals(a.gameId, "racing")
    assertEquals(a.matchStats.totalEvents, 17)
    assertEquals(a.matchStats.durationSeconds, 250L)

  test("analyzeBody rechaza un lote sin matchId"):
    assertEquals(AnalyticsRoutes.analyzeBody(Fixtures.text("invalid_missing_matchid.json")), Left("falta 'matchId'"))

  test("analyzeBody rechaza un juego sin analyzer"):
    val body = Fixtures.text("racing_match_ok.json").replace("\"racing\"", "\"chess\"")
    assertEquals(AnalyticsRoutes.analyzeBody(body), Left("no hay analyzer para gameId 'chess'"))

  test("una vuelta con data rota se descarta y la carrera se analiza igual"):
    // Se rompe lapTimeMs de la 3ra vuelta de p1 (80900 -> "rápido").
    val body = Fixtures.text("racing_match_ok.json").replace("\"lapTimeMs\": 80900", "\"lapTimeMs\": \"rápido\"")
    val batch = AnalyticsRoutes.parseBody(body).fold(fail(_), identity)
    assertEquals(batch.skipped, List("r1-e14: LAP_COMPLETED.data: 'lapTimeMs' debe ser número"))
    val p1 = AnalyticsRoutes.analyzeBody(body).fold(fail(_), identity).playerStats.find(_.playerId == "p1").get
    assertEquals(p1.metrics("bestLapMs"), 81500.0) // sin la vuelta de 80900
    assertEquals(p1.metrics("avgLapMs"), 81750.0)

  test("analyzeBody rechaza JSON inválido"):
    assert(AnalyticsRoutes.analyzeBody("{no es json").left.exists(_.startsWith("JSON inválido")))

  // --- /players/{playerId} ----------------------------------------------------

  val all: List[MatchAnalysis] =
    List("racing_match_ok.json", "comeback_case.json", "connect4_match_ok.json").map(analyzed)

  test("playerReport junta las partidas del jugador, de cualquier juego"):
    val r = AnalyticsRoutes.playerReport("p1", all).getOrElse(fail("p1 debería existir"))
    assertEquals(r("matchesPlayed").num, 3.0)
    assertEquals(r("matches").arr.map(_("matchId").str).toList, List("race-001", "race-002", "c4-001"))
    assertEquals(r("matches")(2)("metrics")("discs").num, 10.0)

  test("playerReport incluye los patrones personales del jugador"):
    val r = AnalyticsRoutes.playerReport("p3", all).getOrElse(fail("p3 debería existir"))
    assertEquals(
      r("patterns").arr.map(p => (p("matchId").str, p("rule").str)).toList,
      List("race-002" -> "COMEBACK", "race-002" -> "CLEAN_RACE")
    )

  test("playerReport de un jugador desconocido es None (404)"):
    assertEquals(AnalyticsRoutes.playerReport("nadie", all), None)
