package connection

import (
	"testing"
	"time"
)

// El motivo de existir de todo el diseño de Safety: el archivo se edita a mano
// y una clave ausente decodifica como cero. Si el cero fuera el lado inseguro,
// la conexión más vieja —que suele ser la más importante— quedaría desprotegida
// sin que nadie lo pidiera.
func TestElValorCeroDeSafetyEsElLadoSeguro(t *testing.T) {
	var s Safety // lo que da un archivo sin ninguna clave de safety

	if !s.RequiresPreview() {
		t.Error("sin configurar, el preview de SQL debería ser obligatorio")
	}
	if s.StatementTimeout() == 0 {
		t.Error("sin configurar, debería haber timeout de sentencia y no 'sin límite'")
	}
	if s.EffectiveRowLimit() == 0 {
		t.Error("sin configurar, debería haber límite de filas y no 'sin límite'")
	}
	if s.IdleDisconnect() == 0 {
		t.Error("sin configurar, debería desconectar por inactividad y no 'nunca'")
	}

	c := Connection{Environment: Dev}
	if !c.RequiresWriteConfirmation() {
		t.Error("sin configurar, escribir debería pedir confirmación")
	}
}

// Y con los defaults de S03.
func TestLosDefaultsSonLosDelDiseno(t *testing.T) {
	var s Safety
	if got, quiere := s.StatementTimeout(), 30*time.Second; got != quiere {
		t.Errorf("StatementTimeout() = %v, se esperaba %v", got, quiere)
	}
	if got, quiere := s.EffectiveRowLimit(), 1000; got != quiere {
		t.Errorf("EffectiveRowLimit() = %d, se esperaba %d", got, quiere)
	}
	if got, quiere := s.IdleDisconnect(), 15*time.Minute; got != quiere {
		t.Errorf("IdleDisconnect() = %v, se esperaba %v", got, quiere)
	}
}

// Sacar un límite tiene que ser explícito: por eso no es cero.
func TestSacarUnLimiteExigePedirloConUnlimited(t *testing.T) {
	s := Safety{
		StatementTimeoutSeconds: Unlimited,
		RowLimit:                Unlimited,
		IdleDisconnectMinutes:   Unlimited,
	}
	if s.StatementTimeout() != 0 {
		t.Errorf("StatementTimeout() = %v, se esperaba sin límite", s.StatementTimeout())
	}
	if s.EffectiveRowLimit() != 0 {
		t.Errorf("EffectiveRowLimit() = %d, se esperaba sin límite", s.EffectiveRowLimit())
	}
	if s.IdleDisconnect() != 0 {
		t.Errorf("IdleDisconnect() = %v, se esperaba nunca", s.IdleDisconnect())
	}
}

func TestLosValoresExplicitosSeRespetan(t *testing.T) {
	s := Safety{
		StatementTimeoutSeconds: 5,
		RowLimit:                250,
		IdleDisconnectMinutes:   60,
	}
	if got := s.StatementTimeout(); got != 5*time.Second {
		t.Errorf("StatementTimeout() = %v", got)
	}
	if got := s.EffectiveRowLimit(); got != 250 {
		t.Errorf("EffectiveRowLimit() = %d", got)
	}
	if got := s.IdleDisconnect(); got != time.Hour {
		t.Errorf("IdleDisconnect() = %v", got)
	}
}

// La confirmación por nombre de base es la protección que no se puede apagar en
// producción, esté como esté la configuración.
func TestProduccionSiempreExigeConfirmacionAunqueSePidaLoContrario(t *testing.T) {
	c := Connection{
		Environment: Production,
		Safety:      Safety{AllowWriteWithoutConfirmation: true},
	}
	if !c.RequiresWriteConfirmation() {
		t.Fatal("producción tiene que exigir confirmación aunque la configuración diga que no")
	}

	// En el resto de los entornos sí se puede apagar.
	for _, env := range []Environment{Local, Dev, Staging} {
		c.Environment = env
		if c.RequiresWriteConfirmation() {
			t.Errorf("en %s la confirmación debería poder apagarse", env)
		}
	}
}

func TestSePuedeApagarElPreviewExplicitamente(t *testing.T) {
	s := Safety{AllowApplyWithoutPreview: true}
	if s.RequiresPreview() {
		t.Error("pedir aplicar sin preview debería funcionar cuando se pide explícitamente")
	}
}

// Un número negativo que no sea Unlimited es un error de tipeo, y dejarlo pasar
// haría que el usuario crea tener un límite que no está.
func TestRechazaNumerosNegativosQueNoSeanUnlimited(t *testing.T) {
	casos := map[string]func(*Safety){
		"statementTimeoutSeconds": func(s *Safety) { s.StatementTimeoutSeconds = -5 },
		"rowLimit":                func(s *Safety) { s.RowLimit = -2 },
		"idleDisconnectMinutes":   func(s *Safety) { s.IdleDisconnectMinutes = -99 },
	}
	for campo, romper := range casos {
		t.Run(campo, func(t *testing.T) {
			c := valid()
			romper(&c.Safety)
			got := fieldErrors(t, c)
			if _, ok := got[campo]; !ok {
				t.Errorf("no reportó %q; se obtuvo %v", campo, got)
			}
		})
	}
}

func TestUnlimitedEsValido(t *testing.T) {
	c := valid()
	c.Safety.StatementTimeoutSeconds = Unlimited
	c.Safety.RowLimit = Unlimited
	c.Safety.IdleDisconnectMinutes = Unlimited
	if err := c.Validate(); err != nil {
		t.Errorf("Unlimited debería ser válido: %v", err)
	}
}

// Los avisos no bloquean, pero tienen que aparecer: apagar una protección es
// legítimo y el usuario tiene derecho a saber qué apagó.
func TestAvisaAlApagarProtecciones(t *testing.T) {
	c := valid()
	c.Environment = Staging
	c.SSLMode = SSLVerifyFull
	c.Safety = Safety{
		AllowApplyWithoutPreview:      true,
		AllowWriteWithoutConfirmation: true,
		StatementTimeoutSeconds:       Unlimited,
	}

	campos := map[string]bool{}
	for _, w := range c.Warnings() {
		campos[w.Field] = true
	}
	for _, esperado := range []string{
		"allowApplyWithoutPreview",
		"allowWriteWithoutConfirmation",
		"statementTimeoutSeconds",
	} {
		if !campos[esperado] {
			t.Errorf("no avisó sobre %q; avisos: %v", esperado, c.Warnings())
		}
	}
}
