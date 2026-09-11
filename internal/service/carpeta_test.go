package service

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCadaSistemaAbreConSuProgram(t *testing.T) {
	casos := map[string]string{
		"windows": "explorer",
		"darwin":  "open",
		"linux":   "xdg-open",
	}
	for goos, quiero := range casos {
		prog, args, ok := comandoParaAbrir(goos, "/un/directorio")
		if !ok {
			t.Errorf("%s: no se eligió ningún programa", goos)
			continue
		}
		if prog != quiero {
			t.Errorf("%s: se eligió %q, se esperaba %q", goos, prog, quiero)
		}
		if len(args) != 1 || args[0] != "/un/directorio" {
			t.Errorf("%s: los argumentos son %q", goos, args)
		}
	}

	if _, _, ok := comandoParaAbrir("plan9", "/tmp"); ok {
		t.Error("en un sistema desconocido se eligió un programa igual")
	}
}

// La carpeta que se abre sale del store, y el directorio que le corresponde es
// el de la libreta. Lo que importa es que nunca salga de otro lado: si esta
// función tomara una ruta del frontend, sería «ejecutá el explorador sobre lo
// que yo te diga».
func TestLaCarpetaQueSeAbreEsLaDeLasPreferencias(t *testing.T) {
	s, prefs, _ := ajustesDePrueba(t)

	dir := filepath.Dir(prefs.Path())
	if dir == "" || dir == "." {
		t.Fatalf("el store quedó con una ruta rara: %q", prefs.Path())
	}

	// El servicio no expone la ruta que va a abrir —abrir es su único efecto—
	// así que lo que se comprueba es que el store sea el mismo que el de las
	// preferencias, que es de donde sale.
	if got := filepath.Dir(s.store.Path()); got != dir {
		t.Errorf("el servicio abriría %q y las preferencias están en %q", got, dir)
	}

	// Y que la ruta que se armaría no lleve nada raro pegado: si algún día
	// alguien le agrega un argumento, esto lo dice.
	_, args, _ := comandoParaAbrir("windows", dir)
	if len(args) != 1 {
		t.Fatalf("se pasarían %d argumentos: %q", len(args), args)
	}
	if strings.ContainsAny(args[0], "&|;") {
		t.Errorf("la ruta lleva caracteres de shell: %q", args[0])
	}
}
