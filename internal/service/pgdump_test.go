package service

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

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
	for _, q := range []string{"pg_dump", "--host=" + abierta.conn.Host, "--no-password"} {
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
