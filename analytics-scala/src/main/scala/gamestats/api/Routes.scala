package gamestats.api

import gamestats.json.Codecs

/** Endpoints HTTP. La lógica de cada endpoint vive en funciones puras del objeto
  * compañero, así se puede testear sin levantar el servidor.
  */
case class AnalyticsRoutes()(using cc: castor.Context, log: cask.Logger) extends cask.Routes:

  @cask.get("/health")
  def health(): ujson.Value = ujson.Obj("status" -> "ok")

  @cask.post("/analyze")
  def analyze(request: cask.Request): cask.Response[ujson.Value] =
    AnalyticsRoutes.summarize(request.text()) match
      case Right(body) => cask.Response(body, statusCode = 200)
      case Left(error) => cask.Response(ujson.Obj("error" -> error), statusCode = 400)

  initialize()

object AnalyticsRoutes:

  /** Paso 2: parsea y valida el lote completo; todavía no analiza.
    * Pura: recibe el texto y devuelve Either, sin excepciones.
    */
  def summarize(body: String): Either[String, ujson.Value] =
    for
      json  <- Codecs.readJson(body)
      batch <- Codecs.parseBatch(json)
    yield ujson.Obj(
      "matchId"  -> batch.matchId,
      "gameId"   -> batch.gameId,
      "received" -> batch.events.size,
      "players"  -> batch.players
    )
