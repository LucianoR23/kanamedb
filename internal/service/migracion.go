package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/drift"
	"github.com/LucianoR23/kanamedb/internal/engine"
)

// MigrationRequest es qué diferencias de la última comparación van al archivo.
type MigrationRequest struct {
	// CompareID es la comparación de la que salen. Tiene que ser la que está en
	// memoria: si se comparó de nuevo en el medio, se rechaza en vez de escribir
	// un archivo que no es el que la persona está mirando.
	CompareID string `json:"compareId"`

	// Include son los IDs de las diferencias que van. Las que no tienen
	// sentencia también pueden ir: salen como comentario, con el motivo, para
	// que el archivo diga lo que NO hace además de lo que hace.
	Include []string `json:"include"`
}

// MigrationText es la migración armada, para mirarla o copiarla.
type MigrationText struct {
	SQL string `json:"sql"`

	// Statements cuenta las sentencias de verdad. Commented cuenta las
	// diferencias incluidas que no tienen sentencia y van como comentario.
	Statements int `json:"statements"`
	Commented  int `json:"commented"`
}

// MigrationSaved cuenta lo que quedó escrito.
type MigrationSaved struct {
	Path       string `json:"path"`
	Bytes      int64  `json:"bytes"`
	Statements int    `json:"statements"`
	Commented  int    `json:"commented"`
}

// Migration arma el archivo de migración de la última comparación.
//
// # Nada de esto se ejecuta
//
// La migración es TEXTO: se guarda en un archivo o se copia, y la corre quien
// la lea, donde y cuando decida. Esta pantalla no aplica nada contra el
// destino, ni siquiera contra el origen. Es la razón por la que comparar
// contra producción no pide la confirmación de escritura: no escribe.
//
// La SQL sale de lo que el motor del destino escribió al comparar —ver
// CompareResult.Statements— y no de lo que mande la interfaz. Que el archivo
// diga exactamente lo que la pantalla mostró es una propiedad, no una
// casualidad, y se sostiene porque las dos leen del mismo lugar.
func (s *Session) Migration(req MigrationRequest) (MigrationText, error) {
	cmp, err := s.comparacionPara(req.CompareID)
	if err != nil {
		return MigrationText{}, err
	}
	return armarMigracion(cmp, req.Include, time.Now())
}

// SaveMigration escribe la migración en path.
func (s *Session) SaveMigration(req MigrationRequest, path string) (MigrationSaved, error) {
	if path == "" {
		return MigrationSaved{}, errors.New("falta la ruta del archivo")
	}
	texto, err := s.Migration(req)
	if err != nil {
		return MigrationSaved{}, err
	}
	n, err := escribirAtomico(path, texto.SQL)
	if err != nil {
		return MigrationSaved{}, err
	}
	return MigrationSaved{
		Path: path, Bytes: n,
		Statements: texto.Statements, Commented: texto.Commented,
	}, nil
}

// comparacionPara devuelve la comparación en memoria si es la que se pide.
func (s *Session) comparacionPara(id string) (*CompareResult, error) {
	if id == "" {
		return nil, errors.New("falta decir de qué comparación sale la migración")
	}
	s.mu.RLock()
	cmp := s.comparacion
	s.mu.RUnlock()
	if cmp == nil || cmp.ID != id {
		return nil, errors.New("esa comparación ya no está: volvé a comparar y generá la migración de nuevo")
	}
	return cmp, nil
}

// armarMigracion escribe el archivo entero como texto.
//
// # El orden no es el de la pantalla
//
// La pantalla lista por esquema y por nombre, que es cómo se lee. Un archivo se
// CORRE, y ahí una clave foránea hacia una tabla que se crea diez líneas más
// abajo falla. Las sentencias van por clase: primero las tablas nuevas, después
// las columnas, después las claves foráneas, y al final el resto. Dentro de cada
// clase se conserva el orden de la comparación, que es estable.
func armarMigracion(cmp *CompareResult, include []string, ahora time.Time) (MigrationText, error) {
	if len(include) == 0 {
		return MigrationText{}, errors.New("no hay ninguna diferencia elegida para la migración")
	}
	if cmp.Result == nil {
		return MigrationText{}, errors.New("la comparación no trajo resultado")
	}

	elegidas := make(map[string]bool, len(include))
	for _, id := range include {
		elegidas[id] = true
	}
	var difs []drift.Diferencia
	for _, d := range cmp.Result.Diferencias {
		if elegidas[d.ID] {
			difs = append(difs, d)
			delete(elegidas, d.ID)
		}
	}
	if len(elegidas) > 0 {
		// Un ID que no está en la comparación es una interfaz que se
		// desincronizó. Escribir el archivo sin esa diferencia y en silencio
		// sería fingir que se incluyó.
		return MigrationText{}, fmt.Errorf("%d de las diferencias elegidas no están en esta comparación", len(elegidas))
	}
	sort.SliceStable(difs, func(i, j int) bool {
		return rangoDeMigracion(difs[i]) < rangoDeMigracion(difs[j])
	})

	var b strings.Builder
	var out MigrationText
	for _, d := range difs {
		if _, hay := cmp.Statements[d.ID]; hay {
			out.Statements++
		} else {
			out.Commented++
		}
	}

	fmt.Fprintf(&b, "-- Migración generada por Kaname el %s\n", ahora.Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "-- Origen:  %s\n", describirLado(cmp.Source))
	fmt.Fprintf(&b, "-- Destino: %s\n", describirLado(cmp.Target))
	fmt.Fprintf(&b, "-- %d %s", out.Statements, plural(out.Statements, "sentencia", "sentencias"))
	if out.Commented > 0 {
		fmt.Fprintf(&b, " · %d %s sin sentencia, %s abajo",
			out.Commented, plural(out.Commented, "diferencia", "diferencias"),
			plural(out.Commented, "comentada", "comentadas"))
	}
	b.WriteString("\n--\n")
	b.WriteString("-- Salió de comparar CATÁLOGOS, no datos. Nada de esto se ejecutó: revisá\n")
	b.WriteString("-- cada sentencia antes de correrla contra el destino. Lo que existe solo en\n")
	b.WriteString("-- el destino no se borra desde acá, nunca.\n")

	env := envolturaPara(engine.Kind(cmp.Target.Engine), reconstruyeAlguna(difs, cmp.Statements))
	if env.apertura != "" {
		b.WriteString("\n" + env.apertura)
	}

	for _, d := range difs {
		b.WriteString("\n")
		fmt.Fprintf(&b, "-- [%s] %s %s\n", marcaDe(d.Lado), claseEnCastellano(d.Clase), nombreCompleto(d))
		comentar(&b, d.Resumen)
		st, hay := cmp.Statements[d.ID]
		if !hay {
			comentar(&b, "Sin sentencia: "+d.SinSentencia)
			if d.Nota != "" {
				comentar(&b, d.Nota)
			}
			continue
		}
		if d.Nota != "" {
			comentar(&b, d.Nota)
		}
		if st.Note != "" {
			comentar(&b, "Antes de correrla: "+st.Note)
		}
		sql := strings.TrimRight(st.SQL, " \t\r\n")
		b.WriteString(sql)
		// Una sentencia sola sale sin `;`; una reconstrucción de SQLite trae
		// varias y termina en `;`. El archivo tiene que ser ejecutable entero.
		if !strings.HasSuffix(sql, ";") {
			b.WriteString(";")
		}
		b.WriteString("\n")
	}

	if env.cierre != "" {
		b.WriteString("\n" + env.cierre)
	}
	out.SQL = b.String()
	return out, nil
}

// envoltura es lo que va antes y después de las sentencias para que el archivo
// se pueda correr como un todo, según el motor que lo va a correr.
type envoltura struct {
	apertura, cierre string
}

// envolturaPara escribe la transacción —y en SQLite, los pragmas— que el apply
// pone alrededor de las mismas sentencias.
//
// # Por qué el archivo no puede ser solo las sentencias
//
// Al aplicar desde la aplicación, `engine.Conn.Begin` abre la transacción y, si
// hay una reconstrucción de tabla, apaga las claves foráneas ANTES del BEGIN
// —adentro de una transacción el pragma es un no-op silencioso— y prende
// `legacy_alter_table`. Sin lo primero, el DROP TABLE del rebuild hace un
// DELETE implícito y dispara los ON DELETE CASCADE de las tablas que la
// referencian: las filas hijas desaparecen sin un solo error. Sin lo segundo,
// una vista sobre la tabla hace fallar el RENAME. Un archivo con las sentencias
// peladas, corrido desde cualquier cliente con las claves encendidas, tiene
// exactamente ese comportamiento. Las sentencias son las mismas; la envoltura
// es lo que las hace seguras.
//
// La transacción va donde el motor la respeta: Postgres y SQLite. En MySQL y
// MariaDB cada DDL confirma solo, y escribir BEGIN/COMMIT prometería un «todo o
// nada» que no existe —ver la § 6 del plan, iteración 6—, así que se dice y no
// se escribe.
func envolturaPara(motor engine.Kind, reconstruye bool) envoltura {
	if !engine.CapsOf(motor).TransactionalDDL {
		return envoltura{
			apertura: "-- " + string(motor) + ": cada sentencia de DDL confirma sola. No hay transacción que\n" +
				"-- revierta las anteriores si una falla: si se corta a mitad de camino,\n" +
				"-- volvé a comparar para ver qué quedó.\n",
		}
	}
	e := envoltura{
		apertura: "-- Corré el archivo con una herramienta que PARE en el primer error\n" +
			"-- (psql -v ON_ERROR_STOP=1, sqlite3 -bail): va en una transacción, y solo\n" +
			"-- revierte si el error corta la corrida antes del COMMIT.\n",
		cierre: "COMMIT;\n",
	}
	if motor == engine.SQLite && reconstruye {
		e.apertura += "-- Hay reconstrucciones de tabla. Las claves foráneas se apagan ANTES del\n" +
			"-- BEGIN —adentro de una transacción el pragma no hace nada— porque el DROP\n" +
			"-- TABLE de una reconstrucción dispara los ON DELETE CASCADE de las tablas\n" +
			"-- hijas si están encendidas. Al final, foreign_key_check lista las filas\n" +
			"-- que quedaron apuntando a nada: si devuelve alguna, ROLLBACK en vez de COMMIT.\n" +
			"PRAGMA foreign_keys = OFF;\n" +
			"PRAGMA legacy_alter_table = ON;\n"
		e.cierre = "PRAGMA foreign_key_check;\n" +
			"COMMIT;\n" +
			"PRAGMA legacy_alter_table = OFF;\n" +
			"PRAGMA foreign_keys = ON;\n"
	}
	e.apertura += "BEGIN;\n"
	return e
}

// reconstruyeAlguna dice si alguna de las sentencias incluidas reconstruye una
// tabla.
func reconstruyeAlguna(difs []drift.Diferencia, sentencias map[string]change.Statement) bool {
	for _, d := range difs {
		if st, hay := sentencias[d.ID]; hay && st.RebuildsTable {
			return true
		}
	}
	return false
}

// rangoDeMigracion es el orden en que las sentencias pueden correr.
func rangoDeMigracion(d drift.Diferencia) int {
	if d.Cambio == nil {
		// Los comentarios van al final, juntos: son lo que quedó sin hacer.
		return 9
	}
	switch d.Cambio.Type {
	case change.CreateTable:
		return 0
	case change.AddColumn, change.SetColumnType, change.SetNotNull, change.DropNotNull,
		// La clave primaria va con las columnas y ANTES de las foráneas: una
		// clave foránea hacia una tabla sin clave primaria falla con «no hay
		// una restricción única que coincida» en Postgres y MySQL.
		change.AddPrimaryKey:
		return 1
	case change.AddForeignKey:
		return 2
	}
	return 3
}

func describirLado(l CompareSide) string {
	entorno := l.Environment
	if l.Production {
		entorno = "PRODUCCIÓN"
	}
	return fmt.Sprintf("%s (%s, %s) · %s", l.Name, l.Engine, entorno, l.Describe)
}

func marcaDe(l drift.Lado) string {
	switch l {
	case drift.SoloEnOrigen:
		return "+"
	case drift.SoloEnDestino:
		return "-"
	}
	return "~"
}

func claseEnCastellano(c drift.Clase) string {
	switch c {
	case drift.ClaseTabla:
		return "tabla"
	case drift.ClaseColumna:
		return "columna"
	case drift.ClaseForanea:
		return "clave foránea"
	case drift.ClaseEsquema:
		return "esquema"
	}
	return "objeto"
}

func nombreCompleto(d drift.Diferencia) string {
	if d.Clase == drift.ClaseEsquema || d.Schema == "" {
		return d.Objeto
	}
	return d.Schema + "." + d.Objeto
}

// comentar escribe un texto como comentario SQL, partido en líneas de largo
// razonable para que se pueda leer en cualquier editor.
func comentar(b *strings.Builder, texto string) {
	const ancho = 76
	linea := "--    "
	for _, palabra := range strings.Fields(texto) {
		if len(linea)+1+len(palabra) > ancho && linea != "--    " {
			b.WriteString(linea + "\n")
			linea = "--    "
		}
		linea += " " + palabra
	}
	if linea != "--    " {
		b.WriteString(linea + "\n")
	}
}

// escribirAtomico escribe contenido en path pasando por un archivo temporal del
// mismo directorio, y devuelve cuántos bytes quedaron.
//
// Un fallo a mitad de camino no deja un archivo cortado con el nombre que la
// persona eligió: una migración a medias que parece entera es peor que ninguna.
func escribirAtomico(path, contenido string) (int64, error) {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, "."+base+"-*")
	if err != nil {
		return 0, fmt.Errorf("no se pudo crear el archivo en %s: %w", dir, err)
	}
	nombre := tmp.Name()
	limpiar := func() { _ = os.Remove(nombre) }

	n, err := tmp.WriteString(contenido)
	if err != nil {
		_ = tmp.Close()
		limpiar()
		return 0, fmt.Errorf("no se pudo escribir %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		limpiar()
		return 0, fmt.Errorf("no se pudo escribir %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		limpiar()
		return 0, fmt.Errorf("no se pudo cerrar %s: %w", path, err)
	}
	if err := os.Rename(nombre, path); err != nil {
		limpiar()
		return 0, fmt.Errorf("no se pudo dejar el archivo en %s: %w", path, err)
	}
	return int64(n), nil
}
