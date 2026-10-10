package gamestats.rules

import gamestats.model.*
import gamestats.stats.{FootballAnalyzer, RacingAnalyzer}

/** COMEBACK en racing: el ganador estuvo en 3° lugar o peor en algún momento. */
object RacingComebackRule extends Rule:

  val name: String = "COMEBACK"

  /** A partir de qué posición se considera que iba "lejos" del 1° (≥ 2 lugares abajo). */
  val worstPositionThreshold: Int = 3

  def detect(batch: MatchBatch): List[Pattern] =
    val timelines = positionTimeline(batch)
    // Option -> List: sin ganador o sin posiciones no hay patrón.
    RacingAnalyzer.winner(batch).toList.flatMap { winner =>
      timelines
        .get(winner)
        .flatMap(_.maxOption) // la peor posición es el número más alto
        .filter(_ >= worstPositionThreshold)
        .map(worst => Pattern(name, Some(winner), s"$winner remontó desde el $worst° lugar y ganó la carrera"))
        .toList
    }

  /** Posiciones de cada jugador en orden de timestamp (los eventos del lote ya
    * vienen ordenados). Un POSITION_CHANGED aporta `from` y `to`: así cuenta
    * también el lugar del que salió, aunque no haya cerrado una vuelta en él.
    */
  def positionTimeline(batch: MatchBatch): Map[String, List[Int]] =
    batch.events
      .flatMap(e => e.playerId.zip(e.action)) // (jugador, acción) solo si existen ambos
      .flatMap {
        case (p, LapCompleted(_, _, position)) => List(p -> position)
        case (p, PositionChanged(from, to, _)) => List(p -> from, p -> to)
        case _                                 => Nil
      }
      .groupMap(_._1)(_._2) // groupMap: agrupa por jugador y se queda solo con la posición

/** COMEBACK en football: el equipo que ganó estuvo perdiendo en algún momento. */
object FootballComebackRule extends Rule:

  val name: String = "COMEBACK"

  /** Lo que se acumula al recorrer los goles en orden. */
  case class Progress(score: Map[String, Int], everBehind: Set[String])

  def detect(batch: MatchBatch): List[Pattern] =
    val end = replayGoals(batch)
    FootballAnalyzer
      .winner(end.score)
      .filter(end.everBehind.contains) // ganó y alguna vez fue perdiendo
      .map { team =>
        val rival = end.score.removed(team).values.maxOption.getOrElse(0)
        Pattern(name, None, s"El equipo $team remontó y ganó ${end.score(team)}-$rival")
      }
      .toList

  /** foldLeft sobre los goles en orden de timestamp (incluidos los autogoles,
    * que suman para el rival), llevando el marcador y el conjunto de equipos que
    * alguna vez estuvieron abajo.
    */
  def replayGoals(batch: MatchBatch): Progress =
    val start = Progress(FootballAnalyzer.teams(batch).map(_ -> 0).toMap, Set.empty)
    FootballAnalyzer.goalsInOrder(batch).foldLeft(start) { (acc, team) =>
      val score  = acc.score.updated(team, acc.score.getOrElse(team, 0) + 1)
      // Un equipo va perdiendo si algún otro tiene más goles que él.
      val behind = score.keySet.filter(t => score.exists((other, g) => other != t && g > score(t)))
      Progress(score, acc.everBehind ++ behind)
    }
