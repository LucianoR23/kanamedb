package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// ErrTablaNoExiste es lo que devuelve Detail cuando la tabla no está en el
// catálogo: o nunca existió, o alguien la borró mientras se la miraba.
var ErrTablaNoExiste = errors.New("la tabla no existe")

// oidDeTabla resuelve un par (esquema, tabla) al OID de pg_class.
//
// Se repite como subselect en cada consulta del detalle en vez de resolverse una
// vez y pasarse como parámetro porque las seis consultas viajan en un solo lote:
// no hay un "después" en el que usar el resultado de la primera. El costo es una
// búsqueda por índice único del catálogo, seis veces.
const oidDeTabla = `SELECT c.oid
	    FROM pg_catalog.pg_class c
	    JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	    WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p')`

// cabeceraQuery trae lo que va en el encabezado de la pantalla de estructura.
//
// El CASE por relkind no es paranoia: pg_total_relation_size NO recorre el árbol
// de particiones, así que una tabla particionada de 400 GB mide cero. Mostrar
// "0 bytes" para una tabla enorme es peor que no mostrar el tamaño.
//
// reltuples se lee igual que en el snapshot, sin sumar el árbol, para que el
// árbol y el encabezado no muestren dos números distintos de la misma tabla.
const cabeceraQuery = `
	SELECT coalesce(obj_description(c.oid, 'pg_class'), ''),
	       c.reltuples::bigint,
	       CASE WHEN c.relkind = 'p' THEN (
	              SELECT coalesce(sum(pg_catalog.pg_total_relation_size(pt.relid)), 0)
	              FROM pg_catalog.pg_partition_tree(c.oid) pt)
	            ELSE pg_catalog.pg_total_relation_size(c.oid) END,
	       CASE WHEN c.relkind = 'p' THEN (
	              SELECT coalesce(sum(pg_catalog.pg_table_size(pt.relid)), 0)
	              FROM pg_catalog.pg_partition_tree(c.oid) pt)
	            ELSE pg_catalog.pg_table_size(c.oid) END
	FROM pg_catalog.pg_class c
	JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
	WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p')`

// detalleColumnasQuery trae las columnas con todo lo que muestra la pantalla.
//
// attgenerated distingue 's' (stored) de 'v' (virtual, PostgreSQL 18). La
// diferencia importa más allá de lo que se ve: es una de las tres cosas que
// Atlas no sabe leer, y marcarla acá es lo que le va a permitir a la Iteración 5
// negarse a generar DDL. Ver kaname-plan.md § 6.
//
// El LATERAL de nn busca un NOT NULL sin validar, que en PostgreSQL 18 es una
// restricción con contype 'n' y convalidated en false. En versiones anteriores
// 'n' no es un contype válido y el LATERAL simplemente no encuentra nada, así
// que la misma consulta sirve para las cuatro versiones sin ramas.
const detalleColumnasQuery = `
	SELECT a.attname,
	       pg_catalog.format_type(a.atttypid, a.atttypmod),
	       NOT a.attnotnull,
	       a.attnum,
	       coalesce(pg_catalog.pg_get_expr(d.adbin, d.adrelid), ''),
	       coalesce(col_description(a.attrelid, a.attnum), ''),
	       coalesce(pk.si, false),
	       coalesce(fk.si, false),
	       CASE a.attgenerated WHEN 's' THEN 'stored' WHEN 'v' THEN 'virtual' ELSE '' END,
	       CASE a.attidentity WHEN 'a' THEN 'always' WHEN 'd' THEN 'by default' ELSE '' END,
	       coalesce(nn.si, false)
	FROM pg_catalog.pg_attribute a
	LEFT JOIN pg_catalog.pg_attrdef d
	       ON d.adrelid = a.attrelid AND d.adnum = a.attnum
	LEFT JOIN LATERAL (
	    SELECT true AS si FROM pg_catalog.pg_index i
	    WHERE i.indrelid = a.attrelid AND i.indisprimary
	      AND a.attnum = ANY((i.indkey::int2[])[0:i.indnkeyatts - 1])
	    LIMIT 1
	) pk ON true
	LEFT JOIN LATERAL (
	    SELECT true AS si FROM pg_catalog.pg_constraint k
	    WHERE k.conrelid = a.attrelid AND k.contype = 'f' AND a.attnum = ANY(k.conkey)
	    LIMIT 1
	) fk ON true
	LEFT JOIN LATERAL (
	    SELECT true AS si FROM pg_catalog.pg_constraint k
	    WHERE k.conrelid = a.attrelid AND k.contype = 'n'
	      AND NOT k.convalidated AND a.attnum = ANY(k.conkey)
	    LIMIT 1
	) nn ON true
	WHERE a.attrelid = (` + oidDeTabla + `)
	  AND a.attnum > 0
	  AND NOT a.attisdropped
	ORDER BY a.attnum`

// ordenDeColumnaDeIndice arma el " DESC" y el " NULLS ..." de una columna clave.
//
// pg_get_indexdef con un número de columna devuelve SOLO la expresión: nunca el
// orden ni la posición de los nulos. (Se comprobó contra el catálogo; la
// suposición contraria costó un test rojo.) El resto está en indoption, un
// int2vector con un bit de DESC y otro de NULLS FIRST por columna clave.
//
// indoption se indexa desde CERO, no desde uno como el resto de los arrays de
// Postgres, de ahí el k-1.
//
// Los NULLS solo se escriben cuando contradicen el default —NULLS LAST para
// ascendente, NULLS FIRST para descendente— para no llenar la columna de ruido
// que no dice nada.
const ordenDeColumnaDeIndice = `
	CASE WHEN g.k > i.indnkeyatts THEN ''
	     WHEN (i.indoption[g.k - 1] & 1) <> 0 THEN ' DESC'
	     ELSE '' END
	|| CASE WHEN g.k > i.indnkeyatts THEN ''
	        WHEN (i.indoption[g.k - 1] & 1) = 0 AND (i.indoption[g.k - 1] & 2) <> 0
	          THEN ' NULLS FIRST'
	        WHEN (i.indoption[g.k - 1] & 1) <> 0 AND (i.indoption[g.k - 1] & 2) = 0
	          THEN ' NULLS LAST'
	        ELSE '' END`

// detalleIndicesQuery trae los índices con sus columnas tal como se declararon.
//
// Las expresiones salen de pg_get_indexdef y no de pg_attribute: así un índice
// funcional muestra "lower(email)" y no una columna que no existe en la tabla.
//
// Las claves y las columnas de INCLUDE se traen por separado. Mezclarlas haría
// ver una columna incluida como si fuera clave, que es lo que lleva a creer que
// un índice sirve para un WHERE que no cubre.
//
// El tamaño tiene el mismo problema que el de la tabla: el índice de una tabla
// particionada tiene relkind 'I' y no guarda nada él mismo, así que
// pg_relation_size devuelve cero y hay que sumar el árbol. Un índice de 400 MB
// que dice pesar 0 B es peor que no mostrar el tamaño.
const detalleIndicesQuery = `
	SELECT ic.relname,
	       am.amname,
	       i.indisunique,
	       i.indisprimary,
	       (SELECT array_agg(pg_catalog.pg_get_indexdef(i.indexrelid, g.k, true)
	                         || ` + ordenDeColumnaDeIndice + ` ORDER BY g.k)
	          FROM generate_series(1, i.indnkeyatts) AS g(k)),
	       coalesce((SELECT array_agg(pg_catalog.pg_get_indexdef(i.indexrelid, g.k, true) ORDER BY g.k)
	          FROM generate_series(i.indnkeyatts + 1, i.indnatts) AS g(k)), '{}'::text[]),
	       coalesce(pg_catalog.pg_get_expr(i.indpred, i.indrelid), ''),
	       CASE WHEN ic.relkind = 'I' THEN (
	              SELECT coalesce(sum(pg_catalog.pg_relation_size(pt.relid)), 0)
	              FROM pg_catalog.pg_partition_tree(i.indexrelid) pt)
	            ELSE pg_catalog.pg_relation_size(i.indexrelid) END,
	       coalesce(st.idx_scan, -1),
	       i.indisvalid,
	       pg_catalog.pg_get_indexdef(i.indexrelid)
	FROM pg_catalog.pg_index i
	JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid
	JOIN pg_catalog.pg_am am ON am.oid = ic.relam
	LEFT JOIN pg_catalog.pg_stat_user_indexes st ON st.indexrelid = i.indexrelid
	WHERE i.indrelid = (` + oidDeTabla + `)
	ORDER BY i.indisprimary DESC, ic.relname`

// detalleChecksQuery trae las restricciones CHECK.
//
// convalidated distingue una restricción que garantiza lo que dice de una que se
// agregó con NOT VALID y nunca miró las filas que ya estaban.
const detalleChecksQuery = `
	SELECT k.conname,
	       pg_catalog.pg_get_constraintdef(k.oid, true),
	       k.convalidated
	FROM pg_catalog.pg_constraint k
	WHERE k.conrelid = (` + oidDeTabla + `)
	  AND k.contype = 'c'
	ORDER BY k.conname`

// detalleTriggersQuery trae los triggers de usuario.
//
// tgisinternal excluye los que Postgres crea solo para implementar claves
// foráneas y restricciones diferibles: no los escribió nadie y llenarían la
// lista de ruido que no se puede editar.
//
// La función se califica con su esquema salvo que esté en public, que es donde
// está casi siempre y donde el prefijo solo agrega ancho.
const detalleTriggersQuery = `
	SELECT t.tgname,
	       t.tgtype::int,
	       t.tgenabled <> 'D',
	       CASE WHEN pn.nspname = 'public' THEN p.proname
	            ELSE pn.nspname || '.' || p.proname END || '()',
	       pg_catalog.pg_get_triggerdef(t.oid, true)
	FROM pg_catalog.pg_trigger t
	JOIN pg_catalog.pg_proc p ON p.oid = t.tgfoid
	JOIN pg_catalog.pg_namespace pn ON pn.oid = p.pronamespace
	WHERE t.tgrelid = (` + oidDeTabla + `)
	  AND NOT t.tgisinternal
	ORDER BY t.tgname`

// Detail lee todo lo que el catálogo sabe de una tabla.
//
// Las siete consultas viajan en un solo lote. No es microoptimización: con un
// túnel SSH de por medio cada viaje cuesta la latencia completa hasta el
// bastión, y seis viajes secuenciales contra un servidor a 50 ms son trescientos
// milisegundos de nada antes de que aparezca la primera fila. El lote los cobra
// una vez.
//
// El precio es que los resultados hay que consumirlos en el mismo orden en que
// se encolaron, y que un error en cualquiera de ellos aparece al leerlo, no al
// mandarlo.
func Detail(ctx context.Context, pool *pgxpool.Pool, esquema, tabla string) (*schema.TableDetail, error) {
	lote := &pgx.Batch{}
	lote.Queue(cabeceraQuery, esquema, tabla)
	lote.Queue(detalleColumnasQuery, esquema, tabla)
	lote.Queue(detalleIndicesQuery, esquema, tabla)
	lote.Queue(fkSalientesQuery, esquema, tabla)
	lote.Queue(fkEntrantesQuery, esquema, tabla)
	lote.Queue(detalleChecksQuery, esquema, tabla)
	lote.Queue(detalleTriggersQuery, esquema, tabla)

	res := pool.SendBatch(ctx, lote)
	defer res.Close()

	d := &schema.TableDetail{Schema: esquema, Name: tabla, CapturedAt: time.Now()}

	if err := res.QueryRow().Scan(
		&d.Comment, &d.RowEstimate, &d.TotalBytes, &d.TableBytes,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%q.%q: %w", esquema, tabla, ErrTablaNoExiste)
		}
		return nil, fmt.Errorf("leer la tabla: %w", err)
	}

	var err error
	if d.Columns, err = leerColumnasDetalle(res); err != nil {
		return nil, err
	}
	if d.Indexes, err = leerIndices(res); err != nil {
		return nil, err
	}
	if d.ForeignKeys, err = consumirFKs(res, "salientes"); err != nil {
		return nil, err
	}
	if d.ReferencedBy, err = consumirFKs(res, "entrantes"); err != nil {
		return nil, err
	}
	if d.Checks, err = leerChecks(res); err != nil {
		return nil, err
	}
	if d.Triggers, err = leerTriggers(res); err != nil {
		return nil, err
	}
	return d, nil
}

func leerColumnasDetalle(res pgx.BatchResults) ([]schema.DetailColumn, error) {
	rows, err := res.Query()
	if err != nil {
		return nil, fmt.Errorf("listar las columnas: %w", err)
	}
	defer rows.Close()

	var out []schema.DetailColumn
	for rows.Next() {
		var c schema.DetailColumn
		if err := rows.Scan(
			&c.Name, &c.DataType, &c.Nullable, &c.Position, &c.Default, &c.Comment,
			&c.PrimaryKey, &c.ForeignKey, &c.Generated, &c.Identity, &c.NotNullNotValid,
		); err != nil {
			return nil, fmt.Errorf("leer una columna: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer las columnas: %w", err)
	}
	return out, nil
}

func leerIndices(res pgx.BatchResults) ([]schema.Index, error) {
	rows, err := res.Query()
	if err != nil {
		return nil, fmt.Errorf("listar los índices: %w", err)
	}
	defer rows.Close()

	var out []schema.Index
	for rows.Next() {
		var i schema.Index
		if err := rows.Scan(
			&i.Name, &i.Method, &i.Unique, &i.Primary, &i.Columns, &i.Included,
			&i.Predicate, &i.SizeBytes, &i.Scans, &i.Valid, &i.Definition,
		); err != nil {
			return nil, fmt.Errorf("leer un índice: %w", err)
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer los índices: %w", err)
	}
	return out, nil
}

func consumirFKs(res pgx.BatchResults, cual string) ([]schema.ForeignKey, error) {
	rows, err := res.Query()
	if err != nil {
		return nil, fmt.Errorf("listar las claves foráneas %s: %w", cual, err)
	}
	return leerFKs(rows)
}

func leerChecks(res pgx.BatchResults) ([]schema.CheckConstraint, error) {
	rows, err := res.Query()
	if err != nil {
		return nil, fmt.Errorf("listar las restricciones: %w", err)
	}
	defer rows.Close()

	var out []schema.CheckConstraint
	for rows.Next() {
		var c schema.CheckConstraint
		var def string
		if err := rows.Scan(&c.Name, &def, &c.Validated); err != nil {
			return nil, fmt.Errorf("leer una restricción: %w", err)
		}
		c.Expression = expresionDeCheck(def)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer las restricciones: %w", err)
	}
	return out, nil
}

func leerTriggers(res pgx.BatchResults) ([]schema.Trigger, error) {
	rows, err := res.Query()
	if err != nil {
		return nil, fmt.Errorf("listar los triggers: %w", err)
	}
	defer rows.Close()

	var out []schema.Trigger
	for rows.Next() {
		var t schema.Trigger
		var tipo int32
		if err := rows.Scan(&t.Name, &tipo, &t.Enabled, &t.Function, &t.Definition); err != nil {
			return nil, fmt.Errorf("leer un trigger: %w", err)
		}
		t.Timing, t.Level, t.Events = descomponerTgtype(tipo)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recorrer los triggers: %w", err)
	}
	return out, nil
}

// Bits de pg_trigger.tgtype, tal como los define src/include/catalog/pg_trigger.h.
// No hay una vista del catálogo que los desarme, así que o se leen los bits o se
// parsea el texto de pg_get_triggerdef; los bits no dependen del formato.
const (
	tgFila     = 1 << 0
	tgAntes    = 1 << 1
	tgInsert   = 1 << 2
	tgDelete   = 1 << 3
	tgUpdate   = 1 << 4
	tgTruncate = 1 << 5
	tgEnLugar  = 1 << 6
)

// descomponerTgtype traduce la máscara de bits a los tres campos que muestra la
// pantalla.
func descomponerTgtype(tipo int32) (timing, nivel string, eventos []string) {
	switch {
	case tipo&tgEnLugar != 0:
		timing = "instead of"
	case tipo&tgAntes != 0:
		timing = "before"
	default:
		timing = "after"
	}

	nivel = "statement"
	if tipo&tgFila != 0 {
		nivel = "row"
	}

	// El orden es el de la declaración de Postgres, no alfabético: es el que
	// espera ver quien conoce la sintaxis de CREATE TRIGGER.
	for _, e := range []struct {
		bit    int32
		nombre string
	}{
		{tgInsert, "insert"},
		{tgUpdate, "update"},
		{tgDelete, "delete"},
		{tgTruncate, "truncate"},
	} {
		if tipo&e.bit != 0 {
			eventos = append(eventos, e.nombre)
		}
	}
	return timing, nivel, eventos
}

// expresionDeCheck saca el envoltorio de pg_get_constraintdef y deja solo la
// expresión.
//
// El catálogo devuelve `CHECK ((total >= 0))` o
// `CHECK ((a IS NULL) OR (b >= c)) NOT VALID`. Lo que la pantalla muestra es la
// expresión: el "CHECK" ya está en el encabezado de la columna y el "NOT VALID"
// tiene su propia columna.
func expresionDeCheck(def string) string {
	s := strings.TrimSpace(def)
	s = strings.TrimSuffix(s, " NOT VALID")
	s = strings.TrimSpace(s)
	if resto, ok := strings.CutPrefix(s, "CHECK ("); ok && strings.HasSuffix(resto, ")") {
		s = strings.TrimSpace(resto[:len(resto)-1])
	}
	// Postgres envuelve la expresión entera una vez más. Se saca solo si el
	// paréntesis inicial es el que cierra al final: en `(a) OR (b)` el primero
	// cierra en el medio, y sacarlos dejaría `a) OR (b`.
	return sinParentesisExternos(s)
}

func sinParentesisExternos(s string) string {
	if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
		return s
	}
	nivel := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			nivel++
		case ')':
			nivel--
			if nivel == 0 && i != len(s)-1 {
				return s
			}
		}
	}
	if nivel != 0 {
		return s
	}
	return strings.TrimSpace(s[1 : len(s)-1])
}
