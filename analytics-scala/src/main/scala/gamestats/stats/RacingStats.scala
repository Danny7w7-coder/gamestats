package gamestats.stats

import gamestats.model.*
import gamestats.rules.{CleanRaceRule, ItemMasterRule, RacingComebackRule, Rule}
import gamestats.validation.{ContextCheck, RacingCheck}

/** Análisis de carreras arcade (por vueltas, posiciones y objetos). Todo puro:
  * recibe el lote y devuelve el resultado.
  */
object RacingAnalyzer extends GameAnalyzer:

  val gameId: String = "racing"

  override def rules: List[Rule] = List(RacingComebackRule, CleanRaceRule, ItemMasterRule)

  override def context: ContextCheck = RacingCheck

  def analyze(batch: MatchBatch): MatchAnalysis =
    // groupBy por jugador; collect descarta el grupo de eventos sin jugador
    // (MATCH_STARTED / MATCH_FINISHED) sin usar `.get` sobre el Option.
    val byPlayer: Map[String, List[Event]] =
      batch.events.groupBy(_.playerId).collect { case (Some(p), events) => p -> events }

    // map sobre `players` (no sobre byPlayer) para que un jugador sin eventos
    // aparezca igual, con sus métricas en 0.0.
    val playerStats = batch.players.map(p => PlayerStats(p, metrics(byPlayer.getOrElse(p, Nil))))

    MatchAnalysis(
      matchId = batch.matchId,
      gameId = gameId,
      matchStats = GenericStats.matchStats(batch).copy(winner = winner(batch)),
      teamStats = None, // racing no tiene equipos
      playerStats = playerStats,
      patterns = Nil
    )

  // ---------------------------------------------------------------------------
  // Métricas por jugador. Cada una es una función List[Event] => valor, así se
  // pueden componer y probar por separado.
  // ---------------------------------------------------------------------------

  /** Tiempos de vuelta en orden. collect = filtrar + extraer en un paso. */
  val lapTimes: List[Event] => List[Long] =
    _.flatMap(_.action).collect { case LapCompleted(_, lapTimeMs, _) => lapTimeMs }

  /** Segundos de penalización acumulados. */
  val penaltySeconds: List[Event] => Int =
    _.flatMap(_.action).collect { case Penalty(seconds, _) => seconds }.sum

  /** Promedio; lista vacía -> 0.0 (nunca divide entre 0). */
  val average: List[Long] => Double =
    xs => if xs.isEmpty then 0.0 else xs.sum.toDouble / xs.size

  /** Desviación estándar poblacional: cuánto se alejan las vueltas de su promedio.
    * Más baja = piloto más consistente. Lista vacía -> 0.0.
    */
  val stdDev: List[Long] => Double = xs =>
    if xs.isEmpty then 0.0
    else
      val mean = average(xs)
      // foldLeft acumula la suma de los cuadrados de las diferencias sin variables mutables.
      val sumSq = xs.foldLeft(0.0)((acc, x) => acc + math.pow(x - mean, 2))
      math.sqrt(sumSq / xs.size)

  // Composición: "sacar los tiempos" y luego "promediar" / "desviación".
  val avgLapMs: List[Event] => Double    = lapTimes andThen average
  val consistency: List[Event] => Double = lapTimes andThen stdDev

  val bestLapMs: List[Event] => Double =
    lapTimes andThen (_.minOption.fold(0.0)(_.toDouble))

  /** Combina dos tipos de evento: vueltas (LAP_COMPLETED) + castigos (PENALTY). */
  val effectiveTimeMs: List[Event] => Double =
    events => (lapTimes(events).sum + penaltySeconds(events) * 1000L).toDouble

  /** Cuántas acciones del jugador cumplen el patrón. Recibe una función parcial
    * (`{ case _: Overtake => }`) y cuenta los eventos en los que está definida.
    */
  def countOf(pattern: PartialFunction[GameAction, Any]): List[Event] => Double =
    _.flatMap(_.action).count(pattern.isDefinedAt).toDouble

  val overtakes: List[Event] => Double   = countOf { case _: Overtake => }
  val itemsPicked: List[Event] => Double = countOf { case _: ItemPickup => }
  val itemsUsed: List[Event] => Double   = countOf { case _: ItemUsed => }
  val boostsUsed: List[Event] => Double  = countOf { case ItemUsed(_, Item.Boost, _) => }
  /** Choques que provocó el jugador (es el que manda el COLLISION). */
  val collisions: List[Event] => Double  = countOf { case _: Collision => }

  def metrics(events: List[Event]): Map[String, Double] =
    Map(
      "bestLapMs"       -> bestLapMs(events),
      "avgLapMs"        -> avgLapMs(events),
      "consistency"     -> consistency(events),
      "penaltySeconds"  -> penaltySeconds(events).toDouble,
      "effectiveTimeMs" -> effectiveTimeMs(events),
      "overtakes"       -> overtakes(events),
      "itemsPicked"     -> itemsPicked(events),
      "itemsUsed"       -> itemsUsed(events),
      "boostsUsed"      -> boostsUsed(events),
      "collisions"      -> collisions(events)
    )

  // ---------------------------------------------------------------------------
  // Partida
  // ---------------------------------------------------------------------------

  /** `MATCH_FINISHED.data.ranking`, si existe. Lo reutiliza la regla COMEBACK. */
  def ranking(batch: MatchBatch): Option[List[String]] =
    batch.events
      .findLast(_.eventType == EventType.MatchFinished)
      .flatMap(_.data.objOpt)
      .flatMap(_.get("ranking"))
      .flatMap(_.arrOpt)
      .map(_.toList.flatMap(_.strOpt)) // flatMap sobre Option ignora entradas que no sean string

  /** Ganador = primero del ranking; sin ranking -> None. */
  def winner(batch: MatchBatch): Option[String] =
    ranking(batch).flatMap(_.headOption)
