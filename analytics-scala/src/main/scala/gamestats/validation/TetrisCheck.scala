package gamestats.validation

import gamestats.model.*

/** Validaciones de contexto de tetris, de un solo jugador (contracts/README.md). */
object TetrisCheck extends ContextCheck:

  /** player: el único jugador (el primero que aparece). level: nivel actual.
    * toppedOut: ya perdió (TOP_OUT).
    */
  case class State(player: Option[String], level: Int, toppedOut: Boolean)

  def initial(batch: MatchBatch): State = State(None, startLevel(batch), toppedOut = false)

  /** MATCH_STARTED.data.startLevel; si falta, se empieza en el nivel 1. */
  def startLevel(batch: MatchBatch): Int =
    batch.events
      .find(_.eventType == EventType.MatchStarted)
      .flatMap(_.data.objOpt)
      .flatMap(_.get("startLevel"))
      .flatMap(_.numOpt)
      .fold(1)(_.toInt)

  def check(s: State, event: Event): Either[String, State] =
    event.playerId match
      case None => Right(s)
      case Some(p) if s.player.exists(_ != p) =>
        Left(s"tetris es de un jugador: la partida es de ${s.player.getOrElse("")}, no de $p")
      case Some(p) if s.toppedOut => Left(s"$p ya perdió (TOP_OUT)")
      case Some(p) =>
        val withPlayer = s.copy(player = Some(p))
        event.action match
          case Some(LevelUp(level)) =>
            Either.cond(level > s.level, withPlayer.copy(level = level), s"el nivel $level no es mayor que el actual (${s.level})")
          case Some(TopOut) => Right(withPlayer.copy(toppedOut = true))
          case _            => Right(withPlayer)
