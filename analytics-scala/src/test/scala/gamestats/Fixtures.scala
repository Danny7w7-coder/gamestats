package gamestats

import java.nio.file.{Files, Paths}

/** Lee los fixtures compartidos de `contracts/fixtures` (sbt corre los tests
  * parado en `analytics-scala/`).
  */
object Fixtures:
  def text(name: String): String =
    Files.readString(Paths.get("..", "contracts", "fixtures", name))

  def json(name: String): ujson.Value = ujson.read(text(name))
