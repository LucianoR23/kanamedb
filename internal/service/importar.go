package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LucianoR23/kanamedb/internal/csvimport"
	"github.com/LucianoR23/kanamedb/internal/engine"
)

// Imports mete un archivo CSV en una tabla.
//
// Es el otro lado de Exports, y comparte con él la forma de trabajar: el
// archivo se lee de a una fila y no se junta en memoria, y los valores viajan
// SIEMPRE como parámetros. La conversión al tipo de la columna la hace el
// servidor: interpretar acá una fecha o un número sería inventar una regla que
// no es la del motor.
type Imports struct {
	queries *Queries
}

func NewImports(q *Queries) *Imports { return &Imports{queries: q} }

// Inspect mira el archivo sin tocar la base.
func (i *Imports) Inspect(path string, o csvimport.Options) (*csvimport.Inspection, error) {
	return csvimport.Inspect(path, o)
}

// OnConflict es qué hacer con una fila que choca con una que ya está.
type OnConflict string

const (
	// ConflictFail aborta la importación entera. Es el default porque es el
	// único que no pierde información en silencio: si hay choques, quien
	// importa se entera y decide.
	ConflictFail OnConflict = "fail"
	// ConflictSkip deja la fila que ya estaba y sigue. Es `ON CONFLICT DO
	// NOTHING` en Postgres y SQLite, e `INSERT IGNORE` en MySQL.
	ConflictSkip OnConflict = "skip"
)

// ImportPlan es una importación lista para correr.
type ImportPlan struct {
	RunID  string `json:"runId"`
	Path   string `json:"path"`
	Schema string `json:"schema"`
	Table  string `json:"table"`

	Options csvimport.Options `json:"options"`

	// Mapping dice a qué columna de la TABLA va cada columna del archivo, en
	// orden. La cadena vacía saltea esa columna del archivo.
	//
	// Es una lista y no un mapa porque el orden importa: lo que identifica a
	// una columna del CSV es su posición, y dos columnas del archivo se pueden
	// llamar igual.
	Mapping []string `json:"mapping"`

	OnConflict OnConflict `json:"onConflict"`

	// Confirm es el nombre de LA BASE escrito a mano, igual que en Apply.
	//
	// Importar es escribir, y contra producción escribir exige tipear. Vale
	// también para el ENSAYO, y no por simetría: el ensayo inserta de verdad
	// antes de revertir, así que toma los mismos candados de la tabla y
	// consume los valores de las secuencias —que un ROLLBACK no devuelve—.
	Confirm string `json:"confirm,omitempty"`
}

// ImportTarget es lo que el asistente necesita saber ANTES de dejar importar.
//
// Va en un pedido propio y no se deduce del estado de la conexión que ya tiene
// el frontend, porque la regla vive en Go: quien decide si hace falta escribir
// el nombre de la base es el mismo código que después lo exige.
type ImportTarget struct {
	// ReadOnly dice que esta sesión no escribe, y Reason por cuál de las tres
	// razones: elegida, réplica, o servidor en solo lectura.
	ReadOnly bool   `json:"readOnly"`
	Reason   string `json:"reason,omitempty"`

	// NeedsConfirmation avisa que hay que escribir ConfirmWord para importar.
	NeedsConfirmation bool   `json:"needsConfirmation"`
	ConfirmWord       string `json:"confirmWord,omitempty"`
}

// Target dice si se puede importar en esta conexión y con qué candados.
func (i *Imports) Target() (ImportTarget, error) {
	sesion, err := i.queries.session.abierta()
	if err != nil {
		return ImportTarget{}, err
	}
	t := ImportTarget{NeedsConfirmation: sesion.conn.Environment.NeedsWriteConfirmation()}
	t.ReadOnly, t.Reason = soloLectura(sesion)
	if t.NeedsConfirmation {
		t.ConfirmWord = nombreDeLaBase(sesion)
	}
	return t, nil
}

// ImportResult es cómo terminó.
type ImportResult struct {
	OK bool `json:"ok"`
	// Inserted es cuántas filas quedaron. Con «saltear», las que chocaron no
	// se cuentan acá.
	Inserted int64 `json:"inserted"`
	// Read es cuántas filas de datos tenía el archivo.
	Read      int   `json:"read"`
	ElapsedMs int64 `json:"elapsedMs"`

	// Failure es el fallo, si lo hubo. Nada quedó escrito: la importación va en
	// UNA transacción.
	Failure *engine.Failure `json:"failure,omitempty"`
	// Line es la línea del archivo donde estaba el problema, si se sabe.
	Line int `json:"line,omitempty"`

	// RolledBack es la afirmación del ensayo —«no quedó nada»— y NO se pone por
	// adelantado: la pone el ROLLBACK cuando devuelve bien.
	//
	// La diferencia importa porque el ensayo INSERTA de verdad. Si el rollback
	// falla —la conexión se cayó entre el último lote y el final— las filas
	// pueden haber quedado, y decir «se revirtió» ahí sería exactamente la
	// clase de afirmación que este proyecto no da sin comprobar.
	RolledBack bool `json:"rolledBack,omitempty"`
}

// filasPorLote es el tope de filas de cada INSERT.
//
// Mil, como dice el diseño: una por fila hace un viaje al servidor por fila, y
// todas juntas se pasa del tamaño máximo de paquete.
const filasPorLote = 1000

// maxParametros es cuántos marcadores admite UNA sentencia.
//
// 65535 porque el protocolo extendido de Postgres cuenta los parámetros en 16
// bits; el protocolo binario de MySQL tiene el mismo tope. No es un detalle de
// un motor: es el mismo número en los dos.
const maxParametros = 65535

// filasDelLote decide cuántas filas entran en un INSERT de `n` columnas.
//
// «Mil deja margen para tablas anchas» era exactamente al revés: el límite de
// parámetros es 65535 POR SENTENCIA, así que a más columnas entran MENOS filas,
// no más. Con 66 columnas mapeadas, mil filas son 66.000 marcadores y el
// servidor rechaza el primer lote con un error crudo del driver — una tabla
// ancha no se podía importar y el motivo no se leía en ninguna parte.
func filasDelLote(n int) int {
	if n <= 0 {
		return filasPorLote
	}
	if cabe := maxParametros / n; cabe < filasPorLote {
		// Al menos una: una tabla de más de 65535 columnas no existe en ningún
		// motor, pero devolver cero sería un bucle infinito.
		if cabe < 1 {
			return 1
		}
		return cabe
	}
	return filasPorLote
}

// Run corre la importación entera adentro de UNA transacción.
//
// Todo o nada: si un lote falla, no queda ninguna fila. Es lo que el diálogo
// promete y lo que hace que se pueda intentar de nuevo sin mirar qué entró.
func (i *Imports) Run(ctx context.Context, p ImportPlan) ImportResult {
	return i.correr(ctx, p, false)
}

// DryRun hace exactamente lo mismo y revierte al final.
//
// Es el paso de validación del asistente, y responde lo que ninguna
// comprobación nuestra podría: si el SERVIDOR acepta estas filas. Comprobarlo
// del lado de Kaname —«esto parece una fecha»— sería inventar las reglas del
// motor y equivocarse justo en los casos raros.
func (i *Imports) DryRun(ctx context.Context, p ImportPlan) ImportResult {
	return i.correr(ctx, p, true)
}

func (i *Imports) correr(ctx context.Context, p ImportPlan, ensayo bool) ImportResult {
	arranque := time.Now()
	sesion, err := i.queries.session.abierta()
	if err != nil {
		return ImportResult{Failure: &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}}
	}
	// Las TRES razones, no solo el interruptor: apuntar a una réplica también
	// impide escribir, y el error del driver no lo explica.
	if ro, motivo := soloLectura(sesion); ro {
		return ImportResult{Failure: &engine.Failure{
			Kind:    engine.FailureOther,
			Message: "Esta conexión no escribe: " + motivo,
			Hint:    "El interruptor está en el gestor de conexiones, en «Abrir en solo lectura».",
		}}
	}
	// La confirmación se verifica ACÁ y no en el asistente. Una comprobación
	// que vive solo del lado de la interfaz no es una protección: es un cartel.
	// Vale para el ensayo también: inserta de verdad antes de revertir.
	if sesion.conn.Environment.NeedsWriteConfirmation() &&
		strings.TrimSpace(p.Confirm) != nombreDeLaBase(sesion) {
		return ImportResult{Failure: &engine.Failure{
			Kind: engine.FailureOther,
			Message: fmt.Sprintf(
				"Esta conexión es de producción: para importar hay que escribir %q.",
				nombreDeLaBase(sesion)),
			Hint: "El ensayo también lo pide: inserta las filas de verdad antes de revertirlas.",
		}}
	}

	destino, err := columnasDestino(p.Mapping)
	if err != nil {
		return ImportResult{Failure: &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}}
	}

	ctx, listo := i.queries.registrar(ctx, p.RunID)
	defer listo()

	tx, err := sesion.db.Begin(ctx, engine.TxOptions{})
	if err != nil {
		return ImportResult{Failure: sesion.db.ClassifyStatement(err, "abrir la transacción de la importación")}
	}
	// Un ensayo SIEMPRE revierte, haya salido bien o mal. El defer es la red
	// para los caminos que se van por un error; el ensayo que llega al final lo
	// hace explícito, porque necesita SABER si el rollback anduvo.
	//
	// `WithoutCancel` es para que revertir funcione aunque lo que cortó la
	// importación haya sido la cancelación: con el contexto muerto, el ROLLBACK
	// no llegaría a salir y la transacción quedaría colgada hasta el timeout.
	cerrada := false
	defer func() {
		if !cerrada {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	res := ImportResult{}
	porLote := filasDelLote(len(destino))
	lote := make([][]*string, 0, porLote)
	primeraDelLote := 0

	descargar := func() error {
		if len(lote) == 0 {
			return nil
		}
		sql, args := sesion.db.InsertBatch(p.Schema, p.Table, destino, lote, p.OnConflict == ConflictSkip)
		n, err := tx.Modify(ctx, sql, args)
		if err != nil {
			return &erroDeLinea{linea: primeraDelLote, err: err}
		}
		res.Inserted += n
		lote = lote[:0]
		return nil
	}

	err = csvimport.Rows(p.Path, p.Options, func(linea int, fila []*string) error {
		res.Read++
		valores, err := elegir(fila, p.Mapping, len(destino))
		if err != nil {
			return &erroDeLinea{linea: linea, err: err}
		}
		if len(lote) == 0 {
			primeraDelLote = linea
		}
		lote = append(lote, valores)
		if len(lote) >= porLote {
			return descargar()
		}
		return nil
	})
	if err == nil {
		err = descargar()
	}
	if err != nil {
		res.ElapsedMs = time.Since(arranque).Milliseconds()
		var el *erroDeLinea
		if errors.As(err, &el) {
			res.Line = el.linea
			res.Failure = fallaDeImportacion(sesion, el.err, el.linea, porLote)
		} else {
			res.Failure = &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}
		}
		return res
	}

	if ensayo {
		// El ensayo revierte acá, mirando el error: es la única forma de que
		// «no quedó nada» sea algo comprobado y no una suposición.
		if err := tx.Rollback(context.WithoutCancel(ctx)); err != nil {
			cerrada = true
			res.ElapsedMs = time.Since(arranque).Milliseconds()
			res.Failure = sesion.db.ClassifyStatement(err, "revertir el ensayo")
			res.Failure.Hint = strings.TrimSpace(res.Failure.Hint +
				" Las filas del ensayo pueden haber quedado: revisá la tabla antes de importar de nuevo.")
			return res
		}
		cerrada = true
		res.RolledBack = true
	} else {
		if err := tx.Commit(ctx); err != nil {
			res.ElapsedMs = time.Since(arranque).Milliseconds()
			res.Failure = sesion.db.ClassifyStatement(err, "confirmar la importación")
			return res
		}
		cerrada = true
	}
	res.OK = true
	res.ElapsedMs = time.Since(arranque).Milliseconds()
	return res
}

// erroDeLinea lleva en qué línea del archivo pasó algo.
//
// Es lo que convierte «falló la importación» en «falló la línea 118», que es la
// diferencia entre poder arreglar el archivo y tener que adivinar.
type erroDeLinea struct {
	linea int
	err   error
}

func (e *erroDeLinea) Error() string { return e.err.Error() }
func (e *erroDeLinea) Unwrap() error { return e.err }

// fallaDeImportacion clasifica el error del motor y le agrega la línea.
//
// La línea del lote es la PRIMERA del lote, no la culpable: un INSERT de varias
// filas que falla no dice cuál fue. Se dice así, en vez de señalar una fila que
// puede no ser la que rompió.
//
// `porLote` es el tamaño REAL del lote y no la constante: en una tabla ancha el
// lote es más chico, y prometer «las 1000 siguientes» mandaría a buscar el
// error mucho más lejos de donde puede estar.
func fallaDeImportacion(sesion *openSession, err error, linea, porLote int) *engine.Failure {
	f := sesion.db.ClassifyStatement(err, "la importación")
	if f == nil {
		f = &engine.Failure{Kind: engine.FailureOther, Message: err.Error()}
	}
	if linea > 0 {
		f.Hint = strings.TrimSpace(f.Hint + fmt.Sprintf(
			" El lote que falló empieza en la línea %d del archivo; el error puede estar en cualquiera de las %d siguientes.",
			linea, porLote))
	}
	return f
}

// columnasDestino saca del mapeo las columnas de la tabla, en orden.
func columnasDestino(mapping []string) ([]string, error) {
	var out []string
	vistas := map[string]bool{}
	for _, c := range mapping {
		if c == "" {
			continue
		}
		if vistas[c] {
			return nil, fmt.Errorf("la columna %q está elegida dos veces: cada columna de la tabla puede recibir una sola del archivo", c)
		}
		vistas[c] = true
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errors.New("no se eligió ninguna columna: la importación no escribiría nada")
	}
	return out, nil
}

// elegir se queda con los campos que van a alguna columna.
func elegir(fila []*string, mapping []string, n int) ([]*string, error) {
	out := make([]*string, 0, n)
	for i, c := range mapping {
		if c == "" {
			continue
		}
		if i >= len(fila) {
			return nil, fmt.Errorf("la línea tiene %d campos y el mapeo espera al menos %d", len(fila), i+1)
		}
		out = append(out, fila[i])
	}
	return out, nil
}
