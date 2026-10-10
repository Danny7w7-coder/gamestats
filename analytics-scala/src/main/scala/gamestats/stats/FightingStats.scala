package gamestats.stats

import gamestats.model.*
import gamestats.rules.{FatalityRule, FightingComebackRule, FlawlessVictoryRule, Rule}
import gamestats.validation.{ContextCheck, FightingCheck}

/** Análisis de peleas 1 contra 1 por rounds, estilo Mortal Kombat. Todo puro. */
object FightingAnalyzer extends GameAnalyzer:

  import GenericStats.countOf

  val gameId: String = "fighting"

  override def rules: List[Rule] = List(FightingComebackRule, FlawlessVictoryRule, FatalityRule)

  override def context: ContextCheck = FightingCheck

  def analyze(batch: MatchBatch): MatchAnalysis =
    val byPlayer: Map[String, List[Event]] =
      batch.events.groupBy(_.playerId).collect { case (Some(p), events) => p -> events }
    MatchAnalysis(
      matchId = batch.matchId,
      gameId = gameId,
      matchStats = GenericStats.matchStats(batch).copy(winner = winner(batch)),
      teamStats = None,
      playerStats = batch.players.map(p => PlayerStats(p, metrics(byPlayer.getOrElse(p, Nil)))),
      patterns = Nil
    )

  // ---------------------------------------------------------------------------
  // Por jugador
  // ---------------------------------------------------------------------------

  /** Daño de cada golpe que conectó (los que fallan ya traen damage 0). */
  val damages: List[Event] => List[Int] =
    _.flatMap(_.action).collect {
      case Attack(_, _, _, d, _)      => d
      case SpecialMove(_, _, _, d, _) => d
      case Combo(_, _, d, _)          => d
      case Throw(_, d, _)             => d
      case FatalBlow(_, _, d, _, _)   => d
    }

  /** Golpes que pueden fallar (ataque, especial y golpe fatal): si conectaron o no. */
  val strikes: List[Event] => List[Boolean] =
    _.flatMap(_.action).collect {
      case Attack(_, _, hit, _, _)      => hit
      case SpecialMove(_, _, hit, _, _) => hit
      case FatalBlow(_, hit, _, _, _)   => hit
    }

  val damageDealt: List[Event] => Double = damages andThen (_.sum.toDouble)

  /** Golpes que conectaron / golpes intentados; sin intentos -> 0.0. */
  val accuracy: List[Event] => Double =
    strikes andThen (hs => if hs.isEmpty then 0.0 else hs.count(identity).toDouble / hs.size)

  /** El combo más largo (en golpes); sin combos -> 0. */
  val maxCombo: List[Event] => Double =
    _.flatMap(_.action).collect { case Combo(_, hits, _, _) => hits }.maxOption.fold(0.0)(_.toDouble)

  val specials: List[Event] => Double      = countOf { case _: SpecialMove => }
  val blocks: List[Event] => Double        = countOf { case _: Block => }
  val comboBreakers: List[Event] => Double = countOf { case _: ComboBreaker => }
  val knockdowns: List[Event] => Double    = countOf { case _: Knockdown => }
  val roundsWon: List[Event] => Double     = countOf { case _: RoundWon => }

  def metrics(events: List[Event]): Map[String, Double] =
    Map(
      "damageDealt"   -> damageDealt(events),
      "accuracy"      -> accuracy(events),
      "maxCombo"      -> maxCombo(events),
      "specials"      -> specials(events),
      "blocks"        -> blocks(events),
      "comboBreakers" -> comboBreakers(events),
      "knockdowns"    -> knockdowns(events),
      "roundsWon"     -> roundsWon(events)
    )

  // ---------------------------------------------------------------------------
  // Pelea
  // ---------------------------------------------------------------------------

  /** Quién ganó cada round, en orden: lista de (round, jugador). */
  def roundWinners(batch: MatchBatch): List[(Int, String)] =
    batch.events.collect { case Event(_, _, _, _, _, Some(p), _, Some(RoundWon(r, _, _)), _) => r -> p }

  /** Gana el que más rounds ganó; empate o sin rounds -> None. */
  def winner(batch: MatchBatch): Option[String] =
    val wins = roundWinners(batch).groupMapReduce(_._2)(_ => 1)(_ + _) // jugador -> rounds ganados
    wins.values.maxOption.flatMap { best =>
      wins.collect { case (p, n) if n == best => p }.toList match
        case List(p) => Some(p)
        case _       => None
    }
