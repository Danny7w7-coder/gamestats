package gamestats.rules

import gamestats.model.*
import gamestats.stats.Connect4Analyzer
import gamestats.validation.Connect4Check

/** Los dos jugadores de la partida, en orden de entrada (los que validó Connect4Check). */
private def seated(batch: MatchBatch): List[String] =
  Connect4Check.timeline(batch).last.players

/** DOUBLE_THREAT: en algún momento el jugador tuvo dos amenazas abiertas a la
  * vez. El rival solo puede tapar una por turno: es la jugada que gana partidas.
  */
object DoubleThreatRule extends Rule:
  val name: String = "DOUBLE_THREAT"

  def detect(batch: MatchBatch): List[Pattern] =
    val states = Connect4Check.timeline(batch) // el tablero después de cada evento
    def open(s: Connect4Check.State, p: String): Int = s.board.threats.values.count(_.contains(p))
    seated(batch)
      .filter(p => states.exists(open(_, p) >= 2))
      .map(p => Pattern(name, Some(p), s"$p tuvo ${states.map(open(_, p)).max} amenazas abiertas a la vez"))

/** QUICK_WIN: ganó con 7 fichas propias o menos en el tablero (el mínimo es 4). */
object QuickWinRule extends Rule:
  val name: String  = "QUICK_WIN"
  val maxDiscs: Int = 7

  def detect(batch: MatchBatch): List[Pattern] =
    // El tablero final ya descuenta las fichas deshechas con UNDO.
    val board = Connect4Check.timeline(batch).last.board
    Connect4Analyzer.winner(batch).toList
      .map(w => w -> board.discs.values.count(_ == w))
      .filter((_, n) => n <= maxDiscs)
      .map((w, n) => Pattern(name, Some(w), s"$w ganó con solo $n fichas"))

/** PERFECT_DEFENSE: el rival armó al menos 2 amenazas y el jugador las tapó todas. */
object PerfectDefenseRule extends Rule:
  val name: String    = "PERFECT_DEFENSE"
  val minThreats: Int = 2

  def detect(batch: MatchBatch): List[Pattern] =
    val moves = batch.events.flatMap(e => e.playerId.zip(e.action)) // (jugador, acción)
    val players = seated(batch)
    players.flatMap { p =>
      players.find(_ != p).toList.flatMap { rival =>
        val created = moves.count { case (`rival`, _: ThreatCreated) => true; case _ => false }
        val blocked = moves.count { case (`p`, ThreatBlocked(_, _, `rival`)) => true; case _ => false }
        Option.when(created >= minThreats && blocked >= created)(
          Pattern(name, Some(p), s"$p tapó las $created amenazas de $rival")
        )
      }
    }
