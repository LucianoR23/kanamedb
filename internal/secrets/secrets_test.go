package secrets

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// nuevo construye un Keyring sobre un servicio propio del test, para no tocar
// las credenciales reales del usuario, y borra lo que haya dejado al terminar.
//
// Si el sistema no tiene keychain disponible —un runner de CI sin sesión
// interactiva, por ejemplo— el test se saltea en vez de fallar: eso diría algo
// del entorno, no del código.
func nuevo(t *testing.T) *Keyring {
	t.Helper()
	k := NewWithService(fmt.Sprintf("Kaname-test-%s", t.Name()))

	if err := k.Set("sonda", "x"); err != nil {
		t.Skipf("no hay keychain disponible en este entorno: %v", err)
	}
	if err := k.Delete("sonda"); err != nil {
		t.Fatalf("limpiar la sonda: %v", err)
	}
	return k
}

func limpiar(t *testing.T, k *Keyring, ids ...string) {
	t.Helper()
	t.Cleanup(func() {
		for _, id := range ids {
			if err := k.Delete(id); err != nil {
				t.Errorf("no se pudo limpiar %q del keychain: %v", id, err)
			}
		}
	})
}

func TestSetYGetIdaYVuelta(t *testing.T) {
	k := nuevo(t)
	limpiar(t, k, "conn1")

	const pw = "s3cr3t con espacios y ñ"
	if err := k.Set("conn1", pw); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	got, err := k.Get("conn1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got != pw {
		t.Errorf("Get() = %q, se esperaba %q", got, pw)
	}
}

// Una contraseña puede tener cualquier cosa adentro y tiene que volver igual.
func TestSobreviveContrasenasHostiles(t *testing.T) {
	casos := map[string]string{
		"comillas":    `pa"ss'wo\` + "`" + `rd`,
		"barras":      `C:\ruta\rara/y/otra`,
		"unicode":     "contraseña ñ á 日本語 🔐",
		"separadores": "a@b:c/d?e#f&g=h",
		"espacios":    "  con espacios  ",
		"larga":       strings.Repeat("x", maxPasswordLen),
	}
	for nombre, pw := range casos {
		t.Run(nombre, func(t *testing.T) {
			k := nuevo(t)
			limpiar(t, k, "c")
			if err := k.Set("c", pw); err != nil {
				t.Fatalf("Set() error: %v", err)
			}
			got, err := k.Get("c")
			if err != nil {
				t.Fatalf("Get() error: %v", err)
			}
			if got != pw {
				t.Errorf("la contraseña no sobrevivió el round-trip")
			}
		})
	}
}

func TestSetReemplazaLaAnterior(t *testing.T) {
	k := nuevo(t)
	limpiar(t, k, "conn1")

	if err := k.Set("conn1", "vieja"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	if err := k.Set("conn1", "nueva"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	got, err := k.Get("conn1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got != "nueva" {
		t.Errorf("Get() = %q, se esperaba la contraseña nueva", got)
	}
}

// No tener contraseña guardada no es una falla: la conexión puede autenticarse
// por otro medio, o el usuario todavía no la cargó en esta máquina.
func TestGetDeAlgoQueNoExisteDaErrNotFound(t *testing.T) {
	k := nuevo(t)
	_, err := k.Get("no-existe")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() devolvió %v, se esperaba ErrNotFound", err)
	}
}

func TestHasNoTraeElSecreto(t *testing.T) {
	k := nuevo(t)
	limpiar(t, k, "conn1")

	has, err := k.Has("conn1")
	if err != nil {
		t.Fatalf("Has() error: %v", err)
	}
	if has {
		t.Error("Has() dijo que sí antes de guardar nada")
	}

	if err := k.Set("conn1", "x"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	has, err = k.Has("conn1")
	if err != nil {
		t.Fatalf("Has() error: %v", err)
	}
	if !has {
		t.Error("Has() dijo que no después de guardar")
	}
}

// Borrar algo que no está no es un error: el resultado buscado ya se cumple.
func TestDeleteEsIdempotente(t *testing.T) {
	k := nuevo(t)
	if err := k.Delete("no-existe"); err != nil {
		t.Errorf("Delete() de algo inexistente devolvió %v", err)
	}

	if err := k.Set("conn1", "x"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	if err := k.Delete("conn1"); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if err := k.Delete("conn1"); err != nil {
		t.Errorf("el segundo Delete() devolvió %v", err)
	}
	if _, err := k.Get("conn1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("después de borrar, Get() devolvió %v", err)
	}
}

// Un ID vacío haría que todas las conexiones sin ID compartan la misma entrada
// del keychain.
func TestRechazaIdsQueNoSirvenComoClave(t *testing.T) {
	k := nuevo(t)
	malos := map[string]string{
		"vacío":            "",
		"espacio adelante": " abc",
		"espacio atrás":    "abc ",
		"salto de línea":   "ab\ncd",
		"nulo":             "ab\x00cd",
	}
	for nombre, id := range malos {
		t.Run(nombre, func(t *testing.T) {
			if err := k.Set(id, "x"); err == nil {
				t.Error("Set() aceptó el id")
			}
			if _, err := k.Get(id); err == nil {
				t.Error("Get() aceptó el id")
			}
			if err := k.Delete(id); err == nil {
				t.Error("Delete() aceptó el id")
			}
		})
	}
}

// Guardar una contraseña vacía deja una credencial inútil y casi siempre es un
// bug de quien llama.
func TestRechazaContrasenaVaciaYDemasiadoLarga(t *testing.T) {
	k := nuevo(t)

	if err := k.Set("conn1", ""); err == nil {
		t.Error("Set() aceptó una contraseña vacía")
	}
	if err := k.Set("conn1", strings.Repeat("x", maxPasswordLen+1)); err == nil {
		t.Error("Set() aceptó una contraseña por encima del tope")
	}
}

// Ningún error puede llevar la contraseña adentro: los errores terminan en
// logs, en toasts y en tickets.
func TestLosErroresNoFiltranLaContrasena(t *testing.T) {
	k := nuevo(t)
	const pw = "ContraseñaQueNoDebeAparecer123"

	errores := []error{}
	if err := k.Set("", pw); err != nil {
		errores = append(errores, err)
	}
	if err := k.Set(" mal ", pw); err != nil {
		errores = append(errores, err)
	}
	if err := k.Set("conn1", strings.Repeat(pw, 100)); err != nil {
		errores = append(errores, err)
	}
	if len(errores) == 0 {
		t.Fatal("ninguna de las llamadas falló, el test no verifica nada")
	}
	for _, err := range errores {
		if strings.Contains(err.Error(), pw) {
			t.Errorf("el error filtra la contraseña: %v", err)
		}
	}
}

// Dos servicios distintos no se pisan. Es lo que permite que los tests no
// toquen las credenciales reales del usuario.
func TestLosServiciosEstanAislados(t *testing.T) {
	a := nuevo(t)
	b := NewWithService(a.Service() + "-otro")
	limpiar(t, a, "conn1")
	limpiar(t, b, "conn1")

	if err := a.Set("conn1", "de-a"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}
	if err := b.Set("conn1", "de-b"); err != nil {
		t.Fatalf("Set() error: %v", err)
	}

	got, err := a.Get("conn1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got != "de-a" {
		t.Errorf("el servicio a leyó %q: los servicios se están pisando", got)
	}
}

// El servicio por defecto es lo que el usuario ve al auditar qué guardó la app.
func TestElServicioPorDefectoEsReconocible(t *testing.T) {
	if got := New().Service(); got != DefaultService {
		t.Errorf("Service() = %q, se esperaba %q", got, DefaultService)
	}
}
