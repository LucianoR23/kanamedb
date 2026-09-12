package csvimport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// archivo escribe un CSV temporal y devuelve su ruta.
func archivo(t *testing.T, contenido string) string {
	t.Helper()
	ruta := filepath.Join(t.TempDir(), "datos.csv")
	if err := os.WriteFile(ruta, []byte(contenido), 0o600); err != nil {
		t.Fatal(err)
	}
	return ruta
}

func TestInspectLeeElEncabezadoYCuentaLasFilas(t *testing.T) {
	ruta := archivo(t, "id,nombre\n1,Ana\n2,Bruno\n3,Caro\n")
	i, err := Inspect(ruta, Options{HasHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(i.Columns, "|") != "id|nombre" {
		t.Errorf("columnas: %q", i.Columns)
	}
	// Tres filas de DATOS: el encabezado no cuenta.
	if i.Rows != 3 {
		t.Errorf("Rows = %d, quería 3", i.Rows)
	}
	if len(i.Sample) != 3 || i.Sample[0][1] != "Ana" {
		t.Errorf("muestra: %q", i.Sample)
	}
	if i.File.Name != "datos.csv" || i.File.Bytes == 0 {
		t.Errorf("archivo: %+v", i.File)
	}
}

func TestSinEncabezadoLasColumnasSeNumeran(t *testing.T) {
	ruta := archivo(t, "1,Ana\n2,Bruno\n")
	i, err := Inspect(ruta, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(i.Columns, "|") != "columna 1|columna 2" {
		t.Errorf("columnas: %q", i.Columns)
	}
	// Y la primera línea es un dato, no un encabezado.
	if i.Rows != 2 {
		t.Errorf("Rows = %d, quería 2", i.Rows)
	}
}

// TestLaMarcaDeExcelNoSeMeteEnElNombreDeLaColumna.
//
// Excel guarda los CSV con la marca de orden de bytes adelante. Sin saltearla,
// la primera columna se llama "<BOM>id" y no coincide con `id` de la tabla —y
// el error que se ve es «no existe la columna id» sobre una tabla que la tiene.
func TestLaMarcaDeExcelNoSeMeteEnElNombreDeLaColumna(t *testing.T) {
	ruta := archivo(t, "\uFEFFid,nombre\n1,Ana\n")
	i, err := Inspect(ruta, Options{HasHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if i.Columns[0] != "id" {
		t.Errorf("la primera columna es %q y tendría que ser \"id\"", i.Columns[0])
	}
	if i.NotUTF8 {
		t.Error("la marca de orden de bytes ES UTF-8 válido")
	}
}

func TestDelimitadores(t *testing.T) {
	casos := []struct {
		nombre string
		texto  string
		delim  string
	}{
		{"coma", "a,b\n1,2\n", ","},
		{"punto y coma", "a;b\n1;2\n", ";"},
		{"tabulación", "a\tb\n1\t2\n", "\t"},
		{"barra", "a|b\n1|2\n", "|"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			i, err := Inspect(archivo(t, c.texto), Options{HasHeader: true, Delimiter: c.delim})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(i.Columns, "|") != "a|b" {
				t.Errorf("columnas: %q", i.Columns)
			}
		})
	}
	if _, err := Inspect(archivo(t, "a,b\n"), Options{Delimiter: "??"}); err == nil {
		t.Error("se aceptó un delimitador que no está en la lista")
	}
}

// TestElDelimitadorEquivocadoSeNota: es el error más común, y lo que lo delata
// es que todas las líneas queden con UN campo.
func TestElDelimitadorEquivocadoSeNota(t *testing.T) {
	i, err := Inspect(archivo(t, "a;b;c\n1;2;3\n"), Options{HasHeader: true, Delimiter: ","})
	if err != nil {
		t.Fatal(err)
	}
	if len(i.Columns) != 1 {
		t.Errorf("con el delimitador equivocado tendría que haber una sola columna: %q", i.Columns)
	}
	// Y las líneas crudas están para poder verlo.
	if !strings.Contains(i.Raw, "a;b;c") {
		t.Errorf("las líneas crudas no muestran el archivo: %q", i.Raw)
	}
}

func TestLasLineasDesparejasSeAvisanSinCortarLaLectura(t *testing.T) {
	ruta := archivo(t, "id,nombre,pais\n1,Ana,AR\n2,Bruno\n3,Caro,UY,de más\n4,Dani,BR\n")
	i, err := Inspect(ruta, Options{HasHeader: true})
	if err != nil {
		t.Fatalf("una línea despareja cortó la lectura: %v", err)
	}
	if i.Rows != 4 {
		t.Errorf("Rows = %d, quería 4: las desparejas se leen igual", i.Rows)
	}
	if len(i.Ragged) != 2 {
		t.Fatalf("desparejas: %+v", i.Ragged)
	}
	if i.Ragged[0].Line != 3 || i.Ragged[0].Fields != 2 {
		t.Errorf("la primera despareja es %+v y quería línea 3 con 2 campos", i.Ragged[0])
	}
	if i.Ragged[1].Line != 4 || i.Ragged[1].Fields != 4 {
		t.Errorf("la segunda despareja es %+v", i.Ragged[1])
	}
}

func TestBytesQueNoSonUTF8SeAvisan(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "latin.csv")
	// 0xF1 es la eñe en latin-1, y no es UTF-8 válido.
	if err := os.WriteFile(ruta, []byte("id,nombre\n1,se\xf1al\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	i, err := Inspect(ruta, Options{HasHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if !i.NotUTF8 {
		t.Error("no se avisó que el archivo no es UTF-8: importarlo escribiría basura en la base")
	}
}

func TestComillasYComasAdentroDeUnCampo(t *testing.T) {
	ruta := archivo(t, "id,nota\n1,\"con, coma\"\n2,\"dice \"\"hola\"\"\"\n3,\"dos\nlíneas\"\n")
	i, err := Inspect(ruta, Options{HasHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if i.Rows != 3 {
		t.Fatalf("Rows = %d, quería 3: un salto de línea adentro de comillas es UNA fila", i.Rows)
	}
	quiero := []string{"con, coma", `dice "hola"`, "dos\nlíneas"}
	for k, q := range quiero {
		if i.Sample[k][1] != q {
			t.Errorf("la fila %d trae %q y quería %q", k+1, i.Sample[k][1], q)
		}
	}
}

func TestVaciosYRecorte(t *testing.T) {
	ruta := archivo(t, "id,nombre\n1,\n2,  Ana  \n")

	// Sin opciones, el vacío es la cadena vacía y los espacios se respetan.
	i, err := Inspect(ruta, Options{HasHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if i.Sample[0][1] != "" || i.Sample[1][1] != "  Ana  " {
		t.Errorf("muestra: %q", i.Sample)
	}

	// Con recorte, los espacios se van.
	i, err = Inspect(ruta, Options{HasHeader: true, Trim: true})
	if err != nil {
		t.Fatal(err)
	}
	if i.Sample[1][1] != "Ana" {
		t.Errorf("con recorte: %q", i.Sample[1])
	}
}

// TestRowsEntregaNULLCuandoSePide es la diferencia que solo quien importa
// conoce: una planilla escribe igual la cadena vacía y el nulo.
func TestRowsEntregaNULLCuandoSePide(t *testing.T) {
	ruta := archivo(t, "id,nombre\n1,\n2,Ana\n")

	leer := func(o Options) [][]*string {
		var out [][]*string
		if err := Rows(ruta, o, func(_ int, f []*string) error {
			out = append(out, f)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}

	sinNull := leer(Options{HasHeader: true})
	if sinNull[0][1] == nil || *sinNull[0][1] != "" {
		t.Errorf("sin la opción, el campo vacío tiene que ser la cadena vacía: %v", sinNull[0][1])
	}
	conNull := leer(Options{HasHeader: true, EmptyAsNull: true})
	if conNull[0][1] != nil {
		t.Errorf("con la opción, el campo vacío tiene que ser NULL: %q", *conNull[0][1])
	}
	if conNull[1][1] == nil || *conNull[1][1] != "Ana" {
		t.Errorf("un campo con valor no puede volverse NULL")
	}
}

func TestRowsSalteaElEncabezadoYNumeraLasLineasDelArchivo(t *testing.T) {
	ruta := archivo(t, "id,nombre\n1,Ana\n2,Bruno\n")
	var lineas []int
	if err := Rows(ruta, Options{HasHeader: true}, func(l int, _ []*string) error {
		lineas = append(lineas, l)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// La numeración es la del ARCHIVO, no la de los datos: es lo que alguien
	// va a buscar en el editor de texto cuando una fila falle.
	if len(lineas) != 2 || lineas[0] != 2 || lineas[1] != 3 {
		t.Errorf("líneas = %v, quería [2 3]", lineas)
	}
}

func TestUnArchivoQueNoExiste(t *testing.T) {
	if _, err := Inspect(filepath.Join(t.TempDir(), "no-existe.csv"), Options{}); err == nil {
		t.Error("se aceptó un archivo que no existe")
	}
	if _, err := Inspect(t.TempDir(), Options{}); err == nil {
		t.Error("se aceptó una carpeta")
	}
}

func TestUnArchivoVacio(t *testing.T) {
	i, err := Inspect(archivo(t, ""), Options{HasHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if i.Rows != 0 || len(i.Columns) != 0 {
		t.Errorf("archivo vacío: %+v", i)
	}
}

// TestUnArchivoGrandeYValidoNoSeAvisaComoNoUTF8.
//
// El aviso se decide mirando un prefijo de 64 KiB, y el corte cae en un byte
// cualquiera. Si un carácter de varios bytes queda partido justo ahí, el
// archivo —perfectamente válido— se marcaba como «no es UTF-8» y el asistente
// mandaba a guardarlo de nuevo desde donde salió.
//
// El caso se construye a propósito: la eñe queda a caballo del límite del
// búfer, con su primer byte adentro y el segundo afuera.
func TestUnArchivoGrandeYValidoNoSeAvisaComoNoUTF8(t *testing.T) {
	const tope = 64 << 10
	cabecera := "id,nombre\n1,"
	relleno := strings.Repeat("a", tope-len(cabecera)-1)
	contenido := cabecera + relleno + "ñ" + strings.Repeat("b", 500) + "\n"

	i, err := Inspect(archivo(t, contenido), Options{HasHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if i.NotUTF8 {
		t.Error("un archivo UTF-8 válido se avisó como que no lo es, solo por ser más grande que el búfer")
	}

	// Y uno GRANDE que de verdad tiene basura se sigue avisando: el arreglo no
	// puede volverse una forma de no avisar nunca.
	//
	// El byte malo va en el medio del prefijo y no pegado al corte, y no es
	// para que el test pase: en el corte exacto la pregunta NO tiene respuesta.
	// Un `0xF1` en la última posición del búfer es a la vez el primer byte de
	// un carácter de cuatro que sigue afuera y una eñe de latin-1, y distinguir
	// los dos casos exige leer el archivo entero, que es justamente lo que este
	// prefijo evita. Se elige no avisar, porque el costo de los dos errores no
	// es el mismo: el falso aviso manda a rehacer un archivo que está bien.
	ruta := filepath.Join(t.TempDir(), "latin-grande.csv")
	crudo := []byte(cabecera + relleno[:len(relleno)/2])
	crudo = append(crudo, 0xF1)
	crudo = append(crudo, []byte(relleno[len(relleno)/2:]+"\n")...)
	if err := os.WriteFile(ruta, crudo, 0o600); err != nil {
		t.Fatal(err)
	}
	j, err := Inspect(ruta, Options{HasHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if !j.NotUTF8 {
		t.Error("un archivo grande con bytes que no son UTF-8 dejó de avisarse")
	}
}

// TestLaBasuraDespuesDelPrefijoTambienSeAvisa.
//
// El aviso se decidía sobre los primeros 64 KiB, y `Inspect` ya recorre el
// archivo entero para contar las filas. Un archivo grande cuya basura empieza
// más adelante se daba por bueno y se importaba como caracteres rotos — que es
// exactamente lo que este aviso existe para evitar.
func TestLaBasuraDespuesDelPrefijoTambienSeAvisa(t *testing.T) {
	ruta := filepath.Join(t.TempDir(), "tarde.csv")
	var b []byte
	b = append(b, []byte("id,nombre\n")...)
	// Bastante más que el prefijo de 64 KiB, todo limpio.
	for i := 0; i < 4000; i++ {
		b = append(b, []byte(fmt.Sprintf("%d,%s\n", i, strings.Repeat("a", 30)))...)
	}
	// Y recién acá, la eñe de latin-1.
	b = append(b, []byte("9999,se")...)
	b = append(b, 0xF1)
	b = append(b, []byte("al\n")...)
	if err := os.WriteFile(ruta, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if len(b) < 64<<10 {
		t.Fatalf("el archivo mide %d y tiene que pasar los 64 KiB para que el caso sirva", len(b))
	}

	i, err := Inspect(ruta, Options{HasHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if !i.NotUTF8 {
		t.Error("no se avisó de bytes que no son UTF-8 porque estaban después del prefijo")
	}
	// Y un archivo grande y limpio sigue sin avisar: el arreglo no puede
	// volverse un aviso permanente.
	limpio := archivo(t, "id,nombre\n"+strings.Repeat("1,áéíóú ñ\n", 8000))
	j, err := Inspect(limpio, Options{HasHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if j.NotUTF8 {
		t.Error("un archivo grande con acentos válidos se avisó como que no es UTF-8")
	}
}

// TestLaLineaEsLaFisicaAunqueUnCampoTengaSaltos.
//
// Un campo citado con un salto de línea adentro ocupa dos líneas del archivo.
// Contar registros desplazaba todos los números de ahí en adelante: «el lote
// que falló empieza en la línea N» apuntaba a otra fila en el editor de texto
// (C-28). La línea es la física, la que cualquier editor muestra.
func TestLaLineaEsLaFisicaAunqueUnCampoTengaSaltos(t *testing.T) {
	ruta := archivo(t, "id,nota\n1,\"dos\nlíneas\"\n2,una\n3,x,de más\n")
	var lineas []int
	if err := Rows(ruta, Options{HasHeader: true}, func(l int, _ []*string) error {
		lineas = append(lineas, l)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// El registro 2 arranca en la línea 4: la 2 y la 3 son el primero.
	if len(lineas) != 3 || lineas[0] != 2 || lineas[1] != 4 || lineas[2] != 5 {
		t.Errorf("líneas = %v, quería [2 4 5]", lineas)
	}
	insp, err := Inspect(ruta, Options{HasHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(insp.Ragged) != 1 || insp.Ragged[0].Line != 5 || insp.Ragged[0].Fields != 3 {
		t.Errorf("la despareja tenía que estar en la línea 5 con 3 campos: %+v", insp.Ragged)
	}
}
