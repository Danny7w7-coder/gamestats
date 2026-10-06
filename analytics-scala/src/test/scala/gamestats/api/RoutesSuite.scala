package gamestats.api

import gamestats.Fixtures

class RoutesSuite extends munit.FunSuite:

  test("analyzeBody devuelve un MatchAnalysis con matchStats reales"):
    val r = AnalyticsRoutes.analyzeBody(Fixtures.text("racing_match_ok.json")).fold(fail(_), identity)
    assertEquals(r("matchId").str, "race-001")
    assertEquals(r("gameId").str, "racing")
    assertEquals(r("matchStats")("totalEvents").num, 17.0)
    assertEquals(r("matchStats")("durationSeconds").num, 250.0)

  test("analyzeBody rechaza un lote sin matchId"):
    assertEquals(AnalyticsRoutes.analyzeBody(Fixtures.text("invalid_missing_matchid.json")), Left("falta 'matchId'"))

  test("analyzeBody rechaza un juego sin analyzer"):
    val body = Fixtures.text("racing_match_ok.json").replace("\"racing\"", "\"chess\"")
    assertEquals(AnalyticsRoutes.analyzeBody(body), Left("no hay analyzer para gameId 'chess'"))

  test("analyzeBody rechaza JSON inválido"):
    assert(AnalyticsRoutes.analyzeBody("{no es json").left.exists(_.startsWith("JSON inválido")))
