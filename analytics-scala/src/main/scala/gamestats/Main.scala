package gamestats

import gamestats.api.AnalyticsRoutes

/** Punto de entrada del servicio HTTP. */
object Main extends cask.Main:

  // 0.0.0.0 para que sea accesible desde fuera del contenedor Docker.
  override def host: String = "0.0.0.0"

  // PORT viene del entorno; si falta o no es número, se usa 8081.
  override def port: Int =
    sys.env.get("PORT").flatMap(_.toIntOption).getOrElse(8081)

  val allRoutes = Seq(AnalyticsRoutes())
