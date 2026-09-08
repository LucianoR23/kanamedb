package postgres

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// La prueba que justifica el diseño del túnel: pgx habla con Postgres a través
// de SSH.
//
// El DSN apunta a un host que desde esta máquina no resuelve —es el nombre de
// servicio de la red de compose—, así que si la consulta funciona es porque
// salió por el túnel y no por otro lado.
//
// Que no se abra ningún puerto lo verifica TestElTunelNoEscuchaEnNingunPuerto,
// que es una comprobación distinta: esta prueba que el camino funciona, aquella
// que es el camino correcto.
func TestPgxHablaPorElTunelSinAbrirPuertos(t *testing.T) {
	// La base tiene que ser alcanzable POR EL BASTIÓN, no por nosotros: es el
	// nombre de servicio de la red de compose. Que el DSN apunte ahí y funcione
	// es parte de la prueba — desde esta máquina, ese nombre no resuelve.
	remoto := os.Getenv("KANAME_TEST_PG_INTERNO")
	if remoto == "" {
		remoto = "postgres:5432"
	}

	cfg := tunnel.Config{
		Enabled: true,
		Host:    "127.0.0.1",
		Port:    52222,
		User:    "kaname",
		Auth:    tunnel.AuthPassword,
	}
	if v := os.Getenv("KANAME_TEST_SSH_PORT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("KANAME_TEST_SSH_PORT=%q no es un número", v)
		}
		cfg.Port = n
	}

	kh := tunnel.NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts"))
	insp, err := tunnel.Inspect(context.Background(), cfg, kh)
	if err != nil {
		if os.Getenv("KANAME_REQUIRE_SSH") != "" {
			t.Fatalf("KANAME_REQUIRE_SSH está puesto y no hay servidor SSH: %v", err)
		}
		t.Skipf("no hay servidor SSH de pruebas (%v).\n"+
			"Levantalo con: docker compose -f docker-compose.test.yml up -d", err)
	}
	if err := kh.Trust(cfg.Address(), insp.AuthorizedKey); err != nil {
		t.Fatalf("Trust() error: %v", err)
	}

	cliente, err := tunnel.Dial(context.Background(), cfg, kh,
		tunnel.Secrets{Password: "kaname"}, tunnel.DialOptions{})
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer cliente.Close()

	// El DSN apunta al host tal como lo ve el bastión.
	dsn := "postgres://kaname:kaname@" + remoto + "/kaname_test?sslmode=disable"
	pool, info, f := Connect(context.Background(), dsn, "por el túnel", ConnectOptions{
		MaxConns: 2,
		DialFunc: cliente.DialContext,
	})
	if f != nil {
		t.Fatalf("Connect() por el túnel falló: %s · %s", f.Message, f.Detail)
	}
	defer pool.Close()

	if info == nil || info.VersionNum == 0 {
		t.Fatal("no se leyó la versión del servidor a través del túnel")
	}

	res, f := Run(context.Background(), pool, "select 1 as uno", RunOptions{})
	if f != nil {
		t.Fatalf("consulta por el túnel: %s", f.Message)
	}
	if len(res.Results) != 1 || len(res.Results[0].Rows) != 1 || *res.Results[0].Rows[0][0] != "1" {
		t.Errorf("la consulta por el túnel devolvió %+v", res.Results)
	}
}
