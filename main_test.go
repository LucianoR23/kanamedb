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
	for _, s := range servicios(appinfo.Paths{}) {
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
