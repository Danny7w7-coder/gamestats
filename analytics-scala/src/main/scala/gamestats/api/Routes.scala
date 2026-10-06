package gamestats.api

import gamestats.json.Codecs
import gamestats.stats.Analyzer

/** Endpoints HTTP. La lógica de cada endpoint vive en funciones puras del objeto
  * compañero, así se puede testear sin levantar el servidor.
  */
case class AnalyticsRoutes()(using cc: castor.Context, log: cask.Logger) extends cask.Routes:

  @cask.get("/health")
  def health(): ujson.Value = ujson.Obj("status" -> "ok")

  @cask.post("/analyze")
  def analyze(request: cask.Request): cask.Response[ujson.Value] =
    AnalyticsRoutes.analyzeBody(request.text()) match
      case Right(body) => cask.Response(body, statusCode = 200)
      case Left(error) => cask.Response(ujson.Obj("error" -> error), statusCode = 400)

  initialize()

object AnalyticsRoutes:

  /** Texto del request -> MatchAnalysis en JSON. Pura: cualquier fallo
    * (JSON, validación o juego sin analyzer) vuelve como Left.
    */
  def analyzeBody(body: String): Either[String, ujson.Value] =
    for
      json     <- Codecs.readJson(body)
      batch    <- Codecs.parseBatch(json)
      analysis <- Analyzer.analyze(batch)
    yield Codecs.encode(analysis)
