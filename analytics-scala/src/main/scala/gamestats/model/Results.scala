package gamestats.model

/** Estadísticas de la partida. `winner` y `score` son Option porque dependen del
  * juego: `score` solo existe en football y `winner` es None en empate o sin datos.
  */
case class MatchStats(
    durationSeconds: Long,
    totalEvents: Int,
    playerCount: Int,
    eventsByAction: Map[String, Int],
    winner: Option[String],
    score: Option[Map[String, Int]]
)

/** Métricas por nombre: cada juego decide cuáles calcula sin cambiar el modelo. */
case class TeamStats(teamId: String, metrics: Map[String, Double])
case class PlayerStats(playerId: String, metrics: Map[String, Double])

/** Patrón detectado por una regla. `playerId` es None en patrones de equipo. */
case class Pattern(rule: String, playerId: Option[String], description: String)

case class MatchAnalysis(
    matchId: String,
    gameId: String,
    matchStats: MatchStats,
    teamStats: Option[List[TeamStats]],
    playerStats: List[PlayerStats],
    patterns: List[Pattern]
)
