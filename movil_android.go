//go:build android

package main

import (
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/LucianoR23/kanamedb/internal/service"
)

// graciaEnSegundoPlano es cuánto puede estar Kaname en segundo plano con la
// sesión abierta. Cambiar de app para copiar algo y volver no tiene que costar
// la conexión; dejar el teléfono en la mesa con una base de producción
// abierta, sí. Al volver, reconectar pide la contraseña al vault, y el vault
// pide el dedo: ese es el bloqueo, no hay una pantalla aparte que simularlo.
const graciaEnSegundoPlano = 2 * time.Minute

// reintentoOcupada es cada cuánto se vuelve a intentar el cierre si al vencer
// la gracia había una operación en curso.
const reintentoOcupada = 15 * time.Second

// bloquearEnSegundoPlano cierra la sesión si la activity lleva más de
// graciaEnSegundoPlano parada. Stopped y no Paused: el propio BiometricPrompt
// pausa la activity, y un diálogo del sistema no es «se fue a otra app».
//
// El tiempo se mide con el reloj de pared, sin la lectura monotónica
// (Round(0)): el monotónico de Go es CLOCK_MONOTONIC, que en Android se
// detiene mientras el equipo duerme. Con la pantalla apagada el timer de dos
// minutos puede tardar horas en disparar, y al despertar Resumed lo cancelaría
// con la sesión todavía abierta. Por eso Resumed no confía en el timer:
// compara cuánto pasó de verdad y cierra ahí si hace falta.
func bloquearEnSegundoPlano(app *application.App, sesion *service.Session) {
	var mu sync.Mutex
	var paradaEn time.Time // cero cuando la app está al frente
	var pendiente *time.Timer

	var cerrar func()
	cerrar = func() {
		mu.Lock()
		defer mu.Unlock()
		if paradaEn.IsZero() {
			return // volvió al frente antes de que esto corriera
		}
		ausente := time.Now().Round(0).Sub(paradaEn)
		if !service.CerrarPorSegundoPlano(sesion, ausente) {
			pendiente = time.AfterFunc(reintentoOcupada, cerrar)
		}
	}

	app.Event.OnApplicationEvent(events.Android.ActivityStopped, func(*application.ApplicationEvent) {
		mu.Lock()
		defer mu.Unlock()
		if !paradaEn.IsZero() {
			return // ya estaba parada
		}
		paradaEn = time.Now().Round(0)
		pendiente = time.AfterFunc(graciaEnSegundoPlano, cerrar)
	})
	app.Event.OnApplicationEvent(events.Android.ActivityResumed, func(*application.ApplicationEvent) {
		mu.Lock()
		if pendiente != nil {
			pendiente.Stop()
			pendiente = nil
		}
		ausente := time.Duration(0)
		if !paradaEn.IsZero() {
			ausente = time.Now().Round(0).Sub(paradaEn)
		}
		paradaEn = time.Time{}
		mu.Unlock()

		if ausente >= graciaEnSegundoPlano {
			// Durmió más de la gracia aunque el timer no lo haya notado. Si
			// está ocupada se deja: al volver al frente la persona ve el
			// progreso, y la próxima parada vuelve a contar.
			service.CerrarPorSegundoPlano(sesion, ausente)
		}
	})
}
