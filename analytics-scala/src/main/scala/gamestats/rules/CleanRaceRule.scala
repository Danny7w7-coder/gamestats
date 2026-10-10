package gamestats.rules

import gamestats.model.*
import gamestats.stats.RacingAnalyzer

/** CLEAN_RACE: el ganador no estuvo en ningún choque (ni lo provocó ni se lo
  * hicieron) y no recibió penalizaciones.
  */
object CleanRaceRule extends Rule:

  val name: String = "CLEAN_RACE"

  def detect(batch: MatchBatch): List[Pattern] =
    RacingAnalyzer.winner(batch).toList.filter(clean(batch, _)).map { winner =>
      Pattern(name, Some(winner), s"$winner ganó sin choques ni penalizaciones")
    }

  /** exists: basta un solo choque o penalización para que no sea una carrera limpia. */
  def clean(batch: MatchBatch, player: String): Boolean =
    !batch.events.exists {
      case Event(_, _, _, _, _, Some(p), _, Some(Collision(_, other, _)), _) => p == player || other == player
      case Event(_, _, _, _, _, Some(p), _, Some(_: Penalty), _)              => p == player
      case _                                                                   => false
    }
