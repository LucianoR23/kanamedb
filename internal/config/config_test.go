package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/connection"
)

func nuevoStore(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "config.toml"))
}

func TestSinArchivoDevuelveLosDefaults(t *testing.T) {
	c, err := nuevoStore(t).Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if c.Theme != ThemeDark {
		t.Errorf("Theme = %q, se esperaba %q", c.Theme, ThemeDark)
	}
	if c.Editor.FontSize != DefaultFontSize {
		t.Errorf("FontSize = %d, se esperaba %d", c.Editor.FontSize, DefaultFontSize)
	}
}

func TestGuardarYVolverALeerDevuelveLoMismo(t *testing.T) {
	s := nuevoStore(t)
	quiero := Config{
		Theme:  ThemeLight,
		Editor: Editor{FontSize: 16, HideLineNumbers: true, NoWrap: true},
		NewConnection: connection.Safety{
			ReadOnly:                true,
			BlockDropTruncate:       true,
			StatementTimeoutSeconds: 90,
			RowLimit:                connection.Unlimited,
		},
	}
	if _, err := s.Save(quiero); err != nil {
		t.Fatalf("Save(): %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	quiero = quiero.Normalize()
	if !reflect.DeepEqual(got, quiero) {
		t.Errorf("volvió %+v\nse esperaba %+v", got, quiero)
	}
}

// Una clave que falta tiene que dejar la aplicación como estaba. El archivo se
// edita a mano y una build nueva lee archivos viejos: si el cero prendiera o
// apagara algo, actualizar Kaname cambiaría el comportamiento sin que nadie lo
// pidiera.
func TestUnArchivoAlQueLeFaltanClavesNoCambiaNada(t *testing.T) {
	s := nuevoStore(t)
	if err := os.WriteFile(s.Path(), []byte("version = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := s.Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if c.Theme.Effective() != ThemeDark {
		t.Errorf("sin la clave theme quedó %q: el default es el tema de siempre", c.Theme)
	}
	if c.Editor.HideLineNumbers {
		t.Error("sin la clave hide_line_numbers se apagaron los números de línea")
	}
	if c.Editor.NoWrap {
		t.Error("sin la clave no_wrap se apagó el ajuste de línea")
	}
	if c.NewConnection.AllowApplyWithoutPreview {
		t.Error("sin la clave, una conexión nueva nacería pudiendo aplicar sin vista previa")
	}
	if c.NewConnection.AllowWriteWithoutConfirmation {
		t.Error("sin la clave, una conexión nueva nacería escribiendo sin confirmar")
	}
}

func TestUnValorQueNoSeEntiendeVuelveAlDefault(t *testing.T) {
	s := nuevoStore(t)
	contenido := "version = 1\ntheme = \"violeta\"\n\n[editor]\nfont_size = 400\n"
	if err := os.WriteFile(s.Path(), []byte(contenido), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := s.Load()
	if err != nil {
		t.Fatalf("Load(): %v", err)
	}
	if c.Theme != ThemeDark {
		t.Errorf("Theme = %q: un tema que no existe tiene que caer al default", c.Theme)
	}
	if c.Editor.FontSize != DefaultFontSize {
		t.Errorf("FontSize = %d: 400 px está fuera de rango", c.Editor.FontSize)
	}
}

func TestUnArchivoDeUnaVersionMasNuevaNoSePisa(t *testing.T) {
	s := nuevoStore(t)
	original := "version = 99\ntheme = \"light\"\n"
	if err := os.WriteFile(s.Path(), []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Load(); err == nil {
		t.Fatal("Load() aceptó un archivo de una versión que esta build no entiende")
	}

	// Y —lo que importa de verdad— GUARDAR tampoco lo pisa.
	//
	// Con el guardado solo en `Load`, la mitad peligrosa quedaba abierta: la
	// pantalla muestra el cartel de que no se pudo leer y deja todos los
	// controles vivos, así que el primer clic escribía un archivo de la versión
	// vieja encima del de la nueva. Quien tenga las dos versiones instaladas
	// pierde sus preferencias abriendo la vieja un momento.
	if _, err := s.Save(Config{Theme: ThemeDark}); err == nil {
		t.Error("Save() pisó un archivo de una versión que esta build no entiende")
	}

	quedo, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if string(quedo) != original {
		t.Errorf("el archivo cambió:\n%s", quedo)
	}
}

func TestUnArchivoCorruptoNoRompeElArranque(t *testing.T) {
	s := nuevoStore(t)
	if err := os.WriteFile(s.Path(), []byte("esto no es TOML {{{"), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := s.Load()
	if err == nil {
		t.Fatal("Load() no reportó el archivo corrupto: la pantalla tiene que poder decirlo")
	}
	// El error se reporta Y se devuelven defaults usables: la app arranca igual
	// y muestra el problema, en vez de no abrir por una preferencia.
	if c.Editor.EffectiveFontSize() != DefaultFontSize {
		t.Errorf("con el archivo corrupto no volvieron defaults usables: %+v", c)
	}
	if strings.Contains(err.Error(), "{{{") {
		t.Error("el error incluye el contenido del archivo")
	}
}

func TestAnotarLaConsultaNoPisaLoQueSeEditoAMano(t *testing.T) {
	s := nuevoStore(t)
	if _, err := s.Save(Config{Theme: ThemeLight}); err != nil {
		t.Fatal(err)
	}
	// Alguien edita el archivo con la app abierta.
	c, _ := s.Load()
	c.Editor.FontSize = 18
	if _, err := s.Save(c); err != nil {
		t.Fatal(err)
	}

	if err := s.AnotarConsulta("2026-09-10T12:00:00Z", "v9.9.9"); err != nil {
		t.Fatalf("AnotarConsulta(): %v", err)
	}

	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Editor.FontSize != 18 || got.Theme != ThemeLight {
		t.Errorf("anotar la consulta pisó las preferencias: %+v", got)
	}
	if got.Updates.LastSeen != "v9.9.9" {
		t.Errorf("LastSeen = %q", got.Updates.LastSeen)
	}
}

// El archivo se escribe en claro. Este test recorre la estructura por reflexión
// —no una lista escrita a mano, que se olvida— para que agregar un campo que
// suene a credencial falle acá y no en una revisión distraída.
func TestNingunCampoDeConfigSuenaAUnSecreto(t *testing.T) {
	prohibidas := []string{"password", "secret", "credential", "token", "passphrase", "apikey"}

	var mirar func(t reflect.Type, camino string)
	vistos := map[reflect.Type]bool{}
	mirar = func(tipo reflect.Type, camino string) {
		if vistos[tipo] {
			return
		}
		vistos[tipo] = true
		for i := 0; i < tipo.NumField(); i++ {
			f := tipo.Field(i)
			nombre := camino + "." + f.Name
			bajo := strings.ToLower(f.Name)
			for _, p := range prohibidas {
				if strings.Contains(bajo, p) {
					t.Errorf("%s: un campo así no va al archivo de preferencias, "+
						"los secretos viven en el keychain del sistema", nombre)
				}
			}
			if f.Type.Kind() == reflect.Struct {
				mirar(f.Type, nombre)
			}
		}
	}
	mirar(reflect.TypeOf(Config{}), "Config")
}

// Y la otra mitad de lo mismo, del lado del archivo: lo que se escribe de
// verdad. Un campo puede llamarse bien y llevar un secreto igual.
func TestElArchivoEscritoNoTieneNadaQueParezcaUnaCredencial(t *testing.T) {
	s := nuevoStore(t)
	if _, err := s.Save(Config{
		Theme:         ThemeLight,
		Editor:        Editor{FontSize: 14},
		NewConnection: connection.Safety{ReadOnly: true},
		Updates:       Updates{LastCheck: "2026-09-10T12:00:00Z", LastSeen: "v1.0.0"},
	}); err != nil {
		t.Fatal(err)
	}

	datos, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	// Se mira el cuerpo y no el encabezado: el comentario de arriba habla
	// justamente de contraseñas y haría pasar el test por la razón equivocada.
	cuerpo := ""
	for _, linea := range strings.Split(string(datos), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(linea), "#") {
			cuerpo += strings.ToLower(linea) + "\n"
		}
	}
	for _, p := range []string{"password", "secret", "token", "credential"} {
		if strings.Contains(cuerpo, p) {
			t.Errorf("el archivo de preferencias contiene %q", p)
		}
	}
}
