package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// ErrSinDefinicion es que Kaname no sabe reconstruir ese tipo de objeto.
//
// Es un error y no una cadena vacía a propósito: una definición vacía en el
// editor se ve igual que un objeto sin cuerpo, y guardarla lo borraría. Los
// objetos que caen acá siguen apareciendo en el árbol y en la cobertura del
// volcado —esconderlos sería el silencio que este proyecto evita— pero se abren
// diciendo qué son y que todavía no se editan.
var ErrSinDefinicion = errors.New("sin definición")

// Definition devuelve la definición de un objeto como un CREATE completo.
//
// Postgres da cada cosa en una forma distinta: `pg_get_functiondef` y
// `pg_get_triggerdef` devuelven el CREATE entero, `pg_get_viewdef` devuelve
// SOLO el SELECT, y de una secuencia o un enum no hay función que los escriba.
// Acá se normaliza todo a un CREATE completo, que es lo único que hace
// verdadera la promesa del editor: lo que se ve es lo que se ejecuta.
//
// El nombre del objeto se cita y se CONCATENA, que es la excepción declarada en
// CLAUDE.md —un identificador no se puede parametrizar— y pasa por la vista
// previa antes de ejecutarse. Lo que se busca en el catálogo, en cambio, viaja
// como parámetro: son valores.
func Definition(ctx context.Context, pool *pgxpool.Pool, o schema.Object) (schema.ObjectDefinition, error) {
	def := schema.ObjectDefinition{Object: o}
	nombre := QualifiedName(o.Schema, o.Name)

	switch o.Kind {
	case schema.ObjView, schema.ObjMatView:
		relkind, palabras := "v", "VIEW"
		if o.Kind == schema.ObjMatView {
			relkind, palabras = "m", "MATERIALIZED VIEW"
		}
		var cuerpo string
		var opciones []string
		err := pool.QueryRow(ctx, `
			SELECT pg_catalog.pg_get_viewdef(c.oid, true),
			       COALESCE(c.reloptions, '{}')
			FROM pg_catalog.pg_class c
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind = $3`,
			o.Schema, o.Name, relkind).Scan(&cuerpo, &opciones)
		if err != nil {
			return def, envolver(err, o)
		}
		// Las OPCIONES no están en `pg_get_viewdef`: esa función devuelve el
		// SELECT y nada más. Viven en `reloptions`, y dejarlas afuera era la
		// peor clase de pérdida que puede tener este código.
		//
		// `security_invoker` y `security_barrier` son propiedades de SEGURIDAD:
		// una vista endurecida que se abre en el editor y se guarda sin tocar
		// volvía siendo una vista permeable, en silencio y con el mismo nombre.
		// Y `check_option` es lo que hace que la vista rechace las escrituras
		// que se saldrían de ella; sin él las acepta y desaparecen de la vista.
		//
		// Se escriben TAL CUAL vienen del catálogo, en el mismo `WITH (…)` que
		// acepta el CREATE. Postgres guarda `check_option=cascaded` como una
		// reloption más, así que traducirla a `WITH CASCADED CHECK OPTION`
		// sería un caso especial que hay que mantener, y esta forma cubre
		// también las que aparezcan mañana sin tocar este código.
		//
		// `pg_get_viewdef` va en modo indentado —el segundo argumento— porque
		// esto se muestra en un editor y se lee: sin él el cuerpo entero sale
		// en un solo renglón. El modo indentado no está pensado para volver a
		// parsearse, y por eso el ciclo completo se prueba contra las cuatro
		// versiones de PostgreSQL que soportamos: si alguna dejara de
		// re-parsearlo, el test lo dice antes que un usuario.
		var conOpciones string
		if len(opciones) > 0 {
			conOpciones = " WITH (" + strings.Join(opciones, ", ") + ")"
		}
		def.SQL = fmt.Sprintf("CREATE %s %s%s AS\n%s", palabras, nombre, conOpciones, cuerpo)

	case schema.ObjFunction, schema.ObjProcedure:
		sql, err := funcion(ctx, pool, o)
		if err != nil {
			return def, err
		}
		def.SQL = sql

	case schema.ObjTrigger:
		sql, err := unTexto(ctx, pool, `
			SELECT pg_catalog.pg_get_triggerdef(t.oid, true)
			FROM pg_catalog.pg_trigger t
			JOIN pg_catalog.pg_class c ON c.oid = t.tgrelid
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND t.tgname = $2 AND c.relname = $3
			  AND NOT t.tgisinternal`,
			o.Schema, o.Name, o.Table)
		if err != nil {
			return def, envolver(err, o)
		}
		def.SQL = sql

	case schema.ObjType:
		return tipo(ctx, pool, def, nombre)

	case schema.ObjSequence:
		return secuencia(ctx, pool, def, nombre)

	default:
		return def, fmt.Errorf("%w: Kaname todavía no sabe leer la definición de %s", ErrSinDefinicion, etiquetaDeTipo(o.Kind))
	}
	return def, nil
}

// funcion busca la definición de una función o un procedimiento por su firma, y
// si la firma no pega y hay una sola candidata, usa esa.
//
// El respaldo no es pereza: `pg_get_function_identity_arguments` escribe los
// tipos SEGÚN EL search_path de la conexión que la corre. La misma función sale
// como `m demo.mood` con el path por defecto y como `m mood` después de un `SET
// search_path TO demo`. Y `Objects` y esta consulta son dos viajes distintos a
// un POOL: pueden caer en conexiones distintas, y basta con que alguien haya
// corrido un `SET search_path` en el editor SQL —que se pega a una sola
// conexión del pool— para que los dos textos difieran y la igualdad no encuentre
// nada. El usuario vería «ya no está en la base» sobre una función que está.
//
// Con una sola candidata la firma no hacía falta para empezar. Con varias
// —sobrecargas de verdad— no se adivina: se dice que no se pudo distinguir.
func funcion(ctx context.Context, pool *pgxpool.Pool, o schema.Object) (string, error) {
	const porFirma = `
		SELECT pg_catalog.pg_get_functiondef(p.oid)
		FROM pg_catalog.pg_proc p
		JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = $1 AND p.proname = $2
		  AND pg_catalog.pg_get_function_identity_arguments(p.oid) = $3`

	sql, err := unTexto(ctx, pool, porFirma, o.Schema, o.Name, o.Args)
	if err == nil {
		return sql, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	const cualquiera = `
		SELECT pg_catalog.pg_get_functiondef(p.oid)
		FROM pg_catalog.pg_proc p
		JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = $1 AND p.proname = $2`

	filas, err := pool.Query(ctx, cualquiera, o.Schema, o.Name)
	if err != nil {
		return "", err
	}
	defer filas.Close()

	var encontradas []string
	for filas.Next() {
		var s string
		if err := filas.Scan(&s); err != nil {
			return "", err
		}
		encontradas = append(encontradas, s)
	}
	if err := filas.Err(); err != nil {
		return "", err
	}
	switch len(encontradas) {
	case 0:
		return "", fmt.Errorf("%s ya no está en la base", o.Completo())
	case 1:
		return encontradas[0], nil
	default:
		return "", fmt.Errorf(
			"hay %d versiones de %s.%s y ninguna coincide con la firma %q: volvé a cargar el esquema",
			len(encontradas), o.Schema, o.Name, o.Args)
	}
}

// tipo escribe un enum. Los dominios y los compuestos se dicen, no se inventan.
func tipo(ctx context.Context, pool *pgxpool.Pool, def schema.ObjectDefinition, nombre string) (schema.ObjectDefinition, error) {
	var clase string
	var valores []string
	err := pool.QueryRow(ctx, `
		SELECT t.typtype,
		       COALESCE(array_agg(e.enumlabel ORDER BY e.enumsortorder)
		                FILTER (WHERE e.enumlabel IS NOT NULL), '{}')
		FROM pg_catalog.pg_type t
		JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
		LEFT JOIN pg_catalog.pg_enum e ON e.enumtypid = t.oid
		WHERE n.nspname = $1 AND t.typname = $2
		GROUP BY t.typtype`,
		def.Object.Schema, def.Object.Name).Scan(&clase, &valores)
	if err != nil {
		return def, envolver(err, def.Object)
	}
	if clase != "e" {
		// Un dominio o un compuesto tiene definición, pero no ESTA. Decir cuál
		// es vale más que devolver un CREATE TYPE … AS ENUM vacío, que sería
		// una mentira ejecutable.
		que := "un tipo compuesto"
		if clase == "d" {
			que = "un dominio"
		}
		return def, fmt.Errorf("%w: %s es %s, y Kaname todavía solo escribe enums",
			ErrSinDefinicion, def.Object.Completo(), que)
	}

	def.Values = valores
	citados := make([]string, 0, len(valores))
	for _, v := range valores {
		citados = append(citados, dialectoDML.QuoteLiteral(v))
	}
	def.SQL = fmt.Sprintf("CREATE TYPE %s AS ENUM (%s)", nombre, strings.Join(citados, ", "))
	return def, nil
}

// secuencia escribe un CREATE SEQUENCE con lo que el catálogo guarda.
//
// `last_value` NO va: es dónde está parada la secuencia ahora, no cómo se
// define. Meterlo en el CREATE haría que dos volcados de la misma base a dos
// horas distintas dieran archivos distintos sin que nada haya cambiado.
func secuencia(ctx context.Context, pool *pgxpool.Pool, def schema.ObjectDefinition, nombre string) (schema.ObjectDefinition, error) {
	var tipoDato string
	var arranca, minimo, maximo, paso int64
	var ciclo bool
	var cache int64
	err := pool.QueryRow(ctx, `
		SELECT data_type::text, start_value, min_value, max_value,
		       increment_by, cycle, cache_size
		FROM pg_catalog.pg_sequences
		WHERE schemaname = $1 AND sequencename = $2`,
		def.Object.Schema, def.Object.Name).Scan(
		&tipoDato, &arranca, &minimo, &maximo, &paso, &ciclo, &cache)
	if err != nil {
		return def, envolver(err, def.Object)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "CREATE SEQUENCE %s\n    AS %s\n    START WITH %d\n    INCREMENT BY %d",
		nombre, tipoDato, arranca, paso)
	fmt.Fprintf(&b, "\n    MINVALUE %d\n    MAXVALUE %d\n    CACHE %d", minimo, maximo, cache)
	if ciclo {
		b.WriteString("\n    CYCLE")
	}
	def.SQL = b.String()
	return def, nil
}

// unTexto corre una consulta que devuelve una sola columna de texto.
func unTexto(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) (string, error) {
	var out string
	if err := pool.QueryRow(ctx, sql, args...).Scan(&out); err != nil {
		return "", err
	}
	return out, nil
}

// envolver convierte «no hay filas» en un error que dice QUÉ no se encontró.
//
// Un `no rows in result set` pelado en la pantalla no le dice a nadie que la
// vista que tenía abierta la borró otra sesión mientras tanto.
func envolver(err error, o schema.Object) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s ya no está en la base", o.Completo())
	}
	return err
}

// etiquetaDeTipo nombra una clase de objeto en castellano, para un mensaje.
func etiquetaDeTipo(k schema.ObjectKind) string {
	switch k {
	case schema.ObjPolicy:
		return "una política de RLS"
	case schema.ObjExtension:
		return "una extensión"
	case schema.ObjEvent:
		return "un evento"
	case schema.ObjColumn:
		return "una columna"
	}
	return string(k)
}
