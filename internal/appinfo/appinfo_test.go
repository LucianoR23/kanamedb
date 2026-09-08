package appinfo

import (
	"path/filepath"
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

	prohibidas := []string{"password", "secret", "credential", "keychain", "token"}
	rutas := []string{got.Paths.Connections, got.Paths.Config, got.Paths.State, got.Paths.Logs}
	for _, ruta := range rutas {
		bajo := strings.ToLower(ruta)
		for _, p := range prohibidas {
			if strings.Contains(bajo, p) {
				t.Errorf("la ruta %q contiene %q: los secretos no van al disco", ruta, p)
			}
		}
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
