package gamestats.stats

import gamestats.model.*
import gamestats.rules.{BackToBackRule, PerfectClearRule, Rule, TSpinMasterRule}
import gamestats.validation.{ContextCheck, TetrisCheck}

import java.time.Duration

/** Análisis de tetris de un solo jugador: no hay rival, así que no hay ganador
  * (`winner` = null); lo que importa es el puntaje y cómo se consiguió. Todo puro.
  */
object TetrisAnalyzer extends GameAnalyzer:

  import GenericStats.countOf

  val gameId: String = "tetris"

  override def rules: List[Rule] = List(BackToBackRule, PerfectClearRule, TSpinMasterRule)

  override def context: ContextCheck = TetrisCheck

  def analyze(batch: MatchBatch): MatchAnalysis =
    val byPlayer: Map[String, List[Event]] =
      batch.events.groupBy(_.playerId).collect { case (Some(p), events) => p -> events }
    val minutes = Duration.between(batch.startedAt, batch.finishedAt).toMillis / 60000.0
    val start   = TetrisCheck.startLevel(batch)
    MatchAnalysis(
      matchId = batch.matchId,
      gameId = gameId,
      matchStats = GenericStats.matchStats(batch), // winner = None: se juega solo
      playerStats = batch.players.map(p => PlayerStats(p, metrics(byPlayer.getOrElse(p, Nil), minutes, start))),
      patterns = Nil
    )

  /** Las limpiezas de líneas del jugador, en orden. */
  val clears: List[Event] => List[LinesCleared] =
    _.flatMap(_.action).collect { case c: LinesCleared => c }

  val score: List[Event] => Double = clears andThen (_.map(_.points).sum.toDouble)
  val lines: List[Event] => Double = clears andThen (_.map(_.lines).sum.toDouble)
  /** Un "tetris" es limpiar 4 líneas de una vez. */
  val tetrises: List[Event] => Double = clears andThen (_.count(_.lines == 4).toDouble)
  val maxCombo: List[Event] => Double = clears andThen (_.map(_.combo).maxOption.fold(0.0)(_.toDouble))

  val piecesPlaced: List[Event] => Double = countOf { case _: PiecePlaced => }
  val tSpins: List[Event] => Double       = countOf { case _: TSpin => }
  val hardDrops: List[Event] => Double    = countOf { case _: HardDrop => }

  /** Nivel más alto que alcanzó: el inicial o el último LEVEL_UP. */
  def maxLevel(events: List[Event], startLevel: Int): Double =
    (startLevel :: events.flatMap(_.action).collect { case LevelUp(l) => l }).max.toDouble

  def metrics(events: List[Event], minutes: Double, startLevel: Int): Map[String, Double] =
    Map(
      "score"           -> score(events),
      "lines"           -> lines(events),
      "tetrises"        -> tetrises(events),
      "piecesPlaced"    -> piecesPlaced(events),
      // Piezas por minuto sobre la duración de la partida; sin duración -> 0.0.
      "piecesPerMinute" -> (if minutes <= 0 then 0.0 else piecesPlaced(events) / minutes),
      "maxLevel"        -> maxLevel(events, startLevel),
      "maxCombo"        -> maxCombo(events),
      "tSpins"          -> tSpins(events),
      "hardDrops"       -> hardDrops(events)
    )
