package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// DefaultRowLimit es el corte cuando la conexión no fija uno.
//
// Mil filas entran en la grilla sin que se note y no llenan la memoria con el
// resultado de un `select * from` contra una tabla grande escrito de apuro. Es
// un corte de lectura, no un LIMIT agregado a la consulta: la consulta que el
// usuario escribió se manda tal cual, y lo que se corta es cuánto traemos.
const DefaultRowLimit = 1000

// RunOptions ajusta una ejecución.
type RunOptions struct {
	// RowLimit corta la lectura. Cero usa DefaultRowLimit; negativo lee todo.
	//
	// Coincide con la convención de connection.Safety, donde cero significa
	// "usá el default" y -1 significa "sin límite", de modo que el valor se
	// pasa derecho desde la conexión sin traducir.
	RowLimit int
}

// Run ejecuta una sentencia y devuelve el resultado listo para la grilla.
//
// La SQL se manda tal como la escribió el usuario. No se envuelve, no se le
// agrega LIMIT y no se parsea: un editor de SQL que reescribe lo que ejecutás
// es un editor en el que no se puede confiar, y cualquier reescritura nuestra
// se equivocaría con CTEs, UNION o funciones que devuelven conjuntos.
//
// Se usa el protocolo simple a propósito. Es el que usa psql, no crea sentencias
// preparadas —que ensucian el servidor con SQL que se escribe una vez— y, sobre
// todo, garantiza que el servidor devuelva todo en formato texto. Ver leerFilas.
func Run(ctx context.Context, pool *pgxpool.Pool, sql string, opts RunOptions) (*query.Result, *Failure) {
	limite := opts.RowLimit
	if limite == 0 {
		limite = DefaultRowLimit
	}

	arranque := time.Now()
	rows, err := pool.Query(ctx, sql, pgx.QueryExecModeSimpleProtocol)
	if err != nil {
		return nil, Classify(err, "la consulta")
	}

	res, oids, err := leerFilas(rows, limite)
	// rows.Err() ya lo mira leerFilas; acá solo queda cerrar.
	rows.Close()
	if err != nil {
		return nil, Classify(err, "la consulta")
	}
	res.ElapsedMs = time.Since(arranque).Milliseconds()
	res.RowLimit = limite

	// Los tipos que pgx no conoce —enums, dominios, tipos del usuario— se
	// resuelven contra el catálogo, y recién cuando aparecen. La consulta normal
	// devuelve tipos incorporados y no paga ningún viaje extra.
	if err := resolverTiposDesconocidos(ctx, pool, res, oids); err != nil {
		return nil, Classify(err, "los tipos del resultado")
	}
	return res, nil
}

// leerFilas arma el Result recorriendo el cursor.
// Devuelve además los OID de cada columna: el Result no los lleva —a la
// interfaz no le sirven— pero hacen falta para resolver los tipos que pgx no
// reconoce, y las descripciones de campo dejan de ser válidas al cerrar rows.
func leerFilas(rows pgx.Rows, limite int) (*query.Result, []uint32, error) {
	campos := rows.FieldDescriptions()
	res := &query.Result{
		Columns:     make([]query.Column, len(campos)),
		Rows:        [][]*string{},
		ReturnsRows: len(campos) > 0,
	}

	tm := rows.TypeMap()
	oids := make([]uint32, len(campos))
	for i, f := range campos {
		res.Columns[i] = columnaDe(f, tm)
		oids[i] = f.DataTypeOID
	}

	for rows.Next() {
		// Se lee una fila de más para saber si quedaron más del otro lado. Sin
		// eso habría que elegir entre mentir —cortar en el límite y no decirlo—
		// o contar las filas con una segunda consulta.
		if limite >= 0 && len(res.Rows) == limite {
			res.Truncated = true
			break
		}

		crudas := rows.RawValues()
		fila := make([]*string, len(crudas))
		for i, b := range crudas {
			// Acá está toda la fidelidad del resultado. En el protocolo, NULL
			// viaja con longitud -1 y la cadena vacía con longitud 0: pgx
			// preserva la diferencia como slice nil contra slice vacío. Un
			// `if len(b) == 0` la borraría, y la grilla mostraría un NULL y un
			// '' exactamente igual.
			if b == nil {
				continue
			}
			// Se copia porque la API lo exige: RawValues documenta que los
			// bytes solo valen hasta el próximo Next o hasta cerrar rows.
			//
			// No es una precaución observada. Se intentó reproducir el daño
			// aliaseando el buffer con unsafe.String y el resultado salió
			// correcto igual, así que en esta versión de pgx y por este camino
			// el buffer no se reusa. Se copia por el contrato, no por el
			// síntoma — que es la razón que sigue valiendo cuando pgx cambie.
			s := string(b)
			fila[i] = &s
		}
		res.Rows = append(res.Rows, fila)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	tag := rows.CommandTag()
	res.Command = tag.String()
	res.AffectedRows = tag.RowsAffected()
	return res, oids, nil
}

// columnaDe traduce la descripción del campo a lo que necesita la interfaz.
func columnaDe(f pgconn.FieldDescription, tm *pgtype.Map) query.Column {
	col := query.Column{Name: f.Name, Class: claseDeOID(f.DataTypeOID)}
	if t, ok := tm.TypeForOID(f.DataTypeOID); ok {
		col.DataType = t.Name
	}
	return col
}

// claseDeOID clasifica los tipos incorporados.
//
// Solo los que cambian cómo se muestra la columna. Todo lo demás cae en
// ClassOther y se muestra como texto, que es lo correcto: inventar una
// categoría para un tipo que no conocemos es peor que admitir que no lo
// conocemos.
func claseDeOID(oid uint32) query.Class {
	switch oid {
	case pgtype.BoolOID:
		return query.ClassBool
	case pgtype.Int2OID, pgtype.Int4OID, pgtype.Int8OID,
		pgtype.Float4OID, pgtype.Float8OID, pgtype.NumericOID,
		pgtype.OIDOID, pgtype.XIDOID, pgtype.CIDOID:
		return query.ClassNumber
	case pgtype.TextOID, pgtype.VarcharOID, pgtype.BPCharOID, pgtype.NameOID,
		pgtype.UUIDOID, pgtype.InetOID, pgtype.CIDROID, pgtype.MacaddrOID:
		return query.ClassText
	case pgtype.DateOID, pgtype.TimeOID, pgtype.TimestampOID,
		pgtype.TimestamptzOID, pgtype.IntervalOID, pgtype.TimetzOID:
		return query.ClassTemporal
	case pgtype.JSONOID, pgtype.JSONBOID:
		return query.ClassJSON
	case pgtype.ByteaOID:
		return query.ClassBinary
	}
	return query.ClassOther
}

// resolverTiposDesconocidos completa el nombre del tipo de las columnas que pgx
// no reconoció, y las clasifica por la categoría que declara el catálogo.
//
// Una columna de un enum llegaba acá con DataType vacío, y "sin nombre de tipo"
// en el encabezado de la grilla no le sirve a nadie.
func resolverTiposDesconocidos(ctx context.Context, pool *pgxpool.Pool, res *query.Result, oids []uint32) error {
	var faltantes []int64
	pendientes := map[uint32][]int{}
	for i, c := range res.Columns {
		if c.DataType != "" {
			continue
		}
		oid := oids[i]
		if _, visto := pendientes[oid]; !visto {
			faltantes = append(faltantes, int64(oid))
		}
		pendientes[oid] = append(pendientes[oid], i)
	}
	if len(faltantes) == 0 {
		return nil
	}

	rows, err := pool.Query(ctx,
		`select oid, typname, typcategory from pg_type where oid = any($1::oid[])`, faltantes)
	if err != nil {
		return fmt.Errorf("leer pg_type: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var oid uint32
		var nombre, categoria string
		if err := rows.Scan(&oid, &nombre, &categoria); err != nil {
			return fmt.Errorf("leer pg_type: %w", err)
		}
		for _, i := range pendientes[oid] {
			res.Columns[i].DataType = nombre
			res.Columns[i].Class = clasePorCategoria(categoria)
		}
	}
	return rows.Err()
}

// clasePorCategoria traduce typcategory de pg_type.
//
// Es una letra sola que Postgres ya mantiene por nosotros, así que un enum del
// usuario se alinea y se muestra bien sin que tengamos que conocerlo.
func clasePorCategoria(c string) query.Class {
	switch c {
	case "N":
		return query.ClassNumber
	case "S":
		return query.ClassText
	case "D":
		return query.ClassTemporal
	case "B":
		return query.ClassBool
	}
	return query.ClassOther
}
