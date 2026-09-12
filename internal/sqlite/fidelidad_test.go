package sqlite_test

import (
	"context"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// Los tests de este archivo prueban que lo que se muestra y se exporta es lo
// que hay en el archivo. Hallazgos C-02, C-12, C-26 y C-29 de la auditoría
// del 2026-09-11.

func celdas(t *testing.T, r *query.Result) map[string]string {
	t.Helper()
	if len(r.Rows) != 1 {
		t.Fatalf("se esperaba 1 fila y hay %d", len(r.Rows))
	}
	out := map[string]string{}
	for i, c := range r.Columns {
		if v := r.Rows[0][i]; v != nil {
			out[c.Name] = *v
		}
	}
	return out
}

// TestLasFechasSeLeenComoEstanGuardadas.
//
// El driver parsea a time.Time todo TEXT de una columna declarada DATE,
// DATETIME o TIMESTAMP y lo devuelve reformateado: un `2021-01-02` se veía
// como `2021-01-02T00:00:00Z`, con una zona que nadie escribió. Si esa columna
// es parte de la clave, el UPDATE de la grilla no encontraba la fila. La
// grilla y la exportación leen esas columnas con CAST(… AS TEXT) y las ven tal
// cual; el editor, que no puede reescribir la consulta, las formatea como se
// escriben en SQLite y no en RFC 3339.
func TestLasFechasSeLeenComoEstanGuardadas(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE f (id integer PRIMARY KEY, d DATE, dt DATETIME, ts TIMESTAMP, juliano DATE)`,
		`INSERT INTO f VALUES (1, '2021-01-02', '2021-01-02 16:39', '2021-01-02T16:39:05.5+03:00', 2459216.5)`,
	)
	ctx := context.Background()
	esperado := map[string]string{
		"d": "2021-01-02", "dt": "2021-01-02 16:39", "ts": "2021-01-02T16:39:05.5+03:00", "juliano": "2459216.5",
	}

	// La grilla.
	pg, f := c.Page(ctx, "main", "f", engine.PageOptions{Limit: 10})
	if f != nil {
		t.Fatal(f.Message)
	}
	got := celdas(t, pg)
	for col, quiero := range esperado {
		if got[col] != quiero {
			t.Errorf("Page: %s = %q, en el archivo hay %q", col, got[col], quiero)
		}
	}
	// Y el encabezado sigue diciendo que son fechas: el CAST no lo borra.
	for _, col := range pg.Columns {
		if col.Name != "id" && col.Class != query.ClassTemporal {
			t.Errorf("Page: la columna %s perdió su tipo declarado: %q/%q", col.Name, col.DataType, col.Class)
		}
	}

	// La exportación.
	flujo, err := c.Scan(ctx, "main", "f", engine.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer flujo.Close()
	if !flujo.Next() {
		t.Fatal("el recorrido no trajo la fila")
	}
	for i, col := range flujo.Columns() {
		if quiero, ok := esperado[col.Name]; ok && *flujo.Row()[i] != quiero {
			t.Errorf("Scan: %s = %q, en el archivo hay %q", col.Name, *flujo.Row()[i], quiero)
		}
		if col.Name != "id" && col.Class != query.ClassTemporal {
			t.Errorf("Scan: la columna %s perdió su tipo declarado", col.Name)
		}
	}

	// El editor: no puede evitar el parseo, pero escribe como SQLite y no como
	// RFC 3339 con Z.
	lote, f := c.Run(ctx, "SELECT d, dt, ts FROM f", engine.RunOptions{})
	if f != nil {
		t.Fatal(f.Message)
	}
	got = celdas(t, &lote.Results[0])
	for col, quiero := range map[string]string{
		"d": "2021-01-02", "dt": "2021-01-02 16:39:00", "ts": "2021-01-02 16:39:05.5+03:00",
	} {
		if got[col] != quiero {
			t.Errorf("Run: %s = %q, se esperaba %q", col, got[col], quiero)
		}
	}
	for _, v := range got {
		if strings.Contains(v, "Z") || strings.Contains(v, "T00:00:00") {
			t.Errorf("Run devolvió una zona que nadie escribió: %q", v)
		}
	}
}

// TestUnBlobSeExportaEnteroYVuelveComoBlob.
//
// La exportación reutilizaba la conversión de la grilla, que escribe `[N
// bytes]` en el lugar de cada BLOB, y el volcado decía que no había dejado
// nada afuera. La grilla sigue mostrando el tamaño; la exportación entrega
// hexadecimal y el escritor SQL lo cita como X'…'.
func TestUnBlobSeExportaEnteroYVuelveComoBlob(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE b (id integer PRIMARY KEY, dato BLOB, r REAL)`,
		`INSERT INTO b VALUES (1, X'00FF10', 3.0)`,
	)
	ctx := context.Background()

	pg, f := c.Page(ctx, "main", "b", engine.PageOptions{Limit: 10})
	if f != nil {
		t.Fatal(f.Message)
	}
	if got := celdas(t, pg)["dato"]; got != "[3 bytes]" {
		t.Errorf("la grilla tenía que mostrar el tamaño y mostró %q", got)
	}

	flujo, err := c.Scan(ctx, "main", "b", engine.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !flujo.Next() {
		t.Fatal("sin fila")
	}
	fila := map[string]string{}
	for i, col := range flujo.Columns() {
		fila[col.Name] = *flujo.Row()[i]
		if col.Name == "dato" && col.Class != query.ClassBinary {
			t.Errorf("la columna BLOB no es de clase binaria: %q", col.Class)
		}
	}
	// Cerrado antes de escribir: un recorrido abierto tiene el lock de lectura.
	flujo.Close()
	if fila["dato"] != "00FF10" {
		t.Errorf("Scan entregó %q y el BLOB es 00FF10", fila["dato"])
	}
	// C-26: 3.0 no es 3. Al recargar en una columna sin afinidad quedaría INTEGER.
	if fila["r"] != "3.0" {
		t.Errorf("Scan entregó %q para el REAL 3.0", fila["r"])
	}

	// Y el literal que escribe el volcado vuelve como BLOB, no como texto.
	q := c.Quoting()
	if err := c.Exec(ctx, "INSERT INTO b (id, dato) VALUES (2, "+q.Binary(fila["dato"])+")"); err != nil {
		t.Fatalf("el literal binario no es válido: %v", err)
	}
	lote, f := c.Run(ctx, "SELECT typeof(dato), hex(dato) FROM b WHERE id = 2", engine.RunOptions{})
	if f != nil {
		t.Fatal(f.Message)
	}
	if got := lote.Results[0].Rows[0]; *got[0] != "blob" || *got[1] != "00FF10" {
		t.Errorf("volvió como %s %s, tenía que ser blob 00FF10", *got[0], *got[1])
	}
}

// TestElConteoDeAfectadasEsDeEstaSentencia: changes() habla del último
// INSERT/UPDATE/DELETE de la conexión, aunque la última sentencia haya sido un
// CREATE TABLE. Con un pool de una conexión el conteo viejo se colaba.
func TestElConteoDeAfectadasEsDeEstaSentencia(t *testing.T) {
	c := preparar(t,
		`CREATE TABLE n (id integer PRIMARY KEY, v int)`,
		`INSERT INTO n VALUES (1, 0), (2, 0), (3, 0)`,
	)
	ctx := context.Background()
	c.DB().SetMaxOpenConns(1)

	lote, f := c.Run(ctx, "UPDATE n SET v = 1", engine.RunOptions{})
	if f != nil {
		t.Fatal(f.Message)
	}
	if lote.Results[0].AffectedRows != 3 {
		t.Fatalf("el UPDATE tenía que afectar 3 y afectó %d", lote.Results[0].AffectedRows)
	}
	for _, sql := range []string{"CREATE TABLE otra (a int)", "PRAGMA foreign_keys", "CREATE INDEX ix ON n (v)"} {
		lote, f := c.Run(ctx, sql, engine.RunOptions{})
		if f != nil {
			t.Fatalf("%s: %s", sql, f.Message)
		}
		for _, r := range lote.Results {
			if !r.ReturnsRows && r.AffectedRows != 0 {
				t.Errorf("%q dice que afectó %d filas: es el conteo del UPDATE anterior", sql, r.AffectedRows)
			}
		}
	}
}
