package gamestats.validation

import gamestats.model.*

/** Validaciones de contexto de racing (contracts/README.md, "Validaciones de contexto"). */
object RacingCheck extends ContextCheck:

  /** Lo que se sabe de la carrera hasta el evento actual.
    *  - hand: el objeto que cada jugador tiene en la mano (uno a la vez).
    *  - incident: vuelta del último choque o trompo de cada jugador que todavía no hizo RESPAWN.
    *  - out: jugadores que abandonaron (DNF).
    */
  case class State(laps: Option[Int], hand: Map[String, Item], incident: Map[String, Int], out: Set[String])

  def initial(batch: MatchBatch): State =
    val laps = batch.events
      .find(_.eventType == EventType.MatchStarted)
      .flatMap(_.data.objOpt)
      .flatMap(_.get("laps"))
      .flatMap(_.numOpt)
      .map(_.toInt)
    State(laps, Map.empty, Map.empty, Set.empty)

  def check(state: State, event: Event): Either[String, State] =
    event.playerId match
      case Some(p) if state.out.contains(p) => Left(s"$p ya abandonó la carrera (DNF)")
      // zip: solo los eventos con jugador y con acción pasan por las reglas de acción.
      case _ => event.playerId.zip(event.action).fold[Either[String, State]](Right(state))((p, a) => action(state, p, a))

  private def action(s: State, p: String, a: GameAction): Either[String, State] =
    for
      _    <- lapOf(a).filter(lap => s.laps.exists(lap > _))
                .map(lap => s"vuelta $lap, pero la carrera tiene ${s.laps.getOrElse(0)}")
                .toLeft(())
      next <- a match
        case PositionChanged(from, to, _) if from == to =>
          Left("'from' y 'to' son iguales")
        case ItemPickup(_, item) =>
          s.hand.get(p) match
            case Some(held) => Left(s"agarra ${item.key} con ${held.key} en la mano")
            case None       => Right(s.copy(hand = s.hand.updated(p, item)))
        case ItemUsed(_, item, _) =>
          Either.cond(s.hand.get(p).contains(item), s.copy(hand = s.hand.removed(p)), s"usa ${item.key} sin tenerlo en la mano")
        case Collision(lap, _, _) => Right(s.copy(incident = s.incident.updated(p, lap)))
        case Spin(lap, _)         => Right(s.copy(incident = s.incident.updated(p, lap)))
        case Respawn(lap, _) =>
          Either.cond(s.incident.get(p).contains(lap), s.copy(incident = s.incident.removed(p)),
            "RESPAWN sin un choque o trompo antes en esa vuelta")
        case _: Dnf => Right(s.copy(out = s.out + p))
        case _      => Right(s)
    yield next

  /** Vuelta de la acción, si la tiene. */
  private def lapOf(a: GameAction): Option[Int] = a match
    case LapCompleted(lap, _, _)    => Some(lap)
    case PositionChanged(_, _, lap) => Some(lap)
    case Overtake(lap, _)           => Some(lap)
    case Lapped(lap, _)             => Some(lap)
    case ItemPickup(lap, _)         => Some(lap)
    case ItemUsed(lap, _, _)        => Some(lap)
    case Drift(lap, _, _)           => Some(lap)
    case Collision(lap, _, _)       => Some(lap)
    case Spin(lap, _)               => Some(lap)
    case Respawn(lap, _)            => Some(lap)
    case Dnf(lap, _)                => Some(lap)
    case _                          => None
