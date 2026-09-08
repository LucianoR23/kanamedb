package tunnel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// El túnel no puede abrir un puerto local, y esto lo verifica leyendo el código.
//
// Un túnel SSH se implementa habitualmente escuchando en 127.0.0.1 y
// reenviando. Funciona igual de bien y es exactamente lo que este proyecto no
// quiere: un puerto en loopback es alcanzable desde cualquier pestaña del
// navegador, y DNS rebinding saltea CORS. Es la misma razón por la que la
// aplicación no tiene servidor HTTP.
//
// Un test funcional no puede distinguir las dos implementaciones —las dos
// conectan— así que la garantía tiene que ser estructural. Si alguien reescribe
// esto con un listener, acá se entera.
func TestElTunelNoEscuchaEnNingunPuerto(t *testing.T) {
	entradas, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("leer el paquete: %v", err)
	}

	prohibidas := []string{"net.Listen", "net.ListenTCP", "net.ListenConfig", "ListenAndServe"}
	mirados := 0

	for _, e := range entradas {
		nombre := e.Name()
		if e.IsDir() || !strings.HasSuffix(nombre, ".go") || strings.HasSuffix(nombre, "_test.go") {
			continue
		}
		datos, err := os.ReadFile(filepath.Clean(nombre))
		if err != nil {
			t.Fatalf("leer %s: %v", nombre, err)
		}
		mirados++
		for _, p := range prohibidas {
			if strings.Contains(string(datos), p) {
				t.Errorf("%s usa %s: el túnel no puede abrir un puerto local", nombre, p)
			}
		}
	}

	// Sin esto, borrar los archivos del paquete haría pasar el test.
	if mirados == 0 {
		t.Fatal("no se revisó ningún archivo del paquete")
	}
}
