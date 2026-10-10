package gamestats.stats

import gamestats.model.*
import gamestats.rules.Rule
import gamestats.validation.{ContextCheck, Validation}

/** Contrato que cumple el análisis de cada juego. */
trait GameAnalyzer:
  def gameId: String
  def analyze(batch: MatchBatch): MatchAnalysis

  /** Reglas de patrones que aplican a este juego (ninguna por defecto). */
  def rules: List[Rule] = Nil

  /** Validaciones de contexto propias del juego (ninguna por defecto). */
  def context: ContextCheck = ContextCheck.NoCheck

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
    List(RacingAnalyzer, FootballAnalyzer, FightingAnalyzer, TetrisAnalyzer)
      .map(a => a.gameId -> a) // map: cada analyzer queda indexado por su gameId
      .toMap

  private def analyzerFor(batch: MatchBatch): Either[String, GameAnalyzer] =
    registry.get(batch.gameId).toRight(s"no hay analyzer para gameId '${batch.gameId}'")

  /** Pura: descarta los eventos que rompen las validaciones de contexto (las
    * comunes y las del juego). Si no queda ninguno, Left.
    */
  def validate(batch: MatchBatch): Either[String, MatchBatch] =
    analyzerFor(batch)
      .map(analyzer => Validation.run(batch, analyzer.context))
      .filterOrElse(_.events.nonEmpty, "todos los eventos se descartaron")

  /** Pura: busca el analyzer del juego, calcula las estadísticas y corre todas
    * sus reglas; si el juego no tiene analyzer, Left. Espera un lote ya validado.
    */
  def analyze(batch: MatchBatch): Either[String, MatchAnalysis] =
    analyzerFor(batch)
      .map { analyzer =>
        val analysis = analyzer.analyze(batch)
        // flatMap: cada regla devuelve 0..n patrones y se juntan en una sola lista.
        // Agregar una regla es sumarla a `rules` del analyzer; esto no cambia.
        analysis.copy(patterns = analysis.patterns ++ analyzer.rules.flatMap(_.detect(batch)))
      }
