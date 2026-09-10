package dump

import (
	"strings"
	"testing"
)

func opts() PgDumpOptions {
	return PgDumpOptions{
		Host: "127.0.0.1", Port: 5432, User: "kaname", Database: "demo",
	}
}

// TestElComandoNoLlevaLaContrasena es un requisito duro y el motivo por el que
// este comando se puede mostrar: se copia a un chat, a un ticket y al historial
// del shell.
func TestElComandoNoLlevaLaContrasena(t *testing.T) {
	o := opts()
	texto := ComandoTexto(o)
	// «password» a secas no sirve como aguja: `--no-password` la contiene, y es
	// justamente la bandera que hay que tener. Se buscan las formas en que una
	// credencial aparecería de verdad.
	for _, prohibido := range []string{"PGPASSWORD", "password=", "--password", "postgres://", ":kaname@"} {
		if strings.Contains(texto, prohibido) {
			t.Errorf("el comando tiene %q: %s", prohibido, texto)
		}
	}
	// Y lleva `--no-password`: sin eso `pg_dump` abre un prompt que, lanzado
	// desde una ventana, no tiene dónde aparecer y cuelga el proceso.
	if !strings.Contains(texto, "--no-password") {
		t.Errorf("falta --no-password: %s", texto)
	}
}

// TestSoloEstructuraYSoloDatosNoSePasanJuntos.
//
// `--schema-only --data-only` es un error de `pg_dump`, y pedir «todo» es no
// pasar ninguno de los dos. Es la clase de detalle que hace que el comando
// armado a mano falle.
func TestSoloEstructuraYSoloDatosNoSePasanJuntos(t *testing.T) {
	casos := []struct {
		estructura, datos bool
		quiero, noQuiero  string
	}{
		{true, false, "--schema-only", "--data-only"},
		{false, true, "--data-only", "--schema-only"},
		{true, true, "", "--schema-only"},
		{true, true, "", "--data-only"},
	}
	for _, c := range casos {
		o := opts()
		o.Structure, o.Data = c.estructura, c.datos
		texto := ComandoTexto(o)
		if c.quiero != "" && !strings.Contains(texto, c.quiero) {
			t.Errorf("con estructura=%v datos=%v falta %q: %s", c.estructura, c.datos, c.quiero, texto)
		}
		if strings.Contains(texto, c.noQuiero) && c.quiero != c.noQuiero {
			if c.quiero == "" || !strings.Contains(c.quiero, c.noQuiero) {
				t.Errorf("con estructura=%v datos=%v sobra %q: %s", c.estructura, c.datos, c.noQuiero, texto)
			}
		}
	}
}

// TestLoQueElShellPartiriaVaEntreComillas: la ruta de Windows con espacios es
// el caso de todos los días.
func TestLoQueElShellPartiriaVaEntreComillas(t *testing.T) {
	o := opts()
	o.Salida = `C:\Users\ana lopez\Mis Documentos\volcado.sql`
	texto := ComandoTexto(o)
	if !strings.Contains(texto, `"--file=C:\\Users\\ana lopez\\Mis Documentos\\volcado.sql"`) {
		t.Errorf("la ruta con espacios no quedó citada: %s", texto)
	}
	// Y lo que no lo necesita queda limpio: una línea llena de comillas se lee
	// peor, y esto se lee tanto como se corre.
	if strings.Contains(texto, `"--port=5432"`) {
		t.Errorf("se citó algo que no hacía falta: %s", texto)
	}
}

func TestElEsquemaYElNombreDeLaBaseVanEnElComando(t *testing.T) {
	o := opts()
	o.Schemas = []string{"public", "ventas"}
	texto := ComandoTexto(o)
	for _, q := range []string{"--schema=public", "--schema=ventas"} {
		if !strings.Contains(texto, q) {
			t.Errorf("falta %q: %s", q, texto)
		}
	}
	// La base va al final y sin bandera, que es como la espera pg_dump.
	if !strings.HasSuffix(texto, " demo") {
		t.Errorf("la base no está al final: %s", texto)
	}
}

// TestUnPgDumpViejoNoSirveParaUnServidorNuevo.
//
// Es la comprobación que separa una herramienta de una trampa. `pg_dump`
// soporta servidores más viejos, nunca más nuevos: uno de la 15 contra un
// servidor 18 falla, y en algunas combinaciones NO falla y escribe un archivo
// que parece completo.
func TestUnPgDumpViejoNoSirveParaUnServidorNuevo(t *testing.T) {
	v18 := Version{Mayor: 18, Menor: 3}
	v15 := Version{Mayor: 15, Menor: 6}

	if v15.AlcanzaPara(v18) {
		t.Error("un pg_dump 15 se dio por bueno contra un servidor 18")
	}
	if !v18.AlcanzaPara(v15) {
		t.Error("un pg_dump 18 tendría que servir para un servidor 15")
	}
	if !v18.AlcanzaPara(v18) {
		t.Error("la misma versión tiene que servir")
	}
	// La menor no decide: es la mayor la que cambia el formato del catálogo.
	if !(Version{Mayor: 18}).AlcanzaPara(Version{Mayor: 18, Menor: 9}) {
		t.Error("una diferencia de versión menor no puede bloquear el volcado")
	}
}

func TestLaVersionSeSacaDeLoQueImprimeCadaHerramienta(t *testing.T) {
	casos := []struct {
		texto  string
		quiero string
		ok     bool
	}{
		{"pg_dump (PostgreSQL) 17.2", "17.2", true},
		{"pg_dump (PostgreSQL) 18.0", "18", true},
		{"PostgreSQL 18.3 on aarch64-unknown-linux-musl, compiled by gcc 14.2.0", "18.3", true},
		{"pg_dump (PostgreSQL) 15.6 (Debian 15.6-1.pgdg120+2)", "15.6", true},
		{"", "", false},
		{"no soy una versión", "", false},
	}
	for _, c := range casos {
		v, ok := ParseVersion(c.texto)
		if ok != c.ok {
			t.Errorf("ParseVersion(%q) ok = %v", c.texto, ok)
			continue
		}
		if ok && v.String() != c.quiero {
			t.Errorf("ParseVersion(%q) = %q, quería %q", c.texto, v.String(), c.quiero)
		}
	}
}
