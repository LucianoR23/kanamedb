package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// «Nunca loguear connection strings, contraseñas ni valores de filas», dice
// CLAUDE.md. La forma más corta de cumplirlo es la que hay: la aplicación no
// loguea nada, no emite eventos hacia el frontend y no escribe en la consola.
// Lo que llega a la interfaz llega como valor de retorno de un binding, y los
// tests de cada motor y del túnel revisan que un fallo de autenticación no
// lleve el secreto que lo provocó.
//
// Estos tests hacen que esa ausencia sea estructural: el día que alguien
// agregue un log, un evento o un `console.log` de datos, va a tener que venir
// acá y decidir qué se redacta antes de escribirlo.

// logueo son las llamadas por ruta de import que escriben a algún lado.
var logueo = map[string][]string{
	"log":      nil, // todo el paquete
	"log/slog": nil,
	"fmt":      {"Print", "Println", "Printf"},
	"os":       {"Stderr", "Stdout"},
}

// eventos son los métodos con que Wails empuja algo hacia el frontend sin que
// lo haya pedido: no hay ninguno en uso, y si aparece uno tiene que pasar por
// la misma revisión que un valor de retorno.
var eventos = []string{"Emit", "EmitEvent"}

func TestLaAplicacionNoLogueaNiEmiteEventos(t *testing.T) {
	fset := token.NewFileSet()

	revisar := func(ruta string, permitidas map[string]bool) {
		f, err := parser.ParseFile(fset, ruta, nil, 0)
		if err != nil {
			t.Fatalf("parsear %s: %v", ruta, err)
		}
		imports := importsDe(f)
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if paquete, ok := sel.X.(*ast.Ident); ok {
				if ruta, es := imports[paquete.Name]; es {
					if nombres, prohibido := logueo[ruta]; prohibido {
						if (nombres == nil || contiene(nombres, sel.Sel.Name)) && !permitidas[ruta+"."+sel.Sel.Name] {
							t.Errorf("%s: usa %s.%s", fset.Position(sel.Pos()), ruta, sel.Sel.Name)
						}
					}
				}
			}
			if contiene(eventos, sel.Sel.Name) {
				t.Errorf("%s: emite un evento hacia el frontend (.%s); lo que cruza el puente se revisa antes", fset.Position(sel.Pos()), sel.Sel.Name)
			}
			return true
		})
	}

	// main.go puede abortar si no sabe dónde guardar la configuración: el
	// mensaje es la ruta del directorio, y sin eso no hay app que arrancar.
	for _, ruta := range archivosDelBinario(t) {
		var permitidas map[string]bool
		if ruta == "main.go" {
			permitidas = map[string]bool{"log.Fatal": true, "log.Fatalf": true}
		}
		revisar(ruta, permitidas)
	}
}

// Wails loguea cada llamada a un binding con sus argumentos y su resultado
// —«Binding call complete: args=… result=…»— en nivel Debug. En producción el
// logger por defecto es io.Discard, así que no pasa nada. Pasaría si main.go
// le diera un Logger propio o subiera el nivel: la contraseña de SaveWithSSH
// y las filas de cada consulta irían a donde ese logger escriba.
func TestMainNoConfiguraElLoggerDeWails(t *testing.T) {
	fset := token.NewFileSet()
	opciones := 0
	// Los .go de la raíz, no solo main.go: las opciones se pueden armar en
	// un helper de al lado, y las dos formas cuentan: la clave del literal
	// (`Logger: …`) y la asignación después (`opts.LogLevel = …`).
	raiz, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("listar la raíz: %v", err)
	}
	for _, ruta := range raiz {
		if strings.HasSuffix(ruta, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, ruta, nil, 0)
		if err != nil {
			t.Fatalf("parsear %s: %v", ruta, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				sel, ok := x.Type.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Options" {
					return true
				}
				opciones++
				for _, e := range x.Elts {
					kv, ok := e.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if k, ok := kv.Key.(*ast.Ident); ok && (k.Name == "Logger" || k.Name == "LogLevel") {
						t.Errorf("%s: application.Options configura %s; Wails loguea los argumentos de cada binding en Debug", fset.Position(kv.Pos()), k.Name)
					}
				}
			case *ast.SelectorExpr:
				if _, ok := x.X.(*ast.Ident); ok && (x.Sel.Name == "Logger" || x.Sel.Name == "LogLevel") {
					t.Errorf("%s: toca .%s; Wails loguea los argumentos de cada binding en Debug", fset.Position(x.Pos()), x.Sel.Name)
				}
			}
			return true
		})
	}
	if opciones == 0 {
		t.Fatal("no se encontró el literal application.Options en la raíz")
	}
}

// Los valores de celda son datos no confiables: se pintan como texto, nunca
// como HTML. Y nada se escribe a la consola del webview, donde una fila o un
// error con DETAIL del servidor quedarían a la vista de cualquier extensión
// de depuración.
func TestElFrontendNoInyectaHTMLNiEscribeEnLaConsola(t *testing.T) {
	prohibidas := []string{"dangerouslySetInnerHTML", ".innerHTML", ".outerHTML", "insertAdjacentHTML", "console.log(", "console.info(", "console.debug(", "console.warn(", "console.error("}
	mirados := 0
	err := filepath.WalkDir("frontend/src", func(ruta string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !(strings.HasSuffix(ruta, ".ts") || strings.HasSuffix(ruta, ".tsx")) {
			return nil
		}
		datos, err := os.ReadFile(ruta)
		if err != nil {
			return err
		}
		mirados++
		for i, linea := range strings.Split(string(datos), "\n") {
			for _, p := range prohibidas {
				if strings.Contains(linea, p) {
					t.Errorf("%s:%d: usa %s", ruta, i+1, p)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("recorrer frontend/src: %v", err)
	}
	if mirados < 20 {
		t.Fatalf("se revisaron %d archivos de frontend/src; hay bastantes más", mirados)
	}
}

// TestElBuildLlevaUnaContentSecurityPolicy: la CSP se inyecta en el build por
// un plugin de Vite (ver frontend/vite.config.ts) y no está en index.html
// porque en desarrollo rompería a Vite. Lo que se puede exigir desde acá es
// que el plugin exista, esté enchufado y prohíba scripts que no sean propios.
func TestElBuildLlevaUnaContentSecurityPolicy(t *testing.T) {
	datos, err := os.ReadFile("frontend/vite.config.ts")
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(datos)
	for _, q := range []string{
		`"script-src 'self'"`, `"object-src 'none'"`, `"base-uri 'none'"`,
		`http-equiv="Content-Security-Policy"`, `apply: "build"`, "csp(),",
	} {
		if !strings.Contains(cfg, q) {
			t.Errorf("vite.config.ts no tiene %s", q)
		}
	}
	if strings.Contains(cfg, "'unsafe-eval'") || strings.Contains(cfg, "script-src 'self' 'unsafe-inline'") {
		t.Error("la CSP permite scripts inline o eval, que es no tener CSP")
	}
}

func contiene(lista []string, s string) bool {
	for _, x := range lista {
		if x == s {
			return true
		}
	}
	return false
}
