package gamestats.validation

import gamestats.model.*

/** Validaciones de contexto de connect4 (contracts/README.md): dos jugadores,
  * turnos, gravedad, amenazas y deshacer. El tablero vive en el estado, así que
  * cada evento se revisa contra cómo está el tablero en ese momento.
  */
object Connect4Check extends ContextCheck:

  val columns: Int = 7
  val rows: Int    = 6

  type Cell = (Int, Int) // (columna, fila); la fila 1 es la de abajo

  /** Lo que cambia con cada jugada (y vuelve atrás con UNDO).
    *  - discs: de quién es cada casilla ocupada.
    *  - threats: amenazas abiertas, casilla -> jugadores a los que les completaría 4.
    *  - turn: jugador que tiene el turno.
    *  - last: última ficha del tablero (quién y dónde).
    */
  case class Board(discs: Map[Cell, String], threats: Map[Cell, Set[String]], turn: Option[String], last: Option[(String, Cell)])

  /** players: los dos que juegan, en orden de entrada. bots: los que son IA.
    * history: tableros anteriores, el más reciente primero (UNDO saca uno).
    * finished: ya hubo CONNECT_FOUR.
    */
  case class State(players: List[String], bots: Set[String], board: Board, history: List[Board], finished: Boolean):
    def rival(p: String): Option[String] = players.find(_ != p)

  def initial(batch: MatchBatch): State =
    State(Nil, Set.empty, Board(Map.empty, Map.empty, None, None), Nil, finished = false)

  /** Primera fila libre de la columna (7 si está llena). */
  def nextRow(board: Board, column: Int): Int =
    board.discs.keySet.count(_._1 == column) + 1

  def check(s: State, event: Event): Either[String, State] =
    event.playerId match
      case None => Right(s)
      case Some(p) =>
        for
          withPlayer <- seat(s, p, event)
          next       <- event.action.fold[Either[String, State]](Right(withPlayer))(action(withPlayer, p, _))
        yield next

  /** Juegan los dos primeros que aparecen; el primero empieza. Un tercero se descarta. */
  private def seat(s: State, p: String, event: Event): Either[String, State] =
    if s.players.contains(p) then Right(s)
    else if s.players.size == 2 then Left(s"connect4 es de dos: ya juegan ${s.players.mkString(" y ")}")
    else
      val isBot = event.data.objOpt.flatMap(_.get("bot")).flatMap(_.boolOpt).contains(true)
      val board = if s.board.turn.isEmpty then s.board.copy(turn = Some(p)) else s.board
      Right(s.copy(players = s.players :+ p, bots = if isBot then s.bots + p else s.bots, board = board))

  private def action(s: State, p: String, a: GameAction): Either[String, State] =
    val b = s.board
    if s.finished then Left("la partida ya terminó con el 4 en línea")
    else a match
      case DiscDropped(c, r) =>
        val expected = nextRow(b, c)
        for
          _ <- myTurn(s, p)
          _ <- Either.cond(expected <= rows, (), s"la columna $c está llena")
          _ <- Either.cond(r == expected, (), s"la ficha cae en la fila $expected de la columna $c, no en la $r")
        yield
          val moved = b.copy(discs = b.discs.updated((c, r), p), turn = s.rival(p), last = Some(p -> (c, r)))
          // El tablero anterior se apila para poder deshacer esta ficha.
          s.copy(board = moved, history = b :: s.history)
      case TurnTimeout(_) =>
        myTurn(s, p).map(_ => s.copy(board = b.copy(turn = s.rival(p))))
      case HintUsed(_) =>
        myTurn(s, p).map(_ => s)
      case ThreatCreated(c, r) =>
        val owners = b.threats.getOrElse((c, r), Set.empty)
        for
          _ <- Either.cond(!b.discs.contains((c, r)), (), s"la casilla ($c, $r) ya está ocupada")
          _ <- Either.cond(!owners.contains(p), (), s"$p ya tenía una amenaza en ($c, $r)")
        yield s.copy(board = b.copy(threats = b.threats.updated((c, r), owners + p)))
      case ThreatBlocked(c, r, attacker) =>
        val owners = b.threats.getOrElse((c, r), Set.empty)
        for
          _ <- Either.cond(owners.contains(attacker), (), s"$attacker no tiene una amenaza abierta en ($c, $r)")
          _ <- Either.cond(b.discs.get((c, r)).contains(p), (), s"la casilla ($c, $r) no tiene una ficha de $p")
        yield s.copy(board = b.copy(threats = b.threats.updated((c, r), owners - attacker)))
      case Undo(c, r) =>
        for
          _    <- Either.cond(s.rival(p).exists(s.bots.contains) && !s.bots.contains(p), (), "UNDO solo vale para el humano en una partida contra la IA")
          _    <- Either.cond(b.last.exists(_._2 == (c, r)), (), s"($c, $r) no es la última ficha del tablero")
          prev <- s.history.headOption.toRight("no hay ninguna ficha que deshacer")
        yield s.copy(board = prev, history = s.history.tail)
      case ConnectFour(_) =>
        Either.cond(b.last.exists(_._1 == p), s.copy(finished = true), s"$p no soltó la última ficha, no puede hacer el 4 en línea")
      case _ => Right(s)

  private def myTurn(s: State, p: String): Either[String, Unit] =
    Either.cond(s.board.turn.contains(p), (), s"no es el turno de $p")

  /** Estados de la partida evento a evento (scanLeft), sin descartar nada: se usa
    * sobre un lote ya validado para que las reglas de patrones vean el tablero.
    */
  def timeline(batch: MatchBatch): List[State] =
    batch.events.scanLeft(initial(batch))((s, e) => check(s, e).getOrElse(s))
