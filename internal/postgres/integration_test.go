package postgres

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// defaultTestDSN apunta al Postgres de docker-compose.test.yml.
const defaultTestDSN = "postgres://kaname:kaname@127.0.0.1:55432/kaname_test?sslmode=disable"

// testDSN devuelve el DSN de la base de pruebas, o saltea el test si no hay
// ninguna escuchando.
//
// No hay build tag a propósito: así este archivo siempre compila y pasa vet, y
// un error acá se ve aunque nadie tenga Docker prendido.
func testDSN(t *testing.T) string {
	t.Helper()

	dsn := os.Getenv("KANAME_TEST_POSTGRES")
	if dsn == "" {
		dsn = defaultTestDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, f := Probe(ctx, dsn, "base de pruebas"); f != nil {
		t.Skipf(
			"no hay Postgres de pruebas escuchando (%s).\n"+
				"Levantalo con: docker compose -f docker-compose.test.yml up -d\n"+
				"O apuntá a otro con KANAME_TEST_POSTGRES=postgres://...",
			f.Message)
	}
	return dsn
}

func TestProbeContraUnaBaseReal(t *testing.T) {
	dsn := testDSN(t)

	info, f := Probe(context.Background(), dsn, "base de pruebas")
	if f != nil {
		t.Fatalf("Probe() falló: %s", f.Message)
	}

	if info.CurrentDB != "kaname_test" {
		t.Errorf("CurrentDB = %q, se esperaba kaname_test", info.CurrentDB)
	}
	if info.CurrentUser != "kaname" {
		t.Errorf("CurrentUser = %q, se esperaba kaname", info.CurrentUser)
	}
	if !strings.HasPrefix(info.Display, "PostgreSQL ") {
		t.Errorf("Display = %q", info.Display)
	}
	if !info.Supported() {
		t.Errorf("la versión %d quedó por debajo del mínimo %d", info.VersionNum, MinServerVersion)
	}
	if info.InRecovery {
		t.Error("InRecovery = true: la base de pruebas no es una réplica")
	}
	if info.LatencyMS < 0 {
		t.Errorf("LatencyMS = %d", info.LatencyMS)
	}
	t.Logf("conectado a %s (%d) en %d ms", info.Display, info.VersionNum, info.LatencyMS)
}

func TestProbeConContrasenaMalaDaAuth(t *testing.T) {
	dsn := testDSN(t)
	malo := cambiarContrasena(t, dsn, "definitivamente-no-es-esta")

	_, f := Probe(context.Background(), malo, "base de pruebas")
	if f == nil {
		t.Fatal("Probe() con contraseña incorrecta no falló")
	}
	if f.Kind != FailureAuth {
		t.Errorf("Kind = %q, se esperaba %q (mensaje: %q)", f.Kind, FailureAuth, f.Message)
	}
	if strings.Contains(f.Message, "definitivamente-no-es-esta") {
		t.Errorf("el mensaje filtra la contraseña: %q", f.Message)
	}
}

func TestProbeConBaseInexistenteDaDatabase(t *testing.T) {
	dsn := testDSN(t)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/no_existe_esta_base"

	_, f := Probe(context.Background(), u.String(), "base de pruebas")
	if f == nil {
		t.Fatal("Probe() contra una base inexistente no falló")
	}
	if f.Kind != FailureDatabase {
		t.Errorf("Kind = %q, se esperaba %q (mensaje: %q)", f.Kind, FailureDatabase, f.Message)
	}
	if f.SQLState != sqlStateUndefinedDatabase {
		t.Errorf("SQLState = %q, se esperaba %q", f.SQLState, sqlStateUndefinedDatabase)
	}
}

func TestProbeContraUnPuertoCerradoDaNetwork(t *testing.T) {
	// No usa testDSN: este caso justamente no necesita una base viva.
	const dsn = "postgres://u:p@127.0.0.1:1/db?sslmode=disable&connect_timeout=2"

	_, f := Probe(context.Background(), dsn, "puerto cerrado")
	if f == nil {
		t.Fatal("Probe() contra un puerto cerrado no falló")
	}
	if f.Kind != FailureNetwork && f.Kind != FailureTimeout {
		t.Errorf("Kind = %q, se esperaba network o timeout (mensaje: %q)", f.Kind, f.Message)
	}
}

func TestConnectDevuelvePoolUsable(t *testing.T) {
	dsn := testDSN(t)

	pool, info, f := Connect(context.Background(), dsn, "base de pruebas", 4)
	if f != nil {
		t.Fatalf("Connect() falló: %s", f.Message)
	}
	defer pool.Close()

	if info.CurrentDB != "kaname_test" {
		t.Errorf("CurrentDB = %q", info.CurrentDB)
	}

	var uno int
	if err := pool.QueryRow(context.Background(), "select 1").Scan(&uno); err != nil {
		t.Fatalf("el pool no sirve para consultar: %v", err)
	}
	if uno != 1 {
		t.Errorf("select 1 devolvió %d", uno)
	}
}

// pgxpool es perezoso: sin verificar al abrir, un error de credenciales
// aparecería recién en la primera query, cuando el usuario ya cree que está
// conectado.
func TestConnectFallaAlAbrirYNoEnLaPrimeraQuery(t *testing.T) {
	dsn := testDSN(t)
	malo := cambiarContrasena(t, dsn, "tampoco-es-esta")

	pool, _, f := Connect(context.Background(), malo, "base de pruebas", 2)
	if f == nil {
		pool.Close()
		t.Fatal("Connect() con credenciales malas devolvió un pool")
	}
	if pool != nil {
		t.Error("Connect() devolvió un pool además del error")
	}
	if f.Kind != FailureAuth {
		t.Errorf("Kind = %q, se esperaba %q", f.Kind, FailureAuth)
	}
}

// La aplicación se identifica en pg_stat_activity para que quien administre el
// servidor sepa de dónde vino una consulta.
func TestLaConexionSeIdentificaComoKaname(t *testing.T) {
	dsn := testDSN(t)
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("application_name", "kaname")
	u.RawQuery = q.Encode()

	pool, _, f := Connect(context.Background(), u.String(), "base de pruebas", 2)
	if f != nil {
		t.Fatalf("Connect() falló: %s", f.Message)
	}
	defer pool.Close()

	var nombre string
	if err := pool.QueryRow(context.Background(),
		"select current_setting('application_name')").Scan(&nombre); err != nil {
		t.Fatal(err)
	}
	if nombre != "kaname" {
		t.Errorf("application_name = %q, se esperaba kaname", nombre)
	}
}

func cambiarContrasena(t *testing.T, dsn, nueva string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	usuario := u.User.Username()
	u.User = url.UserPassword(usuario, nueva)
	return u.String()
}
