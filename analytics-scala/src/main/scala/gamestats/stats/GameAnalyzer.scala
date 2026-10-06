package gamestats.stats

import gamestats.model.*

/** Contrato que cumple el análisis de cada juego. */
trait GameAnalyzer:
  def gameId: String
  def analyze(batch: MatchBatch): MatchAnalysis

/** Analyzer mínimo: solo estadísticas genéricas, sin métricas por jugador.
  * Sirve para cualquier juego mientras no tenga uno propio.
  */
case class BaseAnalyzer(gameId: String) extends GameAnalyzer:
  def analyze(batch: MatchBatch): MatchAnalysis =
    MatchAnalysis(
      matchId = batch.matchId,
      gameId = gameId,
      matchStats = GenericStats.matchStats(batch),
      teamStats = None,
      playerStats = Nil,
      patterns = Nil
    )

object Analyzer:

  /** Juegos soportados. Agregar un juego = agregar su analyzer a esta lista. */
  val registry: Map[String, GameAnalyzer] =
    List(RacingAnalyzer, BaseAnalyzer("football"))
      .map(a => a.gameId -> a) // map: cada analyzer queda indexado por su gameId
      .toMap

  /** Pura: busca el analyzer del juego y lo aplica; si no hay, Left. */
  def analyze(batch: MatchBatch): Either[String, MatchAnalysis] =
    registry
      .get(batch.gameId)
      .toRight(s"no hay analyzer para gameId '${batch.gameId}'")
      .map(_.analyze(batch))
