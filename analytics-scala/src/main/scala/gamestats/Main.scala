package gamestats

import gamestats.api.AnalyticsRoutes
import gamestats.storage.ResultStore

import java.nio.file.Paths

/** Punto de entrada del servicio HTTP. */
object Main extends cask.Main:

  // 0.0.0.0 para que sea accesible desde fuera del contenedor Docker.
  override def host: String = "0.0.0.0"

  // PORT viene del entorno; si falta o no es número, se usa 8081.
  override def port: Int =
    sys.env.get("PORT").flatMap(_.toIntOption).getOrElse(8081)

  private val resultsDir = Paths.get(sys.env.getOrElse("RESULTS_DIR", "./results"))

  // Si la carpeta de resultados no se puede usar, no tiene sentido arrancar.
  private val store: ResultStore = ResultStore.open(resultsDir) match
    case Right(ResultStore.Opened(store, skipped)) =>
      skipped.foreach(s => Console.err.println(s"[results] archivo ignorado: $s"))
      println(s"[results] ${store.all.size} resultado(s) cargados desde ${resultsDir.toAbsolutePath.normalize}")
      store
    case Left(error) =>
      Console.err.println(s"[results] $error")
      sys.exit(1)

  val allRoutes = Seq(AnalyticsRoutes(store))
