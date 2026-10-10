package gamestats.rules

import gamestats.model.*

/** FATALITY: la pelea terminó con el remate final del ganador. */
object FatalityRule extends Rule:

  val name: String = "FATALITY"

  def detect(batch: MatchBatch): List[Pattern] =
    batch.events.collect {
      case Event(_, _, _, _, _, Some(p), _, Some(Fatality(target, move)), _) =>
        Pattern(name, Some(p), s"$p remató a $target con una fatality ($move)")
    }
