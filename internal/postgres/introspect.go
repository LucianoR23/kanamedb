package postgres

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Introspect lee el esquema de la base y lo devuelve en el modelo canónico.
//
// La Iteración 1 solo trae esquemas y tablas: es lo que el árbol necesita para
// existir. Vistas, funciones y demás llegan en la Iteración 8.
func Introspect(ctx context.Context, pool *pgxpool.Pool) (*schema.Snapshot, error) {
	var database string
	if err := pool.QueryRow(ctx, "select current_database()").Scan(&database); err != nil {
		return nil, fmt.Errorf("leer el nombre de la base: %w", err)
	}

	esquemas, err := readSchemas(ctx, pool)
	if err != nil {
		return nil, err
	}
	if err := readTables(ctx, pool, esquemas); err != nil {
		return nil, err
	}

	snap := &schema.Snapshot{
		Database:   database,
		CapturedAt: time.Now(),
	}
	for _, nombre := range ordenDeLectura(esquemas) {
		snap.Schemas = append(snap.Schemas, *esquemas[nombre])
	}
	snap.Normalize()
	return snap, nil
}

// schemasQuery lista los esquemas que el usuario puede usar.
//
// El filtro por has_schema_privilege no es cosmético: sin él el árbol muestra
// esquemas que después dan error al abrirse, y el usuario no tiene forma de
// saber que el problema es de permisos y no de la app.
const schemasQuery = `
	SELECT n.nspname,
	       pg_catalog.pg_get_userbyid(n.nspowner),
	       coalesce(obj_description(n.oid, 'pg_namespace'), '')
	FROM pg_catalog.pg_namespace n
	WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
	  AND n.nspname NOT LIKE 'pg\_toast%'
	  AND n.nspname NOT LIKE 'pg\_temp%'
	  AND n.nspname NOT LIKE 'pg\_toast\_temp%'
	  AND pg_catalog.has_schema_privilege(n.oid, 'USAGE')
	ORDER BY n.nspname`

func readSchemas(ctx context.Context, pool *pgxpool.Pool) (map[string]*schema.Schema, error) {
	rows, err := pool.Query(ctx, schemasQuery)
	if err != nil {
		return nil, fmt.Errorf("listar los esquemas: %w", err)
	}
	defer rows.Close()

	out := make(map[string]*schema.Schema)
	for rows.Next() {
		var s schema.Schema
		if err := rows.Scan(&s.Name, &s.Owner, &s.Comment); err != nil {
			return nil, fmt.Errorf("leer un esquema: %w", err)
		}
		out[s.Name] = &s
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer los esquemas: %w", err)
	}
	return out, nil
}

// tablesQuery lista las tablas de los esquemas accesibles.
//
// relkind 'r' son tablas ordinarias y 'p' particionadas. Se excluyen las
// particiones hijas (relispartition): son detalle de implementación y llenarían
// el árbol de ruido; la tabla particionada padre ya las representa.
//
// reltuples es la estimación del planificador, no un conteo. Un count(*) exacto
// recorrería cada tabla entera cada vez que se abre el árbol.
//
// Las tablas sin permiso de SELECT se listan igual, marcadas con Readable en
// false: esconderlas es más confuso que mostrarlas, pero la UI tiene que poder
// avisar antes de que el usuario haga clic y reciba un error de permisos.
const tablesQuery = `
	SELECT n.nspname,
	       c.relname,
	       c.relkind = 'p',
	       coalesce(obj_description(c.oid, 'pg_class'), ''),
	       c.reltuples::bigint,
	       EXISTS (
	         SELECT 1 FROM pg_catalog.pg_index i
	         WHERE i.indrelid = c.oid AND i.indisprimary
	       ),
	       pg_catalog.has_table_privilege(c.oid, 'SELECT')
	FROM pg_catalog.pg_class c
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	WHERE c.relkind IN ('r', 'p')
	  AND NOT c.relispartition
	  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
	  AND n.nspname NOT LIKE 'pg\_toast%'
	  AND n.nspname NOT LIKE 'pg\_temp%'
	  AND pg_catalog.has_schema_privilege(n.oid, 'USAGE')
	ORDER BY n.nspname, c.relname`

func readTables(ctx context.Context, pool *pgxpool.Pool, esquemas map[string]*schema.Schema) error {
	rows, err := pool.Query(ctx, tablesQuery)
	if err != nil {
		return fmt.Errorf("listar las tablas: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var nsp string
		var t schema.Table
		if err := rows.Scan(
			&nsp, &t.Name, &t.Partitioned, &t.Comment, &t.RowEstimate, &t.HasPrimaryKey,
			&t.Readable,
		); err != nil {
			return fmt.Errorf("leer una tabla: %w", err)
		}
		// Una tabla cuyo esquema no está en el mapa significa que el catálogo
		// cambió entre las dos consultas. Es raro, pero descartarla en silencio
		// es mejor que dejar el snapshot inconsistente.
		if s, ok := esquemas[nsp]; ok {
			s.Tables = append(s.Tables, t)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("recorrer las tablas: %w", err)
	}
	return nil
}

// ordenDeLectura devuelve los nombres del mapa en orden alfabético, para que
// armar el snapshot sea determinístico antes incluso de normalizarlo.
func ordenDeLectura(esquemas map[string]*schema.Schema) []string {
	nombres := make([]string, 0, len(esquemas))
	for n := range esquemas {
		nombres = append(nombres, n)
	}
	sort.Strings(nombres)
	return nombres
}
