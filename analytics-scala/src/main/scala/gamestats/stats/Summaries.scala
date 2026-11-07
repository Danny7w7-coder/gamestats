package gamestats.stats

import gamestats.model.*

/** v1.1: estadísticas por juego y generales a partir de las partidas guardadas.
  * Todo es puro: recibe la lista de MatchAnalysis (la que guarda ResultStore)
  * y no sabe de HTTP ni de disco. Sirve para cualquier gameId, también los que
  * solo tienen estadísticas genéricas.
  */
object Summaries:

  /** Cuántos elementos se muestran en los rankings. */
  val TopWinners  = 10
  val TopOverview = 5

  def game(gameId: String, all: List[MatchAnalysis]): GameSummary =
    val ms = all.filter(_.gameId == gameId)
    val stats = ms.map(_.matchStats)
    GameSummary(
      gameId = gameId,
      matches = ms.size,
      players = distinctPlayers(ms),
      totalEvents = stats.map(_.totalEvents).sum,
      avgDurationSeconds = average(stats.map(_.durationSeconds.toDouble)),
      avgEventsPerMatch = average(stats.map(_.totalEvents.toDouble)),
      matchesWithoutWinner = stats.count(_.winner.isEmpty),
      winners = rank(stats.flatMap(_.winner)).take(TopWinners), // flatMap sobre Option: solo las que tienen ganador
      metrics = metrics(ms),
      actions = stats
        .flatMap(_.eventsByAction.toList)       // todas las parejas (acción, cantidad) de todas las partidas
        .groupMapReduce(_._1)(_._2)(_ + _),     // agrupa por acción y suma las cantidades
      patterns = rank(ms.flatMap(_.patterns.map(_.rule)))
    )

  def overview(all: List[MatchAnalysis]): Overview =
    Overview(
      matches = all.size,
      players = distinctPlayers(all),
      totalEvents = all.map(_.matchStats.totalEvents).sum,
      avgDurationSeconds = average(all.map(_.matchStats.durationSeconds.toDouble)),
      games = all.map(_.gameId).distinct.sorted.map(game(_, all)),
      topWinners = rank(all.flatMap(_.matchStats.winner)).take(TopOverview),
      topPatterns = rank(all.flatMap(_.patterns.map(_.rule))).take(TopOverview)
    )

  /** Cuenta cuántas veces aparece cada nombre; de más a menos, y por nombre si empatan. */
  def rank(names: List[String]): List[Ranked] =
    names
      .groupMapReduce(identity)(_ => 1)(_ + _)
      .toList
      .sortBy((name, count) => (-count, name))
      .map((name, count) => Ranked(name, count))

  /** Promedio, mínimo y máximo de cada métrica, juntando a todos los jugadores. */
  private def metrics(ms: List[MatchAnalysis]): Map[String, MetricSummary] =
    ms.flatMap(_.playerStats)
      .flatMap(_.metrics.toList)                // (métrica, valor) de cada jugador en cada partida
      .groupMap(_._1)(_._2)                     // métrica -> lista de valores
      .view
      .mapValues(vs => MetricSummary(average(vs), round2(vs.min), round2(vs.max)))
      .toMap

  private def distinctPlayers(ms: List[MatchAnalysis]): Int =
    ms.flatMap(_.playerStats.map(_.playerId)).distinct.size

  /** Promedio con 2 decimales; 0 si no hay datos (un juego sin partidas). */
  private def average(xs: List[Double]): Double =
    if xs.isEmpty then 0.0 else round2(xs.sum / xs.size)

  private def round2(x: Double): Double = math.round(x * 100) / 100.0
