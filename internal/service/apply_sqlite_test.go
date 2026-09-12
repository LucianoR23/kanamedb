package service

import (
	"context"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/change"
)

// TestElRebuildDeSQLiteSinTransaccionNoDisparaElCascade cierra el hueco que
// dejaba la casilla «Una sola transacción» apagada.
//
// sqlite.Conn.Begin apaga las claves foráneas antes de reconstruir una tabla,
// y sqlite.Conn.Exec advierte que por él una reconstrucción «borraría las
// filas de las tablas hijas» sin dar error. Con la casilla apagada, cada DDL
// iba por Exec —el camino que la advertencia prohíbe—, así que el mismo cambio
// que con la casilla puesta conservaba las filas hijas, sin ella las borraba.
// Es la clase de invariante que se cuida en un camino y se saltea en el otro.
// Hallazgo K-01 de docs/reviews/audit-fable-2026-09-11.md.
func TestElRebuildDeSQLiteSinTransaccionNoDisparaElCascade(t *testing.T) {
	sesion, _ := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`CREATE TABLE padre (id integer PRIMARY KEY, nombre text)`,
		`CREATE TABLE hija (
			id integer PRIMARY KEY,
			pid integer REFERENCES padre(id) ON DELETE CASCADE)`,
		`INSERT INTO padre VALUES (1,'a'), (2,'b')`,
		`INSERT INTO hija VALUES (10,1), (11,2)`,
	} {
		if err := abierta.db.Exec(ctx, sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	if _, err := sesion.Schema(ctx, true); err != nil {
		t.Fatalf("Schema(refresh): %v", err)
	}

	_, err = sesion.Stage(ctx, change.Change{
		Type: change.SetNotNull, Schema: "main", Table: "padre", Source: "test",
		Column: &change.Column{Name: "nombre", DataType: "text"},
	}, "")
	if err != nil {
		t.Fatalf("Stage(SetNotNull): %v", err)
	}

	res, err := sesion.Apply(ctx, ApplyOptions{SingleTransaction: false})
	if err != nil {
		t.Fatalf("Apply(): %v", err)
	}
	if !res.OK {
		t.Fatalf("Apply() falló: %+v", res.Failure)
	}

	if n := contar(t, abierta, "hija"); n != 2 {
		t.Fatalf("después de reconstruir «padre» sin transacción única, «hija» quedó "+
			"con %d filas de 2: el DROP TABLE del rebuild corrió con las claves "+
			"foráneas encendidas y disparó el ON DELETE CASCADE", n)
	}
	if n := contar(t, abierta, "padre"); n != 2 {
		t.Errorf("«padre» quedó con %d filas de 2", n)
	}
}

func contar(t *testing.T, s *openSession, tabla string) int {
	t.Helper()
	n, f := s.db.Count(context.Background(), "main", tabla, nil)
	if f != nil {
		t.Fatalf("count(%s): %s", tabla, f.Message)
	}
	return int(n)
}
