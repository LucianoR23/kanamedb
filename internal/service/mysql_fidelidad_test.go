package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/export"
)

// Los tests de este archivo son de MySQL y MariaDB, contra el contenedor.
// Hallazgos C-03, C-13, C-14 y C-19 de la auditoría del 2026-09-11.

var motoresMySQL = []struct{ nombre, uri string }{
	{"mysql", "mysql://kaname:kaname@127.0.0.1:53306/kaname_test"},
	{"mariadb", "mariadb://kaname:kaname@127.0.0.1:53307/kaname_test"},
}

// TestEnMySQLUnDMLInformaLasFilasQueAfecto: el driver entrega un paquete OK
// como un resultado sin columnas, no como un error, así que un UPDATE de tres
// filas salía como «0 filas devueltas» (C-03). Y una sentencia que fallaba se
// reenviaba como Exec: nunca más.
func TestEnMySQLUnDMLInformaLasFilasQueAfecto(t *testing.T) {
	for _, caso := range motoresMySQL {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_dml", true)
			q := NewQueries(sesion)

			res := q.Run(ctx, "d1", "UPDATE "+califica(c, esq, tabla)+" SET nombre = 'z'")
			if !res.OK {
				t.Fatal(mensajeDe(res))
			}
			r := res.Batch.Results[0]
			if r.ReturnsRows || r.AffectedRows != 2 {
				t.Errorf("UPDATE: ReturnsRows=%v AffectedRows=%d; tenía que ser false y 2", r.ReturnsRows, r.AffectedRows)
			}
			res = q.Run(ctx, "d2", "CREATE TABLE "+califica(c, esq, "kn_dml_x")+" (a int)")
			if !res.OK {
				t.Fatal(mensajeDe(res))
			}
			t.Cleanup(func() {
				ab, _ := sesion.abierta()
				_ = ab.db.Exec(context.Background(), "DROP TABLE IF EXISTS "+califica(c, esq, "kn_dml_x"))
			})
			if r := res.Batch.Results[0]; r.ReturnsRows || r.AffectedRows != 0 {
				t.Errorf("CREATE TABLE: ReturnsRows=%v AffectedRows=%d", r.ReturnsRows, r.AffectedRows)
			}
			// Una sentencia inválida falla UNA vez y no deja nada: con el
			// reintento, `INSERT` + error del segundo intento habría duplicado.
			res = q.Run(ctx, "d3", "INSERT INTO "+califica(c, esq, tabla)+" (id, nombre) VALUES (7, 'siete'), (7, 'otra vez')")
			if res.OK {
				t.Fatal("un INSERT con clave repetida salió bien")
			}
			ab, _ := sesion.abierta()
			if n, _ := ab.db.Count(ctx, esq, tabla, nil); n != 2 {
				t.Errorf("después del INSERT fallido hay %d filas de 2", n)
			}
		})
	}
}

// TestEnMySQLLosBinariosLleganEnHexYVuelvenComoBinarios: BINARY, VARBINARY y
// BLOB llegaban como bytes crudos adentro de un string —corruptos hacia la
// interfaz, literales en el SQL— y BIT(8)=65 como «A» (C-13).
func TestEnMySQLLosBinariosLleganEnHexYVuelvenComoBinarios(t *testing.T) {
	for _, caso := range motoresMySQL {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq := esquemaDeApply(t, sesion, c)
			ab, _ := sesion.abierta()
			nom := califica(c, esq, "kn_bin")
			_ = ab.db.Exec(ctx, "DROP TABLE IF EXISTS "+nom)
			t.Cleanup(func() { _ = ab.db.Exec(context.Background(), "DROP TABLE IF EXISTS "+nom) })
			for _, sql := range []string{
				"CREATE TABLE " + nom + " (id BINARY(16) PRIMARY KEY, b BIT(8), v VARBINARY(4), t TEXT)",
				"INSERT INTO " + nom + " VALUES (UNHEX('00FF00000000000000000000000000AA'), b'01000001', X'00FF', 'texto')",
			} {
				if err := ab.db.Exec(ctx, sql); err != nil {
					t.Fatal(err)
				}
			}
			q := NewQueries(sesion)
			res := q.Run(ctx, "b1", "SELECT * FROM "+nom)
			if !res.OK {
				t.Fatal(mensajeDe(res))
			}
			got := map[string]string{}
			for i, col := range res.Batch.Results[0].Columns {
				got[col.Name] = *res.Batch.Results[0].Rows[0][i]
			}
			for col, quiero := range map[string]string{
				"id": "00FF00000000000000000000000000AA", "b": "65", "v": "00FF", "t": "texto",
			} {
				if got[col] != quiero {
					t.Errorf("%s = %q, se esperaba %q", col, got[col], quiero)
				}
			}

			// Y la exportación SQL vuelve a correr y deja los mismos bytes.
			e := NewExports(q)
			ruta := filepath.Join(t.TempDir(), "bin.sql")
			if _, err := e.SaveTable(ctx, TableExport{RunID: "b2", Schema: esq, Table: "kn_bin", Format: export.SQL}, ruta); err != nil {
				t.Fatal(err)
			}
			guion := leer(t, ruta)
			if !strings.Contains(guion, "X'00FF00000000000000000000000000AA'") || !strings.Contains(guion, "X'00FF'") {
				t.Fatalf("los binarios no salieron como X'…':\n%s", guion)
			}
			if err := ab.db.Exec(ctx, "DELETE FROM "+nom); err != nil {
				t.Fatal(err)
			}
			if err := ab.db.Exec(ctx, guion); err != nil {
				t.Fatalf("la exportación no corre: %v\n%s", err, guion)
			}
			res = q.Run(ctx, "b3", "SELECT HEX(id), b+0, HEX(v) FROM "+nom)
			if !res.OK {
				t.Fatal(mensajeDe(res))
			}
			fila := res.Batch.Results[0].Rows[0]
			if *fila[0] != "00FF00000000000000000000000000AA" || *fila[1] != "65" || *fila[2] != "00FF" {
				t.Errorf("volvió %q %q %q", *fila[0], *fila[1], *fila[2])
			}
		})
	}
}

// TestEnMySQLCancelarMataLaSentenciaEnElServidor: el driver cerraba el
// socket y nada más; el servidor recién lo notaba al escribir, así que un
// UPDATE cancelado terminaba y confirmaba (C-14). Ahora se manda KILL QUERY.
func TestEnMySQLCancelarMataLaSentenciaEnElServidor(t *testing.T) {
	for _, caso := range motoresMySQL {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_kill", true)
			ab, _ := sesion.abierta()
			q := NewQueries(sesion)

			hecho := make(chan RunResult, 1)
			go func() {
				hecho <- q.Run(ctx, "k1", "UPDATE "+califica(c, esq, tabla)+" SET nombre = CONCAT('lento', BENCHMARK(20000000, SHA2('x', 256)))")
			}()
			time.Sleep(700 * time.Millisecond)
			q.Cancel("k1")
			res := <-hecho
			if res.OK {
				t.Fatal("la ejecución cancelada dijo que salió bien")
			}
			// Un BENCHMARK de unos 3 s por fila, dos filas: si el servidor no la mató,
			// en 9 s terminó y confirmó.
			time.Sleep(9 * time.Second)
			n, f := ab.db.Count(ctx, esq, tabla, nil)
			if f != nil {
				t.Fatal(f.Message)
			}
			_ = n
			res = q.Run(ctx, "k2", "SELECT count(*) FROM "+califica(c, esq, tabla)+" WHERE nombre LIKE 'lento%'")
			if !res.OK {
				t.Fatal(mensajeDe(res))
			}
			if got := *res.Batch.Results[0].Rows[0][0]; got != "0" {
				t.Errorf("el UPDATE cancelado se confirmó igual: %s filas con «lento»", got)
			}
		})
	}
}

// TestEnMySQLUnIndiceUnicoNoEsClavePrimaria: column_key = 'PRI' también se
// reporta para un índice UNIQUE NOT NULL cuando la tabla no tiene PRIMARY
// KEY, y la grilla y la comparación creían que había clave (C-19).
func TestEnMySQLUnIndiceUnicoNoEsClavePrimaria(t *testing.T) {
	for _, caso := range motoresMySQL {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq := esquemaDeApply(t, sesion, c)
			ab, _ := sesion.abierta()
			nom := califica(c, esq, "kn_uq")
			_ = ab.db.Exec(ctx, "DROP TABLE IF EXISTS "+nom)
			t.Cleanup(func() { _ = ab.db.Exec(context.Background(), "DROP TABLE IF EXISTS "+nom) })
			if err := ab.db.Exec(ctx, "CREATE TABLE "+nom+" (id INT NOT NULL, UNIQUE (id))"); err != nil {
				t.Fatal(err)
			}
			snap, err := sesion.Schema(ctx, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range snap.Schemas {
				for _, tb := range s.Tables {
					if tb.Name == "kn_uq" && tb.HasPrimaryKey {
						t.Error("un índice UNIQUE NOT NULL se tomó por clave primaria")
					}
				}
			}
			// Y el detalle dice lo mismo: el volcado escribía PRIMARY KEY (id)
			// desde acá (review del 2026-09-12).
			det, err := ab.db.Detail(ctx, esq, "kn_uq")
			if err != nil {
				t.Fatal(err)
			}
			for _, col := range det.Columns {
				if col.PrimaryKey {
					t.Errorf("el detalle marca %s como clave primaria", col.Name)
				}
			}
		})
	}
}
