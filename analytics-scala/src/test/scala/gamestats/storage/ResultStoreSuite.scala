package gamestats.storage

import gamestats.Fixtures
import gamestats.api.AnalyticsRoutes
import gamestats.json.Codecs
import gamestats.model.MatchAnalysis

import java.nio.charset.StandardCharsets.UTF_8
import java.nio.file.{Files, Path}
import scala.jdk.CollectionConverters.*

class ResultStoreSuite extends munit.FunSuite:

  /** Carpeta temporal nueva para cada test. */
  val tempDir = FunFixture[Path](
    setup = _ => Files.createTempDirectory("gamestats-results"),
    teardown = dir => Files.walk(dir).iterator.asScala.toList.reverse.foreach(Files.deleteIfExists)
  )

  def analyzed(name: String): MatchAnalysis =
    AnalyticsRoutes.analyzeBody(Fixtures.text(name)).fold(fail(_), identity)

  def open(dir: Path): ResultStore.Opened = ResultStore.open(dir).fold(fail(_), identity)

  tempDir.test("encode y decodeAnalysis son inversos (ida y vuelta)"): _ =>
    List("racing_match_ok.json", "comeback_case.json", "fighting_match_ok.json", "connect4_vs_bot.json", "tetris_match_ok.json")
      .map(analyzed)
      .foreach(a => assertEquals(Codecs.decodeAnalysis(Codecs.encode(a)), Right(a)))

  tempDir.test("crea la carpeta si no existe"): dir =>
    val nested = dir.resolve("a").resolve("b")
    open(nested)
    assert(Files.isDirectory(nested))

  tempDir.test("save guarda en memoria y en un archivo por partida"): dir =>
    val store = open(dir).store
    val race  = analyzed("racing_match_ok.json")
    assertEquals(store.save(race), Right(race))
    assertEquals(store.get("race-001"), Some(race))
    assert(Files.exists(dir.resolve("race-001.json")))
    assertEquals(store.get("no-existe"), None)

  tempDir.test("al reabrir se cargan los resultados guardados (sobrevive un reinicio)"): dir =>
    val first = open(dir).store
    val c4    = analyzed("connect4_match_ok.json")
    val race  = analyzed("racing_match_ok.json")
    first.save(race)
    first.save(c4)
    val reopened = open(dir)
    assertEquals(reopened.store.all, List(c4, race)) // ordenados por matchId
    assertEquals(reopened.skipped, Nil)

  tempDir.test("guardar la misma partida dos veces la reemplaza"): dir =>
    val store = open(dir).store
    val race  = analyzed("racing_match_ok.json")
    store.save(race)
    store.save(race)
    assertEquals(store.all.size, 1)

  tempDir.test("un archivo roto se informa y no impide arrancar"): dir =>
    open(dir).store.save(analyzed("racing_match_ok.json"))
    Files.writeString(dir.resolve("roto.json"), "{esto no es json", UTF_8)
    Files.writeString(dir.resolve("notas.txt"), "no es .json, se ignora", UTF_8)
    val reopened = open(dir)
    assertEquals(reopened.store.all.map(_.matchId), List("race-001"))
    assertEquals(reopened.skipped.size, 1)
    assert(reopened.skipped.head.startsWith("roto.json:"))

  test("fileName no deja salir de la carpeta"):
    assertEquals(ResultStore.fileName("race-001"), "race-001.json")
    assertEquals(ResultStore.fileName("../../etc/x"), ".._.._etc_x.json")
    assertEquals(ResultStore.fileName("a\\b c:d"), "a_b_c_d.json")
