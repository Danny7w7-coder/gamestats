package gamestats.model

import java.time.Instant

/** Tipos generales de evento, comunes a todos los juegos. */
enum EventType:
  case MatchStarted, MatchFinished, MatchPaused, MatchResumed, PlayerJoined, PlayerLeft,
       PlayerDisconnected, PlayerReconnected, PlayerAction

  /** Solo los eventos de partida pueden venir sin `playerId`. */
  def requiresPlayer: Boolean = this match
    case MatchStarted | MatchFinished | MatchPaused | MatchResumed => false
    case PlayerJoined | PlayerLeft | PlayerDisconnected | PlayerReconnected | PlayerAction => true

enum Card:
  case None, Yellow, Red

/** Acción de juego dentro de un PLAYER_ACTION. `sealed` hace que el compilador
  * avise si un `match` olvida algún caso.
  */
sealed trait GameAction:

  /** Nombre con el que viaja en el JSON (`action`). Se usa para `eventsByAction`. */
  def actionName: String = this match
    case _: LapCompleted     => "LAP_COMPLETED"
    case _: PositionChanged  => "POSITION_CHANGED"
    case _: Penalty          => "PENALTY"
    case _: Pass             => "PASS"
    case _: Shot             => "SHOT"
    case _: Goal             => "GOAL"
    case _: Foul             => "FOUL"
    case UnknownAction(n, _) => n

// Racing
case class LapCompleted(lap: Int, lapTimeMs: Long, position: Int) extends GameAction
case class PositionChanged(from: Int, to: Int, lap: Int)            extends GameAction
case class Penalty(seconds: Int, reason: String)                    extends GameAction

// Football
case class Pass(teamId: String, targetId: String, success: Boolean, distance: Double, minute: Int) extends GameAction
case class Shot(teamId: String, onTarget: Boolean, minute: Int)                                    extends GameAction
case class Goal(teamId: String, assistId: Option[String], minute: Int)                             extends GameAction
case class Foul(teamId: String, card: Card, minute: Int, targetId: Option[String])                 extends GameAction

/** Acción que ningún juego conoce todavía: se conserva tal cual en vez de fallar. */
case class UnknownAction(name: String, raw: ujson.Value) extends GameAction

case class Event(
    eventId: String,
    timestamp: Instant,
    gameId: String,
    gameVersion: String,
    matchId: String,
    playerId: Option[String],
    eventType: EventType,
    action: Option[GameAction],
    data: ujson.Value
)

/** Lote de una partida ya validado, con los eventos en orden cronológico.
  * `skipped` lista los eventos descartados ("eventId: motivo"), ya sea por tener
  * la `data` de su acción mal formada o por romper una validación de contexto;
  * no participan del análisis.
  */
case class MatchBatch(
    matchId: String,
    gameId: String,
    gameVersion: String,
    startedAt: Instant,
    finishedAt: Instant,
    players: List[String],
    events: List[Event],
    skipped: List[String] = Nil
)

object MatchBatch:

  /** Construye el lote ordenando los eventos y derivando los jugadores. */
  def fromEvents(
      matchId: String,
      gameId: String,
      gameVersion: String,
      startedAt: Instant,
      finishedAt: Instant,
      declaredPlayers: List[String],
      events: List[Event],
      skipped: List[String] = Nil
  ): MatchBatch =
    // sortBy: el orden siempre sale del timestamp, aunque Go mande los eventos desordenados.
    // Es estable, así que dos eventos con el mismo instante conservan el orden de llegada.
    val sorted = events.sortBy(_.timestamp)
    // flatMap sobre Option descarta los eventos sin jugador; distinct quita repetidos
    // y respeta el orden (primero los declarados, luego los que solo aparecen en eventos).
    val players = (declaredPlayers ++ sorted.flatMap(_.playerId)).distinct
    MatchBatch(matchId, gameId, gameVersion, startedAt, finishedAt, players, sorted, skipped)
