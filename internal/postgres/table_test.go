package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestQuoteIdent(t *testing.T) {
	casos := []struct{ dentro, fuera string }{
		{"tabla", `"tabla"`},
		{"order", `"order"`},       // palabra reservada
		{"Mi Tabla", `"Mi Tabla"`}, // espacio y mayúsculas
		{"año", `"año"`},           // no ASCII
		{`a"b`, `"a""b"`},          // la comilla se duplica
		{`"`, `""""`},              // solo una comilla
		{`x"; drop table y --`, `"x""; drop table y --"`}, // hostil
		{"", `""`},
	}
	for _, c := range casos {
		if got := QuoteIdent(c.dentro); got != c.fuera {
			t.Errorf("QuoteIdent(%q) = %q, se esperaba %q", c.dentro, got, c.fuera)
		}
	}
}

// Citar bien no es una cuestión de estilo: es lo que separa un nombre raro de
// una inyección. Se prueba contra el motor porque lo que importa no es cómo se
// ve la cadena sino qué hace Postgres con ella.
func TestTableDataCitaNombresHostiles(t *testing.T) {
	pool, esquema := conectar(t)

	// El nombre lleva comilla, punto y coma, un DROP calificado con esquema y un
	// comentario que se come lo que sobra. Está armado para que, si el citado
	// falla, la SQL resultante sea VÁLIDA y destructiva — no que reviente con un
	// error de sintaxis. Un nombre que rompe el parser haría fallar el test por
	// el motivo equivocado y dejaría sin probar lo que importa.
	nombre := `raro"; drop table "` + esquema + `"."victima" --`
	citado := `"` + esquema + `"."raro""; drop table ""` + esquema + `"".""victima"" --"`

	ejecutar(t, pool, fmt.Sprintf(`create table "%s"."victima" (id int)`, esquema))
	// Una tabla llamada `raro`, que es en lo que se convierte el nombre hostil
	// cuando el citado no escapa: sin ella, el SELECT fallaría antes del DROP y
	// el daño nunca llegaría a ocurrir.
	ejecutar(t, pool, fmt.Sprintf(`create table "%s"."raro" (id int)`, esquema))
	ejecutar(t, pool, fmt.Sprintf(`create table %s (id int)`, citado))
	ejecutar(t, pool, fmt.Sprintf(`insert into %s values (7)`, citado))

	res, f := TableData(context.Background(), pool, esquema, nombre, TableDataOptions{Limit: 10})
	if f != nil {
		t.Fatalf("TableData sobre un nombre hostil falló: %s · %s", f.Message, f.Detail)
	}
	if len(res.Rows) != 1 || *res.Rows[0][0] != "7" {
		t.Errorf("leyó otra tabla, no la del nombre hostil: %+v", res.Rows)
	}

	var existe bool
	err := pool.QueryRow(context.Background(),
		`select exists (select 1 from pg_tables where schemaname = $1 and tablename = $2)`,
		esquema, "victima").Scan(&existe)
	if err != nil {
		t.Fatalf("comprobar la víctima: %v", err)
	}
	if !existe {
		t.Fatal("la tabla víctima desapareció: el identificador se ejecutó como SQL")
	}
}

// Sin ORDER BY, LIMIT/OFFSET puede repetir y saltear filas entre páginas. Con la
// clave primaria, las páginas particionan el conjunto sin superponerse.
func TestTableDataPaginaSinRepetirCuandoHayOrden(t *testing.T) {
	pool, esquema := conectar(t)
	ejecutar(t, pool, fmt.Sprintf(`create table %s.t (id int primary key)`, esquema))
	ejecutar(t, pool, fmt.Sprintf(`insert into %s.t select generate_series(1, 50)`, esquema))

	visto := map[string]bool{}
	for offset := 0; offset < 50; offset += 10 {
		res, f := TableData(context.Background(), pool, esquema, "t", TableDataOptions{
			OrderBy: []string{"id"}, Limit: 10, Offset: offset,
		})
		if f != nil {
			t.Fatalf("offset %d: %s", offset, f.Message)
		}
		if len(res.Rows) != 10 {
			t.Fatalf("offset %d devolvió %d filas", offset, len(res.Rows))
		}
		for _, fila := range res.Rows {
			v := *fila[0]
			if visto[v] {
				t.Errorf("la fila %s apareció en dos páginas", v)
			}
			visto[v] = true
		}
	}
	if len(visto) != 50 {
		t.Errorf("se vieron %d filas distintas de 50", len(visto))
	}
}

func TestTableCountEsExacto(t *testing.T) {
	pool, esquema := conectar(t)
	ejecutar(t, pool, fmt.Sprintf(`create table %s.t (id int)`, esquema))
	ejecutar(t, pool, fmt.Sprintf(`insert into %s.t select generate_series(1, 137)`, esquema))

	n, f := TableCount(context.Background(), pool, esquema, "t")
	if f != nil {
		t.Fatalf("TableCount() falló: %s", f.Message)
	}
	if n != 137 {
		t.Errorf("count = %d, se esperaban 137", n)
	}
}

// El orden de las columnas de una clave compuesta importa: es el orden del
// índice, no el alfabético ni el de la tabla.
func TestPrimaryKeyColumnsRespetaElOrdenDelIndice(t *testing.T) {
	pool, esquema := conectar(t)
	ejecutar(t, pool, fmt.Sprintf(
		`create table %s.t (a int, b int, c int, primary key (c, a))`, esquema))

	cols, err := PrimaryKeyColumns(context.Background(), pool, esquema, "t")
	if err != nil {
		t.Fatalf("PrimaryKeyColumns() error: %v", err)
	}
	if strings.Join(cols, ",") != "c,a" {
		t.Errorf("clave = %v, se esperaba [c a]", cols)
	}
}

func TestPrimaryKeyColumnsSinClaveDevuelveVacio(t *testing.T) {
	pool, esquema := conectar(t)
	ejecutar(t, pool, fmt.Sprintf(`create table %s.t (a int)`, esquema))

	cols, err := PrimaryKeyColumns(context.Background(), pool, esquema, "t")
	if err != nil {
		t.Fatalf("PrimaryKeyColumns() error: %v", err)
	}
	if len(cols) != 0 {
		t.Errorf("una tabla sin clave primaria devolvió %v", cols)
	}
}

// La protección de solo lectura la tiene que hacer cumplir el servidor, no
// nuestro código. Un guard nuestro solo cubre los caminos que nos acordamos de
// cubrir; default_transaction_read_only cubre todos, incluidos los que todavía
// no escribimos.
func TestConnectSoloLecturaLoRechazaElServidor(t *testing.T) {
	dsn := testDSN(t)

	// Primero, un pool normal crea la tabla.
	normal, _, f := Connect(context.Background(), dsn, "base de pruebas", ConnectOptions{MaxConns: 2})
	if f != nil {
		t.Fatalf("Connect() falló: %s", f.Message)
	}
	t.Cleanup(normal.Close)
	ejecutar(t, normal, `drop table if exists kn_solo_lectura`)
	ejecutar(t, normal, `create table kn_solo_lectura (id int)`)
	t.Cleanup(func() { ejecutar(t, normal, `drop table if exists kn_solo_lectura`) })

	ro, _, f := Connect(context.Background(), dsn, "base de pruebas", ConnectOptions{
		MaxConns: 2, ReadOnly: true,
	})
	if f != nil {
		t.Fatalf("Connect(ReadOnly) falló: %s", f.Message)
	}
	t.Cleanup(ro.Close)

	// Leer sí.
	if _, f := Run(context.Background(), ro, `select 1`, RunOptions{}); f != nil {
		t.Fatalf("una conexión de solo lectura no pudo leer: %s", f.Message)
	}

	// Escribir no, y con el código del motor.
	_, f = Run(context.Background(), ro, `insert into kn_solo_lectura values (1)`, RunOptions{})
	if f == nil {
		t.Fatal("una conexión de solo lectura ejecutó un INSERT")
	}
	if f.SQLState != "25006" {
		t.Errorf("SQLState = %q, se esperaba 25006 (read_only_sql_transaction)", f.SQLState)
	}
}

// El corte por tiempo lo aplica el servidor. Cancelar desde el cliente depende
// de que el cliente siga vivo; esto sigue vigente aunque la app se cuelgue.
func TestConnectStatementTimeoutLoAplicaElServidor(t *testing.T) {
	dsn := testDSN(t)

	pool, _, f := Connect(context.Background(), dsn, "base de pruebas", ConnectOptions{
		MaxConns: 2, StatementTimeout: 300 * time.Millisecond,
	})
	if f != nil {
		t.Fatalf("Connect() falló: %s", f.Message)
	}
	t.Cleanup(pool.Close)

	arranque := time.Now()
	_, f = Run(context.Background(), pool, `select pg_sleep(10)`, RunOptions{})
	tardo := time.Since(arranque)

	if f == nil {
		t.Fatal("pg_sleep(10) terminó pese al statement_timeout de 300 ms")
	}
	if f.SQLState != "57014" {
		t.Errorf("SQLState = %q, se esperaba 57014 (query_canceled)", f.SQLState)
	}
	if tardo > 5*time.Second {
		t.Errorf("tardó %s: el timeout no se aplicó", tardo)
	}
}
