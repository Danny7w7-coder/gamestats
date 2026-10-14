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

/** `key` es el nombre con el que viaja cada valor en el JSON. */
enum DnfReason(val key: String):
  case Crash        extends DnfReason("crash")
  case Mechanical   extends DnfReason("mechanical")
  case Disqualified extends DnfReason("disqualified")

/** Golpes básicos de fighting. */
enum Move(val key: String):
  case Punch extends Move("punch")
  case Kick  extends Move("kick")

/** Cómo se ganó un round: por KO o porque se acabó el tiempo. */
enum WinMethod(val key: String):
  case Ko      extends WinMethod("ko")
  case Timeout extends WinMethod("timeout")

/** Cómo terminó empatado un round: los dos en 0 a la vez, o tiempo con la misma vida. */
enum DrawMethod(val key: String):
  case DoubleKo extends DrawMethod("double_ko")
  case Timeout  extends DrawMethod("timeout")

/** Dirección del 4 en línea ganador. */
enum Line(val key: String):
  case Horizontal extends Line("horizontal")
  case Vertical   extends Line("vertical")
  case Diagonal   extends Line("diagonal")

/** Las 7 piezas de tetris; `key` es la letra con la que viajan en el JSON. */
enum Piece(val key: String):
  case I extends Piece("I")
  case O extends Piece("O")
  case T extends Piece("T")
  case S extends Piece("S")
  case Z extends Piece("Z")
  case J extends Piece("J")
  case L extends Piece("L")

enum Side(val key: String):
  case Left  extends Side("left")
  case Right extends Side("right")

enum Rotation(val key: String):
  case Clockwise        extends Rotation("cw")
  case CounterClockwise extends Rotation("ccw")

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
    case _: Collision        => "COLLISION"
    case _: Respawn          => "RESPAWN"
    case _: Dnf              => "DNF"
    case _: Attack           => "ATTACK"
    case _: Combo            => "COMBO"
    case _: Block            => "BLOCK"
    case _: Knockdown        => "KNOCKDOWN"
    case _: RoundWon         => "ROUND_WON"
    case _: RoundDraw        => "ROUND_DRAW"
    case _: DiscDropped      => "DISC_DROPPED"
    case _: ThreatCreated    => "THREAT_CREATED"
    case _: ThreatBlocked    => "THREAT_BLOCKED"
    case _: ConnectFour      => "CONNECT_FOUR"
    case _: TurnTimeout      => "TURN_TIMEOUT"
    case _: Undo             => "UNDO"
    case _: HintUsed         => "HINT_USED"
    case _: PieceSpawned     => "PIECE_SPAWNED"
    case _: PieceMoved       => "PIECE_MOVED"
    case _: PieceRotated     => "PIECE_ROTATED"
    case _: SoftDrop         => "SOFT_DROP"
    case _: HardDrop         => "HARD_DROP"
    case _: Hold             => "HOLD"
    case _: PiecePlaced      => "PIECE_PLACED"
    case _: LinesCleared     => "LINES_CLEARED"
    case _: TSpin            => "T_SPIN"
    case _: AllClear         => "ALL_CLEAR"
    case _: LevelUp          => "LEVEL_UP"
    case _: StackHeight      => "STACK_HEIGHT"
    case TopOut              => "TOP_OUT"
    case UnknownAction(n, _) => n

// Racing
case class LapCompleted(lap: Int, lapTimeMs: Long, position: Int) extends GameAction
case class PositionChanged(from: Int, to: Int, lap: Int)            extends GameAction
case class Penalty(seconds: Int, reason: String)                    extends GameAction
case class Overtake(lap: Int, overtakenId: String)                  extends GameAction
case class Lapped(lap: Int, lappedId: String)                       extends GameAction
case class Collision(lap: Int, otherId: String, damage: Int)        extends GameAction
case class Respawn(lap: Int, position: Int)                         extends GameAction
case class Dnf(lap: Int, reason: DnfReason)                         extends GameAction

// Fighting (pelea básica 1 contra 1 por rounds)
case class Attack(targetId: String, move: Move, hit: Boolean, damage: Int, round: Int) extends GameAction
case class Combo(targetId: String, hits: Int, damage: Int, round: Int)                extends GameAction
case class Block(attackerId: String, round: Int)                                      extends GameAction
case class Knockdown(targetId: String, round: Int)                                    extends GameAction
case class RoundWon(round: Int, remainingHealth: Int, method: WinMethod)              extends GameAction
/** Round empatado entre quien lo manda y `otherId`: no suma para nadie. */
case class RoundDraw(round: Int, method: DrawMethod, otherId: String)                 extends GameAction

// Connect4 (4 en línea: 7 columnas × 6 filas, la fila 1 es la de abajo)
case class DiscDropped(column: Int, row: Int)                        extends GameAction
/** Casilla vacía que le completaría un 4 en línea al jugador. */
case class ThreatCreated(column: Int, row: Int)                      extends GameAction
case class ThreatBlocked(column: Int, row: Int, attackerId: String)  extends GameAction
case class ConnectFour(line: Line)                                   extends GameAction
/** Se acabó el tiempo del turno (`seconds` = límite por turno). */
case class TurnTimeout(seconds: Int)                                 extends GameAction
/** Deshace la última ficha del tablero (solo contra la IA). */
case class Undo(column: Int, row: Int)                               extends GameAction
case class HintUsed(column: Int)                                     extends GameAction

// Tetris (un solo jugador)
case class PieceSpawned(piece: Piece)                          extends GameAction
case class PieceMoved(direction: Side)                         extends GameAction
case class PieceRotated(rotation: Rotation)                    extends GameAction
case class SoftDrop(cells: Int)                                extends GameAction
case class HardDrop(cells: Int)                                extends GameAction
case class Hold(piece: Piece)                                  extends GameAction
case class PiecePlaced(piece: Piece, hardDrop: Boolean)        extends GameAction
case class LinesCleared(lines: Int, points: Int, combo: Int)   extends GameAction
case class TSpin(lines: Int)                                   extends GameAction
case class AllClear(lines: Int)                                extends GameAction
case class LevelUp(level: Int)                                 extends GameAction
case class StackHeight(rows: Int)                              extends GameAction
/** Game over: la pila llegó arriba. No lleva datos. */
case object TopOut                                             extends GameAction

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
