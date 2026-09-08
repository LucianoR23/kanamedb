package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// TableDataOptions describe qué página de una tabla leer.
type TableDataOptions struct {
	// OrderBy son las columnas por las que ordenar. Vacío significa sin ORDER
	// BY, y entonces el paginado no es confiable: ver TableData.
	OrderBy    []string
	Descending bool

	Limit  int
	Offset int
}

// TableData lee una página de una tabla.
//
// Sobre el orden: sin ORDER BY, LIMIT/OFFSET no define qué filas devuelve. El
// motor puede entregar la misma fila en dos páginas y saltearse otra, y no es
// una rareza teórica —pasa apenas hay concurrencia o el plan cambia—. Por eso
// TableData no inventa un orden ni lo esconde: recibe las columnas y quien
// llama decide. El servicio resuelve la clave primaria antes de llamar acá, y
// cuando no hay ninguna, la interfaz tiene que decir que "cargar más" es
// aproximado en vez de fingir que no lo es.
func TableData(ctx context.Context, pool *pgxpool.Pool, schema, table string, opts TableDataOptions) (*query.Result, *Failure) {
	var b strings.Builder
	b.WriteString("select * from ")
	b.WriteString(QualifiedName(schema, table))

	if len(opts.OrderBy) > 0 {
		b.WriteString(" order by ")
		for i, col := range opts.OrderBy {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(QuoteIdent(col))
		}
		if opts.Descending {
			b.WriteString(" desc")
		}
	}

	// El límite va en la consulta y no solo como corte de lectura: traer un
	// millón de filas para descartar todas menos veinticinco hace trabajar al
	// servidor y a la red por nada. Son enteros nuestros, no texto del usuario.
	if opts.Limit > 0 {
		fmt.Fprintf(&b, " limit %d", opts.Limit)
	}
	if opts.Offset > 0 {
		fmt.Fprintf(&b, " offset %d", opts.Offset)
	}

	// RowLimit negativo: el LIMIT de la consulta ya acota, y un segundo corte
	// del lado del cliente solo podría marcar Truncated de más.
	lote, f := Run(ctx, pool, b.String(), RunOptions{RowLimit: Unlimited})
	if f != nil {
		return nil, f
	}
	// Es una sola sentencia, así que hay exactamente un resultado. Se
	// desenvuelve acá para que quien lee una tabla no tenga que pensar en lotes.
	if len(lote.Results) != 1 {
		return nil, &Failure{
			Kind:    FailureOther,
			Message: fmt.Sprintf("La lectura de la tabla devolvió %d resultados y se esperaba uno.", len(lote.Results)),
		}
	}
	res := lote.Results[0]
	return &res, nil
}

// Unlimited desactiva el corte de lectura de Run.
const Unlimited = -1

// TableCount cuenta las filas de una tabla, exacto.
//
// Es un recorrido completo y puede tardar en una tabla grande, así que se pide
// aparte y no junto con los datos: la grilla tiene que poder mostrar las
// primeras filas sin esperar a que termine de contar. El árbol, en cambio, usa
// la estimación del planificador, que es gratis.
func TableCount(ctx context.Context, pool *pgxpool.Pool, schema, table string) (int64, *Failure) {
	var n int64
	sql := "select count(*) from " + QualifiedName(schema, table)
	if err := pool.QueryRow(ctx, sql).Scan(&n); err != nil {
		return 0, Classify(err, "el conteo de "+schema+"."+table)
	}
	return n, nil
}

// PrimaryKeyColumns devuelve las columnas de la clave primaria, en el orden en
// que están definidas en el índice.
//
// Se consulta esta tabla y nada más. Re-inspeccionar el esquema entero para
// saber la clave de un objeto es el tipo de cosa que hace que abrir una tabla
// en una base de doscientas tarde.
func PrimaryKeyColumns(ctx context.Context, pool *pgxpool.Pool, schema, table string) ([]string, error) {
	const sql = `
		select a.attname
		from pg_index i
		join pg_class c on c.oid = i.indrelid
		join pg_namespace n on n.oid = c.relnamespace
		join unnest(i.indkey) with ordinality as k(attnum, ord) on true
		join pg_attribute a on a.attrelid = c.oid and a.attnum = k.attnum
		where n.nspname = $1 and c.relname = $2 and i.indisprimary
		order by k.ord`

	rows, err := pool.Query(ctx, sql, schema, table)
	if err != nil {
		return nil, fmt.Errorf("leer la clave primaria de %s.%s: %w", schema, table, err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, fmt.Errorf("leer la clave primaria de %s.%s: %w", schema, table, err)
		}
		cols = append(cols, c)
	}
	return cols, rows.Err()
}
