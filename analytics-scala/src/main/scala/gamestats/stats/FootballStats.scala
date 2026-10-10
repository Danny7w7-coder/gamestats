package gamestats.stats

import gamestats.model.*
import gamestats.rules.{CleanSheetRule, FootballComebackRule, HatTrickRule, Rule}
import gamestats.validation.{ContextCheck, FootballCheck}

/** Análisis de fútbol 6 vs 6 estilo arcade. Todo puro: recibe el lote y
  * devuelve el resultado.
  */
object FootballAnalyzer extends GameAnalyzer:

  val gameId: String = "football"

  override def rules: List[Rule] = List(FootballComebackRule, HatTrickRule, CleanSheetRule)

  override def context: ContextCheck = FootballCheck

  def analyze(batch: MatchBatch): MatchAnalysis =
    val all = actions(batch.events)
    // groupBy por jugador; collect descarta los eventos sin jugador sin usar `.get`.
    val byPlayer: Map[String, List[Event]] =
      batch.events.groupBy(_.playerId).collect { case (Some(p), events) => p -> events }
    val assists = assistsByPlayer(all)
    val finalScore = score(batch)

    // map sobre `players`: un jugador sin eventos aparece igual, con métricas en 0.0.
    val playerStats = batch.players.map { p =>
      PlayerStats(p, playerMetrics(byPlayer.getOrElse(p, Nil), assists.getOrElse(p, 0)))
    }
    val fouls   = countOccurrences(all.collect { case Foul(team, _, _, _) => team })
    val corners = countOccurrences(all.collect { case Corner(team, _) => team })
    val teamStats = teams(batch).map { t =>
      TeamStats(t, Map(
        "goals"   -> finalScore.getOrElse(t, 0).toDouble,
        "fouls"   -> fouls.getOrElse(t, 0).toDouble,
        "corners" -> corners.getOrElse(t, 0).toDouble
      ))
    }

    MatchAnalysis(
      matchId = batch.matchId,
      gameId = gameId,
      matchStats = GenericStats.matchStats(batch).copy(winner = winner(finalScore), score = Some(finalScore)),
      teamStats = Some(teamStats),
      playerStats = playerStats,
      patterns = Nil
    )

  // ---------------------------------------------------------------------------
  // Por jugador
  // ---------------------------------------------------------------------------

  private def actions(events: List[Event]): List[GameAction] = events.flatMap(_.action)

  /** División segura: sin intentos el resultado es 0.0, nunca se divide entre 0. */
  def ratio(hits: Int, attempts: Int): Double =
    if attempts == 0 then 0.0 else hits.toDouble / attempts

  // collect filtra el tipo de acción y extrae lo que interesa en un solo paso.
  val goals: List[Event] => Int  = e => actions(e).collect { case g: Goal => g }.size
  /** Tiros normales y potentes: los dos pueden terminar en gol. */
  val shots: List[Event] => Int  = e => actions(e).collect { case s: Shot => s; case s: PowerShot => s }.size
  val powerShots: List[Event] => Int = e => actions(e).collect { case s: PowerShot => s }.size
  val passes: List[Event] => List[Pass] = e => actions(e).collect { case p: Pass => p }
  val saves: List[Event] => Int  = e => actions(e).collect { case s: Save => s }.size

  /** Balones recuperados: entradas exitosas más intercepciones. */
  val recoveries: List[Event] => Int =
    e => actions(e).collect { case Tackle(_, _, true, _) => 1; case _: Interception => 1 }.size

  val dribblesWon: List[Event] => Int = e => actions(e).collect { case Dribble(_, true, _) => 1 }.size

  /** Pases exitosos / pases totales. */
  val passAccuracy: List[Event] => Double =
    passes andThen (ps => ratio(ps.count(_.success), ps.size))

  /** Combina dos tipos de evento: GOAL / (SHOT + POWER_SHOT) (todo gol va precedido de su tiro). */
  val shotEffectiveness: List[Event] => Double =
    e => ratio(goals(e), shots(e))

  /** Las asistencias están en los GOAL de otro jugador (`assistId`), no en los
    * eventos propios: por eso se cuentan sobre todas las acciones de la partida.
    */
  def assistsByPlayer(all: List[GameAction]): Map[String, Int] =
    countOccurrences(all.collect { case Goal(_, Some(assistId), _) => assistId })

  def playerMetrics(events: List[Event], assists: Int): Map[String, Double] =
    Map(
      "goals"             -> goals(events).toDouble,
      "assists"           -> assists.toDouble,
      "passAccuracy"      -> passAccuracy(events),
      "shots"             -> shots(events).toDouble,
      "shotEffectiveness" -> shotEffectiveness(events),
      "powerShots"        -> powerShots(events).toDouble,
      "saves"             -> saves(events).toDouble,
      "recoveries"        -> recoveries(events).toDouble,
      "dribblesWon"       -> dribblesWon(events).toDouble
    )

  // ---------------------------------------------------------------------------
  // Por equipo y partida
  // ---------------------------------------------------------------------------

  /** Cuántas veces aparece cada id (equipo o jugador): groupBy junta los iguales
    * y mapValues se queda con el tamaño de cada grupo.
    */
  private def countOccurrences(ids: List[String]): Map[String, Int] =
    ids.groupBy(identity).view.mapValues(_.size).toMap

  private def dataStr(e: Event, key: String): Option[String] =
    e.data.objOpt.flatMap(_.get(key)).flatMap(_.strOpt)

  /** Equipos de la partida, en orden: local, visitante y cualquier otro que
    * aparezca en PLAYER_JOINED o en las acciones. Así un equipo sin goles se
    * conoce igual y aparece en el marcador con 0.
    */
  def teams(batch: MatchBatch): List[String] =
    val fromStart = batch.events
      .filter(_.eventType == EventType.MatchStarted)
      .flatMap(e => List("homeTeam", "awayTeam").flatMap(dataStr(e, _)))
    val fromJoins = batch.events
      .filter(_.eventType == EventType.PlayerJoined)
      .flatMap(dataStr(_, "teamId"))
    val fromActions = actions(batch.events).collect {
      case Pass(team, _, _, _, _) => team
      case Shot(team, _, _)       => team
      case Goal(team, _, _)       => team
      case Foul(team, _, _, _)    => team
    }
    (fromStart ++ fromJoins ++ fromActions).distinct

  /** Equipo que suma cada gol, en orden. Un GOAL suma para su equipo; un
    * OWN_GOAL, para el rival. Lo usan el marcador y la regla COMEBACK.
    */
  def goalsInOrder(batch: MatchBatch): List[String] =
    val all = teams(batch)
    actions(batch.events).flatMap {
      case Goal(team, _, _) => List(team)
      case OwnGoal(team, _) => all.find(_ != team).toList // el rival
      case _                => Nil
    }

  /** Marcador final: goles por equipo, con 0 para los que no anotaron. */
  def score(batch: MatchBatch): Map[String, Int] =
    val goalsByTeam = countOccurrences(goalsInOrder(batch))
    teams(batch).map(t => t -> goalsByTeam.getOrElse(t, 0)).toMap

  /** El equipo con más goles; si el máximo está repetido (empate) -> None. */
  def winner(score: Map[String, Int]): Option[String] =
    score.values.maxOption.flatMap { best =>
      score.collect { case (team, g) if g == best => team }.toList match
        case List(team) => Some(team)
        case _          => None
    }
