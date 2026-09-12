package service

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/export"
	"github.com/LucianoR23/kanamedb/internal/query"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Exports formatea y guarda lo que sale de la aplicación.
//
// Son dos caminos con el mismo escritor. El resultado del editor ya está en
// memoria del lado de la interfaz y vuelve entero para formatearse acá:
// formateo puro, sin consulta. Una TABLA no vuelve por el puente —dos millones
// de filas no caben en un mensaje— y se lee del motor a medida que se escribe
// al archivo, con `engine.Conn.Scan`.
//
// Se apoya en Queries y no solo en Session por el registro de cancelaciones:
// una exportación larga ES una consulta corriendo, y «Cancelar» tiene que
// poder cortarla con el mismo runID que corta cualquier otra.
type Exports struct {
	queries *Queries
}

func NewExports(q *Queries) *Exports { return &Exports{queries: q} }

// ResultExport es un resultado del editor con el formato pedido.
type ResultExport struct {
	Format  export.Format  `json:"format"`
	Options export.Options `json:"options"`
	Columns []query.Column `json:"columns"`
	Rows    [][]*string    `json:"rows"`
}

// FormatInfo describe un formato para que la interfaz no tenga que saber
// las extensiones.
type FormatInfo struct {
	Key       export.Format `json:"key"`
	Extension string        `json:"extension"`
	// NeedsTable dice que el formato necesita saber de qué tabla salen las
	// filas, y que por lo tanto no sirve para el resultado del editor: una
	// consulta puede ser un join de tres tablas y no hay a cuál insertar.
	NeedsTable bool `json:"needsTable"`
}

// Formats lista los formatos en el orden en que se ofrecen.
func (e *Exports) Formats() []FormatInfo {
	out := make([]FormatInfo, 0, len(export.Formats))
	for _, f := range export.Formats {
		out = append(out, FormatInfo{Key: f, Extension: f.Extension(), NeedsTable: f.NeedsTable()})
	}
	return out
}

// Preview devuelve el texto de las primeras filas, sin comprimir.
func (e *Exports) Preview(r ResultExport, limit int) (string, error) {
	if limit <= 0 {
		return "", errors.New("la vista previa necesita un límite de filas")
	}
	if err := sirveParaUnResultado(r.Format); err != nil {
		return "", err
	}
	return export.Render(r.Format, r.Options, r.Columns, r.Rows, limit)
}

// Render devuelve el texto entero, para el portapapeles.
func (e *Exports) Render(r ResultExport) (string, error) {
	if err := sirveParaUnResultado(r.Format); err != nil {
		return "", err
	}
	return export.Render(r.Format, r.Options, r.Columns, r.Rows, 0)
}

// SaveInfo cuenta lo que quedó escrito.
type SaveInfo struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Rows  int    `json:"rows"`
}

// Save escribe el resultado del editor en path.
func (e *Exports) Save(r ResultExport, path string) (SaveInfo, error) {
	if err := sirveParaUnResultado(r.Format); err != nil {
		return SaveInfo{}, err
	}
	return e.guardar(path, func(w io.Writer) (int, error) {
		return export.Write(r.Format, w, r.Options, r.Columns, r.Rows, 0)
	})
}

// sirveParaUnResultado rechaza los formatos que necesitan una tabla.
//
// El resultado del editor puede ser un join de tres tablas o `select 1`: no hay
// a qué insertar. La interfaz no lo ofrece, y acá se comprueba igual, porque lo
// que la interfaz no ofrece hoy lo puede ofrecer mañana por error.
func sirveParaUnResultado(f export.Format) error {
	if f.NeedsTable() {
		return fmt.Errorf("el formato %s necesita una tabla, y el resultado de una consulta no tiene una: "+
			"puede venir de varias, o de ninguna", string(f))
	}
	return nil
}

// guardar escribe en un archivo temporal del mismo directorio y lo renombra al
// final.
//
// Un fallo a mitad de camino —disco lleno, permiso, el servidor que corta el
// recorrido— no deja un archivo cortado con el nombre que la persona eligió,
// que se vería igual que uno entero. Con una tabla grande esto no es teórico:
// el archivo se escribe durante minutos y cualquier cosa puede pasar en el
// medio.
func (e *Exports) guardar(path string, escribir func(io.Writer) (int, error)) (SaveInfo, error) {
	if path == "" {
		return SaveInfo{}, errors.New("falta la ruta del archivo")
	}
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, "."+base+"-*")
	if err != nil {
		return SaveInfo{}, fmt.Errorf("no se pudo crear el archivo en %s: %w", dir, err)
	}
	// Si algo falla, el temporal se va. Después del rename ya no existe con
	// ese nombre y el Remove no hace nada.
	defer os.Remove(tmp.Name())

	n, err := escribir(tmp)
	if err != nil {
		tmp.Close()
		return SaveInfo{}, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return SaveInfo{}, fmt.Errorf("no se pudo escribir %s: %w", base, err)
	}
	info, err := tmp.Stat()
	if err != nil {
		tmp.Close()
		return SaveInfo{}, fmt.Errorf("no se pudo escribir %s: %w", base, err)
	}
	if err := tmp.Close(); err != nil {
		return SaveInfo{}, fmt.Errorf("no se pudo escribir %s: %w", base, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return SaveInfo{}, fmt.Errorf("no se pudo reemplazar %s: %w", base, err)
	}
	return SaveInfo{Path: path, Bytes: info.Size(), Rows: n}, nil
}

/* -------------------------------------------------------------- una tabla */

// TableExport es una tabla que se exporta entera.
//
// No trae las filas: las lee Go del motor. Eso es lo que separa este camino del
// del editor —una tabla puede no entrar en memoria— y por eso la interfaz solo
// dice CUÁL tabla, no QUÉ filas.
type TableExport struct {
	// RunID lo elige la interfaz y sirve para cancelar esta exportación y no
	// otra, con el mismo Queries.Cancel que corta una consulta.
	RunID  string `json:"runId"`
	Schema string `json:"schema"`
	Table  string `json:"table"`

	Format  export.Format  `json:"format"`
	Options export.Options `json:"options"`

	// OrderBy vacío deja que el motor devuelva las filas en el orden que
	// quiera, que para un archivo completo da lo mismo y es más rápido. Se
	// ordena por la clave primaria cuando el archivo tiene que ser comparable
	// entre dos corridas.
	OrderBy    []string `json:"orderBy"`
	Descending bool     `json:"descending"`

	// Where es el filtro que está puesto en la grilla. Exportar «lo que estoy
	// mirando» es exportar la tabla con el mismo filtro, y sin límite.
	Where []query.Condition `json:"where"`

	// Columns acota la lectura. Vacío lee la tabla entera, que es lo que quiere
	// la exportación. Con formato SQL, vacío significa «las insertables»: las
	// generadas se dejan afuera acá adentro, para que cualquier camino que
	// exporte a SQL —la grilla incluida— dé un archivo que se pueda volver a
	// correr (C-17 de la auditoría del 2026-09-11).
	Columns []string `json:"columns,omitempty"`

	// detalle es la estructura de la tabla, si quien llama ya la tiene: el
	// volcado la leyó una vez por tabla y no hay por qué leerla dos. Sin él,
	// y solo con formato SQL, volcar la pide.
	detalle *schema.TableDetail
}

// PreviewTable devuelve el texto de las primeras filas de una tabla.
//
// Lee con el mismo Scan que la exportación y corta apenas tiene las filas que
// necesita: así lo que se ve en la vista previa sale del mismo camino que lo
// que se va a escribir, y no de una lectura parecida.
func (e *Exports) PreviewTable(ctx context.Context, r TableExport, limit int) (string, error) {
	if limit <= 0 {
		return "", errors.New("la vista previa necesita un límite de filas")
	}
	var b strings.Builder
	opts := r.Options
	// El gzip se ignora al renderizar: el texto es para leerlo.
	opts.Gzip = false
	if _, err := e.volcar(ctx, r, opts, &b, limit); err != nil {
		return "", err
	}
	return b.String(), nil
}

// SaveTable escribe una tabla entera en path, leyéndola a medida que la escribe.
func (e *Exports) SaveTable(ctx context.Context, r TableExport, path string) (SaveInfo, error) {
	return e.guardar(path, func(w io.Writer) (int, error) {
		return e.volcar(ctx, r, r.Options, w, 0)
	})
}

// volcar lee la tabla y la escribe. Con limit mayor que cero corta ahí.
func (e *Exports) volcar(
	ctx context.Context, r TableExport, opts export.Options, w io.Writer, limit int,
) (int, error) {
	sesion, err := e.queries.session.abierta()
	if err != nil {
		return 0, err
	}
	ctx, listo := e.queries.registrar(ctx, r.RunID)
	defer listo()

	// Un archivo SQL tiene que poder volver a correrse, y eso depende de la
	// estructura: qué columnas se pueden insertar, y qué necesita el motor
	// alrededor de los INSERT (identity, secuencias). Se resuelve ACÁ, en el
	// único lugar por el que pasa toda exportación a SQL, y no en cada
	// llamador: el volcado lo hacía y la grilla no, y una tabla con una columna
	// generada daba desde la grilla un archivo que fallaba al correr.
	columnas := r.Columns
	var pistas engine.DumpHints
	if r.Format == export.SQL {
		det := r.detalle
		if det == nil {
			if det, err = sesion.db.Detail(ctx, r.Schema, r.Table); err != nil {
				return 0, fmt.Errorf("leer la estructura de %s: %w", nombreDeTabla(r.Schema, r.Table), err)
			}
		}
		if len(columnas) == 0 {
			columnas = insertables(det)
		}
		pistas = sesion.db.DumpHints(*det)
	}

	flujo, err := sesion.db.Scan(ctx, r.Schema, r.Table, engine.ScanOptions{
		OrderBy:    r.OrderBy,
		Descending: r.Descending,
		Where:      r.Where,
		Columns:    columnas,
		Limit:      limit,
	})
	if err != nil {
		return 0, fmt.Errorf("leer %s: %w", nombreDeTabla(r.Schema, r.Table), err)
	}
	defer flujo.Close()

	destino := destinoDe(sesion, r.Schema, r.Table)
	if destino != nil {
		destino.InsertModifier = pistas.InsertModifier
	}
	esc, err := export.NewInto(r.Format, w, opts, destino)
	if err != nil {
		return 0, err
	}
	n, err := volcarFlujo(flujo, esc, limit)
	if err != nil {
		return n, fmt.Errorf("exportar %s: %w", nombreDeTabla(r.Schema, r.Table), err)
	}
	for _, sentencia := range pistas.AfterData {
		if _, err := io.WriteString(w, sentencia+";\n"); err != nil {
			return n, fmt.Errorf("exportar %s: %w", nombreDeTabla(r.Schema, r.Table), err)
		}
	}
	return n, nil
}

// volcarFlujo pasa las filas del recorrido al escritor. Con limit mayor que
// cero corta ahí, sin que eso sea un error: es lo que hace la vista previa.
//
// Está separado de volcar para poder probar con un recorrido que falla A LA
// MITAD, que es el caso que ninguna prueba contra un motor real puede provocar
// cuando quiera. Lo que protege es concreto: si el servidor corta —se cae la
// conexión, salta el statement_timeout— lo que hay escrito no es la tabla, y
// terminar el archivo como si lo fuera daría un archivo cortado idéntico a uno
// entero.
func volcarFlujo(flujo engine.RowStream, esc export.Writer, limit int) (int, error) {
	if err := esc.Begin(flujo.Columns()); err != nil {
		return 0, err
	}
	// Un recorrido que sabe de qué clase es cada celda se lo dice a un
	// escritor que decide por celda (SQLite → SQL); los demás pares siguen
	// por Row.
	conClases, sabe := flujo.(engine.CellClasses)
	porCelda, entiende := esc.(export.ClassAware)
	n := 0
	for flujo.Next() {
		if limit > 0 && n >= limit {
			break
		}
		var err error
		if sabe && entiende {
			err = porCelda.RowClasses(flujo.Row(), conClases.CellClasses())
		} else {
			err = esc.Row(flujo.Row())
		}
		if err != nil {
			return n, err
		}
		n++
	}
	// El error del recorrido se mira ANTES de cerrar el escritor.
	if err := flujo.Err(); err != nil {
		return n, err
	}
	if err := esc.End(); err != nil {
		return n, err
	}
	return n, nil
}

// destinoDe arma lo que el formato SQL necesita: a qué tabla y cómo cita este
// motor. Los demás formatos lo ignoran.
func destinoDe(sesion *openSession, esquema, tabla string) *export.SQLTarget {
	q := sesion.db.Quoting()
	return &export.SQLTarget{
		Table:        q.Table(esquema, tabla),
		QuoteIdent:   q.Ident,
		QuoteLiteral: q.Literal,
		QuoteBinary:  q.Binary,
	}
}

func nombreDeTabla(esquema, tabla string) string {
	if esquema == "" {
		return tabla
	}
	return esquema + "." + tabla
}

/* ---------------------------------------------------------- varias tablas */

// TablesExport son varias tablas exportadas de una vez.
//
// El formato del CONJUNTO no es una decisión libre: depende del formato de cada
// tabla. Con SQL todo va a UN archivo, porque un volcado que se pueda volver a
// correr es un solo script; con los demás va un archivo POR TABLA en un
// directorio, porque un CSV con tres tablas adentro no lo lee nadie. Un zip se
// descartó: no se puede inspeccionar sin abrirlo y no ahorra nada que el disco
// no ahorre solo.
type TablesExport struct {
	RunID  string `json:"runId"`
	Schema string `json:"schema"`
	// Tables son los nombres, sin esquema. Vacío es un error: exportar «nada»
	// escribiría un directorio vacío sin decir por qué.
	Tables []string `json:"tables"`

	Format  export.Format  `json:"format"`
	Options export.Options `json:"options"`
}

// TablesInfo cuenta cómo quedó una exportación de varias tablas.
type TablesInfo struct {
	// Path es el archivo, o el directorio si fue uno por tabla.
	Path  string     `json:"path"`
	Files []SaveInfo `json:"files"`
	Rows  int        `json:"rows"`
	Bytes int64      `json:"bytes"`
}

// OneFile dice si el formato junta todas las tablas en un archivo.
func OneFile(f export.Format) bool { return f == export.SQL }

// SaveTables exporta varias tablas.
//
// `destino` es un archivo cuando el formato junta todo —SQL— y un directorio
// que ya existe cuando va una por tabla.
func (e *Exports) SaveTables(ctx context.Context, r TablesExport, destino string) (TablesInfo, error) {
	// Se registra UNA vez acá, y no solo adentro de cada `volcar`: cada tabla
	// registraba y borraba su propia entrada, así que un «Cancelar» que llegaba
	// entre la tabla N y la N+1 no encontraba nada que cortar y se perdía. Es
	// el mismo arreglo que ya tenía el volcado (C-21 de la auditoría del
	// 2026-09-11); los `volcar` de adentro ven la entrada y no se pisan.
	if len(r.Tables) == 0 {
		return TablesInfo{}, errors.New("no se eligió ninguna tabla")
	}
	if destino == "" {
		return TablesInfo{}, errors.New("falta dónde guardar")
	}
	ctx, listo := e.queries.registrar(ctx, r.RunID)
	defer listo()

	if OneFile(r.Format) {
		return e.aUnArchivo(ctx, r, destino)
	}
	return e.aUnDirectorio(ctx, r, destino)
}

// aUnArchivo escribe todas las tablas en un solo script.
//
// El archivo se arma entero en un temporal y recién al final toma el nombre
// elegido, igual que una tabla sola: si la tercera de cinco falla, no queda un
// script a medias que parece completo.
func (e *Exports) aUnArchivo(ctx context.Context, r TablesExport, destino string) (TablesInfo, error) {
	info := TablesInfo{Path: destino}
	total, err := e.guardar(destino, func(w io.Writer) (int, error) {
		// El gzip va UNA vez alrededor de todo el script, acá. Cada tabla se
		// escribe sin comprimir: comprimir tabla por tabla dejaría varios
		// miembros gzip pegados, que se descomprimen bien pero confunden a
		// cualquiera que mire el archivo.
		//
		// Estaba escrito como si lo pusiera `guardar`, y `guardar` no comprime:
		// el archivo salía con nombre .gz y contenido en claro, así que `gunzip`
		// se negaba a abrir una exportación que la app decía haber comprimido.
		cerrar := func() error { return nil }
		if r.Options.Gzip {
			gz := gzip.NewWriter(w)
			w = gz
			cerrar = gz.Close
		}
		suma := 0
		for _, t := range r.Tables {
			n, err := e.volcar(ctx, TableExport{
				RunID:   r.RunID,
				Schema:  r.Schema,
				Table:   t,
				Format:  r.Format,
				Options: sinComprimir(r.Options),
				OrderBy: nil,
			}, sinComprimir(r.Options), w, 0)
			if err != nil {
				return suma, err
			}
			info.Files = append(info.Files, SaveInfo{Path: destino, Rows: n})
			suma += n
		}
		if err := cerrar(); err != nil {
			return suma, fmt.Errorf("cerrar el gzip: %w", err)
		}
		return suma, nil
	})
	if err != nil {
		return TablesInfo{}, err
	}
	info.Rows = total.Rows
	info.Bytes = total.Bytes
	return info, nil
}

// sinComprimir apaga el gzip de cada tabla. Cuando todo va a un archivo lo
// pone `aUnArchivo`, una sola vez alrededor del script entero.
func sinComprimir(o export.Options) export.Options {
	o.Gzip = false
	return o
}

// aUnDirectorio escribe un archivo por tabla.
func (e *Exports) aUnDirectorio(ctx context.Context, r TablesExport, dir string) (TablesInfo, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return TablesInfo{}, fmt.Errorf("no se pudo usar la carpeta %s: %w", dir, err)
	}
	if !st.IsDir() {
		return TablesInfo{}, fmt.Errorf("%s no es una carpeta", dir)
	}
	ext := r.Format.Extension()
	if r.Options.Gzip {
		ext += ".gz"
	}

	// Los nombres se resuelven ANTES de escribir nada, porque dos tablas
	// distintas pueden dar el mismo archivo: `pedidos/2026` y `pedidos-2026` se
	// limpian igual. La segunda pisaba a la primera y las dos se informaban
	// como escritas — un archivo menos del que la app decía haber dejado, sin
	// error y sin aviso.
	nombres := nombresDeArchivo(r.Tables, ext)

	info := TablesInfo{Path: dir}
	for i, t := range r.Tables {
		ruta := filepath.Join(dir, nombres[i])
		uno, err := e.SaveTable(ctx, TableExport{
			RunID:   r.RunID,
			Schema:  r.Schema,
			Table:   t,
			Format:  r.Format,
			Options: r.Options,
		}, ruta)
		if err != nil {
			// Se corta en la primera que falla y se dice cuáles quedaron
			// escritas. Borrar las anteriores sería peor: son archivos enteros
			// y correctos, y quien exportó puede querer quedárselos.
			return info, fmt.Errorf("%w (quedaron escritas %d de %d)", err, len(info.Files), len(r.Tables))
		}
		info.Files = append(info.Files, uno)
		info.Rows += uno.Rows
		info.Bytes += uno.Bytes
	}
	return info, nil
}

// nombresDeArchivo da un archivo distinto a cada tabla, en el mismo orden.
//
// Limpiar el nombre pierde información —`/` y `-` terminan los dos en `-`— así
// que dos tablas legales pueden querer el mismo archivo. Cuando pasa, la
// segunda lleva un sufijo: es feo y es raro, pero es mejor que perder una
// exportación en silencio.
//
// Volver a exportar a la misma carpeta SÍ reemplaza lo que había: es lo que se
// espera de «guardar acá otra vez», y es la misma semántica que el selector de
// «guardar como» de una tabla sola.
func nombresDeArchivo(tablas []string, ext string) []string {
	out := make([]string, len(tablas))
	usados := make(map[string]bool, len(tablas))
	for i, t := range tablas {
		base := archivoDeTabla(t)
		nombre := base + ext
		for n := 2; usados[strings.ToLower(nombre)]; n++ {
			nombre = fmt.Sprintf("%s-%d%s", base, n, ext)
		}
		usados[strings.ToLower(nombre)] = true
		out[i] = nombre
	}
	return out
}

// archivoDeTabla saca de un nombre de tabla lo que ningún sistema de archivos
// acepta. Una tabla puede llamarse `pedidos/2026` — es raro pero es legal.
func archivoDeTabla(tabla string) string {
	limpio := make([]rune, 0, len(tabla))
	for _, r := range tabla {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			limpio = append(limpio, '-')
		default:
			limpio = append(limpio, r)
		}
	}
	if len(limpio) == 0 {
		return "tabla"
	}
	nombre := string(limpio)
	// Windows se niega a crear un archivo llamado como uno de sus dispositivos,
	// con extensión o sin ella. Son nombres de tabla perfectamente legales
	// —`con` es corriente en castellano— y la máquina de desarrollo de este
	// proyecto es Windows: sin esto, exportar un esquema que tenga una tabla
	// así falla a la mitad, con las anteriores ya escritas.
	if reservadoEnWindows[strings.ToLower(nombre)] {
		nombre = "tabla-" + nombre
	}
	return nombre
}

// reservadoEnWindows son los nombres de dispositivo que el sistema no deja usar
// como nombre de archivo.
var reservadoEnWindows = func() map[string]bool {
	m := map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}
	for i := 1; i <= 9; i++ {
		m[fmt.Sprintf("com%d", i)] = true
		m[fmt.Sprintf("lpt%d", i)] = true
	}
	return m
}()
