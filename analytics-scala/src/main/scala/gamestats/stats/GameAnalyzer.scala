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
      playerStats = Nil,
      patterns = Nil
    )

object Analyzer:

  /** Juegos con análisis propio. Agregar uno = agregar su analyzer a esta lista. */
  val registry: Map[String, GameAnalyzer] =
    List(RacingAnalyzer, FightingAnalyzer, Connect4Analyzer, TetrisAnalyzer)
      .map(a => a.gameId -> a) // map: cada analyzer queda indexado por su gameId
      .toMap

  /** v1.1: el gameId es abierto. Un juego que no está en `registry` (por ejemplo,
    * uno nuevo de GameConsole) se analiza con las estadísticas genéricas en vez
    * de rechazarse, así agregar un juego no obliga a cambiar GameStats.
    */
  def analyzerFor(gameId: String): GameAnalyzer =
    registry.getOrElse(gameId, BaseAnalyzer(gameId))

  /** Pura: descarta los eventos que rompen las validaciones de contexto (las
    * comunes y las del juego). Si no queda ninguno, Left.
    */
  def validate(batch: MatchBatch): Either[String, MatchBatch] =
    Right(Validation.run(batch, analyzerFor(batch.gameId).context))
      .filterOrElse(_.events.nonEmpty, "todos los eventos se descartaron")

  /** Pura: calcula las estadísticas con el analyzer del juego y corre todas sus
    * reglas. Espera un lote ya validado.
    */
  def analyze(batch: MatchBatch): Either[String, MatchAnalysis] =
    val analyzer = analyzerFor(batch.gameId)
    val analysis = analyzer.analyze(batch)
    // flatMap: cada regla devuelve 0..n patrones y se juntan en una sola lista.
    // Agregar una regla es sumarla a `rules` del analyzer; esto no cambia.
    Right(analysis.copy(patterns = analysis.patterns ++ analyzer.rules.flatMap(_.detect(batch))))
