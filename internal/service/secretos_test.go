package service

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/appinfo"
	"github.com/LucianoR23/kanamedb/internal/config"
	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/history"
	"github.com/LucianoR23/kanamedb/internal/layout"
	"github.com/LucianoR23/kanamedb/internal/store"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// Los tres secretos que la aplicación recibe: la contraseña de la base, la del
// bastión SSH y una que alguien escribe adentro de una consulta. Son valores
// que no pueden aparecer por casualidad en un archivo de la app.
const (
	centinelaDB  = "zz-centinela-db-8f3a1c"
	centinelaSSH = "zz-centinela-ssh-5d0e7b"
	centinelaSQL = "zz-centinela-sql-2b9c4d"
)

// todoLoQueEscribeLaApp arma los mismos stores que main.servicios, sobre las
// rutas reales de la aplicación (appinfo.PathsIn) en dos raíces temporales.
//
// Es el reparto real y no uno inventado para el test, porque lo que se quiere
// recorrer es exactamente lo que la aplicación escribe hoy: si mañana aparece
// un archivo nuevo, entra solo.
type todoLoQueEscribeLaApp struct {
	rutas    appinfo.Paths
	keyring  *fakeKeyring
	libreta  *Connections
	prefs    *config.Store
	diagrama *layout.Store
	hist     *history.Store
	known    *tunnel.KnownHosts
}

func armarTodoLoQueEscribeLaApp(t *testing.T) todoLoQueEscribeLaApp {
	t.Helper()
	rutas := appinfo.PathsIn(filepath.Join(t.TempDir(), "libreta"), filepath.Join(t.TempDir(), "estado"))
	kr := newFakeKeyring()
	known := tunnel.NewKnownHosts(rutas.KnownHosts)
	libreta := NewConnections(store.New(rutas.Connections), kr, known)
	prefs := config.New(rutas.Config)
	UsarPreferencias(libreta, prefs)
	return todoLoQueEscribeLaApp{
		rutas:    rutas,
		keyring:  kr,
		libreta:  libreta,
		prefs:    prefs,
		diagrama: layout.New(rutas.Layouts),
		hist:     history.New(rutas.History, rutas.SavedQueries),
		known:    known,
	}
}

// archivosConElCentinela recorre las dos raíces byte a byte y devuelve qué
// archivos contienen alguno de los centinelas, más cuántos archivos miró.
//
// Byte a byte y no por su formato: un JSON se puede leer con json.Unmarshal y
// no ver un campo que el tipo no declara, y un archivo a medio escribir no
// parsea. Lo que importa es si los bytes están en el disco.
func archivosConElCentinela(t *testing.T, rutas appinfo.Paths) (culpables []string, mirados int) {
	t.Helper()
	raices := []string{filepath.Dir(rutas.Connections), rutas.State}
	for _, raiz := range raices {
		err := filepath.WalkDir(raiz, func(ruta string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			datos, err := os.ReadFile(ruta)
			if err != nil {
				return err
			}
			mirados++
			for _, c := range []string{centinelaDB, centinelaSSH, centinelaSQL} {
				if bytes.Contains(datos, []byte(c)) {
					culpables = append(culpables, ruta+" tiene "+c)
				}
			}
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("recorrer %s: %v", raiz, err)
		}
	}
	return culpables, mirados
}

func conexionConSSH() connection.Connection {
	c := base("c-secretos", "Con secretos")
	c.SSH = tunnel.Config{Enabled: true, Host: "bastion", Port: 22, User: "ops", Auth: tunnel.AuthPassword}
	return c
}

// Los secretos entran por SaveWithSSH, y de ahí van al keychain y a ningún
// archivo. El test hace todo lo que la aplicación hace con el disco —libreta,
// preferencias, diagrama, historial, consultas guardadas, known_hosts— y
// después lee cada archivo que quedó.
func TestLosSecretosVanAlKeychainYANingunArchivo(t *testing.T) {
	app := armarTodoLoQueEscribeLaApp(t)
	c := conexionConSSH()

	if _, err := app.libreta.SaveWithSSH(c, PasswordSet, centinelaDB, PasswordSet, centinelaSSH); err != nil {
		t.Fatalf("SaveWithSSH(): %v", err)
	}
	// Editarla y duplicarla vuelve a escribir la libreta: son las otras dos
	// rutas por las que una conexión llega al disco.
	c.Name = "Con secretos, editada"
	if _, err := app.libreta.Save(c, PasswordKeep, ""); err != nil {
		t.Fatalf("Save(): %v", err)
	}
	if _, err := app.libreta.Duplicate(c.ID); err != nil {
		t.Fatalf("Duplicate(): %v", err)
	}

	if _, err := app.prefs.Save(config.Config{}); err != nil {
		t.Fatalf("guardar las preferencias: %v", err)
	}
	if err := app.diagrama.Save(c.ID, "public", layout.Positions{"t": {X: 1, Y: 2}}); err != nil {
		t.Fatalf("guardar el diagrama: %v", err)
	}
	// Una contraseña escrita adentro de una consulta: el historial la tiene
	// que rechazar sin error, las guardadas con error, y en ningún caso
	// escribirla.
	guardada, err := app.hist.Add(history.Entry{ConnectionID: c.ID, SQL: "ALTER USER app PASSWORD '" + centinelaSQL + "'"})
	if err != nil {
		t.Fatalf("historial.Add(): %v", err)
	}
	if guardada {
		t.Error("el historial guardó una consulta con una contraseña escrita")
	}
	if _, err := app.hist.Add(history.Entry{ConnectionID: c.ID, SQL: "SELECT 1"}); err != nil {
		t.Fatalf("historial.Add(SELECT 1): %v", err)
	}
	if _, err := app.hist.Save(history.Saved{Name: "con secreto", SQL: "SET password = '" + centinelaSQL + "'"}); err == nil {
		t.Error("las consultas guardadas aceptaron una con contraseña escrita")
	}
	if _, err := app.hist.Save(history.Saved{Name: "sin secreto", SQL: "SELECT 2"}); err != nil {
		t.Fatalf("historial.Save(): %v", err)
	}
	if err := app.known.Trust("bastion:22", "bastion ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGpF8yTQ1s0m7YQd7xj3cX5v8GfN3rQ8Yb4zK2hL0pQe"); err != nil {
		t.Fatalf("known_hosts.Trust(): %v", err)
	}

	// El keychain sí los tiene: sin esto, un Save que tirara la contraseña a
	// la basura pasaría el test.
	if pw, err := app.keyring.Get(c.ID); err != nil || pw != centinelaDB {
		t.Errorf("keychain[%s] = %q, %v; se esperaba la contraseña de la base", c.ID, pw, err)
	}
	if pw, err := app.keyring.Get(SSHSecretID(c.ID)); err != nil || pw != centinelaSSH {
		t.Errorf("keychain[%s] = %q, %v; se esperaba la del bastión", SSHSecretID(c.ID), pw, err)
	}

	culpables, mirados := archivosConElCentinela(t, app.rutas)
	for _, c := range culpables {
		t.Error(c)
	}
	// Los seis que la aplicación escribe. Si un día son menos, algo dejó de
	// escribirse y el test dejó de mirar algo: hay que saberlo.
	if mirados < 6 {
		t.Errorf("se miraron %d archivos; se esperaban por lo menos la libreta, las preferencias, el diagrama, el historial, las guardadas y known_hosts", mirados)
	}
	libreta, err := os.ReadFile(app.rutas.Connections)
	if err != nil || !strings.Contains(string(libreta), c.Name) {
		t.Errorf("la libreta no tiene la conexión (%v): el recorrido no está mirando lo que cree", err)
	}
}

// Sin keychain —Linux sin Secret Service, un runner sin sesión— la respuesta
// es un error, no un archivo. La conexión tampoco se guarda: quedaría
// apuntando a una credencial que no existe.
func TestSiElKeychainFallaNoSeGuardaNadaEnNingunLado(t *testing.T) {
	app := armarTodoLoQueEscribeLaApp(t)
	app.keyring.failSet = errors.New("The name org.freedesktop.secrets was not provided by any .service files")

	_, err := app.libreta.SaveWithSSH(conexionConSSH(), PasswordSet, centinelaDB, PasswordSet, centinelaSSH)
	if err == nil {
		t.Fatal("SaveWithSSH() guardó con el keychain roto")
	}
	if !strings.Contains(err.Error(), "org.freedesktop.secrets") {
		t.Errorf("el error no dice qué falló del keychain: %v", err)
	}

	lista, err := app.libreta.List()
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(lista) != 0 {
		t.Errorf("quedaron %d conexiones guardadas sin su contraseña", len(lista))
	}
	if culpables, _ := archivosConElCentinela(t, app.rutas); len(culpables) != 0 {
		t.Errorf("con el keychain roto, algo escribió el secreto a disco: %v", culpables)
	}
	if _, err := app.keyring.Get("c-secretos"); err == nil {
		t.Error("el fake guardó a pesar de failSet: el test no prueba nada")
	}
}
