package service

import (
	"context"
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// Explain pide al motor el plan de ejecución de UNA sentencia, sin ejecutarla.
//
// Recibe el texto entero del editor y la línea del cursor, y explica la
// sentencia que está bajo el cursor: la última que empieza en esa línea o
// antes. Con una sola sentencia no hace falta cursor. Si el cursor está antes
// de la primera, se dice en vez de adivinar.
//
// # Lo único que importa acá
//
// La variante que se usa NO ejecuta la consulta: `EXPLAIN` en Postgres, MySQL
// y MariaDB, `EXPLAIN QUERY PLAN` en SQLite. `EXPLAIN ANALYZE` sí ejecuta —un
// DELETE incluido— y no entra ni como opción. Por lo mismo, una sentencia que
// ya empieza con EXPLAIN se rechaza en vez de envolverla: `EXPLAIN EXPLAIN` es
// un error de sintaxis en los cuatro, y un `EXPLAIN ANALYZE` escrito a mano
// correría desde un botón que promete no correr nada. Si se quiere eso, se
// ejecuta como cualquier otra sentencia, por el camino que confirma.
//
// El resultado es un query.Result más —los cuatro motores devuelven filas— y
// viaja en un RunResult, con el mismo runID que Run para poder cancelarlo. No
// se anota en el historial: el historial es lo que se ejecutó, y esto no lo
// fue.
func (q *Queries) Explain(ctx context.Context, runID, sql string, line int) RunResult {
	sesion, err := q.session.abierta()
	if err != nil {
		return RunResult{Failure: &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}}
	}
	prefijo := engine.CapsOf(sesion.conn.Engine).ExplainPrefix
	if prefijo == "" {
		return RunResult{Failure: &engine.Failure{
			Kind:    engine.FailureOther,
			Message: fmt.Sprintf("%s no da un plan de ejecución.", sesion.conn.Engine.Label()),
		}}
	}

	st, f := sentenciaBajoElCursor(query.Split(sql, sesion.db.Dialect()), line)
	if f != nil {
		return RunResult{Failure: f}
	}
	if f := explicable(st, query.Command(st.SQL, sesion.db.Dialect())); f != nil {
		return RunResult{Failure: f}
	}

	ctx, listo := q.registrar(ctx, runID)
	defer listo()

	// Sin límite de filas: un plan es chico y llega entero, y cortado a la
	// mitad del árbol —sin que la vista lo diga— es peor que no darlo.
	uno, f := sesion.db.Run(ctx, prefijo+" "+st.SQL, engine.RunOptions{RowLimit: -1})
	lote := &query.Batch{}
	if uno != nil {
		for _, r := range uno.Results {
			r.Line = st.Line
			lote.Results = append(lote.Results, r)
		}
		lote.ElapsedMs = uno.ElapsedMs
	}
	if f != nil {
		f.Line = st.Line
		return RunResult{Batch: lote, Failure: q.porElTunel(f)}
	}
	return RunResult{OK: true, Batch: lote}
}

// explicable dice si la sentencia es de las que EXPLAIN planifica, y si no,
// por qué no.
//
// Es una lista blanca y no una lista negra, y la diferencia es la garantía
// entera del botón. Rechazar solo lo que empieza con EXPLAIN dejaba pasar
// `ANALYZE DELETE FROM t`, que pegado detrás del prefijo es `EXPLAIN ANALYZE
// DELETE FROM t` y **borra**: Postgres acepta las opciones después de la
// palabra, con o sin paréntesis, y MySQL también tiene EXPLAIN ANALYZE. Lo
// encontró el review, comprobado contra el Postgres de prueba. Con la lista
// blanca, lo único que llega al motor detrás del prefijo es una consulta o un
// cambio de datos, que es exactamente lo que EXPLAIN sabe planificar; el
// resto —DDL, opciones sueltas, un EXPLAIN escrito a mano— se dice.
func explicable(st query.Statement, comando string) *engine.Failure {
	switch comando {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "WITH", "VALUES", "TABLE", "MERGE", "REPLACE":
		return nil
	case "EXPLAIN":
		return &engine.Failure{
			Kind:    engine.FailureOther,
			Message: fmt.Sprintf("La sentencia de la línea %d ya es un EXPLAIN.", st.Line),
			Hint:    "Ejecutala como cualquier otra, o sacale el EXPLAIN y pedí el plan de la consulta de adentro.",
		}
	case "":
		return &engine.Failure{
			Kind:    engine.FailureOther,
			Message: fmt.Sprintf("La sentencia de la línea %d no empieza con un comando que se pueda planificar.", st.Line),
			Hint:    "El plan es de una consulta o un cambio de datos: SELECT, INSERT, UPDATE, DELETE, WITH…",
		}
	}
	return &engine.Failure{
		Kind:    engine.FailureOther,
		Message: fmt.Sprintf("La sentencia de la línea %d empieza con %s, y eso no tiene plan.", st.Line, comando),
		Hint:    "El plan es de una consulta o un cambio de datos: SELECT, INSERT, UPDATE, DELETE, WITH…",
	}
}

// sentenciaBajoElCursor elige qué sentencia explicar.
func sentenciaBajoElCursor(sentencias []query.Statement, line int) (query.Statement, *engine.Failure) {
	switch {
	case len(sentencias) == 0:
		return query.Statement{}, &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "No hay ninguna sentencia que explicar.",
		}
	case len(sentencias) == 1:
		return sentencias[0], nil
	}
	elegida := -1
	for i, st := range sentencias {
		if st.Line <= line {
			elegida = i
		}
	}
	if elegida < 0 {
		return query.Statement{}, &engine.Failure{
			Kind:    engine.FailureOther,
			Message: fmt.Sprintf("Hay %d sentencias y el cursor no está sobre ninguna.", len(sentencias)),
			Hint:    "El plan es de una por vez: dejá el cursor sobre la que querés ver.",
		}
	}
	return sentencias[elegida], nil
}
