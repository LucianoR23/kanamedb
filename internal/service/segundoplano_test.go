package service

import (
	"strings"
	"testing"
	"time"
)

// El bloqueo en segundo plano de Android cierra la sesión como lo hace la
// inactividad —con motivo— y no como Disconnect: si hay una operación en
// curso no corta nada y pide que se reintente.
func TestCerrarPorSegundoPlanoRespetaLoQueEstaEnCurso(t *testing.T) {
	sesion, _ := sesionDe(t, "sqlite", "")

	// Ocupada: una consulta larga, una exportación. No se cierra.
	ocupada := true
	sesion.vigilarActividad(func() bool { return ocupada })
	if CerrarPorSegundoPlano(sesion, 3*time.Minute) {
		t.Fatal("cerró la sesión con una operación en curso")
	}
	if !sesion.Current().Connected {
		t.Fatal("la sesión se cerró aunque CerrarPorSegundoPlano dijo que no")
	}

	// Libre: se cierra, y el motivo dice por qué y qué va a pasar al volver.
	ocupada = false
	if !CerrarPorSegundoPlano(sesion, 3*time.Minute) {
		t.Fatal("no cerró una sesión libre")
	}
	v := sesion.Current()
	if v.Connected {
		t.Fatal("sigue conectada")
	}
	if !strings.Contains(v.ClosedReason, "segundo plano") || !strings.Contains(v.ClosedReason, "3 minutos") {
		t.Errorf("ClosedReason = %q: tenía que decir que fue por segundo plano y cuánto tiempo", v.ClosedReason)
	}

	// Sin sesión no hay nada que cerrar, y no es un error.
	if !CerrarPorSegundoPlano(sesion, time.Hour) {
		t.Error("sin sesión devolvió false")
	}
}
