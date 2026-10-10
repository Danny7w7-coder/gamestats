package gamestats.validation

import gamestats.model.*

/** Validaciones de contexto de football 6 vs 6 (contracts/README.md). */
object FootballCheck extends ContextCheck:

  val maxPerTeam: Int = 6

  /** team: en qué equipo entró cada jugador. sentOff: expulsados con roja. */
  case class State(team: Map[String, String], sentOff: Set[String])

  def initial(batch: MatchBatch): State = State(Map.empty, Set.empty)

  def check(s: State, event: Event): Either[String, State] =
    event.playerId match
      case None => Right(s)
      case Some(p) if event.eventType == EventType.PlayerJoined => join(s, p, event)
      case Some(p) if !s.team.contains(p) => Left(s"$p no está en ningún equipo")
      case Some(p) if s.sentOff.contains(p) => Left(s"$p fue expulsado con roja")
      case Some(p) =>
        event.action match
          case Some(Foul(_, Card.Red, _, _)) => Right(s.copy(sentOff = s.sentOff + p))
          case _                             => Right(s)

  /** Un jugador entra a su equipo si todavía hay lugar (máximo 6). */
  private def join(s: State, p: String, event: Event): Either[String, State] =
    val teamId = event.data.objOpt.flatMap(_.get("teamId")).flatMap(_.strOpt)
    teamId match
      case None => Left("PLAYER_JOINED sin 'teamId'")
      case Some(t) =>
        // count sobre los valores del mapa: cuántos jugadores ya tiene ese equipo.
        val size = s.team.values.count(_ == t)
        Either.cond(size < maxPerTeam, s.copy(team = s.team.updated(p, t)), s"el equipo $t ya tiene $maxPerTeam jugadores")
