package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// fkQuery lee claves foráneas. Las tres consultas que las necesitan —el
// snapshot entero, las salientes de una tabla y las entrantes— comparten esta
// base y solo agregan su filtro.
//
// Decisiones que valen la pena:
//
//   - Los subselects con WITH ORDINALITY conservan el orden de la restricción.
//     Sin él, `array_agg` sobre `unnest` no garantiza ninguno, y en una clave
//     compuesta la columna i de conkey dejaría de emparejar con la columna i de
//     confkey: la UI mostraría las columnas cruzadas sin ningún síntoma.
//
//   - Los códigos de una letra de confdeltype y confupdtype se traducen en SQL
//     y no en Go. Son un mapeo fijo del catálogo, no lógica de la aplicación, y
//     así pgx no tiene que escanear el tipo "char" de Postgres, que no es text
//     y no se mapea solo a un string.
//
//   - `Optional` sale de si ALGUNA columna local acepta nulos. Con MATCH SIMPLE
//     —el default— una sola columna en NULL alcanza para que Postgres no exija
//     la fila padre, así que basta una para que el lado sea opcional.
const fkQuery = `
	SELECT n.nspname,
	       c.relname,
	       k.conname,
	       (SELECT array_agg(a.attname ORDER BY u.ord)
	          FROM unnest(k.conkey) WITH ORDINALITY AS u(num, ord)
	          JOIN pg_catalog.pg_attribute a
	            ON a.attrelid = k.conrelid AND a.attnum = u.num),
	       rn.nspname,
	       rc.relname,
	       (SELECT array_agg(a.attname ORDER BY u.ord)
	          FROM unnest(k.confkey) WITH ORDINALITY AS u(num, ord)
	          JOIN pg_catalog.pg_attribute a
	            ON a.attrelid = k.confrelid AND a.attnum = u.num),
	       CASE k.confdeltype
	         WHEN 'r' THEN 'restrict' WHEN 'c' THEN 'cascade'
	         WHEN 'n' THEN 'set null' WHEN 'd' THEN 'set default'
	         ELSE 'no action' END,
	       CASE k.confupdtype
	         WHEN 'r' THEN 'restrict' WHEN 'c' THEN 'cascade'
	         WHEN 'n' THEN 'set null' WHEN 'd' THEN 'set default'
	         ELSE 'no action' END,
	       CASE WHEN NOT k.condeferrable THEN ''
	            WHEN k.condeferred THEN 'initially deferred'
	            ELSE 'deferrable' END,
	       EXISTS (
	         SELECT 1 FROM unnest(k.conkey) AS ck(num)
	         JOIN pg_catalog.pg_attribute a
	           ON a.attrelid = k.conrelid AND a.attnum = ck.num
	         WHERE NOT a.attnotnull
	       )
	FROM pg_catalog.pg_constraint k
	JOIN pg_catalog.pg_class c ON c.oid = k.conrelid
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	JOIN pg_catalog.pg_class rc ON rc.oid = k.confrelid
	JOIN pg_catalog.pg_namespace rn ON rn.oid = rc.relnamespace
	WHERE k.contype = 'f'`

// foreignKeysQuery trae las claves de todas las tablas visibles de una vez.
//
// Es una consulta y no una por tabla porque son las aristas del ERD y el
// diagrama las dibuja todas juntas: pedirlas tabla por tabla serían tantos
// viajes como tablas antes de mostrar la primera línea.
//
// Se excluyen las particiones hijas con el mismo criterio que tablesQuery: cada
// partición hereda las claves del padre y las duplicaría en el diagrama.
const foreignKeysQuery = fkQuery + `
	  AND c.relkind IN ('r', 'p')
	  AND NOT c.relispartition
	  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
	  AND n.nspname NOT LIKE 'pg\_toast%'
	  AND n.nspname NOT LIKE 'pg\_temp%'
	  AND pg_catalog.has_schema_privilege(n.oid, 'USAGE')
	ORDER BY n.nspname, c.relname, k.conname`

// fkSalientesQuery son las claves que declara una tabla.
const fkSalientesQuery = fkQuery + `
	  AND k.conrelid = (` + oidDeTabla + `)
	ORDER BY k.conname`

// fkEntrantesQuery son las claves de OTRAS tablas que apuntan a esta.
//
// Hacen falta para responder qué pasa si se borra una fila: un ON DELETE
// CASCADE que borra en otro lado no se ve mirando solo las claves propias.
//
// El filtro de particiones NO es copia y pega del snapshot: acá es donde más se
// nota. Cada partición hereda la clave del padre con el mismo nombre, así que
// una tabla referenciada por otra particionada en cincuenta meses aparecería con
// cincuenta y una claves entrantes idénticas.
const fkEntrantesQuery = fkQuery + `
	  AND k.confrelid = (` + oidDeTabla + `)
	  AND c.relkind IN ('r', 'p')
	  AND NOT c.relispartition
	ORDER BY n.nspname, c.relname, k.conname`

// escanearFK lee una fila de fkQuery.
func escanearFK(rows pgx.Rows) (fk schema.ForeignKey, err error) {
	var onDelete, onUpdate string
	err = rows.Scan(
		&fk.Schema, &fk.Table, &fk.Name, &fk.Columns,
		&fk.RefSchema, &fk.RefTable, &fk.RefColumns,
		&onDelete, &onUpdate, &fk.Deferrable, &fk.Optional,
	)
	fk.OnDelete = schema.ReferenceAction(onDelete)
	fk.OnUpdate = schema.ReferenceAction(onUpdate)
	return fk, err
}

// leerFKs consume un cursor de fkQuery y devuelve las claves en orden.
func leerFKs(rows pgx.Rows) ([]schema.ForeignKey, error) {
	defer rows.Close()
	var out []schema.ForeignKey
	for rows.Next() {
		fk, err := escanearFK(rows)
		if err != nil {
			return nil, fmt.Errorf("leer una clave foránea: %w", err)
		}
		out = append(out, fk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer las claves foráneas: %w", err)
	}
	return out, nil
}

// readForeignKeys carga las claves foráneas de todas las tablas del snapshot.
func readForeignKeys(ctx context.Context, pool *pgxpool.Pool, esquemas map[string]*schema.Schema) error {
	rows, err := pool.Query(ctx, foreignKeysQuery)
	if err != nil {
		return fmt.Errorf("listar las claves foráneas: %w", err)
	}
	defer rows.Close()

	porTabla := indicePorTabla(esquemas)
	for rows.Next() {
		fk, err := escanearFK(rows)
		if err != nil {
			return fmt.Errorf("leer una clave foránea: %w", err)
		}
		// Una clave de una tabla que no está en el índice significa que el
		// catálogo cambió entre las dos consultas, o que la tabla es una
		// partición que el árbol no muestra. Se descarta sin ruido: una arista
		// que sale de un nodo inexistente es peor que una arista de menos.
		if t, ok := porTabla[fk.Schema+"."+fk.Table]; ok {
			t.ForeignKeys = append(t.ForeignKeys, fk)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("recorrer las claves foráneas: %w", err)
	}
	return nil
}
