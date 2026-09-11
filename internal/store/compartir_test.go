package store

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

func dosParaCompartir() []connection.Connection {
	return []connection.Connection{
		{
			ID: "a1", Name: "shop local", Engine: connection.Postgres, Host: "127.0.0.1", Port: 5432,
			Database: "shop", User: "dev", Environment: connection.Local, Folder: "Shop",
			SSLMode: connection.SSLPrefer,
		},
		{
			ID: "a2", Name: "shop prod", Engine: connection.MariaDB, Host: "db.prod.internal", Port: 3306,
			Database: "shop", User: "app", Environment: connection.Production, Folder: "Shop",
			SSLMode: connection.SSLVerifyFull, Safety: connection.Safety{ReadOnly: true},
			SSH: tunnel.Config{Enabled: true, Host: "bastion", Port: 22, User: "ops", Auth: tunnel.AuthKeyFile, KeyPath: "~/.ssh/id_ed25519"},
		},
	}
}

// Un archivo exportado ES una libreta: lo que lee Decode es lo que se escribió,
// y un Store lo lee también, para que pegarlo a mano en connections.toml
// funcione.
func TestExportarYVolverALeerPreservaLasConexiones(t *testing.T) {
	quiere := dosParaCompartir()
	data, err := Encode(quiere, time.Date(2026, 9, 11, 10, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}
	if !strings.Contains(string(data), "exportadas el 2026-09-11 10:30") {
		t.Errorf("falta la fecha en el encabezado:\n%s", data)
	}

	dec, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if len(dec.Ignored) != 0 {
		t.Errorf("un archivo escrito por Kaname tiene claves que Kaname no lee: %v", dec.Ignored)
	}
	if len(dec.Connections) != 2 {
		t.Fatalf("Decode() trajo %d conexiones, se esperaban 2", len(dec.Connections))
	}
	for i := range quiere {
		if dec.Connections[i] != quiere[i] {
			t.Errorf("la conexión %d cambió en la ida y vuelta:\n got %+v\nwant %+v", i, dec.Connections[i], quiere[i])
		}
	}

	// Y como libreta.
	path := filepath.Join(t.TempDir(), "connections.toml")
	escribirCrudo(t, path, string(data))
	lista, err := New(path).List()
	if err != nil {
		t.Fatalf("List() sobre el archivo exportado: %v", err)
	}
	if len(lista) != 2 {
		t.Errorf("como libreta trae %d conexiones, se esperaban 2", len(lista))
	}
}

// El archivo lo escribe otra persona, o una versión de Kaname que todavía no
// existe. Lo que no se conoce no se lee, y se dice cuál fue.
func TestDecodeDiceQueClavesIgnoro(t *testing.T) {
	data := `version = 1

[[connection]]
  id = "x1"
  name = "a mano"
  engine = "postgres"
  host = "h"
  database = "d"
  user = "u"
  password = "s3cr3t"
  color = "rojo"

  [connection.ssh]
    enabled = true
    host = "b"
    user = "ops"
    passphrase = "otra"
`
	dec, err := Decode([]byte(data))
	if err != nil {
		t.Fatalf("Decode() error: %v", err)
	}
	if len(dec.Connections) != 1 {
		t.Fatalf("Decode() trajo %d conexiones, se esperaba 1", len(dec.Connections))
	}
	quiere := []string{"connection.color", "connection.password", "connection.ssh.passphrase"}
	if strings.Join(dec.Ignored, ",") != strings.Join(quiere, ",") {
		t.Errorf("Ignored = %v, se esperaba %v", dec.Ignored, quiere)
	}
	if strings.Contains(dec.Connections[0].String(), "s3cr3t") {
		t.Error("la contraseña del archivo terminó en la conexión")
	}
}

func TestDecodeRechazaLoQueNoEsUnaLibreta(t *testing.T) {
	if _, err := Decode([]byte("esto no es toml = = =")); err == nil {
		t.Error("Decode() aceptó un archivo que no es TOML")
	}
	if _, err := Decode([]byte("version = 99\n")); err == nil || !strings.Contains(err.Error(), "actualizá Kaname") {
		t.Errorf("una versión futura tendría que rechazarse pidiendo actualizar; dio %v", err)
	}
	grande := []byte(strings.Repeat("#", MaxSharedFileBytes+1))
	if _, err := Decode(grande); !errors.Is(err, ErrSharedFileTooBig) {
		t.Errorf("un archivo de más de un mega dio %v, se esperaba ErrSharedFileTooBig", err)
	}
	// Vacío es válido: cero conexiones. Quien importa decide qué decir.
	dec, err := Decode([]byte("version = 1\n"))
	if err != nil || len(dec.Connections) != 0 {
		t.Errorf("un archivo sin conexiones dio (%v, %v)", dec, err)
	}
}

// Importar tres y fallar en la cuarta no puede dejar tres en la libreta.
func TestAddAllEsTodoONada(t *testing.T) {
	s := nuevo(t)
	buena := connection.Connection{ID: "b1", Name: "buena", Engine: connection.SQLite, Database: "/x.db"}
	rota := connection.Connection{ID: "b2", Name: "rota", Engine: connection.SQLite} // sin base

	if err := s.AddAll([]connection.Connection{buena, rota}); err == nil {
		t.Fatal("AddAll() aceptó una conexión rota")
	} else if !strings.Contains(err.Error(), `"rota"`) {
		t.Errorf("el error no dice cuál falló: %v", err)
	}
	lista, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(lista) != 0 {
		t.Errorf("quedaron %d conexiones después de un AddAll fallido; se esperaba 0", len(lista))
	}

	otra := buena
	otra.ID, otra.Name = "b3", "otra"
	if err := s.AddAll([]connection.Connection{buena, otra}); err != nil {
		t.Fatalf("AddAll() error: %v", err)
	}
	lista, _ = s.List()
	if len(lista) != 2 {
		t.Errorf("quedaron %d conexiones, se esperaban 2", len(lista))
	}

	// Un ID repetido —contra la libreta o dentro del mismo lote— también es
	// todo o nada.
	repetida := buena
	repetida.Name = "repetida"
	if err := s.AddAll([]connection.Connection{repetida}); !errors.Is(err, ErrDuplicateID) {
		t.Errorf("un ID que ya está dio %v, se esperaba ErrDuplicateID", err)
	}
	c1 := connection.Connection{ID: "c", Name: "c1", Engine: connection.SQLite, Database: "/c.db"}
	c2 := c1
	c2.Name = "c2"
	if err := s.AddAll([]connection.Connection{c1, c2}); !errors.Is(err, ErrDuplicateID) {
		t.Errorf("dos del mismo lote con el mismo ID dieron %v, se esperaba ErrDuplicateID", err)
	}
	lista, _ = s.List()
	if len(lista) != 2 {
		t.Errorf("un lote rechazado dejó %d conexiones, se esperaban 2", len(lista))
	}
}
