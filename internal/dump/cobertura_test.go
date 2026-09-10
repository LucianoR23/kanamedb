package dump

import (
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

func obj(k schema.ObjectKind, nombre string) schema.Object {
	return schema.Object{Kind: k, Schema: "demo", Name: nombre}
}

// TestElAvisoDiceCuantosYCuales es la razón de existir de este tipo: «puede
// faltar algo» no deja decidir nada. Con los nombres, sí.
func TestElAvisoDiceCuantosYCuales(t *testing.T) {
	c := Cobertura{Fuera: []schema.Object{
		obj(schema.ObjFunction, "tocar"),
		obj(schema.ObjView, "activos"),
		obj(schema.ObjFunction, "calcular"),
		obj(schema.ObjFunction, "auditar"),
		obj(schema.ObjView, "resumen"),
		{Kind: schema.ObjPolicy, Schema: "demo", Name: "solo_mios", Table: "pedidos"},
	}}

	if c.Vacia() {
		t.Fatal("Vacia() con seis objetos afuera")
	}
	// El resumen: cuántos de cada cosa, sin nombres, con la «y» antes del
	// último como se escribe en castellano.
	if got := c.Resumen(); got != "2 vistas, 3 funciones y 1 política de RLS" {
		t.Errorf("Resumen() = %q", got)
	}

	// El detalle: con nombre y apellido, y los nombres ordenados.
	det := strings.Join(c.Detalle(), " | ")
	quiero := "2 vistas: demo.activos, demo.resumen | " +
		"3 funciones: demo.auditar, demo.calcular, demo.tocar | " +
		"1 política de RLS: demo.solo_mios (pedidos)"
	if det != quiero {
		t.Errorf("Detalle():\n%s\nquería:\n%s", det, quiero)
	}
}

// TestSingularYPluralPorqueUnAvisoQueDice1VistasNoLoLeeNadie.
func TestSingularYPluralPorqueUnAvisoQueDice1VistasNoLoLeeNadie(t *testing.T) {
	uno := Cobertura{Fuera: []schema.Object{obj(schema.ObjView, "v")}}
	if got := uno.Resumen(); got != "1 vista" {
		t.Errorf("Resumen() con una = %q", got)
	}
	dos := Cobertura{Fuera: []schema.Object{obj(schema.ObjView, "v"), obj(schema.ObjView, "w")}}
	if got := dos.Resumen(); got != "2 vistas" {
		t.Errorf("Resumen() con dos = %q", got)
	}
}

// TestUnTipoDesconocidoNoSeSilencia.
//
// El orden de tipos es una lista fija, y lo que se resuelve con una lista fija
// se puede caer de ella: un motor que devuelva una clase nueva no puede
// desaparecer del aviso. Silenciar lo desconocido es exactamente el defecto que
// esta estructura existe para evitar.
func TestUnTipoDesconocidoNoSeSilencia(t *testing.T) {
	c := Cobertura{Fuera: []schema.Object{
		obj(schema.ObjView, "v"),
		obj(schema.ObjectKind("rule"), "una_regla"),
	}}
	if n := len(c.PorTipo()); n != 2 {
		t.Fatalf("PorTipo() devolvió %d grupos: el tipo desconocido se perdió", n)
	}
	if !strings.Contains(strings.Join(c.Detalle(), " "), "demo.una_regla") {
		t.Errorf("el objeto de tipo desconocido no aparece: %v", c.Detalle())
	}
}

// TestSinNadaAfueraNoHayAviso: un volcado que cubre todo no tiene que inventar
// una advertencia.
func TestSinNadaAfueraNoHayAviso(t *testing.T) {
	var c Cobertura
	if !c.Vacia() || c.Resumen() != "" || len(c.Detalle()) != 0 {
		t.Errorf("una cobertura vacía dijo algo: %q %v", c.Resumen(), c.Detalle())
	}
}
