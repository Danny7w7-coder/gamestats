package gamestats.model

/** Estadísticas de la partida. `winner` es Option porque no siempre hay
  * ganador: empate, partida sin datos o tetris (que se juega solo).
  * `interrupted` (v1.3): la partida se cerró antes de terminar (ver
  * GenericStats.interrupted); los resultados guardados antes no lo traen y se
  * leen como `false`.
  */
case class MatchStats(
    durationSeconds: Long,
    totalEvents: Int,
    playerCount: Int,
    eventsByAction: Map[String, Int],
    winner: Option[String],
    interrupted: Boolean = false
)

/** Métricas por nombre: cada juego decide cuáles calcula sin cambiar el modelo. */
case class PlayerStats(playerId: String, metrics: Map[String, Double])

/** Patrón detectado por una regla. `playerId` es None si el patrón no es de un jugador. */
case class Pattern(rule: String, playerId: Option[String], description: String)

case class MatchAnalysis(
    matchId: String,
    gameId: String,
    matchStats: MatchStats,
    playerStats: List[PlayerStats],
    patterns: List[Pattern]
)
