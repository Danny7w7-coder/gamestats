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

/** Objetos de racing. `key` es el nombre con el que viajan en el JSON. */
enum Item(val key: String):
  case Boost  extends Item("boost")
  case Shield extends Item("shield")
  case Oil    extends Item("oil")

enum DnfReason(val key: String):
  case Crash        extends DnfReason("crash")
  case Mechanical   extends DnfReason("mechanical")
  case Disqualified extends DnfReason("disqualified")

/** Golpes básicos de fighting. */
enum Move(val key: String):
  case Punch    extends Move("punch")
  case Kick     extends Move("kick")
  case Uppercut extends Move("uppercut")

/** Cómo se ganó un round: por KO o porque se acabó el tiempo. */
enum WinMethod(val key: String):
  case Ko      extends WinMethod("ko")
  case Timeout extends WinMethod("timeout")

/** Acción de juego dentro de un PLAYER_ACTION. `sealed` hace que el compilador
  * avise si un `match` olvida algún caso.
  */
sealed trait GameAction:

  /** Nombre con el que viaja en el JSON (`action`). Se usa para `eventsByAction`. */
  def actionName: String = this match
    case _: LapCompleted     => "LAP_COMPLETED"
    case _: PositionChanged  => "POSITION_CHANGED"
    case _: Penalty          => "PENALTY"
    case _: Overtake         => "OVERTAKE"
    case _: Lapped           => "LAPPED"
    case _: ItemPickup       => "ITEM_PICKUP"
    case _: ItemUsed         => "ITEM_USED"
    case _: Drift            => "DRIFT"
    case _: Collision        => "COLLISION"
    case _: Spin             => "SPIN"
    case _: Respawn          => "RESPAWN"
    case _: Dnf              => "DNF"
    case _: Pass             => "PASS"
    case _: Shot             => "SHOT"
    case _: PowerShot        => "POWER_SHOT"
    case _: Goal             => "GOAL"
    case _: OwnGoal          => "OWN_GOAL"
    case _: Foul             => "FOUL"
    case _: Save             => "SAVE"
    case _: Tackle           => "TACKLE"
    case _: Interception     => "INTERCEPTION"
    case _: Dribble          => "DRIBBLE"
    case _: Corner           => "CORNER"
    case _: Attack           => "ATTACK"
    case _: SpecialMove      => "SPECIAL_MOVE"
    case _: Combo            => "COMBO"
    case _: Throw            => "THROW"
    case _: Block            => "BLOCK"
    case _: ComboBreaker     => "COMBO_BREAKER"
    case _: Knockdown        => "KNOCKDOWN"
    case _: FatalBlow        => "FATAL_BLOW"
    case _: RoundWon         => "ROUND_WON"
    case _: Fatality         => "FATALITY"
    case UnknownAction(n, _) => n

// Racing
case class LapCompleted(lap: Int, lapTimeMs: Long, position: Int) extends GameAction
case class PositionChanged(from: Int, to: Int, lap: Int)            extends GameAction
case class Penalty(seconds: Int, reason: String)                    extends GameAction
case class Overtake(lap: Int, overtakenId: String)                  extends GameAction
case class Lapped(lap: Int, lappedId: String)                       extends GameAction
case class ItemPickup(lap: Int, item: Item)                         extends GameAction
/** `durationMs` solo lo trae el boost (nitro). */
case class ItemUsed(lap: Int, item: Item, durationMs: Option[Long]) extends GameAction
case class Drift(lap: Int, corner: Int, durationMs: Long)           extends GameAction
case class Collision(lap: Int, otherId: String, damage: Int)        extends GameAction
case class Spin(lap: Int, corner: Int)                              extends GameAction
case class Respawn(lap: Int, position: Int)                         extends GameAction
case class Dnf(lap: Int, reason: DnfReason)                         extends GameAction

// Football
case class Pass(teamId: String, targetId: String, success: Boolean, distance: Double, minute: Int) extends GameAction
case class Shot(teamId: String, onTarget: Boolean, minute: Int)                                    extends GameAction
case class Goal(teamId: String, assistId: Option[String], minute: Int)                             extends GameAction
case class Foul(teamId: String, card: Card, minute: Int, targetId: Option[String])                 extends GameAction
case class PowerShot(teamId: String, onTarget: Boolean, minute: Int)                               extends GameAction
/** `teamId` es el equipo de quien lo hace: el gol cuenta para el rival. */
case class OwnGoal(teamId: String, minute: Int)                                                    extends GameAction
case class Save(teamId: String, minute: Int)                                                       extends GameAction
case class Tackle(teamId: String, targetId: String, success: Boolean, minute: Int)                 extends GameAction
case class Interception(teamId: String, minute: Int)                                               extends GameAction
case class Dribble(teamId: String, success: Boolean, minute: Int)                                  extends GameAction
case class Corner(teamId: String, minute: Int)                                                     extends GameAction

// Fighting (estilo Mortal Kombat)
case class Attack(targetId: String, move: Move, hit: Boolean, damage: Int, round: Int)              extends GameAction
case class SpecialMove(targetId: String, name: String, hit: Boolean, damage: Int, round: Int)       extends GameAction
case class Combo(targetId: String, hits: Int, damage: Int, round: Int)                             extends GameAction
case class Throw(targetId: String, damage: Int, round: Int)                                        extends GameAction
case class Block(attackerId: String, round: Int)                                                   extends GameAction
case class ComboBreaker(attackerId: String, round: Int)                                            extends GameAction
case class Knockdown(targetId: String, round: Int)                                                 extends GameAction
/** `health` es la vida de quien lo usa (máximo 30: solo se puede con poca vida). */
case class FatalBlow(targetId: String, hit: Boolean, damage: Int, health: Int, round: Int)         extends GameAction
case class RoundWon(round: Int, remainingHealth: Int, method: WinMethod)                           extends GameAction
case class Fatality(targetId: String, name: String)                                                extends GameAction

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
