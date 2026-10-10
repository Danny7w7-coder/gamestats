package gamestats.rules

import gamestats.model.*
import gamestats.stats.FootballAnalyzer

/** CLEAN_SHEET (valla invicta): el equipo que ganó no recibió ningún gol. */
object CleanSheetRule extends Rule:

  val name: String = "CLEAN_SHEET"

  def detect(batch: MatchBatch): List[Pattern] =
    val score = FootballAnalyzer.score(batch)
    FootballAnalyzer
      .winner(score)
      // forall: todos los rivales terminaron en 0 (con un rival, es "el rival hizo 0").
      .filter(team => score.removed(team).values.forall(_ == 0))
      .map(team => Pattern(name, None, s"El equipo $team ganó ${score(team)}-0 sin recibir goles"))
      .toList
