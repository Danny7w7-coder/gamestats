package gamestats.rules

import gamestats.model.*

/** FLAWLESS_VICTORY: un jugador ganó un round sin recibir daño (vida 100). Uno por round. */
object FlawlessVictoryRule extends Rule:

  val name: String = "FLAWLESS_VICTORY"

  def detect(batch: MatchBatch): List[Pattern] =
    batch.events.collect {
      case Event(_, _, _, _, _, Some(p), _, Some(RoundWon(r, 100, _)), _) =>
        Pattern(name, Some(p), s"$p ganó el round $r sin recibir daño")
    }
