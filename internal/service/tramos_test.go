package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/layout"
	"github.com/LucianoR23/kanamedb/internal/store"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// sesionDe abre una sesión contra el motor que se le pida, por el mismo camino
// que recorre la interfaz.
func sesionDe(t *testing.T, nombre, uri string) (*Session, connection.Connection) {
	t.Helper()
	st := store.New(filepath.Join(t.TempDir(), "connections.toml"))
	kr := newFakeKeyring()

	var c connection.Connection
	if uri == "" {
		c = connection.Connection{
			ID: "s1", Name: "archivo", Engine: connection.SQLite,
			Database: filepath.ToSlash(filepath.Join(t.TempDir(), "kaname.db")),
		}
	} else {
		parsed, err := connection.ParseURI(uri)
		if err != nil {
			t.Fatalf("DSN de pruebas: %v", err)
		}
		c = parsed.Connection
		c.ID, c.Name = "s1", nombre
		if parsed.Password != "" {
			_ = kr.Set(c.ID, parsed.Password)
		}
	}
	c.Environment = connection.Local
	if err := st.Add(c.Normalize()); err != nil {
		t.Fatalf("Add(): %v", err)
	}

	sesion := NewSession(st, kr,
		tunnel.NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts")),
		layout.New(filepath.Join(t.TempDir(), "layouts")))
	t.Cleanup(sesion.Disconnect)

	res := sesion.Connect(context.Background(), c.ID)
	if !res.OK {
		msg := "sin detalle"
		if res.Failure != nil {
			msg = res.Failure.Message
		}
		if os.Getenv("KANAME_REQUIRE_ENGINES") != "" || uri == "" {
			t.Fatalf("no conectó a %s: %s", nombre, msg)
		}
		t.Skipf("no hay %s escuchando (%s)", nombre, msg)
	}
	return sesion, c
}

// TestElApplySePartePorMotor es el test que justifica todo el diseño de
// tramos, y el que hace verificable la promesa que S15 muestra en pantalla.
//
// La casilla «Una sola transacción» dice «todo o nada». Contra Postgres y
// SQLite eso es cierto. Contra MySQL y MariaDB es FALSO, y no por falta de
// soporte sino por algo peor: un DDL en el medio de una transacción hace commit
// implícito de todo lo anterior y deja la conexión afuera, así que el ROLLBACK
// final no revierte nada. Prometer «todo o nada» y aplicar todo es la peor
// forma de fallar que hay.
//
// El mismo changeset —tres sentencias, la segunda inválida— tiene que dar dos
// resultados DISTINTOS y los dos correctos:
//
//   - Con DDL transaccional: UN tramo, se revierte todo, la primera tabla no
//     existe.
//   - Sin DDL transaccional: TRES tramos, la primera tabla queda creada, y el
//     resultado lo dice en vez de fingir lo contrario.
func TestElApplySePartePorMotor(t *testing.T) {
	casos := []struct {
		nombre string
		uri    string
		// transaccional es lo que el motor promete de verdad.
		transaccional bool
	}{
		{"postgres", "postgres://kaname:kaname@127.0.0.1:55432/kaname_test?sslmode=disable", true},
		{"mysql", "mysql://kaname:kaname@127.0.0.1:53306/kaname_test", false},
		{"mariadb", "mariadb://kaname:kaname@127.0.0.1:53307/kaname_test", false},
		{"sqlite", "", true},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq := esquemaDeApply(t, sesion, c)
			tipo := tipoEnteroDe(c.Engine)

			limpiar := func() {
				abierta, err := sesion.abierta()
				if err != nil {
					return
				}
				for _, tb := range []string{"kn_tr_una", "kn_tr_tres"} {
					_ = abierta.db.Exec(context.Background(),
						"DROP TABLE IF EXISTS "+califica(c, esq, tb))
				}
			}
			limpiar()
			t.Cleanup(limpiar)

			// Tres cambios, uno imposible: una columna sobre una tabla que no
			// existe. Se declara en el medio y se EJECUTA última, porque
			// Ordered() reordena por fase de dependencias y los CREATE TABLE
			// van primero. Es a propósito que el test pase por ese reordenado:
			// es el que de verdad decide en qué tramo cae cada sentencia.
			pasos := []change.Change{
				{Type: change.CreateTable, Schema: esq, Table: "kn_tr_una", Source: "test",
					Columns: []change.Column{{Name: "id", DataType: tipo}}, Names: []string{"id"}},
				{Type: change.AddColumn, Schema: esq, Table: "kn_tr_no_existe", Source: "test",
					Column: &change.Column{Name: "c", DataType: tipo, Nullable: true}},
				{Type: change.CreateTable, Schema: esq, Table: "kn_tr_tres", Source: "test",
					Columns: []change.Column{{Name: "id", DataType: tipo}}, Names: []string{"id"}},
			}
			for _, p := range pasos {
				if _, err := sesion.Stage(ctx, p, ""); err != nil {
					t.Fatalf("Stage(%s): %v", p.Type, err)
				}
			}

			res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: true})
			if err != nil {
				t.Fatalf("Apply() devolvió error de programa: %v", err)
			}
			if res.OK {
				t.Fatal("Apply() dijo que salió bien con una sentencia imposible")
			}

			quiere := 3
			if caso.transaccional {
				quiere = 1
			}
			if res.Tramos != quiere {
				t.Errorf("el apply se partió en %d tramos y en %s tienen que ser %d",
					res.Tramos, caso.nombre, quiere)
			}
			// Cuál falló: el único que hay, o el tercero —los dos CREATE se
			// ejecutan primero y cada uno va en su propio tramo—.
			quiereFallido := 1
			if !caso.transaccional {
				quiereFallido = 3
			}
			if res.TramoFallido != quiereFallido {
				t.Errorf("dice que falló el tramo %d y tendría que ser el %d",
					res.TramoFallido, quiereFallido)
			}
			if res.RolledBack != caso.transaccional {
				t.Errorf("RolledBack = %v y en %s tiene que ser %v",
					res.RolledBack, caso.nombre, caso.transaccional)
			}

			// Y ahora lo que de verdad importa: qué quedó EN LA BASE.
			_, err = sesion.TableDetail(ctx, esq, "kn_tr_una")
			existe := err == nil
			if caso.transaccional && existe {
				t.Error("la tabla de la primera sentencia quedó creada: la casilla " +
					"«una sola transacción» prometió todo o nada y no lo cumplió")
			}
			if !caso.transaccional && !existe {
				t.Error("la tabla de la primera sentencia NO quedó creada, pero este motor " +
					"no puede revertir DDL: el resultado está describiendo algo que no pasó")
			}

			// La segunda tabla también quedó, y decirlo importa: es la
			// diferencia entre «falló y no pasó nada» y «falló a la mitad».
			if _, err := sesion.TableDetail(ctx, esq, "kn_tr_tres"); (err == nil) == caso.transaccional {
				t.Errorf("kn_tr_tres existe=%v y en %s tendría que ser %v",
					err == nil, caso.nombre, !caso.transaccional)
			}

			// El changeset conserva lo que no quedó aplicado, y solo eso.
			vista, err := sesion.Changeset(ctx)
			if err != nil {
				t.Fatalf("Changeset(): %v", err)
			}
			quedan := 3
			if !caso.transaccional {
				// Las DOS que se aplicaron de verdad salen de la lista. Queda
				// la que falló, para poder corregirla y reintentar.
				quedan = 1
			}
			if vista.Summary.Total != quedan {
				t.Errorf("quedaron %d cambios pendientes y tendrían que quedar %d",
					vista.Summary.Total, quedan)
			}
		})
	}
}

func esquemaDeApply(t *testing.T, s *Session, c connection.Connection) string {
	t.Helper()
	snap, err := s.Schema(context.Background(), true)
	if err != nil {
		t.Fatalf("Schema(): %v", err)
	}
	return esquemaDePrueba(c.Engine, snap)
}

func califica(c connection.Connection, esq, tabla string) string {
	if esq == "" {
		return `"` + tabla + `"`
	}
	if c.Engine == connection.MySQL || c.Engine == connection.MariaDB {
		return "`" + esq + "`.`" + tabla + "`"
	}
	return `"` + esq + `"."` + tabla + `"`
}

// TestLaPantallaNoPrometeLoQueElMotorNoCumple.
//
// La casilla «Una sola transacción» decía «todo o nada» pasara lo que pasara.
// Contra MySQL y MariaDB eso es falso, y la forma de fallar es la peor: la
// interfaz promete que se revierte, el motor aplica todo, y el usuario se
// entera mirando la base.
//
// Ahora `ChangesetView` trae lo que el motor de verdad puede, y la pantalla lo
// usa en vez de suponerlo. Esto comprueba el dato, no el pixel: si el dato
// miente, el pixel también.
func TestLaPantallaNoPrometeLoQueElMotorNoCumple(t *testing.T) {
	casos := []struct {
		nombre        string
		uri           string
		transaccional bool
	}{
		{"postgres", "postgres://kaname:kaname@127.0.0.1:55432/kaname_test?sslmode=disable", true},
		{"mysql", "mysql://kaname:kaname@127.0.0.1:53306/kaname_test", false},
		{"mariadb", "mariadb://kaname:kaname@127.0.0.1:53307/kaname_test", false},
		{"sqlite", "", true},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			sesion, c := sesionDe(t, caso.nombre, caso.uri)
			ctx := context.Background()
			esq := esquemaDeApply(t, sesion, c)
			tipo := tipoEnteroDe(c.Engine)

			for _, tabla := range []string{"kn_pr_a", "kn_pr_b"} {
				if _, err := sesion.Stage(ctx, change.Change{
					Type: change.CreateTable, Schema: esq, Table: tabla, Source: "test",
					Columns: []change.Column{{Name: "id", DataType: tipo}},
					Names:   []string{"id"},
				}, ""); err != nil {
					t.Fatalf("Stage(): %v", err)
				}
			}

			vista, err := sesion.Changeset(ctx)
			if err != nil {
				t.Fatalf("Changeset(): %v", err)
			}

			if vista.TransactionalDDL != caso.transaccional {
				t.Errorf("TransactionalDDL = %v y en %s tiene que ser %v",
					vista.TransactionalDDL, caso.nombre, caso.transaccional)
			}
			if vista.Engine != c.Engine {
				t.Errorf("Engine = %q y la conexión es %q", vista.Engine, c.Engine)
			}

			quiereTramos := 1
			if !caso.transaccional {
				quiereTramos = 2
			}
			if vista.Tramos != quiereTramos {
				t.Errorf("Tramos = %d y en %s tienen que ser %d",
					vista.Tramos, caso.nombre, quiereTramos)
			}

			// Y el aviso, que es lo que se lee antes de apretar Aplicar.
			var avisa bool
			for _, a := range vista.Warnings {
				if strings.Contains(a, "no puede revertir cambios de esquema") {
					avisa = true
				}
			}
			if avisa == caso.transaccional {
				t.Errorf("avisa=%v en %s, y tendría que ser %v.\nAvisos: %q",
					avisa, caso.nombre, !caso.transaccional, vista.Warnings)
			}
			if avisa {
				// El aviso tiene que nombrar el motor: «el motor no revierte»
				// no le dice a nadie contra qué está trabajando.
				var nombra bool
				for _, a := range vista.Warnings {
					if strings.Contains(a, c.Engine.Label()) {
						nombra = true
					}
				}
				if !nombra {
					t.Errorf("el aviso no nombra el motor: %q", vista.Warnings)
				}
			}
		})
	}
}
