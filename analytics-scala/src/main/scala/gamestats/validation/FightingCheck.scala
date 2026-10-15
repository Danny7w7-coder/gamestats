package gamestats.validation

import gamestats.model.*

/** Validaciones de contexto de fighting (contracts/README.md). */
object FightingCheck extends ContextCheck:

  /** Lo que se sabe de la pelea hasta el evento actual.
    *  - rounds: máximo de rounds (MATCH_STARTED.data.rounds).
    *  - wins: rounds ganados por jugador.
    *  - roundsDone: rounds que ya tienen resultado (ganador o empate).
    *  - champion: quien ya ganó la pelea (llegó a la mayoría de los rounds).
    */
  case class State(rounds: Option[Int], wins: Map[String, Int], roundsDone: Set[Int], champion: Option[String])

  def initial(batch: MatchBatch): State =
    val rounds = batch.events
      .find(_.eventType == EventType.MatchStarted)
      .flatMap(_.data.objOpt)
      .flatMap(_.get("rounds"))
      .flatMap(_.numOpt)
      .map(_.toInt)
    State(rounds, Map.empty, Set.empty, champion = None)

  /** Rounds que hay que ganar para llevarse la pelea (mayoría: 2 de 3, 3 de 5). */
  def roundsToWin(s: State): Int = s.rounds.getOrElse(1) / 2 + 1

  def check(s: State, event: Event): Either[String, State] =
    event.playerId.zip(event.action) match
      case None => Right(s)
      case Some(_) if s.champion.isDefined => Left(s"la pelea ya terminó: la ganó ${s.champion.getOrElse("")}")
      case Some((p, a)) =>
        for
          _    <- roundOf(a).filter(r => s.rounds.exists(r > _))
                    .map(r => s"round $r, pero la pelea es a ${s.rounds.getOrElse(0)}")
                    .toLeft(())
          next <- action(s, p, a)
        yield next

  private def action(s: State, p: String, a: GameAction): Either[String, State] = a match
    case RoundWon(r, _, _) =>
      for _ <- open(s, r) yield
        val wins = s.wins.updated(p, s.wins.getOrElse(p, 0) + 1)
        // Option.when: solo hay campeón si este round lo llevó a la mayoría.
        s.copy(wins = wins, roundsDone = s.roundsDone + r, champion = Option.when(wins(p) >= roundsToWin(s))(p))
    case RoundDraw(r, _, _) =>
      open(s, r).map(_ => s.copy(roundsDone = s.roundsDone + r))
    case _ => Right(s)

  /** Un round admite un solo resultado: ROUND_WON o ROUND_DRAW. */
  private def open(s: State, r: Int): Either[String, Unit] =
    Either.cond(!s.roundsDone.contains(r), (), s"el round $r ya tiene resultado")

  private def roundOf(a: GameAction): Option[Int] = a match
    case Attack(_, _, _, _, r) => Some(r)
    case Combo(_, _, _, r)     => Some(r)
    case Block(_, r)           => Some(r)
    case Knockdown(_, r)       => Some(r)
    case RoundWon(r, _, _)     => Some(r)
    case RoundDraw(r, _, _)    => Some(r)
    case _                     => None
