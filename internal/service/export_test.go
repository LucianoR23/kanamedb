package service

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/export"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// NewExports(nil) alcanza para el camino del editor: las filas vienen en el
// pedido y no se toca la sesión. El de la tabla, que sí la usa, se prueba con
// una sesión abierta en export_tabla_test.go.
func resultadoDePrueba() ResultExport {
	s := func(v string) *string { return &v }
	return ResultExport{
		Format: export.CSV,
		Columns: []query.Column{
			{Name: "id", Class: query.ClassNumber},
			{Name: "nombre", Class: query.ClassText},
		},
		Rows: [][]*string{{s("1"), s("Ana")}, {s("2"), nil}},
	}
}

func TestExportSaveEscribeElArchivoEntero(t *testing.T) {
	dir := t.TempDir()
	ruta := filepath.Join(dir, "salida.csv")
	e := NewExports(nil)

	info, err := e.Save(resultadoDePrueba(), ruta)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	contenido, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}
	want := "id,nombre\n1,Ana\n2,\\N\n"
	if string(contenido) != want {
		t.Fatalf("archivo:\n%s", contenido)
	}
	if info.Rows != 2 || info.Bytes != int64(len(want)) || info.Path != ruta {
		t.Fatalf("info: %+v", info)
	}
	// No queda ningún temporal al lado.
	restos, _ := filepath.Glob(filepath.Join(dir, ".salida.csv-*"))
	if len(restos) != 0 {
		t.Fatalf("quedaron temporales: %v", restos)
	}
}

func TestExportSaveReemplazaSinDejarUnArchivoCortado(t *testing.T) {
	dir := t.TempDir()
	ruta := filepath.Join(dir, "salida.json")
	if err := os.WriteFile(ruta, []byte("viejo"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := NewExports(nil)
	r := resultadoDePrueba()
	r.Format = export.JSON

	// Un formato desconocido falla ANTES de tocar el destino: el archivo
	// viejo tiene que seguir entero y no puede quedar un temporal.
	r.Format = export.Format("xml")
	if _, err := e.Save(r, ruta); err == nil {
		t.Fatal("aceptó un formato desconocido")
	}
	if b, _ := os.ReadFile(ruta); string(b) != "viejo" {
		t.Fatalf("el fallo pisó el archivo: %q", b)
	}
	restos, _ := filepath.Glob(filepath.Join(dir, ".salida.json-*"))
	if len(restos) != 0 {
		t.Fatalf("quedaron temporales: %v", restos)
	}

	r.Format = export.JSON
	if _, err := e.Save(r, ruta); err != nil {
		t.Fatalf("Save: %v", err)
	}
	b, _ := os.ReadFile(ruta)
	if !strings.HasPrefix(string(b), "[\n{\"id\":1,\"nombre\":\"Ana\"}") {
		t.Fatalf("archivo:\n%s", b)
	}
}

func TestExportSaveDiceQuePasoCuandoNoPuede(t *testing.T) {
	e := NewExports(nil)
	if _, err := e.Save(resultadoDePrueba(), ""); err == nil {
		t.Fatal("aceptó una ruta vacía")
	}
	ruta := filepath.Join(t.TempDir(), "no-existe", "salida.csv")
	_, err := e.Save(resultadoDePrueba(), ruta)
	if err == nil || !strings.Contains(err.Error(), "no se pudo crear el archivo") {
		t.Fatalf("error: %v", err)
	}
}

func TestExportPreviewYRender(t *testing.T) {
	e := NewExports(nil)
	r := resultadoDePrueba()
	r.Format = export.Markdown

	vista, err := e.Preview(r, 1)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(vista, "\n") != 3 || !strings.Contains(vista, "| 1 | Ana |") || strings.Contains(vista, "| 2 |") {
		t.Fatalf("vista previa:\n%s", vista)
	}
	if _, err := e.Preview(r, 0); err == nil {
		t.Fatal("una vista previa sin límite no es una vista previa")
	}

	todo, err := e.Render(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(todo, "| 2 | NULL |") {
		t.Fatalf("render:\n%s", todo)
	}
}

func TestExportFormats(t *testing.T) {
	fs := NewExports(nil).Formats()
	if len(fs) != 5 || fs[0].Key != export.CSV || fs[0].Extension != ".csv" || fs[3].Extension != ".md" {
		t.Fatalf("formatos: %+v", fs)
	}
	// Solo el SQL necesita tabla, y es el que el editor no puede ofrecer.
	for _, f := range fs {
		if f.NeedsTable != (f.Key == export.SQL) {
			t.Errorf("%s dice needsTable=%v", f.Key, f.NeedsTable)
		}
	}
}

// TestElResultadoDelEditorNoAceptaElFormatoSQL: una consulta puede venir de
// tres tablas o de ninguna, así que no hay a cuál insertar.
func TestElResultadoDelEditorNoAceptaElFormatoSQL(t *testing.T) {
	e := NewExports(nil)
	r := resultadoDePrueba()
	r.Format = export.SQL

	if _, err := e.Render(r); err == nil {
		t.Error("Render() aceptó el formato SQL para un resultado")
	}
	if _, err := e.Preview(r, 2); err == nil {
		t.Error("Preview() aceptó el formato SQL para un resultado")
	}
	dir := t.TempDir()
	ruta := filepath.Join(dir, "x.sql")
	if _, err := e.Save(r, ruta); err == nil {
		t.Error("Save() aceptó el formato SQL para un resultado")
	}
	if _, err := os.Stat(ruta); !os.IsNotExist(err) {
		t.Error("el rechazo dejó un archivo")
	}
}

// TestElGzipDeVariasTablasEnUnArchivoComprimeDeVerdad.
//
// El archivo salía con nombre `.sql.gz` y contenido en claro: el gzip se
// apagaba tabla por tabla —bien— y no se ponía nunca alrededor del conjunto,
// aunque el comentario dijera que sí. `gunzip` se negaba a abrir una
// exportación que la app decía haber comprimido.
func TestElGzipDeVariasTablasEnUnArchivoComprimeDeVerdad(t *testing.T) {
	sesion, c := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	esq, t1 := tablaDeDatos(t, sesion, c, "kn_gz_una", true)
	_, t2 := tablaDeDatos(t, sesion, c, "kn_gz_dos", true)

	exp := NewExports(NewQueries(sesion))
	destino := filepath.Join(t.TempDir(), "volcado.sql.gz")
	if _, err := exp.SaveTables(ctx, TablesExport{
		RunID: "gz1", Schema: esq, Tables: []string{t1, t2},
		Format: export.SQL, Options: export.Options{Gzip: true},
	}, destino); err != nil {
		t.Fatalf("SaveTables(): %v", err)
	}

	f, err := os.Open(destino)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("el archivo .gz no es gzip: %v", err)
	}
	defer gz.Close()
	adentro, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("no se pudo descomprimir entero: %v", err)
	}
	// Y adentro están LAS DOS tablas: el gzip va una vez alrededor de todo, no
	// uno por tabla pegado al siguiente.
	for _, tabla := range []string{t1, t2} {
		if !strings.Contains(string(adentro), tabla) {
			t.Errorf("la tabla %q no está en el script descomprimido", tabla)
		}
	}
}

// TestDosTablasQueSeLimpianIgualNoSePisan.
//
// `pedidos/2026` y `pedidos-2026` son dos tablas legales y distintas que dan el
// mismo nombre de archivo. La segunda pisaba a la primera y las dos se
// informaban como escritas: un archivo menos del que la app decía haber dejado.
func TestDosTablasQueSeLimpianIgualNoSePisan(t *testing.T) {
	nombres := nombresDeArchivo([]string{"pedidos/2026", "pedidos-2026", "pedidos:2026"}, ".csv")
	vistos := map[string]bool{}
	for _, n := range nombres {
		if vistos[n] {
			t.Fatalf("dos tablas comparten el archivo %q: %v", n, nombres)
		}
		vistos[n] = true
	}
	if nombres[0] != "pedidos-2026.csv" {
		t.Errorf("la primera tendría que quedarse con el nombre limpio: %q", nombres[0])
	}
	// Una sola tabla no lleva sufijo: el desempate aparece solo cuando hace
	// falta.
	if uno := nombresDeArchivo([]string{"clientes"}, ".csv"); uno[0] != "clientes.csv" {
		t.Errorf("una tabla sola quedó como %q", uno[0])
	}
}
