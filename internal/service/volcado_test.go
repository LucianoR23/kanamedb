package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if prev.Header != texto[:len(prev.Header)] {
		t.Error("el encabezado de la vista previa no es el que se escribió")
	}
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
