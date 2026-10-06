ThisBuild / scalaVersion := "3.3.4"
ThisBuild / organization := "gamestats"

lazy val root = (project in file("."))
  .settings(
    name := "analytics-scala",
    libraryDependencies ++= Seq(
      "com.lihaoyi"   %% "cask"  % "0.9.4",
      "org.scalameta" %% "munit" % "1.0.0" % Test
    ),
    scalacOptions ++= Seq("-deprecation", "-feature", "-unchecked"),

    // `sbt run` en un proceso aparte: el servidor queda vivo y Ctrl+C lo detiene limpio.
    run / fork := true,
    Compile / mainClass := Some("gamestats.Main"),

    // Fat jar para Docker (Paso 9).
    assembly / mainClass := Some("gamestats.Main"),
    assembly / assemblyJarName := "analytics.jar",
    assembly / assemblyMergeStrategy := {
      // Undertow/XNIO se registran por ServiceLoader: si se descartan, el jar no arranca.
      case PathList("META-INF", "services", _*) => MergeStrategy.concat
      case PathList("META-INF", _*)             => MergeStrategy.discard
      case _                                    => MergeStrategy.first
    }
  )
