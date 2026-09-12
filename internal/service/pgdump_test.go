package service

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/LucianoR23/kanamedb/internal/dump"
	"github.com/LucianoR23/kanamedb/internal/tunnel"
)

// TestElComandoDePgDumpSeArmaAunqueNoSePuedaCorrer.
//
// Es lo que más valor da de las tres piezas y lo más barato: la mitad de los
// errores con `pg_dump` son al escribir la línea. Que la herramienta no esté
// —o que la versión no alcance— NO puede dejar la pantalla sin nada que copiar.
func TestElComandoDePgDumpSeArmaAunqueNoSePuedaCorrer(t *testing.T) {
	d, sesion, esq, _, _ := volcadoDePrueba(t, "postgres", motoresDeDatos[0].uri, "kn_pgd")
	abierta, _ := sesion.abierta()

	st, err := d.PgDump(context.Background(), DumpRequest{
		Schemas: esquemasPedidos(esq), Structure: true, Data: true,
	})
	if err != nil {
		t.Fatalf("PgDump(): %v", err)
	}
	if st.Command == "" {
		t.Fatal("no se armó el comando")
	}
	// El modo TLS es el de la conexión: la de pruebas va con sslmode=disable,
	// y eso tiene que verse en el comando; una en verify-full llevaría el suyo
	// y su raíz (K-04). Sin esto pg_dump volcaba con el default de libpq.
	for _, q := range []string{
		"pg_dump", "--host=" + abierta.conn.Host, "--no-password",
		"sslmode=" + string(abierta.conn.EffectiveSSLMode()),
	} {
		if !strings.Contains(st.Command, q) {
			t.Errorf("al comando le falta %q: %s", q, st.Command)
		}
	}
	// La contraseña NUNCA: el comando se copia a un chat y al historial.
	for _, prohibido := range []string{"PGPASSWORD", "password=", "--password", ":kaname@"} {
		if strings.Contains(st.Command, prohibido) {
			t.Errorf("el comando lleva %q: %s", prohibido, st.Command)
		}
	}
	// Con los dos tildados no va ninguna de las dos banderas.
	if strings.Contains(st.Command, "--schema-only") || strings.Contains(st.Command, "--data-only") {
		t.Errorf("con estructura y datos no va ninguna bandera: %s", st.Command)
	}

	// Y si la herramienta no está, se dice y se deja el comando igual.
	if _, err := exec.LookPath("pg_dump"); err != nil {
		if st.Found || st.CanRun {
			t.Errorf("pg_dump no está en el PATH pero el estado dice que sí: %+v", st)
		}
		if st.Reason == "" || st.Hint == "" {
			t.Errorf("no se explica por qué no se puede correr: %+v", st)
		}
	}
}

// TestConTunelNoSeCorrePgDumpYSeDicePorQue.
//
// El túnel de Kaname es un dialer de Go, no un puerto local escuchando:
// `pg_dump` es otro proceso y no puede pasar por él. Abrirle un puerto en
// 127.0.0.1 sería alcanzable por cualquier cosa de la máquina, y este proyecto
// no abre sockets locales. Lo que NO se puede hacer es intentarlo igual y
// volcar la base equivocada —la que responda en ese host y puerto sin túnel—.
func TestConTunelNoSeCorrePgDumpYSeDicePorQue(t *testing.T) {
	d, sesion, esq, _, _ := volcadoDePrueba(t, "postgres", motoresDeDatos[0].uri, "kn_pgd_tun")
	abierta, _ := sesion.abierta()
	abierta.conn.SSH = tunnel.Config{Enabled: true, Host: "bastion", Port: 22, User: "kaname"}

	st, err := d.PgDump(context.Background(), DumpRequest{
		Schemas: esquemasPedidos(esq), Structure: true,
	})
	if err != nil {
		t.Fatalf("PgDump(): %v", err)
	}
	if st.CanRun {
		t.Fatal("se ofreció correr pg_dump a través de un túnel que no puede usar")
	}
	if !strings.Contains(st.Reason, "bastión") {
		t.Errorf("el motivo no menciona el bastión: %q", st.Reason)
	}
	if !strings.Contains(st.Hint, "ssh -L") {
		t.Errorf("no se dice qué hacer: %q", st.Hint)
	}
	// Y el comando sigue estando, que es lo que deja resolverlo a mano.
	if st.Command == "" {
		t.Error("se perdió el comando por tener túnel")
	}
}

// TestPgDumpNoSeOfreceEnLosOtrosMotores: `pg_dump` es de PostgreSQL, y ofrecerlo
// contra MySQL sería prometer algo que no existe.
func TestPgDumpNoSeOfreceEnLosOtrosMotores(t *testing.T) {
	for _, caso := range motoresDeDatos {
		if caso.nombre == "postgres" {
			continue
		}
		t.Run(caso.nombre, func(t *testing.T) {
			d, _, esq, _, _ := volcadoDePrueba(t, caso.nombre, caso.uri, "kn_pgd_otro")
			_, err := d.PgDump(context.Background(), DumpRequest{
				Schemas: esquemasPedidos(esq), Structure: true,
			})
			if err == nil {
				t.Fatal("se ofreció pg_dump en un motor que no es PostgreSQL")
			}
			if !strings.Contains(err.Error(), "PostgreSQL") {
				t.Errorf("el error no explica por qué: %v", err)
			}
		})
	}
}

// TestElStderrRecortadoNoRompeElProceso.
//
// `pg_dump` con una base rota puede escribir megabytes de avisos. Cortar con un
// error le daría un EPIPE al proceso hijo y lo haría fallar POR EL LÍMITE, que
// es un fallo inventado por nosotros.
func TestElStderrRecortadoNoRompeElProceso(t *testing.T) {
	l := &limitado{w: new(bytes.Buffer), tope: 100}
	grande := strings.Repeat("x", 5000)
	n, err := l.Write([]byte(grande))
	if err != nil {
		t.Fatalf("el escritor devolvió error: %v", err)
	}
	if n != len(grande) {
		t.Errorf("dijo haber escrito %d de %d: un short write también rompe el pipe", n, len(grande))
	}
	// Y sigue aceptando lo que venga después.
	if n2, err := l.Write([]byte("mas")); err != nil || n2 != 3 {
		t.Errorf("después del tope devolvió %d, %v", n2, err)
	}
	if got := l.w.String(); len(got) > 200 {
		t.Errorf("no recortó: %d bytes", len(got))
	}
}

// TestElTunelSeCuentaAntesQueLosDemasImpedimentos.
//
// Es el test que faltaba y por eso CI encontró lo que mi máquina no: acá
// `pg_dump` y el servidor son de la misma versión, así que el túnel quedaba
// como único impedimento y el test de arriba pasaba. En CI hay un `pg_dump` 16
// contra servidores 17 y 18, ganaba el impedimento de la versión, y la pantalla
// no mencionaba el bastión NUNCA.
//
// Que gane el túnel no es un capricho del orden: es el único impedimento que,
// ignorado, no falla. `pg_dump` correría contra lo que responda en ese host y
// puerto SIN el túnel —otra base— y escribiría un archivo de aspecto impecable.
// Los otros dos fallan de frente y se entienden solos.
func TestElTunelSeCuentaAntesQueLosDemasImpedimentos(t *testing.T) {
	vieja := dump.Version{Mayor: 16, Menor: 15}
	servidor := dump.Version{Mayor: 18, Menor: 6}

	// Todo mal a la vez: detrás de un bastión, sin la herramienta en el PATH,
	// sin poder averiguar su versión y con una versión que no alcanza.
	todo := diagnosticoDePgDump{
		tunel:      true,
		versionErr: errors.New("permission denied"),
		version:    vieja,
		servidor:   servidor,
	}

	casos := []struct {
		nombre string
		diag   diagnosticoDePgDump
		razon  string
		hints  []string
	}{
		{
			// El túnel gana, pero el hint no manda a nadie a armar un reenvío
			// para descubrir después que tampoco tiene la herramienta.
			"todos los impedimentos juntos", todo,
			"bastión", []string{"ssh -L", "tampoco está en el PATH"},
		},
		{
			"solo el túnel", diagnosticoDePgDump{tunel: true, enElPath: true},
			"bastión", []string{"ssh -L"},
		},
		{
			// El caso exacto de CI: la herramienta está y es vieja, y además hay
			// bastión. Es el que se contaba al revés.
			"un túnel y una herramienta vieja",
			diagnosticoDePgDump{
				tunel: true, enElPath: true,
				version: vieja, servidor: servidor,
			},
			"bastión", []string{"ssh -L"},
		},
		{
			"sin túnel gana la herramienta que falta", conTunel(todo, false),
			"no está en el PATH", []string{"instalá"},
		},
		{
			"con la herramienta gana no saber su versión", enElPath(conTunel(todo, false)),
			"permission denied", nil,
		},
		{
			"y con la versión sabida, que no alcance",
			diagnosticoDePgDump{enElPath: true, version: vieja, servidor: servidor},
			"16.15", []string{"18.6"},
		},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			razon, hint := c.diag.impedimento()
			if !strings.Contains(razon, c.razon) {
				t.Errorf("el motivo no dice %q: %q", c.razon, razon)
			}
			for _, q := range c.hints {
				if !strings.Contains(hint, q) {
					t.Errorf("al consejo le falta %q: %q", q, hint)
				}
			}
		})
	}

	// Y sin ningún impedimento no se inventa uno: dos cadenas vacías es lo que
	// prende el botón de correr.
	sano := diagnosticoDePgDump{enElPath: true, version: servidor, servidor: servidor}
	if razon, hint := sano.impedimento(); razon != "" || hint != "" {
		t.Errorf("con todo en orden se inventó un impedimento: %q / %q", razon, hint)
	}
}

// TestUnServidorDeVersionDesconocidaNoAcusaALaHerramienta.
//
// Lo que protege es que la acusación de «tu `pg_dump` es viejo» no pueda salir
// de la nada: si la versión del servidor no se pudo leer queda en `Mayor: 0`, y
// contra eso cualquier herramienta alcanza.
//
// Vive acá y no adentro del test del orden porque lo que lo sostiene NO es el
// `switch` —ahí no hay ningún guardia que lo mire, y ponerlo sería una segunda
// forma de decir lo mismo que ningún test podría poner en rojo— sino
// `AlcanzaPara`, que compara con `>=`. La inyección que lo pone en rojo es
// cambiar ese `>=` por `==` en internal/dump/pgdump.go: ahí un servidor
// desconocido pasa a acusar a una herramienta que está perfecta.
func TestUnServidorDeVersionDesconocidaNoAcusaALaHerramienta(t *testing.T) {
	// Así queda el diagnóstico cuando `ParseVersion` no entendió lo que dijo el
	// servidor: `PgDump` no toca el campo y el cero es «no se sabe».
	d := diagnosticoDePgDump{enElPath: true, version: dump.Version{Mayor: 16, Menor: 15}}
	if _, hay := dump.ParseVersion("no soy una versión"); hay {
		t.Fatal("el fixture no representa un servidor sin versión")
	}
	if razon, _ := d.impedimento(); razon != "" {
		t.Errorf("se acusó a la herramienta contra un servidor de versión desconocida: %q", razon)
	}
}

func conTunel(d diagnosticoDePgDump, tunel bool) diagnosticoDePgDump {
	d.tunel = tunel
	return d
}

func enElPath(d diagnosticoDePgDump) diagnosticoDePgDump {
	d.enElPath = true
	return d
}
