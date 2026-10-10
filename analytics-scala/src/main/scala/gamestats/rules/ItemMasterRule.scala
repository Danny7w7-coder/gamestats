package gamestats.rules

import gamestats.model.*

/** ITEM_MASTER: un jugador usó los tres tipos de objeto en la misma carrera. */
object ItemMasterRule extends Rule:

  val name: String = "ITEM_MASTER"

  def detect(batch: MatchBatch): List[Pattern] =
    batch.events
      .collect { case Event(_, _, _, _, _, Some(p), _, Some(ItemUsed(_, item, _)), _) => p -> item }
      .groupMap(_._1)(_._2)                               // jugador -> objetos que usó
      .collect { case (p, items) if items.toSet == Item.values.toSet => p }
      .toList
      .sorted                                             // orden estable en la salida
      .map(p => Pattern(name, Some(p), s"$p usó los tres objetos (boost, shield y oil)"))
