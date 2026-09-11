package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/secrets"
	"github.com/LucianoR23/kanamedb/internal/store"
)

// nuevo arma un servicio sobre un archivo temporal y un keychain en memoria.
//
// No usa el keychain del sistema a propósito: eso se prueba en internal/secrets.
// Cuando estos tests lo usaban, los dos paquetes corriendo en procesos
// paralelos se pisaban sobre el Credential Manager y el resultado era
// intermitente — a veces una contraseña recién guardada no aparecía.
func nuevo(t *testing.T) *Connections {
	t.Helper()
	return &Connections{
		store:   store.New(filepath.Join(t.TempDir(), "connections.toml")),
		keyring: newFakeKeyring(),
	}
}

func base(id, name string) connection.Connection {
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

func TestDraftTraeIdYDefaultsSeguros(t *testing.T) {
	s := nuevo(t)
	v, err := s.Draft()
	if err != nil {
		t.Fatalf("Draft() error: %v", err)
	}
	if v.Connection.ID == "" {
		t.Error("el borrador vino sin id, que es la clave del keychain")
	}
	if v.Connection.Environment == connection.Production {
		t.Error("un borrador nuevo no puede ser producción por defecto")
	}
	if !v.Connection.Safety.RequiresPreview() {
		t.Error("un borrador nuevo debería exigir preview")
	}
	if v.HasPassword {
		t.Error("un borrador nuevo no puede tener contraseña")
	}
}

func TestSaveYListIdaYVuelta(t *testing.T) {
	s := nuevo(t)
	if _, err := s.Save(base("a1", "shop dev"), PasswordSet, "s3cr3t"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	lista, err := s.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(lista) != 1 {
		t.Fatalf("List() = %d, se esperaba 1", len(lista))
	}
	v := lista[0]
	if !v.HasPassword {
		t.Error("HasPassword = false después de guardar una contraseña")
	}
	if !v.Valid() {
		t.Errorf("la conexión quedó con problemas: %v", v.Problems)
	}
	if !strings.Contains(v.URI, "db.local") {
		t.Errorf("URI = %q", v.URI)
	}
	if strings.Contains(v.URI, "s3cr3t") {
		t.Errorf("la URI lleva la contraseña: %q", v.URI)
	}
	if !strings.Contains(v.KeychainRef, "a1") {
		t.Errorf("KeychainRef = %q, debería nombrar la entrada real", v.KeychainRef)
	}
}

// El caso peligroso es el silencioso: abrir el editor para cambiar el nombre,
// no tocar el campo de contraseña, y que guardar borre la credencial.
func TestGuardarSinTocarLaContrasenaNoLaBorra(t *testing.T) {
	s := nuevo(t)
	if _, err := s.Save(base("a1", "antes"), PasswordSet, "s3cr3t"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	renombrada := base("a1", "después")
	if _, err := s.Save(renombrada, PasswordKeep, ""); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	v, err := s.Get("a1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if v.Connection.Name != "después" {
		t.Errorf("Name = %q, no se guardó el cambio", v.Connection.Name)
	}
	if !v.HasPassword {
		t.Fatal("guardar sin tocar la contraseña la borró")
	}
	pw, err := s.RevealPassword("a1")
	if err != nil {
		t.Fatalf("RevealPassword() error: %v", err)
	}
	if pw != "s3cr3t" {
		t.Errorf("la contraseña cambió: %q", pw)
	}
}

func TestQuitarLaContrasenaEsExplicito(t *testing.T) {
	s := nuevo(t)
	if _, err := s.Save(base("a1", "x"), PasswordSet, "s3cr3t"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if _, err := s.Save(base("a1", "x"), PasswordRemove, ""); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	v, err := s.Get("a1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if v.HasPassword {
		t.Error("HasPassword = true después de quitarla")
	}
	if _, err := s.RevealPassword("a1"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("RevealPassword() devolvió %v, se esperaba ErrNotFound", err)
	}
}

// Guardar una contraseña vacía deja una credencial inútil y casi siempre es un
// bug del formulario.
func TestNoSePuedeGuardarUnaContrasenaVacia(t *testing.T) {
	s := nuevo(t)
	if _, err := s.Save(base("a1", "x"), PasswordSet, ""); err == nil {
		t.Error("Save() aceptó una contraseña vacía con PasswordSet")
	}
}

// Si el keychain falla, no puede quedar una conexión guardada apuntando a una
// credencial que no existe.
func TestLaContrasenaSeGuardaAntesQueLaConexion(t *testing.T) {
	s := nuevo(t)
	// Un id que el keychain rechaza fuerza el fallo antes de tocar el store.
	c := base("  id con espacios  ", "x")
	if _, err := s.Save(c, PasswordSet, "algo"); err == nil {
		t.Fatal("Save() no falló con un id inválido")
	}
	lista, err := s.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(lista) != 0 {
		t.Errorf("quedó una conexión guardada pese a que el keychain falló: %+v", lista)
	}
}

func TestDeleteBorraTambienLaContrasena(t *testing.T) {
	s := nuevo(t)
	if _, err := s.Save(base("a1", "x"), PasswordSet, "s3cr3t"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if err := s.Delete("a1"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if _, err := s.Get("a1"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Get() devolvió %v, se esperaba ErrNotFound", err)
	}
	// Y no puede quedar un secreto huérfano en el sistema.
	if _, err := s.RevealPassword("a1"); !errors.Is(err, secrets.ErrNotFound) {
		t.Errorf("quedó la contraseña en el keychain: %v", err)
	}
}

// El id es la clave del keychain: una copia que heredara la contraseña haría
// que borrar el original deje a la copia sin nada, o que dos conexiones
// compartan un secreto sin que se note.
func TestDuplicateNoCopiaLaContrasena(t *testing.T) {
	s := nuevo(t)
	if _, err := s.Save(base("a1", "original"), PasswordSet, "s3cr3t"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	copia, err := s.Duplicate("a1")
	if err != nil {
		t.Fatalf("Duplicate() error: %v", err)
	}
	if copia.Connection.ID == "a1" {
		t.Error("la copia tiene el mismo id que el original")
	}
	if copia.HasPassword {
		t.Error("la copia heredó la contraseña")
	}
	if !strings.Contains(copia.Connection.Name, "original") {
		t.Errorf("Name = %q, debería derivar del original", copia.Connection.Name)
	}

	// Y el original queda intacto.
	orig, err := s.Get("a1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if !orig.HasPassword {
		t.Error("duplicar tocó la contraseña del original")
	}
}

// Duplicar es cómo se arma la conexión de dev a partir de la local, y las dos
// son del mismo proyecto: la copia nace en la misma carpeta.
func TestDuplicateHeredaLaCarpeta(t *testing.T) {
	s := nuevo(t)
	c := base("a1", "shop local")
	c.Folder = "Shop"
	if _, err := s.Save(c, PasswordKeep, ""); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	copia, err := s.Duplicate("a1")
	if err != nil {
		t.Fatalf("Duplicate() error: %v", err)
	}
	if copia.Connection.Folder != "Shop" {
		t.Errorf("la copia quedó en la carpeta %q, se esperaba %q", copia.Connection.Folder, "Shop")
	}
}

// Mover de carpeta cambia la carpeta y nada más: ni la contraseña, ni el
// resto de la configuración. Y vacío la saca de la que tenga.
func TestMoveToFolderCambiaSoloLaCarpeta(t *testing.T) {
	s := nuevo(t)
	c := base("a1", "shop local")
	c.Safety.ReadOnly = true
	if _, err := s.Save(c, PasswordSet, "s3cr3t"); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	v, err := s.MoveToFolder("a1", "  Shop  ")
	if err != nil {
		t.Fatalf("MoveToFolder() error: %v", err)
	}
	if v.Connection.Folder != "Shop" {
		t.Errorf("Folder = %q, se esperaba %q (normalizada)", v.Connection.Folder, "Shop")
	}

	// Lo que se lee después es lo que quedó en disco, no lo que devolvió la
	// llamada.
	got, err := s.Get("a1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Connection.Folder != "Shop" {
		t.Errorf("en disco la carpeta es %q, se esperaba %q", got.Connection.Folder, "Shop")
	}
	if !got.HasPassword {
		t.Error("mover de carpeta tocó la contraseña")
	}
	if !got.Connection.Safety.ReadOnly || got.Connection.Name != "shop local" {
		t.Errorf("mover de carpeta cambió otra cosa: %+v", got.Connection)
	}

	if _, err := s.MoveToFolder("a1", ""); err != nil {
		t.Fatalf("MoveToFolder(\"\") error: %v", err)
	}
	got, err = s.Get("a1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Connection.Folder != "" {
		t.Errorf("vacío tendría que sacarla de la carpeta; quedó en %q", got.Connection.Folder)
	}

	if _, err := s.MoveToFolder("no-existe", "Shop"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("mover una conexión inexistente dio %v, se esperaba ErrNotFound", err)
	}
}

// Una conexión rota se muestra con sus problemas en vez de esconderse: el
// archivo se edita a mano y el usuario necesita verla para arreglarla.
func TestListMuestraLasConexionesRotasConSusProblemas(t *testing.T) {
	s := nuevo(t)
	// Se escribe el archivo a mano: Add y Save validan, y el punto del test es
	// justamente una entrada que nunca habría pasado por ellos. Es lo que
	// produce editar connections.toml con un editor de texto.
	escribirCrudo(t, s.store.Path(), `
version = 1

[[connection]]
id = "a1"
name = "sin base"
engine = "postgres"
host = "db.local"
port = 5432
database = ""
user = "rw"
environment = "dev"
`)

	lista, err := s.List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(lista) != 1 {
		t.Fatalf("List() = %d, la conexión rota tiene que aparecer para poder arreglarla", len(lista))
	}
	v := lista[0]
	if v.Valid() {
		t.Fatal("la conexión sin base se reportó como válida")
	}
	campos := map[string]bool{}
	for _, p := range v.Problems {
		campos[p.Field] = true
	}
	if !campos["database"] {
		t.Errorf("no señala el campo roto; problemas: %v", v.Problems)
	}
	// Y aun rota, la vista se arma sin explotar.
	if v.Connection.Name != "sin base" {
		t.Errorf("Name = %q", v.Connection.Name)
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

func TestCheckReportaProblemasSinGuardar(t *testing.T) {
	s := nuevo(t)
	c := base("a1", "")
	c.Database = ""

	v := s.Check(c)
	if v.Valid() {
		t.Fatal("Check() no reportó problemas en una conexión inválida")
	}
	campos := map[string]bool{}
	for _, p := range v.Problems {
		campos[p.Field] = true
	}
	for _, esperado := range []string{"name", "database"} {
		if !campos[esperado] {
			t.Errorf("no reportó %q; se obtuvo %v", esperado, v.Problems)
		}
	}

	// Y no guardó nada.
	lista, _ := s.List()
	if len(lista) != 0 {
		t.Errorf("Check() guardó la conexión: %+v", lista)
	}
}

func TestCheckDevuelveAvisosSinBloquear(t *testing.T) {
	s := nuevo(t)
	c := base("a1", "prod")
	c.Environment = connection.Production
	c.SSLMode = connection.SSLDisable

	v := s.Check(c)
	if !v.Valid() {
		t.Errorf("los avisos no deberían invalidar: %v", v.Problems)
	}
	if len(v.Warnings) == 0 {
		t.Error("producción sin cifrar debería avisar")
	}
}

// Probar una contraseña nueva antes de reemplazar la que anda es justamente
// para lo que sirve el botón.
func TestTestUsaLaContrasenaQueSeLePasaCuandoEsPasswordSet(t *testing.T) {
	s := nuevo(t)
	c := base("a1", "x")
	c.Host = "127.0.0.1"
	c.Port = 1 // puerto cerrado: alcanza para ver por dónde falla

	got := s.Test(context.Background(), c, PasswordSet, "la-nueva")
	if got.OK {
		t.Fatal("Test() dijo OK contra un puerto cerrado")
	}
	if got.Failure == nil {
		t.Fatal("Test() falló sin Failure")
	}
	if strings.Contains(got.Failure.Message, "la-nueva") {
		t.Errorf("el mensaje filtra la contraseña: %q", got.Failure.Message)
	}
}

func TestTestDeUnaConexionInvalidaExplicaQueFalta(t *testing.T) {
	s := nuevo(t)
	c := base("a1", "x")
	c.Database = ""

	got := s.Test(context.Background(), c, PasswordSet, "x")
	if got.OK {
		t.Fatal("Test() dijo OK sin base")
	}
	if got.Failure == nil || !strings.Contains(got.Failure.Message, "database") {
		t.Errorf("el fallo no explica qué falta: %+v", got.Failure)
	}
}

func TestParseURIPasaPorElServicio(t *testing.T) {
	s := nuevo(t)
	got, err := s.ParseURI("postgresql://app_rw@h:5432/shop?sslmode=require")
	if err != nil {
		t.Fatalf("ParseURI() error: %v", err)
	}
	if got.Connection.User != "app_rw" || got.Connection.Database != "shop" {
		t.Errorf("no completó los campos: %+v", got.Connection)
	}
}

// La vista es lo que cruza al frontend: no puede llevar la contraseña.
func TestLaVistaNoLlevaLaContrasena(t *testing.T) {
	s := nuevo(t)
	const pw = "ContraseñaSecreta123"
	if _, err := s.Save(base("a1", "x"), PasswordSet, pw); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	v, err := s.Get("a1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	rendered := fmt.Sprintf("%+v", v)
	if strings.Contains(rendered, pw) {
		t.Errorf("la vista lleva la contraseña: %s", rendered)
	}
}

// TestElAtajoDeSQLiteDejaLaConexionListaParaConectar.
//
// El atajo de S01 y S02 existe para que abrir un archivo sea elegirlo y nada
// más. Si el borrador que devuelve no es VÁLIDO, el atajo no ahorró nada: el
// editor abre con un campo en rojo y hay que completarlo a mano igual.
func TestElAtajoDeSQLiteDejaLaConexionListaParaConectar(t *testing.T) {
	s := nuevo(t)

	v, err := s.DraftSQLite(`C:\Users\yo\datos\northwind.db`)
	if err != nil {
		t.Fatalf("DraftSQLite: %v", err)
	}
	if !v.Valid() {
		t.Fatalf("el borrador salió con problemas y hay que completarlo a mano: %v", v.Problems)
	}
	if v.Connection.Engine != connection.SQLite {
		t.Errorf("el motor quedó %q", v.Connection.Engine)
	}
	if v.Connection.Database != `C:\Users\yo\datos\northwind.db` {
		t.Errorf("la ruta quedó %q", v.Connection.Database)
	}
	if v.Connection.Name != "northwind" {
		t.Errorf("el nombre quedó %q y se esperaba %q", v.Connection.Name, "northwind")
	}
	if v.Connection.ID == "" {
		t.Error("sin ID: es la clave del keychain y la que usan Get, Update y Delete")
	}
	// Nunca producción por defecto, igual que Draft. Un atajo no es motivo para
	// bajar el default seguro.
	if v.Connection.Environment != connection.Local {
		t.Errorf("el entorno quedó %q", v.Connection.Environment)
	}

	// Y NO se guardó: abrir un archivo para mirarlo no deja rastro en la lista.
	lista, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(lista) != 0 {
		t.Errorf("el atajo guardó %d conexión(es) sin que el usuario las viera", len(lista))
	}

	// Dos archivos distintos con el mismo nombre dan dos conexiones distintas:
	// lo que identifica una conexión es el ID, no el nombre.
	otra, err := s.DraftSQLite(`D:\backup\northwind.db`)
	if err != nil {
		t.Fatalf("DraftSQLite: %v", err)
	}
	if otra.Connection.ID == v.Connection.ID {
		t.Error("los dos borradores comparten el ID: comparten la entrada del keychain")
	}
}

// TestElNombreSaleDelArchivoEnLosDosSeparadores.
//
// `filepath` usa el separador del sistema donde corre el programa, así que en
// Linux una ruta de Windows no se corta en ninguna parte y el nombre de la
// conexión sería la ruta entera. La libreta se sincroniza entre máquinas y CI
// corre esto en Linux: tiene que dar lo mismo en los dos lados.
func TestElNombreSaleDelArchivoEnLosDosSeparadores(t *testing.T) {
	casos := []struct{ ruta, nombre string }{
		{`C:\Users\yo\datos\northwind.db`, "northwind"},
		{"/home/yo/datos/northwind.db", "northwind"},
		{"/var/lib/app/kaname.sqlite3", "kaname"},
		// Muchas bases de SQLite no tienen extensión.
		{`C:\datos\produccion`, "produccion"},
		// Todo extensión y nada de nombre: sacársela dejaría el nombre vacío, y
		// el nombre es obligatorio.
		{`C:\datos\.db`, ".db"},
		{"/datos/.db", ".db"},
		// Varios puntos: solo se saca el último tramo.
		{`C:\datos\app.v2.db`, "app.v2"},
		{"/datos/con espacios.db", "con espacios"},
	}
	for _, c := range casos {
		if got := nombreDeArchivo(c.ruta); got != c.nombre {
			t.Errorf("nombreDeArchivo(%q) = %q, se esperaba %q", c.ruta, got, c.nombre)
		}
	}
}
