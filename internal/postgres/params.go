package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// tipos es lo único que se necesita de la conexión para nombrar una columna.
type tipos interface{ TypeMap() *pgtype.Map }

// aBytes pasa los argumentos de dml —siempre `*string`— a la forma que espera
// el protocolo: nil es NULL, y el resto son los bytes del texto.
func aBytes(args []any) [][]byte {
	if len(args) == 0 {
		return nil
	}
	out := make([][]byte, len(args))
	for i, a := range args {
		v, _ := a.(*string)
		if v == nil {
			continue
		}
		out[i] = []byte(*v)
	}
	return out
}

// ejecutarConParams corre UNA consulta con parámetros y devuelve su lector.
//
// Usa ExecParams y no pool.Query por el mismo motivo que Scan usa el protocolo
// simple: acá los formatos se piden explícitamente y `nil` en resultFormats
// significa TEXTO para todas las columnas. Con pool.Query, pgx pide binario
// para muchos tipos y los decodifica a tipos de Go, y entonces la misma fila
// saldría distinta en la grilla que en un archivo exportado.
//
// Los parámetros también van como texto —paramFormats nil— y sin declarar OID:
// el servidor los interpreta según la columna con la que se comparan, que es la
// misma conversión que haría con un literal escrito a mano.
func ejecutarConParams(ctx context.Context, c *pgconn.PgConn, sql string, args []any) *pgconn.ResultReader {
	return c.ExecParams(ctx, sql, aBytes(args), nil, nil, nil)
}

// leerConParams corre la consulta y arma el resultado entero.
//
// Es el camino de la grilla: una página acotada por LIMIT, así que juntar las
// filas está bien. Exportar usa Scan, que no las junta.
func leerConParams(
	ctx context.Context, c *pgconn.PgConn, tm tipos, sql string, args []any, limite int,
) (*query.Result, []uint32, *Failure) {
	rr := ejecutarConParams(ctx, c, sql, args)

	campos := rr.FieldDescriptions()
	res := &query.Result{Rows: [][]*string{}, RowLimit: limite}
	oids := make([]uint32, len(campos))
	if len(campos) > 0 {
		res.Columns = make([]query.Column, len(campos))
		res.ReturnsRows = true
		for i, f := range campos {
			res.Columns[i] = columnaDe(f, tm.TypeMap())
			oids[i] = f.DataTypeOID
		}
	}
	for rr.NextRow() {
		if limite >= 0 && len(res.Rows) >= limite {
			res.Truncated = true
			// Se sigue leyendo hasta el final: cortar acá dejaría la conexión
			// con datos pendientes y la próxima consulta leería los de esta.
			continue
		}
		crudas := rr.Values()
		fila := make([]*string, len(crudas))
		for i, b := range crudas {
			// NULL viaja con longitud -1 y la cadena vacía con longitud 0.
			if b == nil {
				continue
			}
			s := string(b)
			fila[i] = &s
		}
		res.Rows = append(res.Rows, fila)
	}
	tag, err := rr.Close()
	if err != nil {
		// De la sentencia, no de la conexión: acá el servidor ya contestó. Con
		// Classify, un filtro que compara texto con un entero se mostraba
		// como «el servidor rechazó la conexión con la consulta».
		return nil, nil, ClassifyStatement(err, "la consulta")
	}
	res.Command = tag.String()
	res.AffectedRows = tag.RowsAffected()
	return res, oids, nil
}
