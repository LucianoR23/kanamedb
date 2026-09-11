package engine_test

import (
	"testing"

	"github.com/LucianoR23/kanamedb/internal/engine"
)

// d y e son «esto es dato» y «esto es esquema», para que los casos se lean.
const (
	d = true
	e = false
)

func TestTramosDe(t *testing.T) {
	casos := []struct {
		nombre string
		caps   engine.Caps
		tipos  []bool
		quiere []engine.Tramo
	}{
		{
			nombre: "con DDL transaccional va todo junto",
			caps:   engine.Caps{TransactionalDDL: true},
			tipos:  []bool{e, d, e, d},
			quiere: []engine.Tramo{{Desde: 0, Hasta: 4, Transaccional: true}},
		},
		{
			// Este es el caso que motiva todo el archivo. Sin partir, el ALTER
			// del medio commitearía el UPDATE anterior y sacaría a la conexión
			// de la transacción, así que el UPDATE posterior también quedaría.
			nombre: "sin DDL transaccional, un ALTER parte la corrida de datos",
			caps:   engine.Caps{TransactionalDDL: false},
			tipos:  []bool{d, e, d},
			quiere: []engine.Tramo{
				{Desde: 0, Hasta: 1, Transaccional: true},
				{Desde: 1, Hasta: 2, Transaccional: false},
				{Desde: 2, Hasta: 3, Transaccional: true},
			},
		},
		{
			// Lo que produce la grilla editable: puro dato. Acá MySQL y
			// MariaDB dan exactamente la misma garantía que Postgres.
			nombre: "solo datos es un tramo transaccional aunque el motor no tenga DDL transaccional",
			caps:   engine.Caps{TransactionalDDL: false},
			tipos:  []bool{d, d, d},
			quiere: []engine.Tramo{{Desde: 0, Hasta: 3, Transaccional: true}},
		},
		{
			nombre: "los DDL no se agrupan entre sí",
			caps:   engine.Caps{TransactionalDDL: false},
			tipos:  []bool{e, e},
			quiere: []engine.Tramo{
				{Desde: 0, Hasta: 1, Transaccional: false},
				{Desde: 1, Hasta: 2, Transaccional: false},
			},
		},
		{
			nombre: "sin sentencias no hay tramos",
			caps:   engine.Caps{TransactionalDDL: false},
			tipos:  nil,
			quiere: nil,
		},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got := engine.TramosDe(len(c.tipos), c.caps, func(i int) bool { return c.tipos[i] }, nil)
			if len(got) != len(c.quiere) {
				t.Fatalf("dio %d tramos y se esperaban %d: %+v", len(got), len(c.quiere), got)
			}
			for i := range got {
				if got[i] != c.quiere[i] {
					t.Errorf("tramo %d = %+v, se esperaba %+v", i, got[i], c.quiere[i])
				}
			}
			// Invariante que ningún caso puede violar: los tramos tienen que
			// cubrir TODAS las sentencias, en orden y sin superponerse. Si una
			// se cayera entre dos tramos, no se ejecutaría nunca.
			esperado := 0
			for _, tr := range got {
				if tr.Desde != esperado {
					t.Fatalf("hueco o superposición: el tramo empieza en %d y se esperaba %d",
						tr.Desde, esperado)
				}
				esperado = tr.Hasta
			}
			if esperado != len(c.tipos) {
				t.Fatalf("los tramos cubren %d sentencias de %d", esperado, len(c.tipos))
			}
		})
	}
}

// TestUnaSentenciaAisladaCortaElTramoAunqueElDDLSeaTransaccional.
//
// Es lo que hace posible agregar un valor a un enum y usarlo en el MISMO
// changeset. PostgreSQL agrega el valor dentro de la transacción pero no lo
// deja usar hasta que ésta confirma: el par «ADD VALUE» + «usarlo» da «unsafe
// use of new value … New enum values must be committed before they can be
// used» y tira el changeset entero. Comprobado en las cuatro versiones que
// soportamos, de la 14 a la 18.
//
// Con DDL transaccional el tramo sería UNO solo con todo adentro. La aislada lo
// corta en tres: lo de antes, ella sola, y lo de después — que es lo único que
// deja que lo de después la use.
func TestUnaSentenciaAisladaCortaElTramoAunqueElDDLSeaTransaccional(t *testing.T) {
	caps := engine.Caps{TransactionalDDL: true}
	datos := func(int) bool { return false }

	// La aislada en el medio: corta a los dos lados.
	got := engine.TramosDe(3, caps, datos, func(i int) bool { return i == 1 })
	quiero := []engine.Tramo{
		{Desde: 0, Hasta: 1, Transaccional: true},
		{Desde: 1, Hasta: 2, Transaccional: false},
		{Desde: 2, Hasta: 3, Transaccional: true},
	}
	comparar(t, "en el medio", got, quiero)

	// Primera, que es donde cae de verdad: el enum va en la fase 1.
	got = engine.TramosDe(3, caps, datos, func(i int) bool { return i == 0 })
	quiero = []engine.Tramo{
		{Desde: 0, Hasta: 1, Transaccional: false},
		{Desde: 1, Hasta: 3, Transaccional: true},
	}
	comparar(t, "primera", got, quiero)

	// Sin ninguna aislada no cambia nada: un solo tramo, como antes.
	got = engine.TramosDe(3, caps, datos, nil)
	comparar(t, "ninguna", got, []engine.Tramo{{Desde: 0, Hasta: 3, Transaccional: true}})
}

func comparar(t *testing.T, que string, got, quiero []engine.Tramo) {
	t.Helper()
	if len(got) != len(quiero) {
		t.Fatalf("%s: salieron %d tramos y se esperaban %d: %+v", que, len(got), len(quiero), got)
	}
	for i := range quiero {
		if got[i] != quiero[i] {
			t.Errorf("%s: el tramo %d es %+v y se esperaba %+v", que, i, got[i], quiero[i])
		}
	}
}
