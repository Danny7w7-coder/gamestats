package gamestats.rules

import gamestats.model.*
import gamestats.stats.{FightingAnalyzer, RacingAnalyzer}

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

/** COMEBACK en fighting: el que ganó la pelea había perdido el primer round. */
object FightingComebackRule extends Rule:

  val name: String = "COMEBACK"

  def detect(batch: MatchBatch): List[Pattern] =
    // for sobre Option: si falta el ganador o el round 1, no hay patrón.
    (for
      winner      <- FightingAnalyzer.winner(batch)
      (_, first)  <- FightingAnalyzer.roundWinners(batch).find(_._1 == 1)
      if first != winner
    yield Pattern(name, Some(winner), s"$winner perdió el primer round y ganó la pelea")).toList
