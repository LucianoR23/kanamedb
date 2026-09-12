package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/store"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// El archivo compartido no puede llevar un secreto, y no alcanza con decirlo:
// se guardan la contraseña de la base y la frase de paso del bastión, se
// exporta, y el archivo no puede contener ninguna de las dos. La ruta de la
// clave privada sí: es una ruta, no la clave.
func TestExportarNuncaLlevaUnSecreto(t *testing.T) {
	s := nuevo(t)
	c := base("a1", "shop prod")
	c.SSH = tunnel.Config{Enabled: true, Host: "bastion", Port: 22, User: "ops", Auth: tunnel.AuthKeyFile, KeyPath: "~/.ssh/id_ed25519"}
	if _, err := s.SaveWithSSH(c, PasswordSet, "pw-s3cr3t-base", PasswordSet, "pw-s3cr3t-ssh"); err != nil {
		t.Fatalf("SaveWithSSH() error: %v", err)
	}

	path := filepath.Join(t.TempDir(), "compartida.toml")
	info, err := s.ExportConnections([]string{"a1"}, path)
	if err != nil {
		t.Fatalf("ExportConnections() error: %v", err)
	}
	if info.Count != 1 || info.Bytes == 0 || info.Path != path {
		t.Errorf("SharedFile = %+v", info)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	texto := string(data)
	for _, secreto := range []string{"pw-s3cr3t-base", "pw-s3cr3t-ssh"} {
		if strings.Contains(texto, secreto) {
			t.Errorf("el archivo exportado contiene el secreto %q", secreto)
		}
	}
	for _, quiere := range []string{`name = "shop prod"`, `key_path = "~/.ssh/id_ed25519"`, "Sin contraseñas"} {
		if !strings.Contains(texto, quiere) {
			t.Errorf("falta %q en el archivo:\n%s", quiere, texto)
		}
	}
}

// El archivo exportado es una libreta válida: exportar ENCIMA de la propia la
// dejaría con solo lo exportado, y las contraseñas de las demás huérfanas.
func TestExportarEncimaDeLaLibretaSeNiega(t *testing.T) {
	s := nuevo(t)
	for _, n := range []string{"una", "dos"} {
		if _, err := s.Save(base(n, n), PasswordKeep, ""); err != nil {
			t.Fatal(err)
		}
	}
	libreta := s.store.Path()
	rutas := []string{libreta, filepath.Join(filepath.Dir(libreta), ".", "connections.toml")}
	// La misma ruta en mayúsculas es la libreta SOLO donde el sistema de
	// archivos no distingue mayúsculas —Windows, y macOS por defecto—. En
	// Linux es otro archivo, que además no existe, y negarse ahí sería un
	// error: se pregunta al sistema, no al GOOS.
	if _, err := os.Stat(strings.ToUpper(libreta)); err == nil {
		rutas = append(rutas, strings.ToUpper(libreta))
	}
	for _, path := range rutas {
		if _, err := s.ExportConnections([]string{"una"}, path); err == nil || !strings.Contains(err.Error(), "tu libreta") {
			t.Errorf("exportar a %q dio %v, se esperaba negarse", path, err)
		}
	}
	lista, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(lista) != 2 {
		t.Errorf("la libreta quedó con %d conexiones, se esperaban 2", len(lista))
	}
}

func TestExportarRechazaLoQueNoTiene(t *testing.T) {
	s := nuevo(t)
	path := filepath.Join(t.TempDir(), "x.toml")
	if _, err := s.ExportConnections(nil, path); err == nil {
		t.Error("exportar nada no dio error")
	}
	if _, err := s.ExportConnections([]string{"a1"}, ""); err == nil {
		t.Error("exportar sin ruta no dio error")
	}
	if _, err := s.ExportConnections([]string{"no-existe"}, path); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("exportar un id inexistente dio %v, se esperaba ErrNotFound", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Error("un export fallido dejó un archivo")
	}
}

// Ida y vuelta entre dos libretas: lo que se exporta de una se importa en la
// otra con IDs nuevos, sin contraseña, y con la carpeta puesta.
func TestExportarEImportarEntreDosLibretas(t *testing.T) {
	origen := nuevo(t)
	local := base("a1", "shop local")
	local.Folder = "Shop"
	prod := base("a2", "shop prod")
	prod.Folder, prod.Environment, prod.Host = "Shop", connection.Production, "db.prod.internal"
	prod.SSH = tunnel.Config{Enabled: true, Host: "bastion", Port: 22, User: "ops", Auth: tunnel.AuthAgent}
	for _, c := range []connection.Connection{local, prod} {
		if _, err := origen.Save(c, PasswordSet, "s3cr3t"); err != nil {
			t.Fatalf("Save(%s) error: %v", c.Name, err)
		}
	}
	path := filepath.Join(t.TempDir(), "shop.toml")
	if _, err := origen.ExportConnections([]string{"a1", "a2"}, path); err != nil {
		t.Fatalf("ExportConnections() error: %v", err)
	}

	destino := nuevo(t)
	vista, err := destino.PreviewImport(path)
	if err != nil {
		t.Fatalf("PreviewImport() error: %v", err)
	}
	if vista.Fingerprint == "" || vista.Path != path {
		t.Errorf("ImportPreview = %+v", vista)
	}
	if len(vista.Connections) != 2 {
		t.Fatalf("la vista previa trae %d conexiones, se esperaban 2", len(vista.Connections))
	}
	if len(vista.Notes) != 0 {
		t.Errorf("un archivo escrito por Kaname dio avisos: %v", vista.Notes)
	}
	p := vista.Connections[1]
	if p.Name != "shop prod" || !p.Production || p.Environment != connection.Production || p.Folder != "Shop" || !p.SSH {
		t.Errorf("la candidata de producción no se describe bien: %+v", p)
	}
	if p.Describe != "rw@db.prod.internal:5432/shop" {
		t.Errorf("Describe = %q", p.Describe)
	}
	if p.Existing != "" || len(p.Problems) != 0 {
		t.Errorf("una candidata nueva y válida sale con Existing=%q Problems=%v", p.Existing, p.Problems)
	}
	// Lo que el gestor mostraría después se ve antes: modo TLS efectivo,
	// protecciones y avisos (K-10). `require` sin raíz no verifica el
	// certificado, y eso es un aviso.
	if p.SSLMode != connection.SSLRequire {
		t.Errorf("SSLMode = %q, se esperaba el de la conexión (require)", p.SSLMode)
	}
	if len(p.Warnings) == 0 {
		t.Errorf("una conexión en require sin raíz tiene que traer el aviso de TLS a la vista previa")
	}

	nuevas, err := destino.ImportConnections(path, vista.Fingerprint, []int{0, 1})
	if err != nil {
		t.Fatalf("ImportConnections() error: %v", err)
	}
	if len(nuevas) != 2 {
		t.Fatalf("se importaron %d, se esperaban 2", len(nuevas))
	}
	for _, v := range nuevas {
		if v.Connection.ID == "a1" || v.Connection.ID == "a2" || v.Connection.ID == "" {
			t.Errorf("la importada %q conserva el ID del archivo (%q)", v.Connection.Name, v.Connection.ID)
		}
		if v.HasPassword {
			t.Errorf("la importada %q tiene contraseña; el archivo no puede traerla", v.Connection.Name)
		}
		if v.Connection.Folder != "Shop" {
			t.Errorf("la importada %q perdió la carpeta: %q", v.Connection.Name, v.Connection.Folder)
		}
	}
	lista, err := destino.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(lista) != 2 {
		t.Errorf("la libreta destino tiene %d conexiones, se esperaban 2", len(lista))
	}
}

// Quien escribe `password = "…"` a mano en el archivo espera que se importe.
// No se importa, y hay que decírselo en la vista previa, no al conectar.
func TestImportarIgnoraLaContrasenaDelArchivoYLoDice(t *testing.T) {
	s := nuevo(t)
	path := filepath.Join(t.TempDir(), "a-mano.toml")
	escribir(t, path, `version = 1

[[connection]]
  id = "x1"
  name = "a mano"
  engine = "postgres"
  host = "h"
  database = "d"
  user = "u"
  password = "pw-s3cr3t"
`)
	vista, err := s.PreviewImport(path)
	if err != nil {
		t.Fatalf("PreviewImport() error: %v", err)
	}
	if len(vista.Notes) != 1 || !strings.Contains(vista.Notes[0], "password") || !strings.Contains(vista.Notes[0], "se pide al conectar") {
		t.Errorf("la vista previa no avisa de la contraseña ignorada: %v", vista.Notes)
	}
	if strings.Contains(strings.Join(vista.Notes, " "), "pw-s3cr3t") {
		t.Error("el aviso repite la contraseña del archivo")
	}
	nuevas, err := s.ImportConnections(path, vista.Fingerprint, []int{0})
	if err != nil {
		t.Fatalf("ImportConnections() error: %v", err)
	}
	if nuevas[0].HasPassword {
		t.Error("la contraseña del archivo terminó en el keychain")
	}
}

// Lo que se agrega tiene que ser lo que se vio.
func TestImportarExigeElArchivoQueSeMostro(t *testing.T) {
	s := nuevo(t)
	path := filepath.Join(t.TempDir(), "cambia.toml")
	data, err := store.Encode([]connection.Connection{base("a1", "una")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	escribir(t, path, string(data))
	vista, err := s.PreviewImport(path)
	if err != nil {
		t.Fatalf("PreviewImport() error: %v", err)
	}
	escribir(t, path, string(data)+"\n# cambió\n")
	if _, err := s.ImportConnections(path, vista.Fingerprint, []int{0}); err == nil || !strings.Contains(err.Error(), "cambió") {
		t.Errorf("importar con el archivo cambiado dio %v, se esperaba «cambió desde la vista previa»", err)
	}
	lista, _ := s.List()
	if len(lista) != 0 {
		t.Errorf("se importó algo igual: %d conexiones", len(lista))
	}
}

// Una entrada rota se muestra con sus problemas y no se puede importar; y si
// se pide igual, no se importa nada, ni las buenas.
func TestImportarMuestraLasRotasYEsTodoONada(t *testing.T) {
	s := nuevo(t)
	path := filepath.Join(t.TempDir(), "rotas.toml")
	escribir(t, path, `version = 1

[[connection]]
  name = "buena"
  engine = "sqlite"
  database = "/tmp/buena.db"

[[connection]]
  name = "rota"
  engine = "postgres"
  host = "h"
  user = "u"
`)
	vista, err := s.PreviewImport(path)
	if err != nil {
		t.Fatalf("PreviewImport() error: %v", err)
	}
	if len(vista.Connections) != 2 {
		t.Fatalf("%d candidatas, se esperaban 2", len(vista.Connections))
	}
	if len(vista.Connections[0].Problems) != 0 {
		t.Errorf("la buena sale con problemas: %v", vista.Connections[0].Problems)
	}
	rota := vista.Connections[1]
	if len(rota.Problems) == 0 {
		t.Fatal("la rota sale sin problemas")
	}
	campos := make(map[string]bool, len(rota.Problems))
	for _, p := range rota.Problems {
		campos[p.Field] = true
	}
	if campos["id"] {
		t.Error("la vista previa reclama un id, que al importar se reemplaza")
	}
	if !campos["database"] {
		t.Errorf("la rota no dice que le falta la base: %v", rota.Problems)
	}
	if _, err := s.ImportConnections(path, vista.Fingerprint, []int{0, 1}); err == nil {
		t.Error("importar una rota no dio error")
	}
	lista, _ := s.List()
	if len(lista) != 0 {
		t.Errorf("un import con una rota agregó %d conexiones; se esperaba ninguna", len(lista))
	}
	if _, err := s.ImportConnections(path, vista.Fingerprint, []int{0, 0}); err != nil {
		t.Errorf("importar solo la buena (repetida en la lista) dio %v", err)
	}
	lista, _ = s.List()
	if len(lista) != 1 {
		t.Errorf("quedaron %d conexiones, se esperaba 1", len(lista))
	}
}

// Importar dos veces el mismo archivo no tiene por qué duplicar la libreta sin
// avisar: la vista previa dice cuál ya apunta al mismo lugar.
func TestLaVistaPreviaAvisaSiYaApuntaAlMismoLugar(t *testing.T) {
	s := nuevo(t)
	if _, err := s.Save(base("a1", "la que ya tengo"), PasswordKeep, ""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "otra-vez.toml")
	data, err := store.Encode([]connection.Connection{base("zz", "con otro nombre")}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	escribir(t, path, string(data))
	vista, err := s.PreviewImport(path)
	if err != nil {
		t.Fatalf("PreviewImport() error: %v", err)
	}
	if vista.Connections[0].Existing != "la que ya tengo" {
		t.Errorf("Existing = %q, se esperaba el nombre de la conexión que ya apunta ahí", vista.Connections[0].Existing)
	}
}

func TestImportarRechazaArchivosQueNoSonUnaLibreta(t *testing.T) {
	s := nuevo(t)
	dir := t.TempDir()

	grande := filepath.Join(dir, "grande.toml")
	escribir(t, grande, strings.Repeat("#", store.MaxSharedFileBytes+1))
	if _, err := s.PreviewImport(grande); !errors.Is(err, store.ErrSharedFileTooBig) {
		t.Errorf("un archivo de más de un mega dio %v", err)
	}

	vacio := filepath.Join(dir, "vacio.toml")
	escribir(t, vacio, "version = 1\n")
	if _, err := s.PreviewImport(vacio); err == nil || !strings.Contains(err.Error(), "ninguna conexión") {
		t.Errorf("un archivo sin conexiones dio %v", err)
	}

	if _, err := s.PreviewImport(filepath.Join(dir, "no-existe.toml")); err == nil {
		t.Error("un archivo inexistente no dio error")
	}
	if _, err := s.ImportConnections(vacio, "x", nil); err == nil {
		t.Error("importar sin elegir nada no dio error")
	}
}

func escribir(t *testing.T, path, contenido string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contenido), 0o600); err != nil {
		t.Fatal(err)
	}
}

// La SQL de sesión de un archivo ajeno va a correr con las credenciales de
// quien importa, en cada conexión y sin preguntar: la vista previa la
// muestra entera, y la importación la conserva —es configuración, no un
// secreto—.
func TestLaVistaPreviaMuestraLaSQLDeSesionDelArchivo(t *testing.T) {
	origen := nuevo(t)
	c := base("a1", "con sesión")
	c.Advanced.SessionSQL = "SET lock_timeout = '3s';\nDELETE FROM auditoria;"
	if _, err := origen.Save(c, PasswordKeep, ""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "compartida.toml")
	if _, err := origen.ExportConnections([]string{"a1"}, path); err != nil {
		t.Fatal(err)
	}

	destino := nuevo(t)
	vista, err := destino.PreviewImport(path)
	if err != nil {
		t.Fatalf("PreviewImport() error: %v", err)
	}
	if len(vista.Connections) != 1 || vista.Connections[0].SessionSQL != c.Advanced.SessionSQL {
		t.Errorf("la vista previa no muestra la SQL de sesión: %+v", vista.Connections)
	}
	importadas, err := destino.ImportConnections(path, vista.Fingerprint, []int{0})
	if err != nil {
		t.Fatalf("ImportConnections() error: %v", err)
	}
	if importadas[0].Connection.Advanced.SessionSQL != c.Advanced.SessionSQL {
		t.Errorf("la importación perdió la SQL de sesión: %+v", importadas[0].Connection.Advanced)
	}
}

// TestLaVistaPreviaDeImportarMuestraLasProteccionesQueTraeElArchivo.
//
// Un archivo de otra persona puede traer `ssl_mode = "disable"`,
// `read_only = false` y `allow_apply_without_preview = true` para un host de
// producción. La vista previa mostraba el entorno y el destino, y ninguno de
// esos: la libreta quedaba con una conexión menos protegida de lo que quien
// importó creía, hasta abrir el gestor (K-10).
func TestLaVistaPreviaDeImportarMuestraLasProteccionesQueTraeElArchivo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ajena.toml")
	escribir(t, path, `version = 1

[[connection]]
id = "x"
name = "prod ajena"
engine = "postgres"
host = "db.prod.internal"
port = 5432
database = "shop"
user = "app"
environment = "staging"
ssl_mode = "disable"

[connection.safety]
read_only = false
allow_apply_without_preview = true
allow_write_without_confirmation = true
`)
	vista, err := nuevo(t).PreviewImport(path)
	if err != nil {
		t.Fatalf("PreviewImport(): %v", err)
	}
	c := vista.Connections[0]
	if c.SSLMode != connection.SSLDisable {
		t.Errorf("SSLMode = %q", c.SSLMode)
	}
	if !c.Safety.AllowApplyWithoutPreview || !c.Safety.AllowWriteWithoutConfirmation || c.Safety.ReadOnly {
		t.Errorf("Safety no es la del archivo: %+v", c.Safety)
	}
	campos := map[string]bool{}
	for _, w := range c.Warnings {
		campos[w.Field] = true
	}
	for _, f := range []string{"sslMode", "allowApplyWithoutPreview", "allowWriteWithoutConfirmation"} {
		if !campos[f] {
			t.Errorf("falta el aviso sobre %s en la vista previa: %+v", f, c.Warnings)
		}
	}
}
