package secrets

import (
	"errors"
	"strings"
	"testing"
)

// bridgeFalso imita el almacenamiento seguro de Wails con el contrato que
// documenta application.MobileManager: un solo espacio de claves, un valor
// vacío es válido, borrar lo que no está no falla.
type bridgeFalso struct {
	datos map[string]string
	// lecturas cuenta los SecureGet: en el vault real cada uno pide el dedo.
	lecturas int
	// falla, si no es nil, es lo que devuelve cada llamada: simula el bridge
	// caído o el Keystore que no entrega la clave.
	falla error
}

func nuevoBridgeFalso() *bridgeFalso { return &bridgeFalso{datos: map[string]string{}} }

func (b *bridgeFalso) SecureSet(clave, valor string) error {
	if b.falla != nil {
		return b.falla
	}
	b.datos[clave] = valor
	return nil
}

func (b *bridgeFalso) SecureGet(clave string) (string, bool, error) {
	b.lecturas++
	if b.falla != nil {
		return "", false, b.falla
	}
	v, ok := b.datos[clave]
	return v, ok, nil
}

func (b *bridgeFalso) SecureDelete(clave string) error {
	if b.falla != nil {
		return b.falla
	}
	delete(b.datos, clave)
	return nil
}

func (b *bridgeFalso) SecureHas(clave string) (bool, error) {
	if b.falla != nil {
		return false, b.falla
	}
	_, ok := b.datos[clave]
	return ok, nil
}

// El adaptador de Android cumple el mismo contrato que el keychain de
// escritorio: ida y vuelta, reemplazo, ErrNotFound, Has y Delete idempotente.
func TestElAlmacenMovilCumpleElContratoDelKeyring(t *testing.T) {
	k := nuevoMovil("Kaname-test", nuevoBridgeFalso())

	if _, err := k.Get("conn1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() de algo que no existe: %v, se esperaba ErrNotFound", err)
	}
	if hay, err := k.Has("conn1"); err != nil || hay {
		t.Fatalf("Has() = %v, %v; se esperaba false, nil", hay, err)
	}

	if err := k.Set("conn1", "primera"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	if err := k.Set("conn1", "segunda"); err != nil {
		t.Fatalf("Set() de reemplazo error: %v", err)
	}
	got, err := k.Get("conn1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got != "segunda" {
		t.Errorf("Get() = %q, se esperaba la contraseña de reemplazo", got)
	}
	if hay, err := k.Has("conn1"); err != nil || !hay {
		t.Errorf("Has() = %v, %v; se esperaba true, nil", hay, err)
	}

	if err := k.Delete("conn1"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if err := k.Delete("conn1"); err != nil {
		t.Errorf("Delete() de algo que ya no está: %v, no debería fallar", err)
	}
	if _, err := k.Get("conn1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("después de Delete, Get() = %v, se esperaba ErrNotFound", err)
	}
}

// Has no descifra. En el vault de Android cada SecureGet es un BiometricPrompt,
// y la lista de conexiones pregunta por cada una si tiene contraseña: si Has
// leyera, abrir la app pediría el dedo una vez por conexión.
func TestHasNoLeeElSecreto(t *testing.T) {
	bridge := nuevoBridgeFalso()
	k := nuevoMovil("Kaname-test", bridge)
	if err := k.Set("conn1", "secreto"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	bridge.lecturas = 0

	hay, err := k.Has("conn1")
	if err != nil || !hay {
		t.Fatalf("Has() = %v, %v; se esperaba true, nil", hay, err)
	}
	if hay, err := k.Has("otra"); err != nil || hay {
		t.Fatalf("Has() de algo que no existe = %v, %v; se esperaba false, nil", hay, err)
	}
	if bridge.lecturas != 0 {
		t.Errorf("Has() hizo %d lecturas del secreto; no tiene que hacer ninguna", bridge.lecturas)
	}
}

// Lo que sobrevive en escritorio tiene que sobrevivir acá: el adaptador no
// toca el valor.
func TestElAlmacenMovilNoAlteraLaContrasena(t *testing.T) {
	k := nuevoMovil("Kaname-test", nuevoBridgeFalso())
	for _, pw := range []string{
		"con espacios  al  medio",
		"unicode: contraseña ñ 日本語 🔐",
		"comillas \" ' ` y barras \\ /",
		"saltos\nde\r\nlínea\ty tabs",
		"=igual&amper%por;punto:coma",
		strings.Repeat("x", maxPasswordLen),
	} {
		if err := k.Set("conn", pw); err != nil {
			t.Fatalf("Set(%q) error: %v", pw, err)
		}
		got, err := k.Get("conn")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if got != pw {
			t.Errorf("la contraseña volvió distinta: %q → %q", pw, got)
		}
	}
}

// Wails tiene un solo espacio de claves y el servicio va dentro de la clave.
// Dos servicios no se pisan, y tampoco cuando servicio e id contienen el
// separador: es lo que justifica el prefijo de longitud en almacenMovil.clave.
func TestElAlmacenMovilAislaLosServiciosSinAmbiguedad(t *testing.T) {
	bridge := nuevoBridgeFalso()
	a := nuevoMovil("a", bridge)
	ab := nuevoMovil("a:b", bridge)

	// Con un separador «:» a secas, ("a", "b:c") y ("a:b", "c") serían la
	// misma entrada.
	if err := a.Set("b:c", "de-a"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	if err := ab.Set("c", "de-ab"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	if len(bridge.datos) != 2 {
		t.Fatalf("el bridge tiene %d entradas, se esperaban 2: los servicios se pisan", len(bridge.datos))
	}
	if got, _ := a.Get("b:c"); got != "de-a" {
		t.Errorf("el servicio «a» leyó %q", got)
	}
	if got, _ := ab.Get("c"); got != "de-ab" {
		t.Errorf("el servicio «a:b» leyó %q", got)
	}

	// Y los tests, que usan t.Name() con «/», no ven lo del servicio real.
	real := nuevoMovil(DefaultService, bridge)
	if _, err := real.Get("b:c"); !errors.Is(err, ErrNotFound) {
		t.Errorf("el servicio por defecto ve una entrada de otro servicio: %v", err)
	}
}

// Si el bridge falla —Keystore que no entrega la clave, bridge caído— el error
// llega a quien llama, envuelto con el id y nunca con la contraseña.
func TestElAlmacenMovilPropagaLasFallasDelBridgeSinLaContrasena(t *testing.T) {
	bridge := nuevoBridgeFalso()
	bridge.falla = errors.New("secure storage unavailable")
	k := nuevoMovil("Kaname-test", bridge)
	const pw = "ContraseñaQueNoDebeAparecer123"

	err := k.Set("conn1", pw)
	if err == nil {
		t.Fatal("Set() con el bridge caído no falló")
	}
	if !errors.Is(err, bridge.falla) || !strings.Contains(err.Error(), "conn1") {
		t.Errorf("el error no envuelve la falla del bridge con el id: %v", err)
	}
	if strings.Contains(err.Error(), pw) {
		t.Errorf("el error filtra la contraseña: %v", err)
	}

	if _, err := k.Get("conn1"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("Get() con el bridge caído dio %v: una falla no es «no está»", err)
	}
	if err := k.Delete("conn1"); err == nil {
		t.Error("Delete() con el bridge caído no falló")
	}
}
