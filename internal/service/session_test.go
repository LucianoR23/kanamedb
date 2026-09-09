package service

import (
	"context"

	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/layout"
	"github.com/LucianoR23/kanamedb/internal/store"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
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

	// known_hosts y diagramas propios del test: ninguna conexión de prueba usa
	// túnel ni ERD, pero la sesión los necesita para poder usarlos si alguna lo
	// hiciera. En un directorio temporal, para no tocar los del usuario.
	sesion := NewSession(
		st, kr,
		tunnel.NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts")),
		layout.New(filepath.Join(t.TempDir(), "layouts")),
	)
	t.Cleanup(sesion.Disconnect)
	return sesion, &Connections{store: st, keyring: kr, known: tunnel.NewKnownHosts(filepath.Join(t.TempDir(), "kh"))}, c.ID
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

// connectOptions es el pegamento entre las protecciones de la conexión y el
// pool. Que `Safety.StatementTimeout()` devuelva 30 s y que `Connect` aplique
// lo que recibe están probados por separado; sin este test, borrar la línea que
// los une dejaría los dos verdes y el corte por tiempo sin aplicarse.
func TestConnectOptionsLlevaLasProteccionesAlPool(t *testing.T) {
	c := connection.Connection{
		Engine: connection.Postgres,
		Safety: connection.Safety{
			ReadOnly: true,
			// Cero significa "usá el default", que son 30 s.
			StatementTimeoutSeconds: 0,
		},
	}

	opts := connectOptions(c)
	if !opts.ReadOnly {
		t.Error("la conexión es de solo lectura y el pool no se entera")
	}
	if got, quiere := opts.StatementTimeout, 30*time.Second; got != quiere {
		t.Errorf("StatementTimeout = %v, se esperaba %v", got, quiere)
	}
	if opts.MaxConns <= 0 {
		t.Errorf("MaxConns = %d: el pool quedaría sin tamaño", opts.MaxConns)
	}

	// Y sin solo lectura, el pool tampoco lo inventa.
	c.Safety.ReadOnly = false
	c.Safety.StatementTimeoutSeconds = connection.Unlimited
	sin := connectOptions(c)
	if sin.ReadOnly {
		t.Error("el pool se puso en solo lectura sin que la conexión lo pidiera")
	}
	if sin.StatementTimeout != 0 {
		t.Errorf("StatementTimeout = %v: sin límite tiene que llegar como cero al pool",
			sin.StatementTimeout)
	}
}

// A diferencia del esquema, el detalle de una tabla NO se cachea: se mira justo
// cuando alguien está por cambiar la estructura o acaba de hacerlo, que es
// cuando una lectura vieja sale más cara.
func TestTableDetailNoSeCacheaYVeLosCambios(t *testing.T) {
	sesion, _, id := sesionDePrueba(t)
	saltearSinBase(t, sesion.Connect(context.Background(), id))
	ctx := context.Background()

	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatalf("abierta() error: %v", err)
	}
	const esq = "kn_detalle_servicio"
	for _, sql := range []string{
		"DROP SCHEMA IF EXISTS " + esq + " CASCADE",
		"CREATE SCHEMA " + esq,
		"CREATE TABLE " + esq + ".t (id bigint PRIMARY KEY)",
	} {
		if _, err := abierta.pool.Exec(ctx, sql); err != nil {
			t.Fatalf("no se pudo preparar la fixture (%s): %v", sql, err)
		}
	}
	t.Cleanup(func() {
		_, _ = abierta.pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+esq+" CASCADE")
	})

	antes, err := sesion.TableDetail(ctx, esq, "t")
	if err != nil {
		t.Fatalf("TableDetail() error: %v", err)
	}
	if len(antes.Columns) != 1 {
		t.Fatalf("la tabla arranca con %d columnas", len(antes.Columns))
	}

	if _, err := abierta.pool.Exec(ctx, "ALTER TABLE "+esq+".t ADD COLUMN nota text"); err != nil {
		t.Fatalf("no se pudo alterar la tabla: %v", err)
	}

	despues, err := sesion.TableDetail(ctx, esq, "t")
	if err != nil {
		t.Fatalf("TableDetail() error: %v", err)
	}
	if len(despues.Columns) != 2 {
		t.Errorf("después del ALTER hay %d columnas, se esperaban 2: la lectura vino de un caché",
			len(despues.Columns))
	}
}

func TestTableDetailSinConexionNoInventaNada(t *testing.T) {
	sesion, _, _ := sesionDePrueba(t)
	if _, err := sesion.TableDetail(context.Background(), "public", "cualquiera"); err == nil {
		t.Fatal("TableDetail() sin conexión no devolvió error")
	}
}

// El acomodado del diagrama sobrevive a cerrar la pestaña, y cada esquema tiene
// el suyo.
func TestErdLayoutSeGuardaPorEsquema(t *testing.T) {
	sesion, _, id := sesionDePrueba(t)
	saltearSinBase(t, sesion.Connect(context.Background(), id))

	// Sin nada guardado no hay error: nunca haber abierto el diagrama es normal.
	vacio, err := sesion.ErdLayout("public")
	if err != nil {
		t.Fatalf("ErdLayout() falló: %v", err)
	}
	if len(vacio) != 0 {
		t.Errorf("ErdLayout() = %v en una conexión nueva", vacio)
	}

	if err := sesion.SaveErdLayout("public", layout.Positions{
		"public.pedidos": {X: 12, Y: 34},
	}); err != nil {
		t.Fatalf("SaveErdLayout() falló: %v", err)
	}
	if err := sesion.SaveErdLayout("ventas", layout.Positions{
		"ventas.facturas": {X: 99, Y: 0},
	}); err != nil {
		t.Fatalf("SaveErdLayout(ventas) falló: %v", err)
	}

	pub, err := sesion.ErdLayout("public")
	if err != nil {
		t.Fatal(err)
	}
	if pub["public.pedidos"].X != 12 || pub["public.pedidos"].Y != 34 {
		t.Errorf("public = %v", pub)
	}
	if _, hay := pub["ventas.facturas"]; hay {
		t.Errorf("el diagrama de public trajo una tabla de ventas: %v", pub)
	}
}

func TestErdLayoutSinConexionNoInventaNada(t *testing.T) {
	sesion, _, _ := sesionDePrueba(t)
	if _, err := sesion.ErdLayout("public"); err == nil {
		t.Error("ErdLayout() sin conexión no devolvió error")
	}
	if err := sesion.SaveErdLayout("public", layout.Positions{}); err == nil {
		t.Error("SaveErdLayout() sin conexión no devolvió error")
	}
}

// TestConectarAUnMotorQueNoEstaEnganchadoLoDiceClaro.
//
// El editor de conexiones ofrece los cuatro motores con tres deshabilitados,
// pero el archivo de conexiones se edita a mano —está pensado para eso— así
// que una conexión con `engine = "mysql"` llega igual hasta acá. Sin una
// comprobación del lado del servicio, ese DSN —`usuario:clave@tcp(...)`— se le
// entrega a pgx, que contesta con un error de parseo que no le dice nada a
// nadie.
//
// Ver CLAUDE.md: una comprobación que vive solo del lado de la interfaz no es
// una protección, es un cartel.
func TestConectarAUnMotorQueNoEstaEnganchadoLoDiceClaro(t *testing.T) {
	for _, motor := range []connection.Engine{
		connection.MySQL, connection.MariaDB, connection.SQLite,
	} {
		t.Run(motor.String(), func(t *testing.T) {
			st := store.New(filepath.Join(t.TempDir(), "connections.toml"))
			c := connection.Connection{
				ID: "x1", Name: "editada a mano", Engine: motor,
				Host: "127.0.0.1", User: "kaname", Database: "kaname_test",
				Environment: connection.Local,
			}
			if motor == connection.SQLite {
				c.Host, c.User = "", ""
				c.Database = filepath.ToSlash(filepath.Join(t.TempDir(), "a.db"))
			}
			if err := st.Add(c.Normalize()); err != nil {
				t.Fatalf("el archivo de conexiones no aceptó la conexión: %v", err)
			}

			sesion := NewSession(st, newFakeKeyring(),
				tunnel.NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts")),
				layout.New(filepath.Join(t.TempDir(), "layouts")))
			t.Cleanup(sesion.Disconnect)

			res := sesion.Connect(context.Background(), c.ID)
			if res.OK {
				t.Fatal("dijo que conectó a un motor que el servicio todavía no despacha")
			}
			if res.Failure == nil {
				t.Fatal("falló sin decir por qué")
			}
			if !strings.Contains(res.Failure.Message, motor.Label()) {
				t.Errorf("el mensaje no nombra el motor, así que no se entiende qué pasó: %q",
					res.Failure.Message)
			}
			// El error de pgx sobre un DSN que no es suyo es lo que había antes,
			// y no se puede accionar.
			for _, prohibido := range []string{"parse", "cannot parse", "keyword"} {
				if strings.Contains(strings.ToLower(res.Failure.Message), prohibido) {
					t.Errorf("el mensaje es el error crudo del driver de Postgres: %q",
						res.Failure.Message)
				}
			}
		})
	}
}
