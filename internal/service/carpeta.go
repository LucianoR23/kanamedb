package service

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
)

// OpenConfigFolder abre en el explorador de archivos la carpeta donde viven la
// libreta de conexiones y las preferencias.
//
// Es la entrada «Dónde se guarda todo» del botón de desborde. Mostrar la ruta
// como texto no alcanza: lo que quiere hacer quien pregunta eso es respaldar el
// archivo o copiarlo a otra máquina, y para eso hay que llegar a la carpeta.
//
// # No recibe la ruta
//
// A propósito, y es la única decisión de seguridad de este archivo: si el
// frontend pudiera pasar una ruta, esto sería «ejecutá el explorador sobre lo
// que yo te diga». La ruta sale del store, o sea de `appinfo`, que es quien
// decide dónde vive cada cosa. Y se pasa como ARGUMENTO, no como una línea de
// comandos: no hay shell en el medio que pueda interpretar nada.
// El contexto NO se usa para lanzar el proceso, y eso no es un olvido.
//
// `exec.CommandContext` mata al hijo cuando el contexto termina, y el contexto
// de un binding de Wails es el de LA LLAMADA: se cancela en cuanto el método
// devuelve. Con él, el explorador se lanzaba y moría en el mismo instante — el
// método contestaba sin error, la persona apretaba y no pasaba nada. Encontrado
// probando a mano; ningún test lo habría visto, porque `Start()` devuelve nil
// igual.
//
// La ventana que se abre es del usuario, no de Kaname: no tiene por qué vivir
// atada a una llamada que ya terminó.
func (s *Settings) OpenConfigFolder(_ context.Context) error {
	dir := filepath.Dir(s.store.Path())
	prog, args, ok := comandoParaAbrir(runtime.GOOS, dir)
	if !ok {
		return fmt.Errorf("no sé cómo abrir una carpeta en %s. Está en %s", runtime.GOOS, dir)
	}
	if err := exec.Command(prog, args...).Start(); err != nil {
		// La ruta va en el error: si no se pudo abrir, lo que queda es poder
		// copiarla a mano, y esconderla ahí sería dejar a la persona sin nada.
		return fmt.Errorf("no se pudo abrir %s: %w", dir, err)
	}
	return nil
}

// comandoParaAbrir elige con qué se abre una carpeta en cada sistema.
//
// Está separada de OpenConfigFolder —que lanza el proceso— para poder probar la
// elección sin abrir ventanas: un test que corre en CI no puede comprobar que
// se haya abierto el explorador, pero sí que en Linux no se llame a `explorer`.
func comandoParaAbrir(goos, dir string) (string, []string, bool) {
	switch goos {
	case "windows":
		return "explorer", []string{dir}, true
	case "darwin":
		return "open", []string{dir}, true
	case "linux":
		// El estándar de freedesktop. Está en cualquier escritorio; en un
		// servidor sin entorno gráfico no, y ahí el error lo dice.
		return "xdg-open", []string{dir}, true
	default:
		return "", nil, false
	}
}
