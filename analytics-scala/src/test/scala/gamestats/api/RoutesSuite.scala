package gamestats.api

import gamestats.Fixtures

class RoutesSuite extends munit.FunSuite:

  test("summarize responde matchId y cantidad de eventos de un lote válido"):
    val r = AnalyticsRoutes.summarize(Fixtures.text("racing_match_ok.json")).fold(fail(_), identity)
    assertEquals(r("matchId").str, "race-001")
    assertEquals(r("received").num, 17.0)

  test("summarize rechaza un lote sin matchId"):
    assertEquals(AnalyticsRoutes.summarize(Fixtures.text("invalid_missing_matchid.json")), Left("falta 'matchId'"))

  test("summarize rechaza JSON inválido"):
    assert(AnalyticsRoutes.summarize("{no es json").left.exists(_.startsWith("JSON inválido")))
