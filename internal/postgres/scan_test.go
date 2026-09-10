package postgres_test

import (
	"context"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/postgres"
)

// TestUnRecorridoNoNecesitaUnaSegundaConexion.
//
// Con un pool de UNA sola conexión, abrir un recorrido y leerlo entero tiene
// que funcionar. Si en el medio se pidiera otra conexión al pool —por ejemplo
// para resolver los tipos que pgx no conoce— esto se colgaría hasta que el
// contexto lo corte, que es exactamente lo que pasaría en producción con una
// conexión de solo lectura, cuyo pool tiene dos.
func TestUnRecorridoNoNecesitaUnaSegundaConexion(t *testing.T) {
	ctx := context.Background()
	// UNA sola conexión en el pool: es el peor caso, y hace determinista lo
	// que en producción sería una espera intermitente.
	c, f := postgres.Open(ctx, dsnPrueba, "base de pruebas", engine.OpenOptions{MaxConns: 1})
	if f != nil {
		t.Skipf("no hay Postgres escuchando (%s)", f.Message)
	}
	t.Cleanup(c.Close)

	// En el esquema de pruebas y no en public: los binarios de cada paquete
	// corren en paralelo contra la MISMA base, y una tabla de más en public la
	// cuenta el test que cuenta las tablas visibles.
	const tabla = esquemaDePrueba + ".kn_scan_pool"
	const tipo = esquemaDePrueba + ".kn_scan_estado"

	exec := func(sql string) {
		t.Helper()
		if err := c.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec("CREATE SCHEMA IF NOT EXISTS " + esquemaDePrueba)
	exec("DROP TABLE IF EXISTS " + tabla)
	exec("DROP TYPE IF EXISTS " + tipo)
	// Un enum a propósito: es el tipo que obliga a preguntarle al catálogo.
	exec("CREATE TYPE " + tipo + " AS ENUM ('a', 'b')")
	exec("CREATE TABLE " + tabla + " (id int primary key, estado " + tipo + ")")
	exec("INSERT INTO " + tabla + " VALUES (1, 'a'), (2, 'b')")
	t.Cleanup(func() {
		_ = c.Exec(context.Background(), "DROP TABLE IF EXISTS "+tabla)
		_ = c.Exec(context.Background(), "DROP TYPE IF EXISTS "+tipo)
	})

	st, err := c.Scan(ctx, esquemaDePrueba, "kn_scan_pool", engine.ScanOptions{OrderBy: []string{"id"}})
	if err != nil {
		t.Fatalf("Scan(): %v", err)
	}
	defer st.Close()

	// El nombre del tipo del enum sale del catálogo, no de pgx: si no se
	// resolvió, viene vacío.
	if len(st.Columns()) != 2 || st.Columns()[1].DataType != "kn_scan_estado" {
		t.Errorf("las columnas son %+v y la segunda tendría que ser kn_scan_estado", st.Columns())
	}
	n := 0
	for st.Next() {
		n++
	}
	if err := st.Err(); err != nil {
		t.Fatalf("Err(): %v", err)
	}
	if n != 2 {
		t.Errorf("el recorrido dio %d filas y la tabla tiene 2", n)
	}
}
