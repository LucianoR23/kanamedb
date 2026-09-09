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
	if err := readColumns(ctx, pool, esquemas); err != nil {
		return nil, err
	}
	if err := readForeignKeys(ctx, pool, esquemas); err != nil {
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
		// Readable se escanea como puntero porque has_table_privilege devuelve
		// NULL, no false, cuando el OID ya no existe.
		//
		// Y eso pasa de verdad: entre que pg_class lista la tabla y que se
		// evalúa el permiso, otra sesión puede haberla borrado. Escanear a bool
		// hacía fallar la lectura del esquema entera con "cannot scan NULL into
		// *bool" — un mensaje que no se parece en nada a "alguien borró una
		// tabla". Lo encontraron los tests de integración corriendo en paralelo
		// contra la misma base.
		var legible *bool
		if err := rows.Scan(
			&nsp, &t.Name, &t.Partitioned, &t.Comment, &t.RowEstimate, &t.HasPrimaryKey,
			&legible,
		); err != nil {
			return fmt.Errorf("leer una tabla: %w", err)
		}
		if legible == nil {
			// La tabla dejó de existir mientras leíamos. Se omite en vez de
			// mostrarla como no legible: un fantasma en el árbol es peor que
			// una tabla de menos, porque al hacerle clic da un error que no
			// explica nada.
			continue
		}
		t.Readable = *legible
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

// columnsQuery trae las columnas de todas las tablas visibles de una vez.
//
// Es una consulta más y no una por tabla: con doscientas tablas, una por tabla
// serían doscientos viajes para armar el autocompletado. Los LATERAL resuelven
// PK y FK sin multiplicar filas, que es lo que pasaría con JOINs directos
// contra pg_index y pg_constraint cuando una columna está en varias claves.
//
// El recorte de indkey a indnkeyatts es lo que distingue una columna clave de
// una de INCLUDE: `indkey` las trae a las dos, así que preguntar por el vector
// entero marca como clave primaria a una columna que solo viaja adentro del
// índice. Y se recorta DESDE CERO: indkey es un int2vector, que a diferencia de
// todos los demás arrays de Postgres empieza en 0, y el casteo conserva ese
// límite inferior.
const columnsQuery = `
	SELECT n.nspname,
	       c.relname,
	       a.attname,
	       pg_catalog.format_type(a.atttypid, a.atttypmod),
	       NOT a.attnotnull,
	       a.atthasdef,
	       a.attnum,
	       coalesce(pk.si, false),
	       coalesce(fk.si, false)
	FROM pg_catalog.pg_attribute a
	JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	LEFT JOIN LATERAL (
	    SELECT true AS si
	    FROM pg_catalog.pg_index i
	    WHERE i.indrelid = c.oid AND i.indisprimary
	      AND a.attnum = ANY((i.indkey::int2[])[0:i.indnkeyatts - 1])
	    LIMIT 1
	) pk ON true
	LEFT JOIN LATERAL (
	    SELECT true AS si
	    FROM pg_catalog.pg_constraint k
	    WHERE k.conrelid = c.oid AND k.contype = 'f' AND a.attnum = ANY(k.conkey)
	    LIMIT 1
	) fk ON true
	WHERE a.attnum > 0
	  AND NOT a.attisdropped
	  AND c.relkind IN ('r', 'p')
	  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
	  AND n.nspname NOT LIKE 'pg\_toast%'
	  AND n.nspname NOT LIKE 'pg\_temp%'
	  AND pg_catalog.has_schema_privilege(n.oid, 'USAGE')
	ORDER BY n.nspname, c.relname, a.attnum`

func readColumns(ctx context.Context, pool *pgxpool.Pool, esquemas map[string]*schema.Schema) error {
	rows, err := pool.Query(ctx, columnsQuery)
	if err != nil {
		return fmt.Errorf("listar las columnas: %w", err)
	}
	defer rows.Close()

	porTabla := indicePorTabla(esquemas)

	for rows.Next() {
		var nsp, tabla string
		var col schema.Column
		if err := rows.Scan(&nsp, &tabla, &col.Name, &col.DataType,
			&col.Nullable, &col.HasDefault, &col.Position,
			&col.PrimaryKey, &col.ForeignKey); err != nil {
			return fmt.Errorf("leer una columna: %w", err)
		}
		// Las particiones no están en el mapa —el árbol no las muestra— así que
		// sus columnas se descartan acá sin ruido.
		if t, ok := porTabla[nsp+"."+tabla]; ok {
			t.Columns = append(t.Columns, col)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("recorrer las columnas: %w", err)
	}
	return nil
}

// indicePorTabla arma un índice "esquema.tabla" → tabla para no recorrer el
// slice en cada fila leída.
//
// Con doscientas tablas por veinte columnas, buscar linealmente serían
// ochocientas mil comparaciones para armar algo que ya viene ordenado. Lo usan
// las columnas y las claves foráneas, que llegan planas y hay que repartir.
//
// Los punteros apuntan dentro de los slices de `esquemas`, así que el mapa vale
// solo mientras nadie agregue tablas: un append reasignaría el array y los
// punteros quedarían apuntando al viejo.
func indicePorTabla(esquemas map[string]*schema.Schema) map[string]*schema.Table {
	porTabla := map[string]*schema.Table{}
	for _, esq := range esquemas {
		for i := range esq.Tables {
			porTabla[esq.Name+"."+esq.Tables[i].Name] = &esq.Tables[i]
		}
	}
	return porTabla
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
