package service

import (
	"fmt"
	"time"

	"github.com/LucianoR23/kanamedb/internal/change"
)

// Desconexión por inactividad.
//
// «Desconectar por inactividad · 15 minutos» se mostraba en la pestaña Safety
// y en el gestor, se validaba, y ningún temporizador lo usaba: una conexión a
// producción abierta a las 10 seguía abierta a las 18 con el pool y el túnel
// vivos (K-07 de la auditoría del 2026-09-11). Esto es el temporizador.
//
// # Cómo se mide la actividad
//
// Actividad es que algún binding haya usado la sesión: abierta() lo anota, y
// por ahí pasa todo lo que toca la base. Current() y ApplyStatus() NO cuentan,
// porque la interfaz los consulta sola —una barra de estado que se refresca no
// es una persona trabajando—.
//
// El temporizador no se reinicia con cada llamada. Vence a los N minutos de
// abrir, mira cuándo fue el último uso y, si fue hace menos de N, se vuelve a
// armar por lo que falta. Es el patrón de siempre para un timeout de
// inactividad: una escritura atómica por llamada en vez de un Reset por
// llamada, y sin carreras entre el Reset y el disparo.
//
// # Lo que NO corta
//
// Un apply o una exportación de veinte minutos no son inactividad, aunque
// entre el primer binding y el último no llegue ninguno más. Antes de cerrar
// se pregunta si hay un apply en curso o una ejecución registrada en Queries;
// si la hay, se vuelve a mirar en un minuto. Cortar el pool en medio de un
// volcado sería peor que la sesión abierta que esto quiere evitar.

// vigilanciaDeOcupado es cada cuánto se vuelve a mirar cuando hay algo
// corriendo al vencer. Un minuto: lo bastante corto para no sobrepasar por
// mucho el límite configurado, y lo bastante largo para no despertar por nada.
const vigilanciaDeOcupado = time.Minute

// tocar anota que alguien usó la sesión ahora.
func (o *openSession) tocar(ahora time.Time) {
	o.ultimoUso.Store(ahora.UnixNano())
}

// usadaHace dice cuánto hace del último uso.
func (o *openSession) usadaHace(ahora time.Time) time.Duration {
	return ahora.Sub(time.Unix(0, o.ultimoUso.Load()))
}

// armarInactividad programa el cierre de la sesión que se acaba de instalar.
// Con «nunca» (-1) no arma nada.
func (s *Session) armarInactividad(o *openSession) {
	d := o.conn.Safety.IdleDisconnect()
	if d <= 0 {
		return
	}
	o.tocar(s.ahora())
	o.inactividad = time.AfterFunc(d, func() { s.vencerInactividad(o) })
}

// vencerInactividad corre cuando el temporizador se dispara.
func (s *Session) vencerInactividad(o *openSession) {
	d := o.conn.Safety.IdleDisconnect()

	s.mu.Lock()
	if s.current != o {
		// Ya la cerró otro: Disconnect, o un Connect a otra base. El
		// temporizador se paró en cerrar(), pero pudo haber disparado justo
		// antes; no hay nada que hacer.
		s.mu.Unlock()
		return
	}
	if resto := d - o.usadaHace(s.ahora()); resto > 0 {
		o.inactividad.Reset(resto)
		s.mu.Unlock()
		return
	}
	if s.progreso.State == ApplyRunning || (s.ocupado != nil && s.ocupado()) {
		// Estar ocupada ES actividad: cuando termine, la persona va a estar
		// mirando el resultado, y merece la ventana entera desde ahí y no
		// sesenta segundos. Se anota acá y también al terminar (tocarActual),
		// que cubre lo que termina entre un vencimiento y el siguiente.
		o.tocar(s.ahora())
		o.inactividad.Reset(vigilanciaDeOcupado)
		s.mu.Unlock()
		return
	}
	s.current = nil
	s.motivoDeCierre = fmt.Sprintf(
		"La conexión se cerró tras %s sin actividad. Es la protección «Desconectar "+
			"por inactividad» de la pestaña Safety.", duracionLegible(d))
	// El changeset no se tira con la sesión: la persona preparó treinta
	// ediciones, se fue quince minutos y al volver «Reconectar» tiene que
	// devolvérselas. Se guarda atado a la conexión Y a la base, y se
	// restituye solo si la próxima conexión es a esa misma base (review del
	// 2026-09-12).
	if n := o.cambios.Summarize().Total; n > 0 {
		s.rescatado = &rescate{connID: o.conn.ID, base: nombreDeLaBase(o), cambios: o.cambios}
		s.motivoDeCierre += fmt.Sprintf(" %s se conservan si volvés a conectar a la misma base.",
			cuentaDeCambios(n))
	}
	s.mu.Unlock()

	o.cerrar()
}

// rescate es un changeset que sobrevivió a la desconexión por inactividad.
type rescate struct {
	connID  string
	base    string
	cambios *change.Set
}

// devolverRescatado le da a la sesión recién abierta el changeset que la
// inactividad dejó sin sesión, si es la misma conexión contra la misma base.
// Cualquier otra conexión lo descarta: un changeset contra otra base es
// peligroso, no útil. Se llama con s.mu tomado.
func (s *Session) devolverRescatado(o *openSession) {
	r := s.rescatado
	s.rescatado = nil
	if r == nil || r.connID != o.conn.ID || r.base != nombreDeLaBase(o) {
		return
	}
	o.cambios = r.cambios
}

func cuentaDeCambios(n int) string {
	if n == 1 {
		return "El cambio pendiente"
	}
	return fmt.Sprintf("Los %d cambios pendientes", n)
}

// tocarActual anota actividad en la sesión en curso, si hay una. Lo llaman
// terminarApply y la limpieza de Queries.registrar: una consulta de catorce
// minutos que termina a los 14:40 no puede cerrar la sesión a los 15:00 con
// la persona leyendo el resultado.
func (s *Session) tocarActual() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.current != nil {
		s.current.tocar(s.ahora())
	}
}

// vigilarActividad registra quién sabe si hay una ejecución en curso que no
// pasa por Apply: las consultas, exportaciones y volcados registrados en
// Queries. Lo llama NewQueries.
func (s *Session) vigilarActividad(ocupado func() bool) {
	if s == nil {
		// Algún test arma Queries sin sesión para probar lo que no la toca.
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ocupado = ocupado
}

// usarReloj cambia la fuente de tiempo. Es para los tests: un timeout de
// minutos no se prueba esperando minutos. No se exporta a propósito: un método
// exportado del servicio es un binding (K-14).
func (s *Session) usarReloj(ahora func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reloj = ahora
}

func (s *Session) ahora() time.Time {
	if s.reloj != nil {
		return s.reloj()
	}
	return time.Now()
}

func duracionLegible(d time.Duration) string {
	if d < time.Hour || d%time.Hour != 0 {
		m := int(d / time.Minute)
		if m == 1 {
			return "1 minuto"
		}
		return fmt.Sprintf("%d minutos", m)
	}
	h := int(d / time.Hour)
	if h == 1 {
		return "1 hora"
	}
	return fmt.Sprintf("%d horas", h)
}
