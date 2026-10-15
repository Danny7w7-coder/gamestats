package gamestats.stats

import gamestats.model.*

import java.time.Duration

/** Estadísticas que sirven para cualquier juego: solo usan el lote y los nombres
  * de las acciones, nunca acciones concretas. Cada analyzer parte de aquí y
  * completa `winner` con `copy`.
  */
object GenericStats:

  def matchStats(batch: MatchBatch): MatchStats =
    MatchStats(
      durationSeconds = Duration.between(batch.startedAt, batch.finishedAt).getSeconds,
      totalEvents = batch.events.size,
      playerCount = batch.players.size,
      eventsByAction = eventsByAction(batch.events),
      winner = None
    )

  /** Cuántas acciones cumplen el patrón. Recibe una función parcial
    * (`{ case _: Overtake => }`) y cuenta los eventos en los que está definida.
    */
  def countOf(pattern: PartialFunction[GameAction, Any]): List[Event] => Double =
    _.flatMap(_.action).count(pattern.isDefinedAt).toDouble

  /** Cuántas veces ocurrió cada acción (incluidas las desconocidas). */
  def eventsByAction(events: List[Event]): Map[String, Int] =
    events
      .flatMap(_.action)     // flatMap sobre Option: deja solo los eventos que traen acción
      .groupBy(_.actionName) // groupBy: una lista de acciones por nombre
      .view
      .mapValues(_.size)     // mapValues: de cada lista solo interesa cuántas hay
      .toMap
