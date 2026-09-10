package service

import (
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
	if len(fs) != 4 || fs[0].Key != export.CSV || fs[0].Extension != ".csv" || fs[3].Extension != ".md" {
		t.Fatalf("formatos: %+v", fs)
	}
}
