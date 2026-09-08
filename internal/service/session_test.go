package service

import (
	"context"

	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/store"
)

// sesionDePrueba arma una libreta con una conexión al Postgres de pruebas y su
// contraseña, y devuelve los dos servicios apuntando al mismo par store/keyring
// —igual que en main.go.
//
// Cubre el camino completo que la interfaz recorre al apretar Conectar: leer la
// conexión del archivo, sacar la contraseña del keychain, armar el DSN, abrir el
// pool y leer el catálogo.
func sesionDePrueba(t *testing.T) (*Session, *Connections, string) {
	t.Helper()

	dsn := os.Getenv("KANAME_TEST_POSTGRES")
	if dsn == "" {
		dsn = "postgres://kaname:kaname@127.0.0.1:55432/kaname_test?sslmode=disable"
	}
	parsed, err := connection.ParseURI(dsn)
	if err != nil {
		t.Fatalf("el DSN de pruebas no se pudo interpretar: %v", err)
	}

	st := store.New(filepath.Join(t.TempDir(), "connections.toml"))
	kr := newFakeKeyring()

	c := parsed.Connection
	c.ID = "sesion01"
	c.Name = "base de pruebas"
	c.Environment = connection.Local
	if err := st.Add(c.Normalize()); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if parsed.Password != "" {
		if err := kr.Set(c.ID, parsed.Password); err != nil {
			t.Fatalf("guardar la contraseña: %v", err)
		}
	}

	sesion := NewSession(st, kr)
	t.Cleanup(sesion.Disconnect)
	return sesion, &Connections{store: st, keyring: kr}, c.ID
}

// saltearSinBase saltea el test si no hay Postgres, con el mismo criterio que
// internal/postgres: en CI, KANAME_REQUIRE_POSTGRES lo convierte en un fallo.
func saltearSinBase(t *testing.T, res ConnectResult) {
	t.Helper()
	if res.OK {
		return
	}
	msg := "sin detalle"
	if res.Failure != nil {
		msg = res.Failure.Message
	}
	if os.Getenv("KANAME_REQUIRE_POSTGRES") != "" {
		t.Fatalf("KANAME_REQUIRE_POSTGRES está puesto y no se pudo conectar: %s", msg)
	}
	t.Skipf("no hay Postgres de pruebas escuchando (%s).\n"+
		"Levantalo con: docker compose -f docker-compose.test.yml up -d", msg)
}

func TestConnectAbreLaSesionYLeeElEsquema(t *testing.T) {
	sesion, _, id := sesionDePrueba(t)

	res := sesion.Connect(context.Background(), id)
	saltearSinBase(t, res)

	if !res.Session.Connected {
		t.Fatal("Connect() dijo OK pero la sesión no figura conectada")
	}
	if res.Session.Server == nil {
		t.Fatal("la sesión vino sin información del servidor")
	}
	if !strings.HasPrefix(res.Session.Server.Display, "PostgreSQL ") {
		t.Errorf("Display = %q", res.Session.Server.Display)
	}
	if res.Session.ConnectionID != id {
		t.Errorf("ConnectionID = %q", res.Session.ConnectionID)
	}
	// Current tiene que decir lo mismo que devolvió Connect.
	if got := sesion.Current(); got.ConnectionID != id || !got.Connected {
		t.Errorf("Current() = %+v", got)
	}

	snap, err := sesion.Schema(context.Background(), false)
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	if snap.Database != res.Session.Server.CurrentDB {
		t.Errorf("el esquema es de %q y la sesión de %q", snap.Database, res.Session.Server.CurrentDB)
	}
	if len(snap.Schemas) == 0 {
		t.Error("el esquema no trajo ningún schema, ni siquiera public")
	}
}

// El árbol pide el esquema en cada render; leer el catálogo cada vez sería una
// consulta por clic.
func TestSchemaSeCacheaHastaQueSePidaRefrescar(t *testing.T) {
	sesion, _, id := sesionDePrueba(t)
	saltearSinBase(t, sesion.Connect(context.Background(), id))

	primera, err := sesion.Schema(context.Background(), false)
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	segunda, err := sesion.Schema(context.Background(), false)
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	if primera != segunda {
		t.Error("la segunda lectura no vino del caché")
	}

	tercera, err := sesion.Schema(context.Background(), true)
	if err != nil {
		t.Fatalf("Schema(refresh) error: %v", err)
	}
	if tercera == primera {
		t.Error("refrescar devolvió el mismo snapshot cacheado")
	}
}

func TestSchemaSinConexionNoInventaNada(t *testing.T) {
	sesion, _, _ := sesionDePrueba(t)
	if _, err := sesion.Schema(context.Background(), false); err == nil {
		t.Fatal("Schema() sin conexión no devolvió error")
	}
}

// Conectar cierra la anterior: dos pools contra bases distintas sin que la
// interfaz lo muestre es cómo se aplica un cambio donde no era.
func TestConectarDeNuevoDejaUnaSolaSesion(t *testing.T) {
	sesion, _, id := sesionDePrueba(t)
	saltearSinBase(t, sesion.Connect(context.Background(), id))

	res := sesion.Connect(context.Background(), id)
	if !res.OK {
		t.Fatalf("la segunda conexión falló: %+v", res.Failure)
	}
	if got := sesion.Current(); !got.Connected || got.ConnectionID != id {
		t.Errorf("Current() = %+v", got)
	}

	sesion.Disconnect()
	if got := sesion.Current(); got.Connected {
		t.Errorf("después de Disconnect, Current() = %+v", got)
	}
	// Desconectar dos veces no es un error.
	sesion.Disconnect()
}

// Sin contraseña el servidor rechaza, y el fallo tiene que llegar clasificado,
// no como un error crudo: es lo que S24 usa para decir qué revisar.
func TestConnectSinContrasenaDaUnFalloClasificado(t *testing.T) {
	sesion, _, id := sesionDePrueba(t)
	// Confirmar primero que con contraseña sí conecta, para no confundir un
	// entorno sin base con el caso que se quiere probar.
	saltearSinBase(t, sesion.Connect(context.Background(), id))
	sesion.Disconnect()

	sesion.keyring.(*fakeKeyring).datos = map[string]string{}

	res := sesion.Connect(context.Background(), id)
	if res.OK {
		t.Fatal("conectó sin contraseña contra un servidor que la exige")
	}
	if res.Failure == nil {
		t.Fatal("falló sin Failure")
	}
	if res.Failure.Kind == "" || res.Failure.Message == "" {
		t.Errorf("el fallo llegó sin clasificar: %+v", res.Failure)
	}
	if res.Failure.Detail == "" {
		t.Error("el fallo llegó sin el mensaje del motor")
	}
}

// La conexión marcada como solo lectura tiene que decirlo, y decir por qué.
func TestLaSesionExplicaPorQueEsDeSoloLectura(t *testing.T) {
	sesion, conns, id := sesionDePrueba(t)
	saltearSinBase(t, sesion.Connect(context.Background(), id))
	sesion.Disconnect()

	c, err := conns.store.Get(id)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	c.Safety.ReadOnly = true
	if err := conns.store.Update(c); err != nil {
		t.Fatalf("Update() error: %v", err)
	}

	res := sesion.Connect(context.Background(), id)
	if !res.OK {
		t.Fatalf("no conectó: %+v", res.Failure)
	}
	if !res.Session.ReadOnly {
		t.Fatal("la conexión es de solo lectura y la sesión no lo dice")
	}
	if !strings.Contains(res.Session.ReadOnlyReason, "configurada") {
		t.Errorf("ReadOnlyReason = %q: no explica que fue una decisión, no el servidor",
			res.Session.ReadOnlyReason)
	}
}
