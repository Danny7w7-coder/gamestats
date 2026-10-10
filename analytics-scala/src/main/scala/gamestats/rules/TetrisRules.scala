package gamestats.rules

import gamestats.model.*

/** Acciones de tetris de cada jugador, en orden (los eventos ya vienen ordenados). */
private def actionsByPlayer(batch: MatchBatch): Map[String, List[GameAction]] =
  batch.events.flatMap(e => e.playerId.zip(e.action)).groupMap(_._1)(_._2)

/** Un patrón por jugador que cumpla la condición, en orden estable. */
private def perPlayer(batch: MatchBatch, rule: String)(describe: (String, List[GameAction]) => Option[String]): List[Pattern] =
  actionsByPlayer(batch).toList.sortBy(_._1).flatMap((p, actions) => describe(p, actions).map(Pattern(rule, Some(p), _)))

/** BACK_TO_BACK: dos tetris (limpiezas de 4 líneas) seguidos, sin otra limpieza en el medio. */
object BackToBackRule extends Rule:
  val name: String = "BACK_TO_BACK"

  def detect(batch: MatchBatch): List[Pattern] =
    perPlayer(batch, name) { (p, actions) =>
      val clears = actions.collect { case LinesCleared(lines, _, _) => lines }
      // sliding(2): cada par de limpiezas consecutivas.
      Option.when(clears.sliding(2).exists(_ == List(4, 4)))(s"$p hizo dos tetris seguidos (back-to-back)")
    }

/** PERFECT_CLEAR: el jugador dejó el tablero vacío al menos una vez. */
object PerfectClearRule extends Rule:
  val name: String = "PERFECT_CLEAR"

  def detect(batch: MatchBatch): List[Pattern] =
    perPlayer(batch, name) { (p, actions) =>
      Option.when(actions.collectFirst { case a: AllClear => a }.isDefined)(s"$p dejó el tablero vacío (perfect clear)")
    }

/** T_SPIN_MASTER: 3 o más T-spins en la partida. */
object TSpinMasterRule extends Rule:
  val name: String   = "T_SPIN_MASTER"
  val minTSpins: Int = 3

  def detect(batch: MatchBatch): List[Pattern] =
    perPlayer(batch, name) { (p, actions) =>
      val n = actions.collect { case t: TSpin => t }.size
      Option.when(n >= minTSpins)(s"$p hizo $n T-spins")
    }
