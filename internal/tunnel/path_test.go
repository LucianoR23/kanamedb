package tunnel

import (
	"os"
	"path/filepath"
	"testing"
)

// `~` se expande al usar la ruta, no al guardarla.
//
// El archivo de conexiones se sincroniza entre máquinas y `~` significa algo
// distinto en cada una. Guardarla expandida ataría la conexión a la máquina
// donde se configuró.
func TestExpandirRuta(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("sin directorio de usuario en este entorno: %v", err)
	}

	casos := []struct {
		nombre string
		dentro string
		fuera  string
	}{
		{"tilde sola", "~", home},
		{"tilde con barra", "~/.ssh/id_ed25519", filepath.Join(home, ".ssh", "id_ed25519")},
		// La forma de Windows tiene que funcionar TAMBIÉN en Linux y macOS: el
		// archivo de conexiones se sincroniza, y quien la configuró en Windows
		// escribió barras invertidas. Ahí no separan nada por sí solas, así que
		// hay que traducirlas o la ruta apunta a un archivo llamado
		// `.ssh\id_ed25519`.
		{"tilde con barra invertida", `~\.ssh\id_ed25519`, filepath.Join(home, ".ssh", "id_ed25519")},
		// Y la traducción NO puede pasarse de lista. Con `~/`, la barra
		// invertida es parte del nombre: en Linux un archivo se puede llamar
		// así, y convertirla en separador lo rompería. En Windows este caso no
		// distingue nada —las dos barras separan igual—, así que es un test que
		// solo puede fallar en la pata de Linux de la matriz.
		{"barra invertida adentro de una ruta con tilde y barra", `~/carpeta\rara`,
			filepath.Join(home, `carpeta\rara`)},
		// Lo que importa que NO se toque: una ruta absoluta de Windows, que es
		// como la va a pegar cualquiera que arrastre el archivo o copie del
		// explorador.
		{"absoluta de Windows", `C:\Users\alguien\.ssh\clave.key`, `C:\Users\alguien\.ssh\clave.key`},
		{"absoluta de Unix", "/home/alguien/.ssh/id_ed25519", "/home/alguien/.ssh/id_ed25519"},
		{"relativa", "claves/id_ed25519", "claves/id_ed25519"},
		// Un nombre que empieza con ~ pero no es el home: `~raro` es un archivo
		// llamado así, no el directorio de nadie.
		{"nombre que arranca con tilde", "~raro/clave", "~raro/clave"},
		{"vacía", "", ""},
	}

	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			got, err := ExpandHome(c.dentro)
			if err != nil {
				t.Fatalf("ExpandHome(%q) error: %v", c.dentro, err)
			}
			if got != c.fuera {
				t.Errorf("ExpandHome(%q) = %q, se esperaba %q", c.dentro, got, c.fuera)
			}
		})
	}
}

// "Copiar como ruta" del Explorador de Windows agrega comillas, y es la forma
// más común de copiar una ruta en ese sistema. La cadena va directo a la API de
// archivos, no a una shell, así que las comillas serían parte del nombre.
func TestLimpiarRuta(t *testing.T) {
	casos := []struct{ dentro, fuera string }{
		{`"C:\Users\lr231\.ssh\clave.key"`, `C:\Users\lr231\.ssh\clave.key`},
		{`  "C:\ruta con espacios\clave.key"  `, `C:\ruta con espacios\clave.key`},
		{`'/home/alguien/.ssh/id_ed25519'`, `/home/alguien/.ssh/id_ed25519`},
		// Sin comillas, intacta. Los espacios internos no se tocan: son parte
		// del nombre y no hacen falta comillas para tenerlos.
		{`C:\ruta con espacios\clave.key`, `C:\ruta con espacios\clave.key`},
		// Una comilla de un solo lado no es un envoltorio: adivinar sería peor.
		{`"C:\rara`, `"C:\rara`},
		{`C:\rara"`, `C:\rara"`},
		{"", ""},
	}
	for _, c := range casos {
		if got := CleanPath(c.dentro); got != c.fuera {
			t.Errorf("CleanPath(%q) = %q, se esperaba %q", c.dentro, got, c.fuera)
		}
	}
}
