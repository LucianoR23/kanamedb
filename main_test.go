package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/appinfo"
)

// modulo es el prefijo que Wails le pone a las rutas de los bindings.
const modulo = "github.com/LucianoR23/kanamedb"

// importaDeBindings captura el servicio que el frontend está importando.
//
// La forma con DOS segmentos después de `internal/` es la que designa un
// servicio: `internal/service/history` son los métodos de `service.History`.
// La forma con uno solo —`internal/service`, `internal/schema`— es el índice
// del paquete, que trae tipos y no métodos, y por eso no se mira acá.
var importaDeBindings = regexp.MustCompile(
	`bindings/` + regexp.QuoteMeta(modulo) + `/(internal/[a-z]+/[a-z]+)`)

// TestTodoServicioQueLaInterfazLlamaEstaRegistrado compara la lista de
// `servicios()` contra lo que el frontend importa de verdad.
//
// Es el test que faltaba cuando el historial quedó sin registrar. La razón por
// la que ese error no lo agarra nada más: el generador de bindings NO lee la
// lista de servicios, recorre el código. Así que un servicio ausente de main.go
// igual tiene su archivo `.ts` generado, con sus tipos, y el frontend lo
// importa y lo llama sin que ni `go vet` ni `tsc` tengan nada que decir. El
// primer síntoma es un botón que no hace nada, en una build de release.
func TestTodoServicioQueLaInterfazLlamaEstaRegistrado(t *testing.T) {
	registrados := map[string]bool{}
	lista, _ := servicios(appinfo.Paths{})
	for _, s := range lista {
		registrados[claveDelServicio(s.Instance())] = true
	}
	if len(registrados) == 0 {
		t.Fatal("servicios() no devolvió nada: el resto del test no probaría nada")
	}

	usados := serviciosQueImportaElFrontend(t)
	if len(usados) == 0 {
		t.Fatal("no se encontró ningún import de bindings en frontend/src: " +
			"si cambió la forma del import, este test dejó de mirar algo")
	}

	for clave, donde := range usados {
		if !registrados[clave] {
			t.Errorf("%s llama al servicio %q y servicios() no lo registra.\n"+
				"Compila y genera bindings igual; falla recién al usarlo.",
				donde, clave)
		}
	}
}

var (
	usaVariable   = regexp.MustCompile(`var\(\s*(--[a-z0-9-]+)`)
	defineEnCSS   = regexp.MustCompile(`(--[a-z0-9-]+)\s*:`)
	defineDesdeJS = regexp.MustCompile(`"(--[a-z0-9-]+)"`)
)

// TestTodaVariableCSSQueSeUsaEstaDefinida recorre los estilos y exige que cada
// `var(--algo)` tenga de dónde salir.
//
// Es el test que faltaba cuando el panel de dependientes se pintó con cuatro
// tokens inventados —`--surface`, `--surface-2`, `--text`, `--text-faint`—. Un
// `var()` que no resuelve NO es un error: la propiedad simplemente no se aplica.
// Así que los tres estados del panel se dibujaban idénticos mientras el
// comentario de al lado decía que estaban distinguidos por color, y nada falló:
// ni el build, ni `tsc`, ni la pantalla, que se veía bien.
//
// Vale también para lo que se define desde JavaScript —`--env-color` sale de un
// `style` en línea— y por eso los nombres se buscan también en el TSX, en vez de
// mantener una lista de excepciones que se desactualiza.
func TestTodaVariableCSSQueSeUsaEstaDefinida(t *testing.T) {
	definidas := map[string]bool{}
	usadas := map[string]string{}

	raiz := filepath.Join("frontend", "src")
	err := filepath.WalkDir(raiz, func(ruta string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := filepath.Ext(ruta)
		if ext != ".css" && ext != ".ts" && ext != ".tsx" {
			return nil
		}
		datos, err := os.ReadFile(ruta)
		if err != nil {
			return err
		}
		texto := string(datos)

		if ext == ".css" {
			for _, m := range defineEnCSS.FindAllStringSubmatch(texto, -1) {
				definidas[m[1]] = true
			}
			for _, m := range usaVariable.FindAllStringSubmatch(texto, -1) {
				if _, ya := usadas[m[1]]; !ya {
					usadas[m[1]] = filepath.ToSlash(ruta)
				}
			}
			return nil
		}
		// Desde el TSX solo interesa qué se DEFINE: `style={{ "--env-color": … }}`
		// y `setProperty("--sql-font-size", …)`.
		for _, m := range defineDesdeJS.FindAllStringSubmatch(texto, -1) {
			definidas[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("recorrer %s: %v", raiz, err)
	}

	if len(usadas) < 50 {
		t.Fatalf("solo se encontraron %d variables usadas: el test dejó de mirar algo", len(usadas))
	}
	for nombre, donde := range usadas {
		if !definidas[nombre] {
			t.Errorf("%s usa %s y no está definida en ningún lado.\n"+
				"Un var() que no resuelve no falla: no pinta.", donde, nombre)
		}
	}
}

// versionEnArchivo es dónde dice la versión cada pieza del build. Hay una por
// sistema porque cada empaquetador lee la suya: el syso de Windows, el
// Info.plist del .app, el nfpm de los .deb/.rpm, el build.gradle del APK.
var versionEnArchivo = []struct {
	ruta   string
	patron *regexp.Regexp
}{
	{"build/config.yml", regexp.MustCompile(`(?m)^\s*version:\s*"([^"]+)"`)},
	{"build/windows/info.json", regexp.MustCompile(`"file_version":\s*"([^"]+)"`)},
	{"build/windows/info.json", regexp.MustCompile(`"ProductVersion":\s*"([^"]+)"`)},
	{"build/darwin/Info.plist", regexp.MustCompile(`<key>CFBundleVersion</key>\s*<string>([^<]+)</string>`)},
	{"build/darwin/Info.plist", regexp.MustCompile(`<key>CFBundleShortVersionString</key>\s*<string>([^<]+)</string>`)},
	{"build/darwin/Info.dev.plist", regexp.MustCompile(`<key>CFBundleVersion</key>\s*<string>([^<]+)</string>`)},
	{"build/darwin/Info.dev.plist", regexp.MustCompile(`<key>CFBundleShortVersionString</key>\s*<string>([^<]+)</string>`)},
	{"build/linux/nfpm/nfpm.yaml", regexp.MustCompile(`(?m)^version:\s*"([^"]+)"`)},
	{"build/android/app/build.gradle", regexp.MustCompile(`(?m)^\s*versionName\s+"([^"]+)"`)},
}

// TestLaVersionEsLaMismaEnTodosLados compara `appinfo.Version` —lo que muestra
// About— con lo que declara cada empaquetador.
//
// Son siete lugares escritos a mano porque `wails3 task common:update:build-assets`,
// que los regeneraría desde build/config.yml, pisa también lo que se editó a
// propósito: el Info.plist, el nfpm.yaml, el .desktop. Así que la versión se
// sube a mano, y esto es lo que avisa cuando quedó una atrás: un .deb que dice
// 0.1.0 con un About que dice 0.2.0 no lo nota nadie hasta que alguien
// pregunta cuál tiene instalado.
func TestLaVersionEsLaMismaEnTodosLados(t *testing.T) {
	for _, v := range versionEnArchivo {
		datos, err := os.ReadFile(v.ruta)
		if err != nil {
			t.Fatalf("leer %s: %v", v.ruta, err)
		}
		m := v.patron.FindSubmatch(datos)
		if m == nil {
			t.Fatalf("%s: no se encontró la versión con %s", v.ruta, v.patron)
		}
		if got := string(m[1]); got != appinfo.Version {
			t.Errorf("%s dice %q y appinfo.Version es %q", v.ruta, got, appinfo.Version)
		}
	}
}

// claveDelServicio traduce una instancia a la ruta con la que el frontend la
// importa: `*service.History` es `internal/service/history`.
func claveDelServicio(instancia any) string {
	t := reflect.TypeOf(instancia)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	paquete := strings.TrimPrefix(t.PkgPath(), modulo+"/")
	return paquete + "/" + strings.ToLower(t.Name())
}

// serviciosQueImportaElFrontend recorre el código de la interfaz y devuelve qué
// servicios importa, con el archivo donde apareció cada uno.
//
// Lee `frontend/src`, que está commiteado, y no `frontend/bindings`, que está
// en .gitignore: un test que dependiera de los bindings generados pasaría o
// fallaría según si alguien corrió la tarea de Wails antes.
func serviciosQueImportaElFrontend(t *testing.T) map[string]string {
	t.Helper()

	usados := map[string]string{}
	raiz := filepath.Join("frontend", "src")
	err := filepath.WalkDir(raiz, func(ruta string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(ruta) {
		case ".ts", ".tsx":
		default:
			return nil
		}
		datos, err := os.ReadFile(ruta)
		if err != nil {
			return err
		}
		for _, m := range importaDeBindings.FindAllStringSubmatch(string(datos), -1) {
			clave := m[1]
			// `models` e `index` son archivos del generador, no servicios.
			if base := filepath.Base(clave); base == "models" || base == "index" {
				continue
			}
			if _, ya := usados[clave]; !ya {
				usados[clave] = filepath.ToSlash(ruta)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("recorrer %s: %v", raiz, err)
	}
	return usados
}
