package gamestats.validation

import gamestats.model.*

/** Validaciones de contexto de fighting estilo Mortal Kombat (contracts/README.md). */
object FightingCheck extends ContextCheck:

  /** Lo que se sabe de la pelea hasta el evento actual.
    *  - rounds: máximo de rounds (MATCH_STARTED.data.rounds).
    *  - comboOn: víctima -> (quién le hizo el último combo, en qué round), si no lo cortó.
    *  - fatalBlowUsed: jugadores que ya usaron su FATAL_BLOW.
    *  - wins: rounds ganados por jugador; roundsDone: rounds que ya tienen ganador.
    *  - finished: ya hubo FATALITY, la pelea terminó.
    */
  case class State(
      rounds: Option[Int],
      comboOn: Map[String, (String, Int)],
      fatalBlowUsed: Set[String],
      wins: Map[String, Int],
      roundsDone: Set[Int],
      finished: Boolean
  )

  def initial(batch: MatchBatch): State =
    val rounds = batch.events
      .find(_.eventType == EventType.MatchStarted)
      .flatMap(_.data.objOpt)
      .flatMap(_.get("rounds"))
      .flatMap(_.numOpt)
      .map(_.toInt)
    State(rounds, Map.empty, Set.empty, Map.empty, Set.empty, finished = false)

  /** Rounds que hay que ganar para llevarse la pelea (mayoría: 2 de 3, 3 de 5). */
  def roundsToWin(s: State): Int = s.rounds.getOrElse(1) / 2 + 1

  def check(s: State, event: Event): Either[String, State] =
    event.playerId.zip(event.action) match
      case None => Right(s)
      case Some(_) if s.finished => Left("la pelea ya terminó con la FATALITY")
      case Some((p, a)) =>
        for
          _    <- roundOf(a).filter(r => s.rounds.exists(r > _))
                    .map(r => s"round $r, pero la pelea es a ${s.rounds.getOrElse(0)}")
                    .toLeft(())
          next <- action(s, p, a)
        yield next

  private def action(s: State, p: String, a: GameAction): Either[String, State] = a match
    case Combo(target, _, _, r) =>
      Right(s.copy(comboOn = s.comboOn.updated(target, p -> r)))
    case ComboBreaker(attacker, r) =>
      Either.cond(s.comboOn.get(p).contains(attacker -> r), s.copy(comboOn = s.comboOn.removed(p)),
        s"no hay un combo de $attacker que cortar en el round $r")
    case _: FatalBlow =>
      Either.cond(!s.fatalBlowUsed.contains(p), s.copy(fatalBlowUsed = s.fatalBlowUsed + p),
        s"$p ya usó su FATAL_BLOW")
    case RoundWon(r, _, _) =>
      Either.cond(!s.roundsDone.contains(r),
        s.copy(roundsDone = s.roundsDone + r, wins = s.wins.updated(p, s.wins.getOrElse(p, 0) + 1)),
        s"el round $r ya tiene ganador")
    case _: Fatality =>
      Either.cond(s.wins.getOrElse(p, 0) >= roundsToWin(s), s.copy(finished = true),
        s"$p no ganó la pelea, no puede hacer la FATALITY")
    case _ => Right(s)

  private def roundOf(a: GameAction): Option[Int] = a match
    case Attack(_, _, _, _, r)      => Some(r)
    case SpecialMove(_, _, _, _, r) => Some(r)
    case Combo(_, _, _, r)          => Some(r)
    case Throw(_, _, r)             => Some(r)
    case Block(_, r)                => Some(r)
    case ComboBreaker(_, r)         => Some(r)
    case Knockdown(_, r)            => Some(r)
    case FatalBlow(_, _, _, _, r)   => Some(r)
    case RoundWon(r, _, _)          => Some(r)
    case _                          => None
