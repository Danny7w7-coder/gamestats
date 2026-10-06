package gamestats.storage

import gamestats.json.Codecs
import gamestats.model.MatchAnalysis

import java.nio.charset.StandardCharsets.UTF_8
import java.nio.file.{Files, Path, StandardCopyOption}
import scala.collection.concurrent.TrieMap
import scala.jdk.CollectionConverters.*
import scala.util.{Try, Using}

/** Resultados guardados: un mapa en memoria + un archivo JSON por partida.
  *
  * Es el ÚNICO lugar del módulo con efectos (disco) y estado mutable (TrieMap).
  * El análisis es puro y no sabe que esto existe; las rutas HTTP le pasan los
  * resultados ya calculados. Se usa TrieMap porque cask atiende requests en
  * varios hilos a la vez y TrieMap es seguro para lecturas y escrituras concurrentes.
  * Los errores de disco vuelven como Either (no se lanzan excepciones hacia afuera).
  */
final class ResultStore private (val dir: Path, cache: TrieMap[String, MatchAnalysis]):

  /** Escribe el archivo y, si salió bien, actualiza la memoria. Si la partida ya
    * existía, se reemplaza (Go puede reenviar el mismo lote).
    */
  def save(analysis: MatchAnalysis): Either[String, MatchAnalysis] =
    val target = dir.resolve(ResultStore.fileName(analysis.matchId))
    val tmp    = dir.resolve(ResultStore.fileName(analysis.matchId) + ".tmp")
    Try {
      // Se escribe a un temporal y se mueve: nunca queda un archivo a medio escribir.
      Files.writeString(tmp, ujson.write(Codecs.encode(analysis), indent = 2), UTF_8)
      Files.move(tmp, target, StandardCopyOption.REPLACE_EXISTING, StandardCopyOption.ATOMIC_MOVE)
    }.toEither
      .left.map(e => s"no se pudo guardar '${analysis.matchId}': ${e.getMessage}")
      .map { _ =>
        cache.update(analysis.matchId, analysis)
        analysis
      }

  def get(matchId: String): Option[MatchAnalysis] = cache.get(matchId)

  /** Todos los resultados, ordenados por matchId para que la respuesta sea estable. */
  def all: List[MatchAnalysis] = cache.values.toList.sortBy(_.matchId)

object ResultStore:

  /** Store abierto + archivos que no se pudieron leer (se informan, no frenan el arranque). */
  case class Opened(store: ResultStore, skipped: List[String])

  /** Crea la carpeta si no existe y carga los `.json` que ya haya. */
  def open(dir: Path): Either[String, Opened] =
    for
      _     <- Try(Files.createDirectories(dir)).toEither.left.map(e => s"no se pudo crear '$dir': ${e.getMessage}")
      files <- Try(Using.resource(Files.list(dir))(_.iterator.asScala.toList)).toEither
                 .left.map(e => s"no se pudo leer '$dir': ${e.getMessage}")
    yield
      val results = files
        .filter(_.getFileName.toString.endsWith(".json"))
        .sortBy(_.getFileName.toString)
        .map(f => readFile(f).left.map(err => s"${f.getFileName}: $err"))
      // collect separa los que cargaron de los que fallaron, sin recorrer dos veces a mano.
      val loaded  = results.collect { case Right(a) => a.matchId -> a }
      val skipped = results.collect { case Left(err) => err }
      Opened(ResultStore(dir, TrieMap.from(loaded)), skipped)

  private def readFile(f: Path): Either[String, MatchAnalysis] =
    Try(Files.readString(f, UTF_8)).toEither.left.map(_.getMessage)
      .flatMap(Codecs.readJson)
      .flatMap(Codecs.decodeAnalysis)

  /** matchId -> nombre de archivo seguro: todo lo que no sea letra, número, `_`,
    * `.` o `-` pasa a `_`, así un matchId con `/` o `\` no puede salirse de la carpeta.
    */
  def fileName(matchId: String): String =
    matchId.replaceAll("[^A-Za-z0-9_.-]", "_") + ".json"
