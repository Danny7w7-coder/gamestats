package gamestats.stats

import gamestats.model.*

import java.time.Duration

/** Estadísticas que sirven para cualquier juego: solo usan el lote y los nombres
  * de las acciones, nunca acciones concretas. Cada analyzer parte de aquí y
  * completa `winner`/`score` con `copy`.
  */
object GenericStats:

  def matchStats(batch: MatchBatch): MatchStats =
    MatchStats(
      durationSeconds = Duration.between(batch.startedAt, batch.finishedAt).getSeconds,
      totalEvents = batch.events.size,
      playerCount = batch.players.size,
      eventsByAction = eventsByAction(batch.events),
      winner = None,
      score = None
    )

  /** Cuántas veces ocurrió cada acción (incluidas las desconocidas). */
  def eventsByAction(events: List[Event]): Map[String, Int] =
    events
      .flatMap(_.action)     // flatMap sobre Option: deja solo los eventos que traen acción
      .groupBy(_.actionName) // groupBy: una lista de acciones por nombre
      .view
      .mapValues(_.size)     // mapValues: de cada lista solo interesa cuántas hay
      .toMap
