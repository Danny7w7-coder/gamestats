package gamestats.validation

import gamestats.Fixtures
import gamestats.api.AnalyticsRoutes
import gamestats.model.*
import gamestats.stats.Analyzer

import java.time.Instant

class ValidationSuite extends munit.FunSuite:

  val t0: Instant = Instant.parse("2026-10-05T14:00:00Z")

  /** Evento de prueba: las acciones van como UnknownAction (aquí solo importa el contexto). */
  def ev(id: String, kind: EventType, player: Option[String] = Some("p1"), data: ujson.Value = ujson.Obj()): Event =
    val action = Option.when(kind == EventType.PlayerAction)(UnknownAction("TEST", data))
    Event(id, t0, "racing", "1.0", "race-001", player, kind, action, data)

  def act(id: String, player: String = "p1", data: ujson.Value = ujson.Obj()): Event =
    ev(id, EventType.PlayerAction, Some(player), data)

  def batch(events: Event*): MatchBatch =
    MatchBatch("race-001", "racing", "1.0", t0, t0, List("p1", "p2"), events.toList)

  def ids(b: MatchBatch): List[String] = b.events.map(_.eventId)

  // --- Pausa --------------------------------------------------------------------

  test("una acción durante la pausa se descarta; antes y después se acepta"):
    val b = Validation.run(
      batch(act("a1"), ev("pausa", EventType.MatchPaused, None), act("a2"), ev("sigue", EventType.MatchResumed, None), act("a3")),
      ContextCheck.NoCheck
    )
    assertEquals(ids(b), List("a1", "pausa", "sigue", "a3"))
    assertEquals(b.skipped, List("a2: acción durante la pausa"))

  test("MATCH_RESUMED sin pausa y una pausa doble se descartan"):
    val b = Validation.run(
      batch(ev("r", EventType.MatchResumed, None), ev("p1", EventType.MatchPaused, None), ev("p2", EventType.MatchPaused, None)),
      ContextCheck.NoCheck
    )
    assertEquals(ids(b), List("p1"))
    assertEquals(b.skipped, List("r: MATCH_RESUMED sin una pausa antes", "p2: la partida ya estaba en pausa"))

  test("en pausa se aceptan eventos que no son acciones (por ejemplo, una desconexión)"):
    val b = Validation.run(
      batch(ev("pausa", EventType.MatchPaused, None), ev("desc", EventType.PlayerDisconnected)),
      ContextCheck.NoCheck
    )
    assertEquals(b.skipped, Nil)

  // --- Referencias a otros jugadores ----------------------------------------------

  test("una referencia a sí mismo o a alguien que no está en la partida se descarta"):
    val b = Validation.run(
      batch(
        act("ok", data = ujson.Obj("targetId" -> "p2")),
        act("mismo", data = ujson.Obj("otherId" -> "p1")),
        act("nadie", data = ujson.Obj("attackerId" -> "p9"))
      ),
      ContextCheck.NoCheck
    )
    assertEquals(ids(b), List("ok"))
    assertEquals(b.skipped, List(
      "mismo: 'otherId' es el mismo jugador que hace la acción",
      "nadie: 'attackerId' ('p9') no es un jugador de la partida"
    ))

  // --- Reglas propias de un juego ---------------------------------------------------

  /** Regla de prueba: cada jugador puede hacer como máximo 2 acciones. */
  object MaxTwo extends ContextCheck:
    type State = Map[String, Int]
    def initial(batch: MatchBatch): State = Map.empty
    def check(state: State, event: Event): Either[String, State] =
      event.playerId.filter(_ => event.eventType == EventType.PlayerAction) match
        case Some(p) =>
          val n = state.getOrElse(p, 0)
          Either.cond(n < 2, state.updated(p, n + 1), "más de 2 acciones")
        case None => Right(state)

  test("las reglas del juego llevan su estado de evento en evento"):
    val b = Validation.run(batch(act("a1"), act("a2"), act("a3"), act("b1", "p2")), MaxTwo)
    assertEquals(ids(b), List("a1", "a2", "b1"))
    assertEquals(b.skipped, List("a3: más de 2 acciones"))

  test("un evento descartado por las reglas comunes no cambia el estado del juego"):
    // a2 cae por la pausa: no cuenta para MaxTwo, así que a3 todavía entra.
    val events = List(act("a1"), ev("pausa", EventType.MatchPaused, None), act("a2"),
      ev("sigue", EventType.MatchResumed, None), act("a3"), act("a4"))
    val b = Validation.run(batch(events*), MaxTwo)
    assertEquals(ids(b).filter(_.startsWith("a")), List("a1", "a3"))
    assertEquals(b.skipped, List("a2: acción durante la pausa", "a4: más de 2 acciones"))

  test("los descartados por formato (Codecs) se conservan delante de los de contexto"):
    val b = Validation.run(batch(act("a1"), ev("r", EventType.MatchResumed, None)).copy(skipped = List("x: data rota")), ContextCheck.NoCheck)
    assertEquals(b.skipped, List("x: data rota", "r: MATCH_RESUMED sin una pausa antes"))

  // --- Integración con el lote real ---------------------------------------------------

  test("los fixtures válidos no pierden ningún evento"):
    for name <- List("racing_match_ok.json", "comeback_case.json", "racing_full_ok.json", "fighting_match_ok.json",
                     "fighting_comeback.json", "connect4_match_ok.json", "connect4_vs_bot.json", "connect4_draw.json",
                     "tetris_match_ok.json", "tetris_perfect_clear.json") do
      val b = AnalyticsRoutes.parseBody(Fixtures.text(name)).fold(fail(_), identity)
      assertEquals(b.skipped, Nil, name)

  test("racing_invalid_events: se descartan los casos de pausa y de referencias"):
    val b = AnalyticsRoutes.parseBody(Fixtures.text("racing_invalid_events.json")).fold(fail(_), identity)
    val skippedIds = b.skipped.map(_.takeWhile(_ != ':'))
    for id <- List("bad-ctx-resume-without-pause", "bad-ctx-action-while-paused", "bad-ctx-overtake-self", "bad-ctx-unknown-player") do
      assert(skippedIds.contains(id), s"$id debería estar descartado: $skippedIds")
    // Ningún evento válido (los que no empiezan con "bad-") se descarta.
    assert(skippedIds.forall(_.startsWith("bad-")), skippedIds)

  test("MATCH_PAUSED y MATCH_RESUMED se leen sin playerId"):
    val b = AnalyticsRoutes.parseBody(Fixtures.text("racing_full_ok.json")).fold(fail(_), identity)
    assertEquals(b.events.count(e => e.eventType == EventType.MatchPaused || e.eventType == EventType.MatchResumed), 2)

  test("si la validación descarta todos los eventos, el lote se rechaza"):
    assertEquals(Analyzer.validate(batch(ev("r", EventType.MatchResumed, None))), Left("todos los eventos se descartaron"))
