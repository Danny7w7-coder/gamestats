package gamestats.api

class RoutesSuite extends munit.FunSuite:

  test("summarize cuenta los eventos de un lote con matchId"):
    val body = """{"matchId":"race-001","events":[{},{},{}]}"""
    assertEquals(
      AnalyticsRoutes.summarize(body),
      Right(ujson.Obj("matchId" -> "race-001", "received" -> 3))
    )

  test("summarize rechaza un lote sin matchId"):
    assertEquals(AnalyticsRoutes.summarize("""{"events":[]}"""), Left("falta matchId"))

  test("summarize rechaza JSON inválido"):
    assert(AnalyticsRoutes.summarize("{no es json").isLeft)
