package gamestats.rules

import gamestats.model.*

/** COMBO_MASTER: un jugador encadenó un combo de 5 golpes o más. Uno por
  * jugador, con su mejor combo.
  */
object ComboMasterRule extends Rule:

  val name: String = "COMBO_MASTER"
  val minHits: Int = 5

  def detect(batch: MatchBatch): List[Pattern] =
    batch.events
      .collect { case Event(_, _, _, _, _, Some(p), _, Some(Combo(_, hits, _, _)), _) => p -> hits }
      .groupMapReduce(_._1)(_._2)(_ max _) // jugador -> su combo más largo
      .filter((_, best) => best >= minHits)
      .toList
      .sorted // orden estable en la salida
      .map((p, best) => Pattern(name, Some(p), s"$p hizo un combo de $best golpes"))
