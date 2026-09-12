package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/query"
)

// volcadoDePrueba deja una sesión abierta con dos tablas RELACIONADAS y
// devuelve el servicio, el esquema y los nombres.
//
// La madre y la hija son el punto: sin la clave foránea el orden del volcado no
// prueba nada, porque cualquier orden serviría.
func volcadoDePrueba(t *testing.T, motor, uri, base string) (*Dumps, *Session, string, string, string) {
	t.Helper()
	sesion, c := sesionDe(t, motor, uri)
	ctx := context.Background()
	esq := esquemaDeApply(t, sesion, c)
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatalf("abierta(): %v", err)
	}

	// La hija se llama de forma que quede ANTES alfabéticamente: así, si el
	// orden topológico no existiera, el test fallaría en vez de pasar de
	// casualidad por el orden de los nombres.
	madre := base + "_zmadre"
	hija := base + "_ahija"
	nomMadre, nomHija := califica(c, esq, madre), califica(c, esq, hija)

	limpiar := func() {
		_ = abierta.db.Exec(context.Background(), "DROP TABLE IF EXISTS "+nomHija)
		_ = abierta.db.Exec(context.Background(), "DROP TABLE IF EXISTS "+nomMadre)
	}
	limpiar()
	t.Cleanup(limpiar)

	ent, txt := tipoEnteroDe(c.Engine), tipoTextoDe(c.Engine)
	if err := abierta.db.Exec(ctx, fmt.Sprintf(
		"CREATE TABLE %s (id %s PRIMARY KEY, nombre %s)", nomMadre, ent, txt)); err != nil {
		t.Fatalf("crear la madre: %v", err)
	}
	// La clave va como restricción DE TABLA: MySQL acepta la forma pegada a la
	// columna y después la ignora en InnoDB, así que el test daría verde sin
	// haber probado la relación.
	if err := abierta.db.Exec(ctx, fmt.Sprintf(
		"CREATE TABLE %s (id %s PRIMARY KEY, madre_id %s, FOREIGN KEY (madre_id) REFERENCES %s (id))",
		nomHija, ent, ent, nomMadre)); err != nil {
		t.Fatalf("crear la hija: %v", err)
	}
	if err := abierta.db.Exec(ctx, fmt.Sprintf(
		"INSERT INTO %s (id, nombre) VALUES (1, 'una')", nomMadre)); err != nil {
		t.Fatalf("cargar la madre: %v", err)
	}
	if err := abierta.db.Exec(ctx, fmt.Sprintf(
		"INSERT INTO %s (id, madre_id) VALUES (10, 1)", nomHija)); err != nil {
		t.Fatalf("cargar la hija: %v", err)
	}

	consultas := NewQueries(sesion)
	return NewDumps(NewExports(consultas), consultas), sesion, esq, hija, madre
}

func leer(t *testing.T, ruta string) string {
	t.Helper()
	b, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestElVolcadoPoneLaMadreAntesQueLaHija.
//
// Es lo que separa un volcado de datos de una exportación de varias tablas: el
// archivo tiene que poder volver a correrse, y las filas de la hija no entran
// antes que las de la madre.
func TestElVolcadoPoneLaMadreAntesQueLaHija(t *testing.T) {
	for _, caso := range motoresDeDatos {
		t.Run(caso.nombre, func(t *testing.T) {
			d, _, esq, hija, madre := volcadoDePrueba(t, caso.nombre, caso.uri, "kn_dump")
			ruta := filepath.Join(t.TempDir(), "volcado.sql")

			info, err := d.Save(context.Background(), DumpRequest{
				RunID: "v1", Schemas: esquemasPedidos(esq), Structure: true, Data: true,
			}, ruta)
			if err != nil {
				t.Fatalf("Save(): %v", err)
			}
			if info.Bytes == 0 {
				t.Fatal("el archivo salió vacío")
			}

			texto := leer(t, ruta)
			// Se busca por el comentario que el volcado escribe antes de cada
			// tabla: es igual en los cuatro motores, mientras que el INSERT
			// lleva las comillas propias de cada uno.
			datos := texto[strings.Index(texto, "-- Datos"):]
			iMadre := donde(datos, madre)
			iHija := donde(datos, hija)
			if iMadre < 0 || iHija < 0 {
				t.Fatalf("no están las dos tablas (madre=%d hija=%d):\n%s", iMadre, iHija, recorte(texto))
			}
			if iMadre > iHija {
				t.Errorf("la hija se vuelca antes que la madre: el archivo no se puede volver a correr")
			}
			// Y las filas están: un orden correcto sobre un archivo vacío no
			// prueba nada.
			if !strings.Contains(datos, "INSERT INTO") {
				t.Errorf("no se escribió ningún INSERT:\n%s", recorte(texto))
			}
		})
	}
}

// TestLasClavesForaneasVanDespuesDeTodasLasTablas.
//
// Pegada a su CREATE, una clave apunta a una tabla que puede no existir
// todavía y el archivo falla al correrse. Al final deja de importar incluso el
// ciclo, que es lo único que el orden de inserción no resuelve.
func TestLasClavesForaneasVanDespuesDeTodasLasTablas(t *testing.T) {
	d, _, esq, hija, madre := volcadoDePrueba(t, "postgres", motoresDeDatos[0].uri, "kn_dump_fk")
	ruta := filepath.Join(t.TempDir(), "volcado.sql")
	if _, err := d.Save(context.Background(), DumpRequest{
		RunID: "v2", Schemas: esquemasPedidos(esq), Structure: true,
	}, ruta); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	texto := leer(t, ruta)

	iFK := strings.Index(texto, "FOREIGN KEY")
	if iFK < 0 {
		t.Fatalf("no se escribió la clave foránea:\n%s", recorte(texto))
	}
	for _, tabla := range []string{madre, hija} {
		iCreate := strings.Index(texto, "CREATE TABLE")
		if iCreate < 0 {
			t.Fatalf("no hay CREATE TABLE de %s", tabla)
		}
	}
	// La última tabla creada tiene que estar ANTES de la primera clave.
	iUltimoCreate := strings.LastIndex(texto, "CREATE TABLE")
	if iUltimoCreate > iFK {
		t.Errorf("una tabla se crea después de una clave foránea: el archivo falla al correrse")
	}
	if !strings.Contains(texto, "Claves foráneas") {
		t.Errorf("falta la sección de claves foráneas:\n%s", recorte(texto))
	}
}

// TestElEncabezadoDelArchivoDiceQueDejaAfuera.
//
// La cobertura declarada es la condición para que el volcado de estructura
// exista, y tiene que estar EN EL ARCHIVO: quien lo abre dentro de seis meses
// no tiene la pantalla al lado.
func TestElEncabezadoDelArchivoDiceQueDejaAfuera(t *testing.T) {
	d, sesion, esq, hija, _ := volcadoDePrueba(t, "postgres", motoresDeDatos[0].uri, "kn_dump_cob")
	abierta, _ := sesion.abierta()
	ctx := context.Background()

	vista := "kn_dump_cob_vista"
	completo := fmt.Sprintf(`"%s"."%s"`, esq, vista)
	if err := abierta.db.Exec(ctx, fmt.Sprintf(
		`CREATE VIEW %s AS SELECT id FROM "%s"."%s"`, completo, esq, hija)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = abierta.db.Exec(context.Background(), "DROP VIEW IF EXISTS "+completo) })

	ruta := filepath.Join(t.TempDir(), "volcado.sql")
	if _, err := d.Save(ctx, DumpRequest{
		RunID: "v3", Schemas: esquemasPedidos(esq), Structure: true,
	}, ruta); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	texto := leer(t, ruta)

	if !strings.Contains(texto, "LO QUE NO ESTÁ EN ESTE ARCHIVO") {
		t.Errorf("el archivo no declara su cobertura:\n%s", recorte(texto))
	}
	if !strings.Contains(texto, vista) {
		t.Errorf("la vista %q no se nombra en el encabezado:\n%s", vista, recorte(texto))
	}

	// Y la pantalla ve lo mismo antes de escribir: enterarse al abrir el
	// archivo sería tarde.
	prev, err := d.Preview(ctx, DumpRequest{
		RunID: "v3p", Schemas: esquemasPedidos(esq), Structure: true,
	})
	if err != nil {
		t.Fatalf("Preview(): %v", err)
	}
	if prev.Summary == "" || !strings.Contains(strings.Join(prev.Detail, " "), vista) {
		t.Errorf("la vista previa no nombra la vista: %q %v", prev.Summary, prev.Detail)
	}
	// El encabezado de la vista previa tiene que ser EL QUE SE ESCRIBE, salvo
	// la fecha: son dos llamadas distintas y cada una pone la suya, que es lo
	// correcto —la del archivo es cuándo se escribió—. Compararlos enteros
	// hacía que el test fallara cuando las dos caían en segundos distintos, y
	// pasara el resto de las veces: un test intermitente es peor que ninguno.
	if sinFecha(prev.Header) != sinFecha(texto[:len(prev.Header)]) {
		t.Errorf("el encabezado de la vista previa no es el que se escribió:\n%s\n---\n%s",
			prev.Header, texto[:len(prev.Header)])
	}
}

// sinFecha saca el renglón del «Generado por», que lleva la hora.
func sinFecha(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "-- Generado por") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// TestUnVolcadoSinEstructuraNiDatosSeRechaza: no escribiría nada, y un archivo
// vacío con nombre de volcado es peor que un error.
func TestUnVolcadoSinEstructuraNiDatosSeRechaza(t *testing.T) {
	d, _, esq, _, _ := volcadoDePrueba(t, "postgres", motoresDeDatos[0].uri, "kn_dump_nada")
	ruta := filepath.Join(t.TempDir(), "volcado.sql")
	if _, err := d.Save(context.Background(), DumpRequest{
		RunID: "v4", Schemas: esquemasPedidos(esq),
	}, ruta); err == nil {
		t.Fatal("se aceptó un volcado que no lleva nada")
	}
	if _, err := os.Stat(ruta); err == nil {
		t.Error("quedó un archivo de un volcado que se rechazó")
	}
}

// TestElArchivoNoLlevaCredenciales es un requisito duro: el volcado se manda
// por correo y se sube a un ticket.
func TestElArchivoNoLlevaCredenciales(t *testing.T) {
	d, sesion, esq, _, _ := volcadoDePrueba(t, "postgres", motoresDeDatos[0].uri, "kn_dump_cred")
	abierta, _ := sesion.abierta()
	ruta := filepath.Join(t.TempDir(), "volcado.sql")
	if _, err := d.Save(context.Background(), DumpRequest{
		RunID: "v5", Schemas: esquemasPedidos(esq), Structure: true, Data: true,
	}, ruta); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	texto := leer(t, ruta)
	// La contraseña de los contenedores de prueba. No está en la Connection
	// —vive en el keychain, que es el punto— así que se busca el literal.
	if strings.Contains(texto, "password") || strings.Contains(texto, ":kaname@") {
		t.Error("hay algo que parece una credencial en el volcado")
	}
	for _, prohibido := range []string{"postgres://", "password=", "sslmode="} {
		if strings.Contains(texto, prohibido) {
			t.Errorf("el volcado tiene %q, que es parte de una DSN", prohibido)
		}
	}
	// Y sí está el origen sin credenciales.
	if !strings.Contains(texto, abierta.conn.Describe()) {
		t.Errorf("falta el origen en el encabezado:\n%s", recorte(texto))
	}
}

// esquemasPedidos arma la lista para el pedido. El esquema vacío de SQLite se
// manda como lista vacía, que significa «todos los que haya».
func esquemasPedidos(esq string) []string {
	if esq == "" {
		return nil
	}
	return []string{esq}
}

// donde busca el comentario que el volcado escribe antes de cada tabla.
//
// El esquema NO se compone acá: SQLite lo llama `main` aunque el test no le
// pase ninguno, así que armar `esq + "." + tabla` no encontraría nada. Se busca
// el renglón que TERMINA con el nombre de la tabla, que es igual en los cuatro
// motores.
func donde(texto, tabla string) int {
	for _, forma := range []string{"-- " + tabla + "\n", "." + tabla + "\n"} {
		if i := strings.Index(texto, forma); i >= 0 {
			return i
		}
	}
	return -1
}

func recorte(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "\n…"
	}
	return s
}

// TestElVolcadoSePuedeVolverACorrer.
//
// Es la prueba que importa y la única que cubre todo junto: se vuelca un
// esquema entero, se BORRA, se corre el archivo y se comprueba que las tablas y
// las filas volvieron. Todo lo demás —el orden, las claves al final, la clave
// primaria adentro del CREATE— existe para que esto funcione, y comprobarlo por
// separado con `strings.Contains` deja pasar justo lo que el archivo tiene de
// difícil: que las piezas encajen entre sí.
//
// Va contra Postgres porque restaurar es distinto en cada motor y este es el
// principal. Las piezas de abajo sí se prueban en los cuatro.
func TestElVolcadoSePuedeVolverACorrer(t *testing.T) {
	sesion, _ := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	abierta, _ := sesion.abierta()
	consultas := NewQueries(sesion)
	d := NewDumps(NewExports(consultas), consultas)

	esq := "kn_ida_vuelta"
	crearEsquema := func() {
		if err := abierta.db.Exec(ctx, `CREATE SCHEMA `+esq); err != nil {
			t.Fatalf("crear el esquema: %v", err)
		}
	}
	_ = abierta.db.Exec(ctx, `DROP SCHEMA IF EXISTS `+esq+` CASCADE`)
	crearEsquema()
	t.Cleanup(func() {
		_ = abierta.db.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+esq+` CASCADE`)
	})

	// Una madre, una hija que la referencia, y un índice: lo suficiente para
	// que el orden y el lugar de las claves importen de verdad.
	for _, ddl := range []string{
		`CREATE TABLE ` + esq + `.madre (id int PRIMARY KEY, nombre text NOT NULL)`,
		`CREATE TABLE ` + esq + `.hija (id int PRIMARY KEY, madre_id int REFERENCES ` + esq + `.madre(id), nota text)`,
		`CREATE INDEX hija_nota ON ` + esq + `.hija (nota)`,
		`INSERT INTO ` + esq + `.madre VALUES (1, 'Una'), (2, 'Dos')`,
		`INSERT INTO ` + esq + `.hija VALUES (10, 1, 'a'), (11, 2, 'b'), (12, 1, NULL)`,
	} {
		if err := abierta.db.Exec(ctx, ddl); err != nil {
			t.Fatalf("preparar con %q: %v", ddl, err)
		}
	}

	ruta := filepath.Join(t.TempDir(), "ida.sql")
	if _, err := d.Save(ctx, DumpRequest{
		RunID: "ida", Schemas: []string{esq}, Structure: true, Data: true,
	}, ruta); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	guion := leer(t, ruta)

	// Se borra TODO y se vuelve a crear el esquema vacío, que es lo que hace
	// cualquiera al restaurar. El archivo tiene que poder con el resto.
	if err := abierta.db.Exec(ctx, `DROP SCHEMA `+esq+` CASCADE`); err != nil {
		t.Fatalf("borrar el esquema: %v", err)
	}
	crearEsquema()

	if err := abierta.db.Exec(ctx, guion); err != nil {
		t.Fatalf("el volcado NO se puede volver a correr: %v\n\n%s", err, recorte(guion))
	}

	// Y volvió todo: las filas, la clave primaria, la foránea y el índice.
	if n, _ := abierta.db.Count(ctx, esq, "madre", nil); n != 2 {
		t.Errorf("la madre quedó con %d filas, quería 2", n)
	}
	if n, _ := abierta.db.Count(ctx, esq, "hija", nil); n != 3 {
		t.Errorf("la hija quedó con %d filas, quería 3", n)
	}
	detalle, err := abierta.db.Detail(ctx, esq, "hija")
	if err != nil {
		t.Fatalf("Detail(): %v", err)
	}
	if len(detalle.ForeignKeys) != 1 {
		t.Errorf("la clave foránea no volvió: %+v", detalle.ForeignKeys)
	}
	var conPK, conIndice bool
	for _, ix := range detalle.Indexes {
		if ix.Primary {
			conPK = true
		}
		if ix.Name == "hija_nota" {
			conIndice = true
		}
	}
	if !conPK {
		t.Error("la clave primaria no volvió")
	}
	if !conIndice {
		t.Error("el índice no volvió")
	}
	// El NULL siguió siendo NULL y no la cadena «NULL», que es el error clásico
	// de un volcado que arma los INSERT a mano.
	nulos, fallo := abierta.db.Count(ctx, esq, "hija", []query.Condition{
		{Column: "nota", Operator: query.OpIsNull},
	})
	if fallo != nil {
		t.Fatalf("contar los nulos: %+v", fallo)
	}
	if nulos != 1 {
		t.Errorf("quedaron %d filas con nota NULL, quería 1: el NULL se escribió como texto", nulos)
	}
}

// TestUnSerialYUnaColumnaGeneradaSobrevivenAlVolcado.
//
// Los dos casos que hacían un archivo roto Y silencioso, que es la peor
// combinación posible:
//
//   - Un `serial` llegaba como `integer DEFAULT nextval('t_id_seq')` y se
//     escribía tal cual. El archivo apuntaba a una secuencia que nunca creaba,
//     así que no se podía correr — y la cobertura decía que no faltaba nada.
//   - Una columna GENERADA se saltea del CREATE TABLE (el catálogo no da la
//     expresión) pero el INSERT la incluía igual, porque los datos se leían con
//     `SELECT *`. El archivo fallaba con «column "total" does not exist».
//
// Se prueba de la única forma que cierra el caso: borrando el esquema y
// corriendo el archivo.
func TestUnSerialYUnaColumnaGeneradaSobrevivenAlVolcado(t *testing.T) {
	sesion, _ := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	abierta, _ := sesion.abierta()
	consultas := NewQueries(sesion)
	d := NewDumps(NewExports(consultas), consultas)

	esq := "kn_serial"
	_ = abierta.db.Exec(ctx, `DROP SCHEMA IF EXISTS `+esq+` CASCADE`)
	if err := abierta.db.Exec(ctx, `CREATE SCHEMA `+esq); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = abierta.db.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+esq+` CASCADE`) })
	if err := abierta.db.Exec(ctx, `CREATE TABLE `+esq+`.t (
		id serial PRIMARY KEY,
		precio numeric(10,2),
		cantidad int,
		total numeric(10,2) GENERATED ALWAYS AS (precio * cantidad) STORED)`); err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, `INSERT INTO `+esq+`.t (precio, cantidad) VALUES (10.00, 3), (2.50, 4)`); err != nil {
		t.Fatal(err)
	}

	ruta := filepath.Join(t.TempDir(), "serial.sql")
	if _, err := d.Save(ctx, DumpRequest{
		RunID: "s1", Schemas: []string{esq}, Structure: true, Data: true,
	}, ruta); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	guion := leer(t, ruta)

	// El serial se escribe como serial, no como el nextval de una secuencia
	// que el archivo no crea.
	if strings.Contains(guion, "nextval(") {
		t.Errorf("el volcado apunta a una secuencia que no crea:\n%s", recorte(guion))
	}
	if !strings.Contains(guion, `"id" serial`) {
		t.Errorf("el serial no se escribió como serial:\n%s", recorte(guion))
	}
	// Y la generada NO está en el INSERT.
	datos := guion[strings.Index(guion, "-- Datos"):]
	if strings.Contains(datos, `"total"`) {
		t.Errorf("la columna generada entró en el INSERT:\n%s", datos)
	}

	// La cobertura la nombra: el archivo no la tiene y hay que decirlo.
	prev, err := d.Preview(ctx, DumpRequest{RunID: "s2", Schemas: []string{esq}, Structure: true})
	if err != nil {
		t.Fatalf("Preview(): %v", err)
	}
	if !strings.Contains(strings.Join(prev.Detail, " "), "t.total") {
		t.Errorf("la columna generada no se nombra en la cobertura: %q %v", prev.Summary, prev.Detail)
	}

	// Y todo junto: se borra y se vuelve a correr.
	if err := abierta.db.Exec(ctx, `DROP SCHEMA `+esq+` CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, `CREATE SCHEMA `+esq); err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, guion); err != nil {
		t.Fatalf("el volcado NO se puede volver a correr: %v\n\n%s", err, recorte(guion))
	}
	if n, _ := abierta.db.Count(ctx, esq, "t", nil); n != 2 {
		t.Errorf("volvieron %d filas, quería 2", n)
	}
	// La columna generada volvió a existir y con su valor calculado: se
	// perdió la definición, no los datos.
	detalle, err := abierta.db.Detail(ctx, esq, "t")
	if err != nil {
		t.Fatal(err)
	}
	var hayTotal, haySerial bool
	for _, c := range detalle.Columns {
		if c.Name == "total" {
			hayTotal = true
		}
		if c.Name == "id" && strings.HasPrefix(c.Default, "nextval(") {
			haySerial = true
		}
	}
	if hayTotal {
		t.Error("la columna generada volvió: el volcado no la escribe, así que no puede aparecer")
	}
	if !haySerial {
		t.Errorf("el serial no volvió a numerar solo: %+v", detalle.Columns)
	}
}

// TestLasSecuenciasVuelvenPosicionadasYLaIdentityAlwaysSeRestaura.
//
// Dos maneras de que un volcado de Postgres «correcto» falle después:
//
//   - Una columna `GENERATED ALWAYS AS IDENTITY` rechaza el INSERT con valor
//     explícito (428C9) salvo `OVERRIDING SYSTEM VALUE`: el archivo no se podía
//     correr.
//   - Con `serial` sí corría, pero ninguna secuencia quedaba posicionada: el
//     primer INSERT sin id sobre la base restaurada chocaba con una clave que
//     ya existía, una vez por cada fila vieja. En una restauración de
//     producción eso aparece semanas después.
//
// Hallazgo C-05 de la auditoría del 2026-09-11. Se prueba de la única forma
// que cierra el caso: restaurar e insertar sin id.
func TestLasSecuenciasVuelvenPosicionadasYLaIdentityAlwaysSeRestaura(t *testing.T) {
	sesion, _ := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	abierta, _ := sesion.abierta()
	consultas := NewQueries(sesion)
	d := NewDumps(NewExports(consultas), consultas)

	esq := "kn_secuencias"
	_ = abierta.db.Exec(ctx, `DROP SCHEMA IF EXISTS `+esq+` CASCADE`)
	if err := abierta.db.Exec(ctx, `CREATE SCHEMA `+esq); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = abierta.db.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+esq+` CASCADE`) })
	for _, sql := range []string{
		`CREATE TABLE ` + esq + `.siempre (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, n text)`,
		`CREATE TABLE ` + esq + `.pordefecto (id int GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY, n text)`,
		`CREATE TABLE ` + esq + `.viejo (id serial PRIMARY KEY, n text)`,
		`INSERT INTO ` + esq + `.siempre (n) VALUES ('a'), ('b'), ('c')`,
		`INSERT INTO ` + esq + `.pordefecto (n) VALUES ('a'), ('b')`,
		`INSERT INTO ` + esq + `.viejo (n) VALUES ('a'), ('b'), ('c'), ('d')`,
	} {
		if err := abierta.db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}

	ruta := filepath.Join(t.TempDir(), "secuencias.sql")
	if _, err := d.Save(ctx, DumpRequest{
		RunID: "sq", Schemas: []string{esq}, Structure: true, Data: true,
	}, ruta); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	guion := leer(t, ruta)
	if !strings.Contains(guion, "OVERRIDING SYSTEM VALUE") {
		t.Errorf("la identity ALWAYS se inserta sin OVERRIDING SYSTEM VALUE:\n%s", recorte(guion))
	}
	if n := strings.Count(guion, "setval("); n != 3 {
		t.Errorf("se esperaban 3 setval (uno por columna que se numera sola) y hay %d:\n%s", n, recorte(guion))
	}

	if err := abierta.db.Exec(ctx, `DROP SCHEMA `+esq+` CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, `CREATE SCHEMA `+esq); err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, guion); err != nil {
		t.Fatalf("el volcado NO se puede volver a correr: %v\n\n%s", err, recorte(guion))
	}
	// Las filas volvieron con sus ids, y el próximo INSERT sin id no choca.
	for tabla, siguiente := range map[string]string{"siempre": "4", "pordefecto": "3", "viejo": "5"} {
		if err := abierta.db.Exec(ctx, `INSERT INTO `+esq+`.`+tabla+` (n) VALUES ('nueva')`); err != nil {
			t.Errorf("INSERT sin id en %s después de restaurar: %v (la secuencia no se reposicionó)", tabla, err)
			continue
		}
		res := consultas.Run(ctx, "sq2", `SELECT max(id)::text FROM `+esq+`.`+tabla)
		if !res.OK || len(res.Batch.Results[0].Rows) != 1 || *res.Batch.Results[0].Rows[0][0] != siguiente {
			t.Errorf("%s: el id nuevo tenía que ser %s: %s", tabla, siguiente, mensajeDe(res))
		}
	}
}

// TestDropFirstSirveSobreUnaBaseQueYaTieneLasTablas.
//
// El único motivo para pedir «DROP primero» es correr el archivo sobre una
// base donde las tablas ya existen. Los DROP salían intercalados con los
// CREATE y en orden madre → hija, que es el único orden en que fallan: `DROP
// TABLE madre` con `hija` todavía apuntándole (C-10). Todos juntos al
// principio, hijas primero.
func TestDropFirstSirveSobreUnaBaseQueYaTieneLasTablas(t *testing.T) {
	d, sesion, esq, hija, madre := volcadoDePrueba(t, "postgres", motoresDeDatos[0].uri, "kn_dropfirst")
	ctx := context.Background()
	abierta, _ := sesion.abierta()

	ruta := filepath.Join(t.TempDir(), "drop.sql")
	if _, err := d.Save(ctx, DumpRequest{
		RunID: "df", Schemas: []string{esq}, Structure: true, Data: true, DropFirst: true,
	}, ruta); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	guion := leer(t, ruta)
	// Los DROP van antes que el primer CREATE, y la hija antes que la madre.
	primerCreate := strings.Index(guion, "CREATE TABLE")
	dropHija := strings.Index(guion, `DROP TABLE IF EXISTS "`+esq+`"."`+hija+`"`)
	dropMadre := strings.Index(guion, `DROP TABLE IF EXISTS "`+esq+`"."`+madre+`"`)
	if dropHija < 0 || dropMadre < 0 || dropHija > dropMadre || dropMadre > primerCreate {
		t.Errorf("los DROP no van todos primero y en orden hija → madre:\n%s", recorte(guion))
	}
	antes, _ := abierta.db.Count(ctx, esq, hija, nil)
	// Y lo que importa: corre SIN borrar nada antes.
	if err := abierta.db.Exec(ctx, guion); err != nil {
		t.Fatalf("el volcado con DROP primero no corre sobre la base que ya tiene las tablas: %v\n\n%s",
			err, recorte(guion))
	}
	if n, _ := abierta.db.Count(ctx, esq, hija, nil); n != antes {
		t.Errorf("la hija quedó con %d filas, quería %d", n, antes)
	}
}

// TestUnaClaveHaciaOtroEsquemaNoVaAlArchivoYSeNombra.
//
// `Orden` ya ignoraba las aristas hacia tablas fuera del volcado; la sección
// de claves foráneas no, y renderizaba todas: sobre una base vacía, `ADD
// FOREIGN KEY … REFERENCES otro_esquema.t` falla y la cobertura no lo
// nombraba (C-25).
func TestUnaClaveHaciaOtroEsquemaNoVaAlArchivoYSeNombra(t *testing.T) {
	sesion, _ := sesionDe(t, "postgres", motoresDeDatos[0].uri)
	ctx := context.Background()
	abierta, _ := sesion.abierta()
	consultas := NewQueries(sesion)
	d := NewDumps(NewExports(consultas), consultas)

	for _, sql := range []string{
		`DROP SCHEMA IF EXISTS kn_fk_a CASCADE`, `DROP SCHEMA IF EXISTS kn_fk_b CASCADE`,
		`CREATE SCHEMA kn_fk_a`, `CREATE SCHEMA kn_fk_b`,
		`CREATE TABLE kn_fk_b.padre (id int PRIMARY KEY)`,
		`CREATE TABLE kn_fk_a.hija (id int PRIMARY KEY, padre_id int CONSTRAINT hija_padre_fk REFERENCES kn_fk_b.padre(id))`,
	} {
		if err := abierta.db.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = abierta.db.Exec(context.Background(), `DROP SCHEMA IF EXISTS kn_fk_a CASCADE`)
		_ = abierta.db.Exec(context.Background(), `DROP SCHEMA IF EXISTS kn_fk_b CASCADE`)
	})

	ruta := filepath.Join(t.TempDir(), "fk.sql")
	if _, err := d.Save(ctx, DumpRequest{
		RunID: "fk", Schemas: []string{"kn_fk_a"}, Structure: true,
	}, ruta); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	guion := leer(t, ruta)
	if strings.Contains(guion, "FOREIGN KEY") {
		t.Errorf("la clave hacia un esquema que no está en el archivo se escribió igual:\n%s", recorte(guion))
	}
	prev, err := d.Preview(ctx, DumpRequest{RunID: "fk2", Schemas: []string{"kn_fk_a"}, Structure: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(prev.Detail, " "), "hija_padre_fk") {
		t.Errorf("la cobertura no nombra la clave que quedó afuera: %q %v", prev.Summary, prev.Detail)
	}
	// Y el archivo corre sobre el esquema vacío.
	if err := abierta.db.Exec(ctx, `DROP SCHEMA kn_fk_a CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, `CREATE SCHEMA kn_fk_a`); err != nil {
		t.Fatal(err)
	}
	if err := abierta.db.Exec(ctx, guion); err != nil {
		t.Fatalf("el volcado no corre: %v\n%s", err, recorte(guion))
	}
}
