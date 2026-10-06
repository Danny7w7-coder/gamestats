package gamestats.api

import scala.util.Try

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

  /** Paso 1 (mínimo): valida que el lote tenga `matchId` y cuenta los eventos.
    * Pura: recibe el texto y devuelve Either, sin excepciones.
    */
  def summarize(body: String): Either[String, ujson.Value] =
    for
      // ujson.read lanza excepción con JSON inválido; Try la convierte en Left.
      json    <- Try(ujson.read(body)).toEither.left.map(_ => "JSON inválido")
      obj     <- json.objOpt.toRight("el lote debe ser un objeto JSON")
      matchId <- obj.get("matchId").flatMap(_.strOpt).filter(_.nonEmpty).toRight("falta matchId")
      // `events` ausente cuenta como 0; si existe pero no es arreglo, es error.
      received <- obj.get("events") match
                    case None    => Right(0)
                    case Some(v) => v.arrOpt.map(_.size).toRight("events debe ser un arreglo")
    yield ujson.Obj("matchId" -> matchId, "received" -> received)
