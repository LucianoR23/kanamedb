package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/LucianoR23/kanamedb/internal/connection"
	"github.com/LucianoR23/kanamedb/internal/dump"
)

// PgDumpStatus es todo lo que la pantalla necesita saber sobre `pg_dump`.
//
// El comando se arma SIEMPRE, se pueda ejecutar o no. Es la parte más barata de
// las tres piezas del volcado y la que más valor da sola: la mitad de los
// errores con `pg_dump` son al escribir la línea, y una línea correcta que se
// copia ya resuelve el problema aunque Kaname no la corra.
type PgDumpStatus struct {
	// Command es la línea lista para copiar y pegar. Nunca lleva la contraseña.
	Command string `json:"command"`

	// Found dice si `pg_dump` está en el PATH, y Path dónde.
	Found bool   `json:"found"`
	Path  string `json:"path,omitempty"`

	// Version es la de `pg_dump` y ServerVersion la del servidor.
	Version       string `json:"version,omitempty"`
	ServerVersion string `json:"serverVersion,omitempty"`

	// CanRun dice si Kaname puede ejecutarlo. Reason explica por qué no.
	CanRun bool   `json:"canRun"`
	Reason string `json:"reason,omitempty"`

	// Hint es qué hacer al respecto, cuando hay algo que hacer.
	Hint string `json:"hint,omitempty"`
}

// PgDumpInfo es cómo terminó la corrida.
type PgDumpInfo struct {
	OK        bool   `json:"ok"`
	Path      string `json:"path"`
	Bytes     int64  `json:"bytes"`
	ElapsedMs int64  `json:"elapsedMs"`

	// Stderr es lo que la herramienta escribió. Se muestra tal cual: es el
	// mensaje de `pg_dump`, y traducirlo sería inventar.
	Stderr string `json:"stderr,omitempty"`
}

// PgDump mira si se puede usar `pg_dump` y arma el comando.
//
// Las tres respuestas posibles son las del plan: armar y mostrar el comando
// —siempre—, ejecutarlo si está y su versión alcanza, y si no está decirlo y
// dejar la línea para copiar.
func (d *Dumps) PgDump(ctx context.Context, r DumpRequest) (PgDumpStatus, error) {
	sesion, err := d.queries.session.abierta()
	if err != nil {
		return PgDumpStatus{}, err
	}
	if sesion.conn.Engine != connection.Postgres {
		return PgDumpStatus{}, fmt.Errorf(
			"`pg_dump` es de PostgreSQL, y esta conexión es %s", sesion.db.Kind().Label())
	}

	st := PgDumpStatus{Command: dump.ComandoTexto(opcionesDePgDump(sesion, r, ""))}

	ruta, err := exec.LookPath("pg_dump")
	if err != nil {
		st.Reason = "`pg_dump` no está en el PATH de esta máquina."
		st.Hint = "Copiá el comando y corrélo donde sí esté, o instalá las herramientas cliente de PostgreSQL."
		return st, nil
	}
	st.Found, st.Path = true, ruta

	version, err := versionDePgDump(ctx, ruta)
	if err != nil {
		st.Reason = "No se pudo averiguar la versión de `pg_dump`: " + err.Error()
		return st, nil
	}
	st.Version = version.String()

	servidor, hay := dump.ParseVersion(versionDelServidor(sesion))
	if hay {
		st.ServerVersion = servidor.String()
		// Es la comprobación que separa una herramienta de una trampa.
		// `pg_dump` soporta servidores más VIEJOS que él, nunca más nuevos: uno
		// de la 15 contra un servidor 18 falla, y en algunas combinaciones no
		// falla —escribe un archivo que parece completo y no lo está—.
		if !version.AlcanzaPara(servidor) {
			st.Reason = fmt.Sprintf(
				"`pg_dump` es de la versión %s y el servidor es %s. Una herramienta más vieja "+
					"que el servidor puede fallar, o —peor— escribir un archivo incompleto sin avisar.",
				version, servidor)
			st.Hint = "Actualizá las herramientas cliente de PostgreSQL a la " + servidor.String() + " o más."
			return st, nil
		}
	}

	// El túnel de Kaname es un dialer de Go, no un puerto local escuchando:
	// `pg_dump` es otro proceso y no puede pasar por él. Abrirle un puerto en
	// 127.0.0.1 sería alcanzable por cualquier cosa que corra en la máquina, y
	// este proyecto no abre sockets locales. Ver CLAUDE.md.
	if sesion.conn.SSH.Enabled {
		st.Reason = "Esta conexión pasa por un bastión SSH, y `pg_dump` es otro proceso: " +
			"no puede usar el túnel de Kaname."
		st.Hint = "Abrí vos el reenvío con `ssh -L` y corré el comando contra el puerto local."
		return st, nil
	}

	st.CanRun = true
	return st, nil
}

// RunPgDump ejecuta la herramienta y espera a que termine.
func (d *Dumps) RunPgDump(ctx context.Context, r DumpRequest, path string) (PgDumpInfo, error) {
	arranque := time.Now()
	if path == "" {
		return PgDumpInfo{}, errors.New("falta la ruta del archivo")
	}
	// La misma puerta que el volcado de Kaname, y por el mismo motivo: sin
	// ninguna de las dos, `pg_dump` no recibe ni `--schema-only` ni
	// `--data-only` y vuelca TODO, que es lo contrario de lo que se pidió.
	if !r.Structure && !r.Data {
		return PgDumpInfo{}, errors.New(
			"el volcado no lleva ni estructura ni datos: no escribiría nada")
	}
	estado, err := d.PgDump(ctx, r)
	if err != nil {
		return PgDumpInfo{}, err
	}
	if !estado.CanRun {
		return PgDumpInfo{}, fmt.Errorf("%s %s", estado.Reason, estado.Hint)
	}
	sesion, err := d.queries.session.abierta()
	if err != nil {
		return PgDumpInfo{}, err
	}

	ctx, listo := d.queries.registrar(ctx, r.RunID)
	defer listo()

	args := dump.Comando(opcionesDePgDump(sesion, r, path))
	cmd := exec.CommandContext(ctx, estado.Path, args[1:]...)

	// La contraseña viaja por el entorno del PROCESO HIJO y nada más: no va en
	// la línea de comandos —que se ve en la lista de procesos de toda la
	// máquina— ni en ningún log. `Env` reemplaza el entorno entero, así que se
	// parte del propio y se le agrega una variable.
	clave, err := d.queries.session.keyring.Get(sesion.conn.ID)
	if err == nil && clave != "" {
		cmd.Env = append(os.Environ(), "PGPASSWORD="+clave)
	}

	var errBuf bytes.Buffer
	// El stderr se acota: `pg_dump` con una base rota puede escribir megabytes
	// de avisos, y lo que hace falta mostrar es el principio.
	cmd.Stderr = &limitado{w: &errBuf, tope: 8 << 10}

	if err := cmd.Run(); err != nil {
		info := PgDumpInfo{Stderr: strings.TrimSpace(errBuf.String())}
		info.ElapsedMs = time.Since(arranque).Milliseconds()
		// El archivo a medio escribir se borra SIEMPRE que la corrida no haya
		// terminado bien, cancelación incluida: uno cortado con el nombre
		// correcto se ve igual que uno entero, que es la peor forma de fallar.
		//
		// Va antes de mirar por qué falló, y no después: el camino de la
		// cancelación salía por su `return` y dejaba el archivo trunco, que era
		// justo lo que este borrado existe para evitar. Kaname escribe en un
		// temporal para no tener este problema; `pg_dump` escribe donde le
		// digan, así que la limpieza es nuestra.
		_ = os.Remove(path)
		if ctx.Err() != nil {
			return info, fmt.Errorf("el volcado se canceló")
		}
		if info.Stderr != "" {
			return info, fmt.Errorf("`pg_dump` falló: %s", info.Stderr)
		}
		return info, fmt.Errorf("`pg_dump` falló: %w", err)
	}

	info := PgDumpInfo{OK: true, Path: path, Stderr: strings.TrimSpace(errBuf.String())}
	if st, err := os.Stat(path); err == nil {
		info.Bytes = st.Size()
	}
	info.ElapsedMs = time.Since(arranque).Milliseconds()
	return info, nil
}

// opcionesDePgDump traduce el pedido de Kaname a las banderas de la herramienta.
func opcionesDePgDump(sesion *openSession, r DumpRequest, salida string) dump.PgDumpOptions {
	return dump.PgDumpOptions{
		Host:      sesion.conn.Host,
		Port:      sesion.conn.Port,
		User:      sesion.conn.User,
		Database:  nombreDeLaBase(sesion),
		Schemas:   r.Schemas,
		Structure: r.Structure,
		Data:      r.Data,
		Clean:     r.DropFirst,
		Salida:    salida,
	}
}

// versionDePgDump corre `pg_dump --version`.
func versionDePgDump(ctx context.Context, ruta string) (dump.Version, error) {
	ctx, cancelar := context.WithTimeout(ctx, 5*time.Second)
	defer cancelar()

	salida, err := exec.CommandContext(ctx, ruta, "--version").Output()
	if err != nil {
		return dump.Version{}, err
	}
	v, ok := dump.ParseVersion(string(salida))
	if !ok {
		return dump.Version{}, fmt.Errorf("no se entiende lo que imprimió: %q", strings.TrimSpace(string(salida)))
	}
	return v, nil
}

func versionDelServidor(sesion *openSession) string {
	if sesion.server != nil {
		return sesion.server.Version
	}
	return ""
}

// limitado escribe hasta un tope y descarta el resto.
type limitado struct {
	w     *bytes.Buffer
	tope  int
	total int
}

func (l *limitado) Write(p []byte) (int, error) {
	if l.total >= l.tope {
		// Se dice que se escribió todo: cortar acá con un error haría que el
		// proceso hijo recibiera un EPIPE y fallara por culpa del límite.
		return len(p), nil
	}
	queda := l.tope - l.total
	if len(p) > queda {
		l.w.Write(p[:queda])
		l.w.WriteString("\n… (recortado)")
		l.total = l.tope
		return len(p), nil
	}
	l.w.Write(p)
	l.total += len(p)
	return len(p), nil
}
