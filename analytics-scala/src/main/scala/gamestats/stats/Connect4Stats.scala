package gamestats.stats

import gamestats.model.*
import gamestats.rules.{DoubleThreatRule, PerfectDefenseRule, QuickWinRule, Rule}
import gamestats.validation.{Connect4Check, ContextCheck}

/** Análisis de 4 en línea (dos jugadores por turnos, o contra la IA). Todo puro. */
object Connect4Analyzer extends GameAnalyzer:

  import GenericStats.countOf

  val gameId: String = "connect4"

  /** La columna del medio: la más valiosa del tablero (entra en más líneas de 4). */
  val centerColumn: Int = 4

  override def rules: List[Rule] = List(DoubleThreatRule, QuickWinRule, PerfectDefenseRule)

  override def context: ContextCheck = Connect4Check

  def analyze(batch: MatchBatch): MatchAnalysis =
    val byPlayer: Map[String, List[Event]] =
      batch.events.groupBy(_.playerId).collect { case (Some(p), events) => p -> events }
    MatchAnalysis(
      matchId = batch.matchId,
      gameId = gameId,
      matchStats = GenericStats.matchStats(batch).copy(winner = winner(batch)),
      playerStats = batch.players.map(p => PlayerStats(p, metrics(byPlayer.getOrElse(p, Nil)))),
      patterns = Nil
    )

  // ---------------------------------------------------------------------------
  // Por jugador
  // ---------------------------------------------------------------------------

  /** Columnas donde soltó cada ficha, en orden. */
  val dropColumns: List[Event] => List[Int] =
    _.flatMap(_.action).collect { case DiscDropped(c, _) => c }

  /** Proporción de fichas en la columna del centro (0 a 1); sin fichas -> 0.0. */
  val centerControl: List[Event] => Double =
    dropColumns andThen (cs => if cs.isEmpty then 0.0 else cs.count(_ == centerColumn).toDouble / cs.size)

  val discs: List[Event] => Double          = countOf { case _: DiscDropped => }
  val threatsCreated: List[Event] => Double = countOf { case _: ThreatCreated => }
  val threatsBlocked: List[Event] => Double = countOf { case _: ThreatBlocked => }
  val hintsUsed: List[Event] => Double      = countOf { case _: HintUsed => }
  val undos: List[Event] => Double          = countOf { case _: Undo => }
  val turnTimeouts: List[Event] => Double   = countOf { case _: TurnTimeout => }

  def metrics(events: List[Event]): Map[String, Double] =
    Map(
      "discs"          -> discs(events),
      "threatsCreated" -> threatsCreated(events),
      "threatsBlocked" -> threatsBlocked(events),
      "centerControl"  -> centerControl(events),
      "hintsUsed"      -> hintsUsed(events),
      "undos"          -> undos(events),
      "turnTimeouts"   -> turnTimeouts(events)
    )

  // ---------------------------------------------------------------------------
  // Partida
  // ---------------------------------------------------------------------------

  /** Gana quien hizo el 4 en línea; sin CONNECT_FOUR (tablero lleno) es empate -> None. */
  def winner(batch: MatchBatch): Option[String] =
    batch.events.collect { case Event(_, _, _, _, _, Some(p), _, Some(_: ConnectFour), _) => p }.lastOption
