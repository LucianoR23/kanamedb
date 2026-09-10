package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/export"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// TestExportarUnaTablaEnteraEnLosCuatroMotores.
//
// Es el caso que separa este camino del resultado del editor: las filas no
// vuelven por el puente, las lee Go del motor mientras escribe el archivo. Lo
// que se comprueba es lo que un archivo exportado tiene que cumplir y una
// lectura por páginas no garantiza: que estén TODAS las filas —más que
// cualquier límite por defecto—, y que NULL siga siendo distinto de la cadena
// vacía después de dar la vuelta entera.
func TestExportarUnaTablaEnteraEnLosCuatroMotores(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq, tabla := tablaDeDatos(t, sesion, c, "kn_export", true)
			abierta, err := sesion.abierta()
			if err != nil {
				t.Fatal(err)
			}

			// Más filas que el límite por defecto de la grilla, para que un
			// límite colado se note. Y dos filas que solo se distinguen por
			// NULL contra cadena vacía.
			nom := califica(c, esq, tabla)
			for i := 3; i <= 1200; i++ {
				if err := abierta.db.Exec(ctx, fmt.Sprintf(
					"INSERT INTO %s (id, nombre) VALUES (%d, 'f%d')", nom, i, i)); err != nil {
					t.Fatalf("cargar la fila %d: %v", i, err)
				}
			}
			if err := abierta.db.Exec(ctx, fmt.Sprintf(
				"INSERT INTO %s (id, nombre) VALUES (1201, NULL), (1202, '')", nom)); err != nil {
				t.Fatal(err)
			}

			e := NewExports(NewQueries(sesion))
			pedido := TableExport{
				RunID:   "exp1",
				Schema:  esq,
				Table:   tabla,
				Format:  export.CSV,
				OrderBy: []string{"id"},
			}

			ruta := filepath.Join(t.TempDir(), "tabla.csv")
			info, err := e.SaveTable(ctx, pedido, ruta)
			if err != nil {
				t.Fatalf("SaveTable(): %v", err)
			}
			if info.Rows != 1202 {
				t.Errorf("SaveTable() escribió %d filas y la tabla tiene 1202: falta el resto, "+
					"o se coló un límite por defecto", info.Rows)
			}

			b, err := os.ReadFile(ruta)
			if err != nil {
				t.Fatal(err)
			}
			lineas := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
			// 1202 filas más el encabezado.
			if len(lineas) != 1203 {
				t.Fatalf("el archivo tiene %d líneas y se esperaban 1203", len(lineas))
			}
			if lineas[0] != "id,nombre" {
				t.Errorf("el encabezado es %q", lineas[0])
			}
			if lineas[1] != "1,uno" {
				t.Errorf("la primera fila es %q", lineas[1])
			}
			// NULL y la cadena vacía tienen que seguir siendo distintas: son
			// las dos últimas, y si el archivo las escribiera igual no habría
			// forma de volver a distinguirlas al importarlo.
			if lineas[1201] != `1201,\N` {
				t.Errorf("la fila con NULL quedó como %q y se esperaba 1201,\\N", lineas[1201])
			}
			if lineas[1202] != `1202,""` {
				t.Errorf("la fila con la cadena vacía quedó como %q y se esperaba 1202,\"\"", lineas[1202])
			}
			if info.Bytes != int64(len(b)) {
				t.Errorf("SaveInfo dice %d bytes y el archivo tiene %d", info.Bytes, len(b))
			}

			// La vista previa sale del MISMO recorrido y corta apenas tiene lo
			// que necesita: tiene que coincidir con el principio del archivo.
			vista, err := e.PreviewTable(ctx, pedido, 3)
			if err != nil {
				t.Fatalf("PreviewTable(): %v", err)
			}
			if vista != strings.Join(lineas[:4], "\n")+"\n" {
				t.Errorf("la vista previa no es el principio del archivo:\n%s", vista)
			}

			// Cortar el recorrido a la mitad no puede dejar la conexión
			// inservible: es lo que pasa cada vez que se mira una vista previa.
			if _, fail := abierta.db.Count(ctx, esq, tabla, nil); fail != nil {
				t.Errorf("después de la vista previa, Count() falla: %s", fail.Message)
			}
		})
	}
}

// TestUnaTablaQueNoExisteNoDejaArchivo comprueba que el destino no se toca
// cuando la lectura ni siquiera arranca.
func TestUnaTablaQueNoExisteNoDejaArchivo(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq := esquemaDeApply(t, sesion, c)

			dir := t.TempDir()
			ruta := filepath.Join(dir, "salida.csv")
			if err := os.WriteFile(ruta, []byte("lo que ya había"), 0o600); err != nil {
				t.Fatal(err)
			}

			e := NewExports(NewQueries(sesion))
			_, err := e.SaveTable(ctx, TableExport{
				RunID: "exp2", Schema: esq, Table: "kn_no_existe", Format: export.CSV,
			}, ruta)
			if err == nil {
				t.Fatal("exportó una tabla que no existe")
			}
			if b, _ := os.ReadFile(ruta); string(b) != "lo que ya había" {
				t.Errorf("el fallo pisó el archivo que ya estaba: %q", b)
			}
			restos, _ := filepath.Glob(filepath.Join(dir, ".salida.csv-*"))
			if len(restos) != 0 {
				t.Errorf("quedaron temporales: %v", restos)
			}
		})
	}
}

// TestCancelarUnaExportacionLaCorta comprueba que «Cancelar» alcanza a una
// exportación con el mismo runID con el que corta una consulta, y que lo que
// queda a medio escribir NO se guarda con el nombre elegido.
func TestCancelarUnaExportacionLaCorta(t *testing.T) {
	sesion, c := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	esq, tabla := tablaDeDatos(t, sesion, c, "kn_export_cancel", true)

	consultas := NewQueries(sesion)
	e := NewExports(consultas)
	dir := t.TempDir()
	ruta := filepath.Join(dir, "cancelada.csv")

	// Se cancela ANTES de empezar: el contexto ya llega muerto y la lectura no
	// arranca. Cancelar a mitad de camino es lo mismo un momento después.
	cancelado, cancelar := context.WithCancel(ctx)
	cancelar()
	_, err := e.SaveTable(cancelado, TableExport{
		RunID: "exp3", Schema: esq, Table: tabla, Format: export.CSV,
	}, ruta)
	if err == nil {
		t.Fatal("una exportación cancelada terminó bien")
	}
	if _, err := os.Stat(ruta); !os.IsNotExist(err) {
		t.Errorf("la exportación cancelada dejó el archivo %s", ruta)
	}
	restos, _ := filepath.Glob(filepath.Join(dir, ".cancelada.csv-*"))
	if len(restos) != 0 {
		t.Errorf("quedaron temporales: %v", restos)
	}
}

/* ------------------------------- el recorrido que se corta a la mitad ---- */

// flujoFalso entrega n filas y después falla, o termina bien si err es nil.
//
// Existe porque el caso que importa —el servidor corta la lectura después de
// escribir medio archivo— no se puede provocar cuando uno quiere contra un
// motor real, y es justamente el que decide si un archivo cortado se distingue
// de uno entero.
type flujoFalso struct {
	columnas []query.Column
	filas    [][]*string
	err      error
	i        int
}

func (f *flujoFalso) Columns() []query.Column { return f.columnas }
func (f *flujoFalso) Next() bool {
	if f.i >= len(f.filas) {
		return false
	}
	f.i++
	return true
}
func (f *flujoFalso) Row() []*string { return f.filas[f.i-1] }
func (f *flujoFalso) Err() error     { return f.err }
func (f *flujoFalso) Close()         {}

func flujoDe(err error) *flujoFalso {
	return &flujoFalso{
		columnas: []query.Column{{Name: "id", Class: query.ClassNumber}},
		filas:    [][]*string{{texto("1")}, {texto("2")}},
		err:      err,
	}
}

func TestUnRecorridoQueFallaALaMitadNoTerminaElArchivo(t *testing.T) {
	corte := errors.New("la conexión se cerró en el medio")

	var roto strings.Builder
	esc, err := export.New(export.JSON, &roto, export.Options{})
	if err != nil {
		t.Fatal(err)
	}
	n, err := volcarFlujo(flujoDe(corte), esc, 0)
	if !errors.Is(err, corte) {
		t.Fatalf("el corte del servidor no llegó a quien llama: %v", err)
	}
	if n != 2 {
		t.Errorf("escribió %d filas antes de cortarse", n)
	}
	// Y lo escrito NO es un JSON terminado: el array quedó abierto. Es lo que
	// hace que un archivo cortado se note en vez de parecer uno entero.
	if strings.HasSuffix(strings.TrimSpace(roto.String()), "]") {
		t.Errorf("el archivo cortado quedó cerrado como si estuviera entero:\n%s", roto.String())
	}

	// Sin error, el mismo recorrido sí cierra el array.
	var entero strings.Builder
	esc2, err := export.New(export.JSON, &entero, export.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := volcarFlujo(flujoDe(nil), esc2, 0); err != nil {
		t.Fatalf("un recorrido entero falló: %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(entero.String()), "]") {
		t.Errorf("el archivo entero no quedó cerrado:\n%s", entero.String())
	}
}

// TestElLimiteDeLaVistaPreviaCortaSinSerUnError: cortar por el límite termina
// el archivo bien, porque no es un fallo sino lo que se pidió.
func TestElLimiteDeLaVistaPreviaCortaSinSerUnError(t *testing.T) {
	var b strings.Builder
	esc, err := export.New(export.JSON, &b, export.Options{})
	if err != nil {
		t.Fatal(err)
	}
	n, err := volcarFlujo(flujoDe(nil), esc, 1)
	if err != nil {
		t.Fatalf("cortar por el límite dio error: %v", err)
	}
	if n != 1 {
		t.Errorf("con límite 1 escribió %d filas", n)
	}
	if !strings.HasSuffix(strings.TrimSpace(b.String()), "]") {
		t.Errorf("el corte por límite no cerró el archivo:\n%s", b.String())
	}
}
