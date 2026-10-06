package gamestats.rules

import gamestats.model.{MatchBatch, Pattern}

/** Regla que busca un patrón en la secuencia de eventos. Pura: el mismo lote
  * siempre da los mismos patrones. Cada analyzer declara qué reglas le aplican.
  */
trait Rule:
  def name: String
  def detect(batch: MatchBatch): List[Pattern]
