package gamestats.rules

import gamestats.model.*

/** HAT_TRICK: un jugador anota 3 goles o más en la partida (uno por jugador). */
object HatTrickRule extends Rule:

  val name: String = "HAT_TRICK"
  val minGoals: Int = 3

  def detect(batch: MatchBatch): List[Pattern] =
    batch.events
      .collect { case Event(_, _, _, _, _, Some(player), _, Some(_: Goal), _) => player } // quién anotó cada gol
      .groupBy(identity)                     // un grupo por goleador
      .collect { case (player, goals) if goals.size >= minGoals => player -> goals.size }
      .toList
      .sortBy(_._1)                          // orden estable en la salida
      .map((player, n) => Pattern(name, Some(player), s"$player anotó $n goles"))
