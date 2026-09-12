package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LucianoR23/kanamedb/internal/change"
)

// TestLaSesionSeCierraSolaTrasLaInactividadConfigurada prueba el temporizador
// de «Desconectar por inactividad» con un reloj inyectado y disparándolo a
// mano: un timeout de minutos no se prueba esperando minutos. Lo que sí es
// real es todo lo demás —qué cuenta como actividad, qué lo frena, y que al
// vencer la sesión deja de existir y lo explica—.
func TestLaSesionSeCierraSolaTrasLaInactividadConfigurada(t *testing.T) {
	sesion, _ := sesionDe(t, "sqlite", "")
	ctx := context.Background()
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	// La conexión de prueba nació con el default (15 min). Se baja a 2 para que
	// los números del test se lean solos.
	abierta.conn.Safety.IdleDisconnectMinutes = 2
	limite := abierta.conn.Safety.IdleDisconnect()
	if abierta.inactividad == nil {
		t.Fatal("la sesión se abrió sin temporizador de inactividad")
	}

	ahora := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	sesion.usarReloj(func() time.Time { return ahora })
	abierta.tocar(ahora)

	// 1. Vence antes de tiempo (alguien la usó hace 1 minuto): sigue abierta.
	ahora = ahora.Add(time.Minute)
	sesion.vencerInactividad(abierta)
	if !sesion.Current().Connected {
		t.Fatal("se cerró a 1 minuto de uso con un límite de 2")
	}

	// 2. Un binding cualquiera cuenta como actividad: abierta() la toca.
	ahora = ahora.Add(time.Minute + 30*time.Second)
	if _, err := sesion.abierta(); err != nil {
		t.Fatal(err)
	}
	ahora = ahora.Add(time.Minute)
	sesion.vencerInactividad(abierta)
	if !sesion.Current().Connected {
		t.Fatal("se cerró a 1 minuto del último binding")
	}

	// 3. Consultar el estado NO cuenta: la barra de estado lo hace sola.
	ahora = ahora.Add(limite)
	_ = sesion.Current()
	_ = sesion.ApplyStatus()

	// 4. …pero con un apply en curso no se corta, aunque el plazo venció.
	sesion.iniciarApply([]change.Statement{{}})
	sesion.vencerInactividad(abierta)
	if !sesion.Current().Connected {
		t.Fatal("cortó la sesión en medio de un apply")
	}
	sesion.terminarApply()

	// 5. Con una consulta registrada tampoco.
	q := NewQueries(sesion)
	_, listo := q.registrar(ctx, "larga")
	sesion.vencerInactividad(abierta)
	if !sesion.Current().Connected {
		t.Fatal("cortó la sesión con una ejecución registrada en Queries")
	}
	listo()

	// Estar ocupada contó como actividad: la ventana arranca de nuevo desde
	// ahí, y recién vencida otra vez, sin nada corriendo, se cierra.
	sesion.vencerInactividad(abierta)
	if !sesion.Current().Connected {
		t.Fatal("se cerró justo después de terminar una ejecución larga: terminar es actividad")
	}

	// 6. Sin nada corriendo y con el plazo vencido: se cierra, y se explica.
	ahora = ahora.Add(limite)
	sesion.vencerInactividad(abierta)
	vista := sesion.Current()
	if vista.Connected {
		t.Fatal("la sesión sigue abierta con el plazo de inactividad vencido")
	}
	if !strings.Contains(vista.ClosedReason, "2 minutos") {
		t.Errorf("ClosedReason = %q: tenía que decir cuánto tiempo pasó", vista.ClosedReason)
	}
	_, err = sesion.abierta()
	if !errors.Is(err, ErrNotConnected) || !strings.Contains(err.Error(), "inactividad") {
		t.Errorf("la próxima llamada tenía que explicar el cierre y devolvió %v", err)
	}
	// El pool quedó cerrado de verdad, no solo desinstalado.
	if err := abierta.db.Exec(ctx, "SELECT 1"); err == nil {
		t.Error("el pool sigue vivo después de la desconexión por inactividad")
	}

	// 7. Volver a conectar limpia el motivo.
	if res := sesion.Connect(ctx, abierta.conn.ID); !res.OK {
		t.Fatalf("no reconectó: %+v", res.Failure)
	}
	if v := sesion.Current(); !v.Connected || v.ClosedReason != "" {
		t.Errorf("después de reconectar: Connected=%v ClosedReason=%q", v.Connected, v.ClosedReason)
	}
}

// TestConNuncaNoHayTemporizador: -1 es «no desconectar nunca», y eso es no
// armar nada.
func TestConNuncaNoHayTemporizador(t *testing.T) {
	sesion, c := sesionDe(t, "sqlite", "")
	c.Safety.IdleDisconnectMinutes = -1
	if err := sesion.store.Update(c); err != nil {
		t.Fatal(err)
	}
	if res := sesion.Connect(context.Background(), c.ID); !res.OK {
		t.Fatalf("no reconectó: %+v", res.Failure)
	}
	abierta, err := sesion.abierta()
	if err != nil {
		t.Fatal(err)
	}
	if abierta.inactividad != nil {
		t.Error("con «nunca» se armó un temporizador igual")
	}
}
