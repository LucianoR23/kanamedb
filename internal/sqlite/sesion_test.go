package sqlite_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/sqlite"
)

// La SQL de sesión de SQLite son pragmas, y van por el conector por lo mismo
// que los del DSN: tienen que llegar a CADA conexión del pool. Y query_only
// se vuelve a pedir después, así que una SQL de sesión que lo apague no gana.
func TestLaSQLDeSesionCorreEnCadaConexionYNoApagaQueryOnly(t *testing.T) {
	ctx := context.Background()
	f := filepath.Join(t.TempDir(), "sesion.db")
	dsn := "file:" + filepath.ToSlash(f) + "?_pragma=foreign_keys%281%29"

	c, fail := sqlite.Open(ctx, dsn, f, engine.OpenOptions{
		MaxConns:   3,
		SessionSQL: "-- lo que corre al abrir\nPRAGMA cache_size = -4321;\nPRAGMA temp_store = MEMORY;",
	})
	if fail != nil {
		t.Fatalf("Open() con SQL de sesión falló: %s — %s", fail.Message, fail.Detail)
	}
	t.Cleanup(c.Close)
	for i := 0; i < 12; i++ {
		lote, fail := c.Run(ctx, "PRAGMA cache_size", engine.RunOptions{})
		if fail != nil {
			t.Fatalf("PRAGMA: %s", fail.Message)
		}
		if v := lote.Results[0].Rows[0][0]; v == nil || *v != "-4321" {
			t.Fatalf("consulta %d: cache_size = %v, la SQL de sesión no llegó a esa conexión", i, v)
		}
	}

	// Rota, con su línea.
	_, fail = sqlite.Open(ctx, dsn, f, engine.OpenOptions{
		MaxConns:   2,
		SessionSQL: "PRAGMA cache_size = -4321;\nSELCT 1;",
	})
	if fail == nil {
		t.Fatal("una SQL de sesión con un error de sintaxis abrió la base igual")
	}
	if !strings.Contains(fail.Message, "SQL de sesión") || !strings.Contains(fail.Message, "línea 2") {
		t.Errorf("el fallo no dice que es la SQL de sesión ni la línea: %+v", fail)
	}

	// Las protecciones ganan.
	if err := c.Exec(ctx, "CREATE TABLE kn_sesion_ro (id int)"); err != nil {
		t.Fatal(err)
	}
	ro, fail := sqlite.Open(ctx, dsn, f, engine.OpenOptions{
		MaxConns:   2,
		ReadOnly:   true,
		SessionSQL: "PRAGMA query_only = 0;",
	})
	if fail != nil {
		t.Fatalf("abrir en solo lectura con SQL de sesión: %s — %s", fail.Message, fail.Detail)
	}
	t.Cleanup(ro.Close)
	for i := 0; i < 6; i++ {
		if err := ro.Exec(ctx, "INSERT INTO kn_sesion_ro VALUES (1)"); err == nil {
			t.Fatalf("la SQL de sesión apagó query_only y la escritura %d se ejecutó", i)
		}
	}

	// Y la SQL de sesión misma corre protegida: query_only ya vino en el DSN.
	_, fail = sqlite.Open(ctx, dsn, f, engine.OpenOptions{
		MaxConns: 2, ReadOnly: true, SessionSQL: "INSERT INTO kn_sesion_ro VALUES (2);",
	})
	if fail == nil {
		t.Fatal("una escritura en la SQL de sesión de una base de solo lectura la abrió igual")
	}
}
