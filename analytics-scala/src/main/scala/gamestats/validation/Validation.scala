package gamestats.validation

import gamestats.model.*

/** Validaciones de un juego que dependen de otros eventos de la partida (por
  * ejemplo, "no se puede usar un objeto que no se agarró"). Cada juego lleva su
  * propio estado (`State`) y lo va actualizando evento a evento.
  */
trait ContextCheck:
  type State
  def initial(batch: MatchBatch): State

  /** Right(estado nuevo): el evento es coherente. Left(motivo): se descarta. */
  def check(state: State, event: Event): Either[String, State]

object ContextCheck:

  /** Juego sin validaciones de contexto propias. */
  object NoCheck extends ContextCheck:
    type State = Unit
    def initial(batch: MatchBatch): Unit = ()
    def check(state: Unit, event: Event): Either[String, Unit] = Right(())

/** Aplica las validaciones de contexto a un lote ya parseado. Pura: devuelve el
  * mismo lote sin los eventos incoherentes, anotados en `skipped`.
  */
object Validation:

  /** Campos de `data` que nombran a otro jugador de la partida. */
  val PlayerRefs: List[String] = List("targetId", "overtakenId", "otherId", "lappedId", "attackerId", "assistId")

  /** Lo que se acumula al recorrer los eventos: el estado de las reglas comunes
    * (pausa), el del juego, y los eventos aceptados y descartados (al revés).
    */
  private case class Acc[S](paused: Boolean, state: S, kept: List[Event], skipped: List[String])

  /** foldLeft recorre los eventos en orden. Un evento pasa si cumple las reglas
    * comunes y las del juego; si no, se descarta y el estado queda igual, así un
    * evento inválido nunca cambia lo que se sabe de la partida.
    */
  def run(batch: MatchBatch, game: ContextCheck): MatchBatch =
    val start = Acc[game.State](paused = false, game.initial(batch), Nil, Nil)
    val end = batch.events.foldLeft(start) { (acc, event) =>
      val next =
        for
          paused <- pause(acc.paused, event)
          _      <- references(batch.players, event)
          state  <- game.check(acc.state, event)
        yield acc.copy(paused = paused, state = state, kept = event :: acc.kept)
      next.fold(reason => acc.copy(skipped = s"${event.eventId}: $reason" :: acc.skipped), identity)
    }
    // Se acumuló con :: (al principio de la lista), por eso se invierte al final.
    batch.copy(events = end.kept.reverse, skipped = batch.skipped ++ end.skipped.reverse)

  /** MATCH_PAUSED y MATCH_RESUMED se alternan, y no hay acciones durante la pausa. */
  private def pause(paused: Boolean, event: Event): Either[String, Boolean] =
    event.eventType match
      case EventType.MatchPaused  => Either.cond(!paused, true, "la partida ya estaba en pausa")
      case EventType.MatchResumed => Either.cond(paused, false, "MATCH_RESUMED sin una pausa antes")
      case EventType.PlayerAction => Either.cond(!paused, paused, "acción durante la pausa")
      case _                      => Right(paused)

  /** Cada referencia a otro jugador debe ser alguien de la partida y no el mismo
    * que hace la acción. Se lee de `data` porque el campo es el mismo en todos los juegos.
    */
  private def references(players: List[String], event: Event): Either[String, Unit] =
    val fields = event.data.objOpt.map(_.toMap).getOrElse(Map.empty[String, ujson.Value])
    PlayerRefs
      .flatMap(key => fields.get(key).flatMap(_.strOpt).map(key -> _))
      .collectFirst {
        case (key, id) if event.playerId.contains(id) => s"'$key' es el mismo jugador que hace la acción"
        case (key, id) if !players.contains(id)       => s"'$key' ('$id') no es un jugador de la partida"
      }
      .toLeft(())
