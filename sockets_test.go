package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Sin servidor HTTP y sin ninguna ruta que ignore la clave de un host SSH: son
// los dos requisitos duros de CLAUDE.md que un test funcional no puede
// distinguir de su violación —una app que además escucha en 127.0.0.1 hace
// todo lo que hace la que no—, así que la garantía es estructural: se lee el
// código.
//
// Es la versión de todo el módulo de lo que `internal/tunnel` ya hacía para su
// paquete, y va por el árbol sintáctico y no por `strings.Contains` para que un
// comentario que mencione `net.Listen` —como este— no lo rompa.
//
// Lo que Wails v3 trae de fábrica queda afuera por construcción: sus dos
// listeners —`application_server.go` y `mcp_enabled.go`— están detrás de los
// build tags `server` y `mcp`, y `wails3 task build` compila con `production`.
// Que ningún Taskfile compile con esos tags lo verifica el test de abajo.

// llamadasProhibidas son `paquete.Función` que abren un socket para escuchar o
// desactivan la verificación del host, por la RUTA del import y no por el
// nombre con que se lo usa: `import n "net"` seguido de `n.Listen` tiene que
// caer igual.
var llamadasProhibidas = map[string][]string{
	"net":                     {"Listen", "ListenTCP", "ListenUDP", "ListenIP", "ListenUnix", "ListenUnixgram", "ListenPacket", "ListenMulticastUDP", "ListenConfig"},
	"net/http":                {"ListenAndServe", "ListenAndServeTLS", "Serve", "ServeTLS"},
	"net/http/httptest":       {"NewServer", "NewTLSServer", "NewUnstartedServer"},
	"golang.org/x/crypto/ssh": {"InsecureIgnoreHostKey"},
}

// metodosProhibidos son métodos que, con cualquier receptor, levantan un
// servidor: `srv.ListenAndServe()` o `srv.Serve(l)` sobre un `*http.Server`.
// Nada en el módulo se llama Serve, así que el nombre alcanza.
var metodosProhibidos = []string{"ListenAndServe", "ListenAndServeTLS", "Serve", "ServeTLS"}

// importsDe devuelve, para cada nombre con que el archivo usa un paquete, la
// ruta importada. Sin alias, el nombre es el último segmento de la ruta.
func importsDe(f *ast.File) map[string]string {
	nombres := map[string]string{}
	for _, imp := range f.Imports {
		ruta := strings.Trim(imp.Path.Value, `"`)
		nombre := ruta[strings.LastIndex(ruta, "/")+1:]
		if imp.Name != nil {
			nombre = imp.Name.Name
		}
		nombres[nombre] = ruta
	}
	return nombres
}

func TestNingunPaqueteAbreUnSocketNiIgnoraLaClaveDelHost(t *testing.T) {
	fset := token.NewFileSet()
	mirados := 0

	revisar := func(ruta string) {
		f, err := parser.ParseFile(fset, ruta, nil, 0)
		if err != nil {
			t.Fatalf("parsear %s: %v", ruta, err)
		}
		mirados++
		imports := importsDe(f)
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if paquete, ok := sel.X.(*ast.Ident); ok {
				for _, prohibida := range llamadasProhibidas[imports[paquete.Name]] {
					if sel.Sel.Name == prohibida {
						t.Errorf("%s: usa %s.%s", fset.Position(sel.Pos()), imports[paquete.Name], prohibida)
					}
				}
			}
			for _, m := range metodosProhibidos {
				if sel.Sel.Name == m {
					t.Errorf("%s: llama a .%s", fset.Position(sel.Pos()), m)
				}
			}
			return true
		})
	}

	revisar("main.go")
	err := filepath.WalkDir("internal", func(ruta string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(ruta, ".go") && !strings.HasSuffix(ruta, "_test.go") {
			revisar(ruta)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("recorrer internal/: %v", err)
	}
	// Sin esto, borrar los paquetes haría pasar el test.
	if mirados < 20 {
		t.Fatalf("se revisaron %d archivos; el módulo tiene bastantes más", mirados)
	}
}

// El template de Wails trae `build:server`, `run:server`, `build:docker` y
// `run:docker`: la aplicación compilada como servidor HTTP sin ventana, con
// los bindings —y las credenciales— detrás de un puerto. Es exactamente lo que
// CLAUDE.md prohíbe, y el plan decía desde la iteración 0 que estaban
// borradas. No lo estaban: nadie lo había mirado. Este test mira.
//
// `build:docker` no se busca por nombre: los Taskfiles de cada sistema tienen
// uno homónimo que cross-compila la app de escritorio dentro de una imagen, y
// no levanta nada. El del modo servidor cae por el tag.
func TestNingunTaskfileCompilaElModoServidor(t *testing.T) {
	tags := regexp.MustCompile(`-tags[= ]+["']?([A-Za-z0-9_,{}. ]+)`)
	tareas := regexp.MustCompile(`(?m)^\s{2}(build:server|run:server|run:docker):`)
	// Solo los Taskfiles que el build usa: el de la raíz y los de build/. Un
	// recorrido de todo el repo miraría también lo que no es del proyecto
	// —un worktree viejo en .claude/, una copia en bin/— y fallaría por un
	// archivo que no compila nada.
	rutas := []string{"Taskfile.yml"}
	err := filepath.WalkDir("build", func(ruta string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == "Taskfile.yml" {
			rutas = append(rutas, ruta)
		}
		return err
	})
	if err != nil {
		t.Fatalf("recorrer build/: %v", err)
	}

	for _, ruta := range rutas {
		datos, err := os.ReadFile(ruta)
		if err != nil {
			t.Fatalf("leer %s: %v", ruta, err)
		}
		for _, m := range tareas.FindAllStringSubmatch(string(datos), -1) {
			t.Errorf("%s define la tarea %s: el modo servidor no existe en este proyecto", ruta, m[1])
		}
		for _, m := range tags.FindAllStringSubmatch(string(datos), -1) {
			// Go acepta los tags separados por coma y, en la forma vieja,
			// por espacio: `-tags "server production"` es un solo token si
			// se corta solo por coma.
			for _, tag := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || r == ' ' }) {
				// `server{{if eq .DEV "true"}}…`: el tag termina donde
				// empieza el template.
				tag, _, _ = strings.Cut(tag, "{")
				if tag == "server" || tag == "mcp" {
					t.Errorf("%s compila con -tags %s: eso levanta un listener de Wails", ruta, tag)
				}
			}
		}
	}
	if len(rutas) < 4 {
		t.Fatalf("se miraron %d Taskfiles; hay uno en la raíz y uno por sistema en build/", len(rutas))
	}
}
