package appinfo

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestGetDevuelveIdentidadCompleta(t *testing.T) {
	got, err := New().Get()
	if err != nil {
		t.Fatalf("Get() devolvió error: %v", err)
	}

	if got.Name != "Kaname" {
		t.Errorf("Name = %q, se esperaba %q", got.Name, "Kaname")
	}
	if got.Version == "" {
		t.Error("Version está vacía")
	}
	if got.BuildDate == "" {
		t.Error("BuildDate está vacía")
	}
	if !strings.HasPrefix(got.GoVersion, "go") {
		t.Errorf("GoVersion = %q, se esperaba algo que empiece con \"go\"", got.GoVersion)
	}

	wantPlatform := runtime.GOOS + "/" + runtime.GOARCH
	if got.Platform != wantPlatform {
		t.Errorf("Platform = %q, se esperaba %q", got.Platform, wantPlatform)
	}
}

func TestGetDevuelveRutasAbsolutas(t *testing.T) {
	got, err := New().Get()
	if err != nil {
		t.Fatalf("Get() devolvió error: %v", err)
	}

	rutas := map[string]string{
		"Connections": got.Paths.Connections,
		"Config":      got.Paths.Config,
		"State":       got.Paths.State,
		"Logs":        got.Paths.Logs,
	}
	for nombre, ruta := range rutas {
		if ruta == "" {
			t.Errorf("Paths.%s está vacía", nombre)
			continue
		}
		if !filepath.IsAbs(ruta) {
			t.Errorf("Paths.%s = %q, se esperaba una ruta absoluta", nombre, ruta)
		}
		if !strings.Contains(ruta, appDirName) {
			t.Errorf("Paths.%s = %q, no contiene %q", nombre, ruta, appDirName)
		}
	}
}

// La app promete que las credenciales viven solo en el keychain. Este test
// existe para que agregar una ruta de secretos a Paths falle en CI en vez de
// pasar en una revisión distraída.
func TestPathsNoExponeNingunaRutaDeSecretos(t *testing.T) {
	got, err := New().Get()
	if err != nil {
		t.Fatalf("Get() devolvió error: %v", err)
	}

	// Las rutas se recorren POR REFLEXIÓN y no con una lista escrita a mano.
	// Con la lista, agregar un campo a Paths lo dejaba sin mirar y el test
	// seguía en verde diciendo que había revisado todo — que es la forma exacta
	// en que una ruta de secretos entraría sin que nadie se entere.
	v := reflect.ValueOf(got.Paths)
	prohibidas := []string{"password", "secret", "credential", "keychain", "token"}
	for i := 0; i < v.NumField(); i++ {
		campo := v.Type().Field(i)
		if campo.Type.Kind() != reflect.String {
			t.Fatalf("Paths.%s no es un string: este test dejó de cubrir el tipo", campo.Name)
		}
		bajo := strings.ToLower(v.Field(i).String())
		for _, p := range prohibidas {
			if strings.Contains(bajo, p) {
				t.Errorf("Paths.%s (%q) contiene %q: los secretos no van al disco",
					campo.Name, v.Field(i).String(), p)
			}
		}
	}
}

// El historial y las consultas guardadas están de lados opuestos de la línea
// que separa "esta máquina" de "lo que se sincroniza", y ésa es toda la razón
// por la que son dos archivos. Si alguna vez caen en el mismo directorio, el
// historial de lo que corriste empieza a viajar a la otra máquina.
// Se prueba contra `rutasDe` con dos raíces distintas, no contra las rutas
// reales: en Windows el directorio de estado ES el de configuración, así que
// con las reales los dos lados de la afirmación coinciden y el caso pasa diga
// lo que diga el código.
func TestElHistorialEsLocalYLasGuardadasViajanConLaLibreta(t *testing.T) {
	const base, state = "/libreta", "/estado"
	p := rutasDe(base, state)

	if dir := filepath.Dir(p.History); dir != filepath.Clean(state) {
		t.Errorf("Paths.History está en %q y el estado local es %q: "+
			"el historial es de esta máquina y no se sincroniza", dir, state)
	}
	if dir := filepath.Dir(p.SavedQueries); dir != filepath.Dir(p.Connections) {
		t.Errorf("Paths.SavedQueries está en %q y la libreta en %q: "+
			"las guardadas viajan con las conexiones", dir, filepath.Dir(p.Connections))
	}
}

func TestConfigCuelgaDelDirectorioDeLaApp(t *testing.T) {
	got, err := New().Get()
	if err != nil {
		t.Fatalf("Get() devolvió error: %v", err)
	}
	if base := filepath.Base(got.Paths.Config); base != "config.toml" {
		t.Errorf("Paths.Config apunta a %q, se esperaba config.toml", base)
	}
	if dir := filepath.Base(filepath.Dir(got.Paths.Config)); dir != appDirName {
		t.Errorf("Paths.Config cuelga de %q, se esperaba %q", dir, appDirName)
	}
}

func TestLaLibretaDeConexionesTieneSuPropioArchivo(t *testing.T) {
	got, err := New().Get()
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if filepath.Base(got.Paths.Connections) != "connections.toml" {
		t.Errorf("Paths.Connections = %q", got.Paths.Connections)
	}
	// Separada de las preferencias: la libreta se sincroniza entre máquinas y
	// las preferencias pueden ser propias de cada una.
	if got.Paths.Connections == got.Paths.Config {
		t.Error("la libreta y las preferencias comparten archivo")
	}
}
