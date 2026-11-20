package gamestats.model

/** v1.1: estadísticas acumuladas sobre las partidas ya analizadas (para el panel
  * de GameConsole). Se calculan con las mismas MatchAnalysis que devuelve
  * /results, así el panel nunca recalcula nada por su cuenta.
  */

/** Un nombre y cuántas veces aparece (ganadores, patrones). */
case class Ranked(name: String, count: Int)

/** Promedio, mínimo y máximo de una métrica entre todos los jugadores de un juego. */
case class MetricSummary(avg: Double, min: Double, max: Double)

/** Una fila de la clasificación: cómo le fue a un jugador en las partidas terminadas.
  * played = wins + losses + noResult.
  */
case class Standing(
    playerId: String,
    played: Int,                         // partidas en las que jugó
    wins: Int,
    losses: Int,                         // partidas con ganador, y no fue él
    noResult: Int,                       // partidas sin ganador (empate, abandono, de un solo jugador)
    winRate: Double,                     // wins / played, de 0 a 1 con 2 decimales
    metrics: Map[String, MetricSummary]  // sus métricas (vacío en el resumen general: mezclaría juegos)
)

case class GameSummary(
    gameId: String,
    matches: Int,                        // partidas terminadas y analizadas
    players: Int,                        // jugadores distintos con métricas
    totalEvents: Int,
    avgDurationSeconds: Double,
    avgEventsPerMatch: Double,
    matchesWithoutWinner: Int,           // empates, abandonos o juegos de uno (tetris)
    winners: List[Ranked],               // de más a menos victorias
    metrics: Map[String, MetricSummary], // por nombre de métrica
    actions: Map[String, Int],           // eventsByAction sumado
    patterns: List[Ranked],              // reglas detectadas, de más a menos frecuentes
    standings: List[Standing]            // clasificación de los jugadores de este juego
)

case class Overview(
    matches: Int,
    players: Int,
    totalEvents: Int,
    avgDurationSeconds: Double,
    games: List[GameSummary],            // una por gameId, ordenadas
    topWinners: List[Ranked],
    topPatterns: List[Ranked],
    standings: List[Standing]            // clasificación general, sumando todos los juegos
)
