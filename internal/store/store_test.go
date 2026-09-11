package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/connection"
)

func nuevo(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "connections.toml"))
}

func conn(id, name string) connection.Connection {
	return connection.Connection{
		ID:          id,
		Name:        name,
		Engine:      connection.Postgres,
		Host:        "db.local",
		Port:        5432,
		Database:    "shop",
		User:        "rw",
		Environment: connection.Dev,
		SSLMode:     connection.SSLRequire,
	}
}

// No haber configurado nada todavía es un estado normal, no una falla.
func TestListSinArchivoDevuelveVacio(t *testing.T) {
	got, err := nuevo(t).List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("List() = %d conexiones, se esperaba 0", len(got))
	}
}

func TestAddYGetIdaYVuelta(t *testing.T) {
	s := nuevo(t)
	want := conn("a1", "shop dev")
	if err := s.Add(want); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	got, err := s.Get("a1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got != want {
		t.Errorf("Get() = %+v\nse esperaba %+v", got, want)
	}
}

func TestListOrdenaPorNombreSinDistinguirMayusculas(t *testing.T) {
	s := nuevo(t)
	for _, c := range []connection.Connection{
		conn("3", "zeta"), conn("1", "Alfa"), conn("2", "beta"),
	} {
		if err := s.Add(c); err != nil {
			t.Fatalf("Add() error: %v", err)
		}
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	nombres := make([]string, 0, len(got))
	for _, c := range got {
		nombres = append(nombres, c.Name)
	}
	if want := "Alfa,beta,zeta"; strings.Join(nombres, ",") != want {
		t.Errorf("orden = %v, se esperaba %v", nombres, want)
	}
}

func TestAddRechazaIdDuplicado(t *testing.T) {
	s := nuevo(t)
	if err := s.Add(conn("a1", "uno")); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if err := s.Add(conn("a1", "otro")); !errors.Is(err, ErrDuplicateID) {
		t.Errorf("Add() duplicado devolvió %v, se esperaba ErrDuplicateID", err)
	}
}

func TestAddRechazaConexionInvalida(t *testing.T) {
	s := nuevo(t)
	err := s.Add(conn("a1", ""))
	var ve *connection.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Add() devolvió %T (%v), se esperaba *ValidationError", err, err)
	}
	// Y no debe haber escrito nada.
	if _, statErr := os.Stat(s.Path()); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("Add() inválido creó el archivo igual")
	}
}

func TestUpdateReemplazaYFallaSiNoExiste(t *testing.T) {
	s := nuevo(t)
	if err := s.Add(conn("a1", "viejo")); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	c := conn("a1", "nuevo")
	c.Database = "otra_base"
	if err := s.Update(c); err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	got, err := s.Get("a1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Name != "nuevo" || got.Database != "otra_base" {
		t.Errorf("Update() no aplicó los cambios: %+v", got)
	}

	if err := s.Update(conn("zz", "fantasma")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Update() inexistente devolvió %v, se esperaba ErrNotFound", err)
	}
}

func TestDeleteQuitaYFallaSiNoExiste(t *testing.T) {
	s := nuevo(t)
	for _, c := range []connection.Connection{conn("a1", "uno"), conn("a2", "dos")} {
		if err := s.Add(c); err != nil {
			t.Fatalf("Add() error: %v", err)
		}
	}
	if err := s.Delete("a1"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if _, err := s.Get("a1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("después de borrar, Get() devolvió %v", err)
	}
	if got, _ := s.List(); len(got) != 1 {
		t.Errorf("quedaron %d conexiones, se esperaba 1", len(got))
	}
	if err := s.Delete("zz"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete() inexistente devolvió %v, se esperaba ErrNotFound", err)
	}
}

// El archivo se sincroniza entre máquinas: no puede tener nada secreto.
func TestElArchivoNoContieneSecretos(t *testing.T) {
	s := nuevo(t)
	if err := s.Add(conn("a1", "shop dev")); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("no se pudo leer el archivo: %v", err)
	}
	texto := strings.ToLower(string(data))
	for _, p := range []string{"password", "passwd", "secret", "token", "credential"} {
		if strings.Contains(texto, p) {
			t.Errorf("el archivo contiene %q:\n%s", p, data)
		}
	}
}

func TestArchivoCorruptoDaUnErrorClaro(t *testing.T) {
	s := nuevo(t)
	escribirCrudo(t, s.Path(), "esto no es toml [[[\n")

	_, err := s.List()
	if err == nil {
		t.Fatal("List() sobre un archivo corrupto no devolvió error")
	}
	if !strings.Contains(err.Error(), "corrupto") {
		t.Errorf("el error no dice qué pasó: %v", err)
	}
}

// Un archivo escrito por una versión futura no se debe leer a medias ni, peor,
// sobrescribir perdiendo campos que esta build no entiende.
func TestRechazaUnaVersionMasNuevaDelArchivo(t *testing.T) {
	s := nuevo(t)
	escribirCrudo(t, s.Path(), "version = 99\n")

	_, err := s.List()
	if err == nil {
		t.Fatal("List() aceptó un archivo de una versión futura")
	}
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("el error no menciona la versión encontrada: %v", err)
	}
}

// Una escritura interrumpida no puede dejar al usuario sin sus conexiones, y
// tampoco puede ir dejando temporales tirados en el directorio.
func TestLaEscrituraNoDejaTemporales(t *testing.T) {
	s := nuevo(t)
	for _, c := range []connection.Connection{conn("a1", "uno"), conn("a2", "dos")} {
		if err := s.Add(c); err != nil {
			t.Fatalf("Add() error: %v", err)
		}
	}
	entradas, err := os.ReadDir(filepath.Dir(s.Path()))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entradas {
		if e.Name() != filepath.Base(s.Path()) {
			t.Errorf("quedó un archivo suelto en el directorio: %s", e.Name())
		}
	}
}

// Los servicios de Wails corren en goroutines distintas y varias ventanas
// pueden tocar la lista a la vez.
func TestUsoConcurrente(t *testing.T) {
	s := nuevo(t)
	if err := s.Add(conn("base", "base")); err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	errs := make(chan error, n*3)
	for i := 0; i < n; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			_, err := s.List()
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := s.Get("base")
			errs <- err
		}()
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("id%02d", i)
			errs <- s.Add(conn(id, "n"+id))
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("operación concurrente falló: %v", err)
		}
	}
	got, err := s.List()
	if err != nil {
		t.Fatalf("List() final: %v", err)
	}
	if len(got) != n+1 {
		t.Errorf("quedaron %d conexiones, se esperaban %d", len(got), n+1)
	}
}

// Guardar y volver a leer no puede cambiar nada, ni siquiera un campo que la UI
// todavía no muestra.
func TestIdaYVueltaPorDiscoPreservaTodosLosCampos(t *testing.T) {
	s := nuevo(t)
	want := connection.Connection{
		ID:          "a1",
		Name:        "prod réplica",
		Engine:      connection.Postgres,
		Host:        "prod-db.internal",
		Port:        6432,
		Database:    "shop_prod",
		User:        "app_ro",
		Environment: connection.Production,
		Folder:      "Shop",
		Safety:      connection.Safety{ReadOnly: true},
		SSLMode:     connection.SSLVerifyFull,
		TLS: connection.TLS{
			RootCertPath:   "~/.postgresql/root.crt",
			ClientCertPath: `C:\certs\app.crt`,
			ClientKeyPath:  `C:\certs\app.key`,
		},
	}
	if err := s.Add(want); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	// Un Store nuevo, para forzar la lectura desde disco.
	got, err := New(s.Path()).Get("a1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got != want {
		t.Errorf("ida y vuelta cambió la conexión:\n got %+v\nwant %+v", got, want)
	}
}

func escribirCrudo(t *testing.T, path, contenido string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contenido), 0o600); err != nil {
		t.Fatal(err)
	}
}

// El ID es la clave de la contraseña en el keychain y la que resuelven Get,
// Update y Delete. Con IDs repetidos, editar una conexión tocaría la otra.
func TestRechazaIdsRepetidosEnElArchivo(t *testing.T) {
	s := nuevo(t)
	escribirCrudo(t, s.Path(), `
version = 1

[[connection]]
id = "mismo"
name = "dev"
engine = "postgres"
host = "db.local"
port = 5432
database = "shop"
user = "rw"
environment = "dev"

[[connection]]
id = "mismo"
name = "prod"
engine = "postgres"
host = "prod.local"
port = 5432
database = "shop"
user = "rw"
environment = "production"
`)
	_, err := s.List()
	if err == nil {
		t.Fatal("List() aceptó dos conexiones con el mismo id")
	}
	for _, esperado := range []string{"mismo", "dev", "prod"} {
		if !strings.Contains(err.Error(), esperado) {
			t.Errorf("el error no menciona %q: %v", esperado, err)
		}
	}
}

func TestRechazaUnaEntradaSinId(t *testing.T) {
	s := nuevo(t)
	escribirCrudo(t, s.Path(), `
version = 1

[[connection]]
name = "sin id"
engine = "postgres"
host = "db.local"
port = 5432
database = "shop"
user = "rw"
environment = "dev"
`)
	_, err := s.List()
	if err == nil {
		t.Fatal("List() aceptó una conexión sin id")
	}
	if !strings.Contains(err.Error(), "sin id") {
		t.Errorf("el error no identifica la entrada: %v", err)
	}
}

// El archivo se edita a mano y está documentado como tal. Un enum con espacios
// o mayúsculas tiene que llegar limpio a la UI, no como "motor desconocido".
func TestNormalizaAlLeerUnArchivoEditadoAMano(t *testing.T) {
	s := nuevo(t)
	escribirCrudo(t, s.Path(), `
version = 1

[[connection]]
id = "a1"
name = "  shop dev  "
engine = " Postgres "
host = " db.local "
port = 5432
database = " shop "
user = " rw "
environment = " DEV "
`)
	got, err := s.Get("a1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Engine != connection.Postgres {
		t.Errorf("Engine = %q, se esperaba %q", got.Engine, connection.Postgres)
	}
	if got.Environment != connection.Dev {
		t.Errorf("Environment = %q, se esperaba %q", got.Environment, connection.Dev)
	}
	if got.Name != "shop dev" || got.Host != "db.local" {
		t.Errorf("no limpió los espacios: %+v", got)
	}
	// Y el modo SSL ausente queda explícito, para que Warnings pueda avisar.
	if got.SSLMode != connection.SSLPrefer {
		t.Errorf("SSLMode = %q, se esperaba que Normalize lo completara", got.SSLMode)
	}
	if err := got.Validate(); err != nil {
		t.Errorf("la conexión normalizada no valida: %v", err)
	}
}

// El archivo se edita a mano y read() solo normaliza. Get valida para que nada
// roto llegue al resto de la app; List devuelve todo para que el gestor pueda
// mostrar la entrada rota y dejar arreglarla.
func TestGetValidaYListNo(t *testing.T) {
	s := nuevo(t)
	escribirCrudo(t, s.Path(), `
version = 1

[[connection]]
id = "rota"
name = "sin base"
engine = "postgres"
host = "db.local"
port = 5432
database = ""
user = "rw"
environment = "dev"
`)
	// List la muestra: el usuario tiene que poder verla para arreglarla.
	todas, err := s.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(todas) != 1 {
		t.Fatalf("List() devolvió %d conexiones, se esperaba 1", len(todas))
	}

	// Get la rechaza: sin base, Postgres usaría el nombre del usuario como base.
	_, err = s.Get("rota")
	if err == nil {
		t.Fatal("Get() devolvió una conexión sin base")
	}
	var ve *connection.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Get() devolvió %T (%v), se esperaba *ValidationError", err, err)
	}
	if !ve.Has("database") {
		t.Errorf("el error no señala el campo database: %v", ve.Errors)
	}
}

// El archivo se escribe siempre completo: si una versión futura agrega una
// clave de seguridad, la ausencia en un archivo viejo tiene que caer del lado
// seguro. Este test fija que las claves se escriben y vuelven intactas.
func TestLasProteccionesSobrevivenElDisco(t *testing.T) {
	s := nuevo(t)
	want := conn("a1", "prod")
	want.Environment = connection.Production
	want.Safety = connection.Safety{
		ReadOnly:                true,
		BlockDropTruncate:       true,
		StatementTimeoutSeconds: 5,
		RowLimit:                connection.Unlimited,
		IdleDisconnectMinutes:   60,
	}
	if err := s.Add(want); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	got, err := New(s.Path()).Get("a1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Safety != want.Safety {
		t.Errorf("las protecciones cambiaron al pasar por disco:\n got %+v\nwant %+v", got.Safety, want.Safety)
	}
}

// Y si el archivo NO tiene la sección de seguridad, la conexión tiene que
// quedar protegida y no desprotegida.
func TestUnArchivoSinSeccionDeSeguridadQuedaProtegido(t *testing.T) {
	s := nuevo(t)
	escribirCrudo(t, s.Path(), `
version = 1

[[connection]]
id = "vieja"
name = "conexión de antes"
engine = "postgres"
host = "db.local"
port = 5432
database = "shop"
user = "rw"
environment = "production"
`)
	got, err := s.Get("vieja")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if !got.Safety.RequiresPreview() {
		t.Error("sin sección de seguridad, el preview debería ser obligatorio")
	}
	if !got.RequiresWriteConfirmation() {
		t.Error("sin sección de seguridad, escribir debería pedir confirmación")
	}
	if got.Safety.StatementTimeout() == 0 {
		t.Error("sin sección de seguridad, debería haber timeout y no 'sin límite'")
	}
	if got.Safety.EffectiveRowLimit() == 0 {
		t.Error("sin sección de seguridad, debería haber límite de filas")
	}
}

// La carpeta es opcional y la mayoría de las libretas no la usan: un archivo
// lleno de `folder = ""` sería ruido para quien lo edita a mano, y una clave
// que aparece «de la nada» en un archivo que se sincroniza asusta.
func TestUnaConexionSinCarpetaNoEscribeLaClave(t *testing.T) {
	s := nuevo(t)
	sin := connection.Connection{
		ID: "a1", Name: "suelta", Engine: connection.SQLite, Database: "/tmp/x.db",
	}
	con := sin
	con.ID, con.Name, con.Folder = "a2", "en carpeta", "Shop"
	for _, c := range []connection.Connection{sin, con} {
		if err := s.Add(c); err != nil {
			t.Fatalf("Add(%s) error: %v", c.Name, err)
		}
	}
	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(data), "folder"); n != 1 {
		t.Errorf("la clave folder aparece %d veces; se esperaba 1 —solo en la conexión que tiene carpeta—:\n%s", n, data)
	}
	if !strings.Contains(string(data), `folder = "Shop"`) {
		t.Errorf("la carpeta no quedó escrita:\n%s", data)
	}
}

// Los certificados son un bloque aparte y el bloque no se escribe si está
// vacío: una libreta común no tiene por qué llevar `[connection.tls]` vacío en
// cada conexión, que es lo que se lee a mano.
func TestUnaConexionSinCertificadosNoEscribeElBloqueTLS(t *testing.T) {
	s := nuevo(t)
	sin := conn("a1", "sin certificados")
	con := conn("a2", "con raíz")
	con.TLS.RootCertPath = "~/.postgresql/root.crt"
	for _, c := range []connection.Connection{sin, con} {
		if err := s.Add(c); err != nil {
			t.Fatalf("Add(%s) error: %v", c.Name, err)
		}
	}
	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	texto := string(data)
	if n := strings.Count(texto, "[connection.tls]"); n != 1 {
		t.Errorf("el bloque tls aparece %d veces; se esperaba 1:\n%s", n, texto)
	}
	if !strings.Contains(texto, `root_cert_path = "~/.postgresql/root.crt"`) {
		t.Errorf("la raíz no quedó escrita con el ~ sin resolver:\n%s", texto)
	}
	// Y las rutas que no se cargaron no aparecen ni vacías.
	if strings.Contains(texto, "client_cert_path") || strings.Contains(texto, "client_key_path") {
		t.Errorf("se escribieron claves de certificados que no se cargaron:\n%s", texto)
	}
}
