package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/LucianoR23/kanamedb/internal/query"
)

func correr(t *testing.T, pool *pgxpool.Pool, sql string, opts RunOptions) *query.Result {
	t.Helper()
	res, f := Run(context.Background(), pool, sql, opts)
	if f != nil {
		t.Fatalf("Run(%q) falló: %s · %s", sql, f.Message, f.Detail)
	}
	return res
}

// La invariante central de la grilla: en una base, NULL y ” son cosas
// distintas, y mostrarlas igual es mentir sobre los datos. Un `len(b) == 0`
// en el lector las colapsaría y ningún test de "trae filas" lo notaría.
func TestRunDistingueNullDeCadenaVacia(t *testing.T) {
	pool, _ := conectar(t)

	res := correr(t, pool, `select null::text as nulo, ''::text as vacio`, RunOptions{})

	if len(res.Rows) != 1 {
		t.Fatalf("se esperaba 1 fila, vinieron %d", len(res.Rows))
	}
	fila := res.Rows[0]
	if fila[0] != nil {
		t.Errorf("NULL llegó como %q y tiene que llegar como nil", *fila[0])
	}
	if fila[1] == nil {
		t.Fatal("la cadena vacía llegó como nil: es indistinguible de NULL")
	}
	if *fila[1] != "" {
		t.Errorf("la cadena vacía llegó como %q", *fila[1])
	}
}

// Cada fila tiene que traer su valor, en orden. Un test que solo cuente filas
// pasaría igual si todas trajeran lo mismo.
//
// Nace de querer proteger la copia de RawValues, y no la protege: aliaseando el
// buffer con unsafe.String el resultado sale correcto igual. Queda porque
// verifica orden y contenido, que es más de lo que verifica contar. La copia se
// hace por el contrato de la API, y eso ningún test lo puede demostrar acá.
func TestRunDevuelveCadaFilaConSuValorYEnOrden(t *testing.T) {
	pool, _ := conectar(t)

	res := correr(t, pool, `select i from generate_series(1, 5) as i`, RunOptions{})

	if len(res.Rows) != 5 {
		t.Fatalf("se esperaban 5 filas, vinieron %d", len(res.Rows))
	}
	for i, fila := range res.Rows {
		quiere := fmt.Sprint(i + 1)
		if fila[0] == nil {
			t.Fatalf("fila %d llegó nula", i)
		}
		if *fila[0] != quiere {
			t.Errorf("fila %d = %q, se esperaba %q", i, *fila[0], quiere)
		}
	}
}

// El corte tiene que avisar cuando corta, y no avisar cuando no corta. El caso
// del borde —exactamente tantas filas como el límite— es el que se rompe solo.
func TestRunAvisaCuandoTruncaYNoCuandoJusto(t *testing.T) {
	pool, _ := conectar(t)
	const total = 10

	casos := []struct {
		limite      int
		quiereFilas int
		quiereCorte bool
	}{
		{limite: 3, quiereFilas: 3, quiereCorte: true},
		{limite: total - 1, quiereFilas: total - 1, quiereCorte: true},
		{limite: total, quiereFilas: total, quiereCorte: false},
		{limite: total + 1, quiereFilas: total, quiereCorte: false},
		{limite: -1, quiereFilas: total, quiereCorte: false},
	}
	for _, c := range casos {
		t.Run(fmt.Sprintf("limite=%d", c.limite), func(t *testing.T) {
			res := correr(t, pool,
				fmt.Sprintf(`select i from generate_series(1, %d) as i`, total),
				RunOptions{RowLimit: c.limite})

			if len(res.Rows) != c.quiereFilas {
				t.Errorf("filas = %d, se esperaban %d", len(res.Rows), c.quiereFilas)
			}
			if res.Truncated != c.quiereCorte {
				t.Errorf("Truncated = %v, se esperaba %v", res.Truncated, c.quiereCorte)
			}
		})
	}
}

// Un UPDATE sin RETURNING no tiene resultado que mostrar. Si eso llega como
// "cero filas", la interfaz dibuja una grilla vacía y quien mira concluye que
// no se modificó nada.
func TestRunSeparaSinResultadoDeResultadoVacio(t *testing.T) {
	pool, esquema := conectar(t)
	ejecutar(t, pool, fmt.Sprintf(`create table %s.t (id int)`, esquema))

	ins := correr(t, pool, fmt.Sprintf(`insert into %s.t values (1), (2), (3)`, esquema), RunOptions{})
	if ins.ReturnsRows {
		t.Error("un INSERT sin RETURNING dice que devuelve filas")
	}
	if ins.AffectedRows != 3 {
		t.Errorf("AffectedRows = %d, se esperaban 3", ins.AffectedRows)
	}
	if ins.Command == "" {
		t.Error("el tag del motor llegó vacío")
	}

	vacio := correr(t, pool, fmt.Sprintf(`select id from %s.t where id = 999`, esquema), RunOptions{})
	if !vacio.ReturnsRows {
		t.Error("un SELECT sin coincidencias dice que no devuelve filas")
	}
	if len(vacio.Rows) != 0 {
		t.Errorf("se esperaban 0 filas, vinieron %d", len(vacio.Rows))
	}
	if len(vacio.Columns) != 1 {
		t.Errorf("un SELECT vacío igual tiene columnas: llegaron %d", len(vacio.Columns))
	}
}

// El texto lo genera el servidor. Si lo formateáramos nosotros, un numeric
// perdería los ceros de la derecha —1.50 se volvería 1.5— y la grilla mostraría
// algo distinto de lo que devuelve cualquier otro cliente contra la misma base.
func TestRunMuestraElTextoDelServidorSinReformatear(t *testing.T) {
	pool, _ := conectar(t)

	res := correr(t, pool,
		`select 1.50::numeric as n, '2024-03-01 12:00:00+00'::timestamptz at time zone 'UTC' as t`,
		RunOptions{})

	if got := *res.Rows[0][0]; got != "1.50" {
		t.Errorf("numeric = %q, se esperaba \"1.50\" con el cero final del servidor", got)
	}
	if got := *res.Rows[0][1]; got != "2024-03-01 12:00:00" {
		t.Errorf("timestamp = %q", got)
	}
}

func TestRunClasificaLosTipos(t *testing.T) {
	pool, _ := conectar(t)

	res := correr(t, pool, `select
		1::int4        as n,
		'a'::text      as s,
		true           as b,
		now()          as ts,
		'{}'::jsonb    as j,
		'\x00'::bytea  as bin`, RunOptions{})

	quiere := []struct {
		nombre string
		clase  query.Class
		tipo   string
	}{
		{"n", query.ClassNumber, "int4"},
		{"s", query.ClassText, "text"},
		{"b", query.ClassBool, "bool"},
		{"ts", query.ClassTemporal, "timestamptz"},
		{"j", query.ClassJSON, "jsonb"},
		{"bin", query.ClassBinary, "bytea"},
	}
	if len(res.Columns) != len(quiere) {
		t.Fatalf("columnas = %d, se esperaban %d", len(res.Columns), len(quiere))
	}
	for i, q := range quiere {
		got := res.Columns[i]
		if got.Name != q.nombre {
			t.Errorf("columna %d: nombre = %q, se esperaba %q", i, got.Name, q.nombre)
		}
		if got.Class != q.clase {
			t.Errorf("columna %q: clase = %q, se esperaba %q", q.nombre, got.Class, q.clase)
		}
		if got.DataType != q.tipo {
			t.Errorf("columna %q: tipo = %q, se esperaba %q", q.nombre, got.DataType, q.tipo)
		}
	}
}

// pgx no conoce los tipos que define el usuario, así que sin resolverlos contra
// el catálogo la columna llega sin nombre de tipo y el encabezado de la grilla
// queda mudo.
func TestRunResuelveTiposDefinidosPorElUsuario(t *testing.T) {
	pool, esquema := conectar(t)
	ejecutar(t, pool, fmt.Sprintf(`create type %s.humor as enum ('bien', 'mal')`, esquema))

	res := correr(t, pool, fmt.Sprintf(`select 'bien'::%s.humor as h`, esquema), RunOptions{})

	if len(res.Columns) != 1 {
		t.Fatalf("columnas = %d", len(res.Columns))
	}
	if res.Columns[0].DataType != "humor" {
		t.Errorf("DataType = %q, se esperaba \"humor\"", res.Columns[0].DataType)
	}
	if got := *res.Rows[0][0]; got != "bien" {
		t.Errorf("valor = %q", got)
	}
}

// Cancelar tiene que cortar la consulta, no esperar a que termine. Sin esto,
// el botón de cancelar de S06 sería decorativo.
func TestRunSeCancela(t *testing.T) {
	pool, _ := conectar(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	arranque := time.Now()
	_, f := Run(ctx, pool, `select pg_sleep(10)`, RunOptions{})
	tardo := time.Since(arranque)

	if f == nil {
		t.Fatal("la consulta cancelada devolvió resultado")
	}
	if tardo > 5*time.Second {
		t.Errorf("tardó %s: no se canceló, esperó a que terminara", tardo)
	}
}

// Un error de SQL tiene que llegar clasificado, con el SQLSTATE, que es lo que
// permite señalar la línea y decir qué pasó.
func TestRunClasificaLosErroresDeSQL(t *testing.T) {
	pool, _ := conectar(t)

	_, f := Run(context.Background(), pool, `select * from tabla_que_no_existe`, RunOptions{})
	if f == nil {
		t.Fatal("una tabla inexistente no dio error")
	}
	if f.SQLState != "42P01" {
		t.Errorf("SQLState = %q, se esperaba 42P01 (undefined_table)", f.SQLState)
	}
	if f.Message == "" {
		t.Error("el fallo llegó sin mensaje")
	}
	if f.Detail == "" {
		t.Error("el fallo llegó sin el texto del motor, que es el que se reconoce")
	}
}
