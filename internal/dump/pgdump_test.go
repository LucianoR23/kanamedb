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
	// La base va en la cadena de conexión de libpq, al final.
	if !strings.HasSuffix(texto, "--dbname=dbname=demo") {
		t.Errorf("la base no está al final como cadena de conexión: %s", texto)
	}
}

// TestElComandoLlevaElTLSDeLaConexion.
//
// `pg_dump` es otro proceso: no hereda el modo TLS ni los certificados que
// pgx usa en la conexión de Kaname, y sin ellos arranca con `prefer`, que no
// verifica nada. Una conexión `verify-full` con raíz propia tiene que volcar
// con `verify-full` y esa raíz, y el comando que se copia tiene que mostrarlo:
// quien lo corre a mano hereda lo mismo. Hallazgo K-04 de la auditoría del
// 2026-09-11.
func TestElComandoLlevaElTLSDeLaConexion(t *testing.T) {
	o := opts()
	o.SSLMode = "verify-full"
	o.SSLRootCert = `C:\Users\ana\certs\raiz ca.pem`
	o.SSLCert = "/home/ana/cliente.crt"
	o.SSLKey = "/home/ana/cliente.key"
	// Se mira el argumento tal como lo recibe pg_dump, no el texto para el
	// shell: el shell le agrega su propia capa de comillas encima.
	args := Comando(o)
	dbname := args[len(args)-1]
	for _, q := range []string{
		"--dbname=dbname=demo ",
		"sslmode=verify-full",
		// Citado como lo pide libpq: comillas simples por el espacio, y las
		// barras de Windows dobladas adentro.
		`sslrootcert='C:\\Users\\ana\\certs\\raiz ca.pem'`,
		"sslcert=/home/ana/cliente.crt",
		"sslkey=/home/ana/cliente.key",
	} {
		if !strings.Contains(dbname, q) {
			t.Errorf("falta %q: %s", q, dbname)
		}
	}
	// Todo adentro de UN --dbname: libpq lo lee de ahí. Y el texto copiable
	// lo muestra, para que quien lo corra a mano herede el mismo modo.
	texto := ComandoTexto(o)
	if strings.Count(texto, "--dbname=") != 1 || !strings.Contains(texto, "sslmode=verify-full") {
		t.Errorf("el comando copiable no lleva el TLS en una sola cadena: %s", texto)
	}

	// Sin TLS configurado no se inventa nada.
	texto = ComandoTexto(opts())
	if strings.Contains(texto, "ssl") {
		t.Errorf("sin TLS el comando no tiene por qué mencionarlo: %s", texto)
	}
	// Y un nombre de base que empieza con guion ya no es una opción.
	o = opts()
	o.Database = "-rara"
	if !strings.Contains(ComandoTexto(o), "dbname=-rara") {
		t.Errorf("una base llamada -rara tiene que ir dentro de la cadena: %s", ComandoTexto(o))
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
