package gamestats.api

import gamestats.json.Codecs
import gamestats.model.{MatchAnalysis, MatchBatch, PlayerStats}
import gamestats.stats.Analyzer
import gamestats.storage.ResultStore

/** Endpoints HTTP. Junto con `storage/` es la única capa con efectos: la lógica
  * de cada endpoint vive en funciones puras del objeto compañero, así se puede
  * testear sin levantar el servidor.
  */
case class AnalyticsRoutes(store: ResultStore)(using cc: castor.Context, log: cask.Logger) extends cask.Routes:

  @cask.get("/health")
  def health(): ujson.Value = ujson.Obj("status" -> "ok")

  /** Analiza, guarda y responde. Lote inválido -> 400; fallo de disco -> 500. */
  @cask.post("/analyze")
  def analyze(request: cask.Request): cask.Response[ujson.Value] =
    val result =
      for
        batch    <- AnalyticsRoutes.parseBody(request.text())
        _         = batch.skipped.foreach(s => println(s"[analyze] ${batch.matchId}: evento descartado ($s)"))
        analysis <- Analyzer.analyze(batch)
      yield analysis
    result match
      case Left(error) => AnalyticsRoutes.error(400, error)
      case Right(analysis) =>
        store.save(analysis) match
          case Right(saved) => cask.Response(Codecs.encode(saved), statusCode = 200)
          case Left(error)  => AnalyticsRoutes.error(500, error)

  @cask.get("/results")
  def results(): ujson.Value = ujson.Arr.from(store.all.map(Codecs.encode))

  @cask.get("/results/:matchId")
  def result(matchId: String): cask.Response[ujson.Value] =
    store.get(matchId) match
      case Some(analysis) => cask.Response(Codecs.encode(analysis), statusCode = 200)
      case None           => AnalyticsRoutes.error(404, s"no hay resultado para '$matchId'")

  @cask.get("/players/:playerId")
  def player(playerId: String): cask.Response[ujson.Value] =
    AnalyticsRoutes.playerReport(playerId, store.all) match
      case Some(report) => cask.Response(report, statusCode = 200)
      case None         => AnalyticsRoutes.error(404, s"el jugador '$playerId' no aparece en ninguna partida")

  initialize()

object AnalyticsRoutes:

  /** Texto del request -> MatchAnalysis. Pura: cualquier fallo (JSON,
    * validación o juego sin analyzer) vuelve como Left.
    */
  def analyzeBody(body: String): Either[String, MatchAnalysis] =
    parseBody(body).flatMap(Analyzer.analyze)

  /** Texto del request -> lote validado: primero el formato de cada evento
    * (Codecs) y después el contexto de la partida (Analyzer.validate). Los
    * eventos descartados en cualquiera de los dos pasos quedan en `skipped`.
    */
  def parseBody(body: String): Either[String, MatchBatch] =
    Codecs.readJson(body).flatMap(Codecs.parseBatch).flatMap(Analyzer.validate)

  /** Estadísticas de un jugador en todas sus partidas; None si no jugó ninguna. */
  def playerReport(playerId: String, results: List[MatchAnalysis]): Option[ujson.Value] =
    // flatMap: cada partida aporta 0 o 1 entrada; filter se queda solo con este jugador.
    val matches: List[(MatchAnalysis, PlayerStats)] =
      results.flatMap(a => a.playerStats.filter(_.playerId == playerId).map(a -> _))
    // Patrones de este jugador en cualquiera de sus partidas.
    val patterns = results.flatMap(a => a.patterns.filter(_.playerId.contains(playerId)).map(a.matchId -> _))

    Option.when(matches.nonEmpty)(
      ujson.Obj(
        "playerId"      -> playerId,
        "matchesPlayed" -> matches.size,
        "matches" -> ujson.Arr.from(matches.map((a, ps) =>
          ujson.Obj("matchId" -> a.matchId, "gameId" -> a.gameId, "metrics" -> Codecs.encodeMetrics(ps.metrics))
        )),
        "patterns" -> ujson.Arr.from(patterns.map((matchId, p) =>
          ujson.Obj("matchId" -> matchId, "rule" -> p.rule, "description" -> p.description)
        ))
      )
    )

  def error(status: Int, message: String): cask.Response[ujson.Value] =
    cask.Response(ujson.Obj("error" -> message), statusCode = status)
