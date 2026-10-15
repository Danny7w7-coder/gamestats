package gamestats.rules

import gamestats.model.*
import gamestats.stats.RacingAnalyzer

/** WIRE_TO_WIRE: el ganador fue 1° toda la carrera (de la salida a la meta):
  * todas las posiciones que se le conocen son 1. Es lo contrario de COMEBACK y
  * usa la misma línea de tiempo de posiciones.
  */
object WireToWireRule extends Rule:

  val name: String = "WIRE_TO_WIRE"

  def detect(batch: MatchBatch): List[Pattern] =
    val timelines = RacingComebackRule.positionTimeline(batch)
    RacingAnalyzer.winner(batch).toList
      .filter(w => timelines.get(w).exists(_.forall(_ == 1))) // forall: ni una sola posición distinta de 1
      .map(w => Pattern(name, Some(w), s"$w lideró la carrera de principio a fin"))
