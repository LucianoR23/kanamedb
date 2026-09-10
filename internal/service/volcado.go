package service

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/LucianoR23/kanamedb/internal/appinfo"
	"github.com/LucianoR23/kanamedb/internal/dump"
	"github.com/LucianoR23/kanamedb/internal/export"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Dumps escribe el volcado de una base: la estructura, los datos, o los dos.
//
// Es la mitad de lo que la gente llama «backup», y la mitad que se puede hacer
// bien. La palabra no se usa para lo que genera Kaname: un backup es una copia
// de la que se restaura TODO, y esto no lo es. Lo que sí hace —y lo que ningún
// export de esquema suele hacer— es DECIR qué deja afuera, con nombre y
// apellido. Ver `dump.Cobertura`.
type Dumps struct {
	exports *Exports
	queries *Queries
}

func NewDumps(e *Exports, q *Queries) *Dumps { return &Dumps{exports: e, queries: q} }

// DumpRequest es un volcado pedido.
type DumpRequest struct {
	RunID string `json:"runId"`

	// Schemas son los esquemas que entran. Vacío significa «el de la conexión»,
	// que en MySQL es la base abierta y en SQLite es `main`.
	Schemas []string `json:"schemas"`

	// Structure y Data dicen qué va en el archivo. Los dos en false no escribe
	// nada y se rechaza: un volcado vacío es un error de quien lo pidió, no un
	// archivo válido.
	Structure bool `json:"structure"`
	Data      bool `json:"data"`

	// DropFirst antepone un DROP TABLE IF EXISTS a cada CREATE.
	//
	// Apagado por defecto, y a propósito: es la única opción de este diálogo
	// que puede DESTRUIR datos al correr el archivo, y un default que borra es
	// un default que muerde.
	DropFirst bool `json:"dropFirst"`
}

// DumpInfo es cómo quedó.
type DumpInfo struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`

	// Tables y Rows son cuántas tablas y filas se escribieron de verdad.
	Tables int `json:"tables"`
	Rows   int `json:"rows"`

	ElapsedMs int64 `json:"elapsedMs"`
}

// DumpPreview es lo que la pantalla muestra ANTES de escribir nada.
//
// Existe para que la cobertura se pueda ver antes de decidir: enterarse de que
// quedan tres funciones afuera al abrir el archivo es tarde.
type DumpPreview struct {
	// Tables son las tablas que van a entrar, YA en el orden del archivo.
	Tables []dump.Ref `json:"tables"`

	// Cycles son los grupos de tablas que se apuntan entre sí.
	Cycles [][]dump.Ref `json:"cycles"`

	// Uncovered es lo que queda afuera, y Summary/Detail su texto ya armado.
	Uncovered []schema.Object `json:"uncovered"`
	Summary   string          `json:"summary"`
	Detail    []string        `json:"detail"`

	// Header es el encabezado tal como va a salir en el archivo. La pantalla
	// lo muestra: es más honesto enseñar lo que se va a escribir que describirlo.
	Header string `json:"header"`

	// ReadOnly no impide volcar —leer no escribe— pero se informa igual.
	Engine string `json:"engine"`
}

// Preview arma el plan sin tocar el disco.
func (d *Dumps) Preview(ctx context.Context, r DumpRequest) (DumpPreview, error) {
	sesion, err := d.queries.session.abierta()
	if err != nil {
		return DumpPreview{}, err
	}
	plan, err := d.planear(ctx, sesion, r)
	if err != nil {
		return DumpPreview{}, err
	}
	var b strings.Builder
	if err := dump.Encabezado(&b, plan.info); err != nil {
		return DumpPreview{}, err
	}
	return DumpPreview{
		Tables:    plan.tablas,
		Cycles:    plan.info.Ciclos,
		Uncovered: plan.info.Cobertura.Fuera,
		Summary:   plan.info.Cobertura.Resumen(),
		Detail:    plan.info.Cobertura.Detalle(),
		Header:    b.String(),
		Engine:    sesion.db.Kind().Label(),
	}, nil
}

// Save escribe el volcado.
//
// Va al archivo a medida que se lee, igual que la exportación: una base de dos
// millones de filas no pasa por la memoria. Y se escribe en un temporal que
// solo toma el nombre elegido al terminar, porque un volcado a medias con el
// nombre correcto es peor que ninguno — se ve igual que uno entero.
func (d *Dumps) Save(ctx context.Context, r DumpRequest, path string) (DumpInfo, error) {
	arranque := time.Now()
	sesion, err := d.queries.session.abierta()
	if err != nil {
		return DumpInfo{}, err
	}
	plan, err := d.planear(ctx, sesion, r)
	if err != nil {
		return DumpInfo{}, err
	}

	ctx, listo := d.queries.registrar(ctx, r.RunID)
	defer listo()

	info := DumpInfo{Tables: len(plan.tablas)}
	guardado, err := d.exports.guardar(path, func(w io.Writer) (int, error) {
		if err := dump.Encabezado(w, plan.info); err != nil {
			return 0, err
		}
		if r.Structure {
			if err := d.estructura(ctx, sesion, plan, w, r.DropFirst); err != nil {
				return 0, err
			}
		}
		if r.Data {
			n, err := d.datos(ctx, sesion, plan, w, r.RunID)
			if err != nil {
				return n, err
			}
			return n, nil
		}
		return 0, nil
	})
	if err != nil {
		return DumpInfo{}, err
	}
	info.Path = guardado.Path
	info.Bytes = guardado.Bytes
	info.Rows = guardado.Rows
	info.ElapsedMs = time.Since(arranque).Milliseconds()
	return info, nil
}

// plan es lo resuelto antes de escribir: qué tablas, en qué orden, y qué queda
// afuera.
type plan struct {
	tablas []dump.Ref
	info   dump.Info
}

func (d *Dumps) planear(ctx context.Context, sesion *openSession, r DumpRequest) (plan, error) {
	if !r.Structure && !r.Data {
		return plan{}, fmt.Errorf("el volcado no lleva ni estructura ni datos: no escribiría nada")
	}
	snap, err := sesion.db.Introspect(ctx)
	if err != nil {
		return plan{}, fmt.Errorf("leer el esquema: %w", err)
	}

	quiere := map[string]bool{}
	for _, e := range r.Schemas {
		quiere[e] = true
	}

	var tablas []dump.Ref
	var aristas []dump.Arista
	var esquemas []string
	var sinLeer []string
	for _, esq := range snap.Schemas {
		if len(quiere) > 0 && !quiere[esq.Name] {
			continue
		}
		esquemas = append(esquemas, esq.Name)
		for _, t := range esq.Tables {
			de := dump.Ref{Schema: esq.Name, Table: t.Name}
			// Una tabla que se ve en el catálogo pero no se puede leer rompe
			// el volcado de DATOS, y rompe tarde: a mitad de archivo. Se avisa
			// antes de escribir nada y con los nombres, que es lo que deja
			// arreglarlo —pedir el permiso, o sacar el esquema del volcado—.
			if r.Data && !t.Readable {
				sinLeer = append(sinLeer, de.Completo())
				continue
			}
			tablas = append(tablas, de)
			for _, fk := range t.ForeignKeys {
				aristas = append(aristas, dump.Arista{
					De: de,
					A:  dump.Ref{Schema: fk.RefSchema, Table: fk.RefTable},
				})
			}
		}
	}
	if len(sinLeer) > 0 {
		return plan{}, fmt.Errorf(
			"esta conexión no puede leer %s, así que el volcado de datos quedaría incompleto; "+
				"pedí permiso de lectura o volcá solo la estructura",
			strings.Join(sinLeer, ", "))
	}

	orden, ciclos := dump.Orden(tablas, aristas)

	// La cobertura solo hace falta cuando se escribe estructura: un volcado de
	// datos no promete tener las vistas.
	var fuera []schema.Object
	if r.Structure {
		fuera, err = sesion.db.Uncovered(ctx, esquemas)
		if err != nil {
			// No se traga: quedarse callado acá produce exactamente el archivo
			// silencioso que la cobertura existe para evitar.
			return plan{}, fmt.Errorf("averiguar qué queda afuera del volcado: %w", err)
		}
	}

	return plan{
		tablas: orden,
		info: dump.Info{
			Origen:     sesion.conn.Describe(),
			Motor:      motorDe(sesion),
			Base:       nombreDeLaBase(sesion),
			Esquemas:   esquemas,
			Cuando:     time.Now(),
			Version:    appinfo.Version,
			Estructura: r.Structure,
			Datos:      r.Data,
			Cobertura:  dump.Cobertura{Fuera: fuera},
			Ciclos:     ciclos,
		},
	}, nil
}

// estructura escribe el DDL de cada tabla.
//
// El DDL no se arma acá: se arma un `change.Change` desde lo que la
// introspección leyó y se lo renderiza con `RenderDDL`, que es EL MISMO
// renderizador que usa el changeset. Escribir un segundo generador de DDL sería
// tener dos verdades sobre cómo se escribe una columna en cada motor, y la
// segunda se atrasaría sin que nadie lo note.
func (d *Dumps) estructura(
	ctx context.Context, sesion *openSession, p plan, w io.Writer, dropFirst bool,
) error {
	if err := dump.Seccion(w, "Estructura"); err != nil {
		return err
	}
	q := sesion.db.Quoting()
	for _, ref := range p.tablas {
		detalle, err := sesion.db.Detail(ctx, ref.Schema, ref.Table)
		if err != nil {
			return fmt.Errorf("leer la estructura de %s: %w", ref.Completo(), err)
		}
		if dropFirst {
			if _, err := fmt.Fprintf(w, "DROP TABLE IF EXISTS %s;\n", q.Table(ref.Schema, ref.Table)); err != nil {
				return err
			}
		}
		sentencias, err := dump.DDLDeTabla(ctx, sesion.db, *detalle)
		if err != nil {
			return fmt.Errorf("escribir la estructura de %s: %w", ref.Completo(), err)
		}
		for _, s := range sentencias {
			if _, err := fmt.Fprintf(w, "%s;\n", s); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}

	// Las claves foráneas, después de TODAS las tablas.
	//
	// No es prolijidad: una clave apunta a otra tabla, y pegada a su CREATE
	// haría fallar el archivo cuando la tabla destino todavía no existe. Al
	// final deja de importar incluso el ciclo, que es lo único que el orden de
	// inserción no puede resolver.
	var claves []string
	for _, ref := range p.tablas {
		detalle, err := sesion.db.Detail(ctx, ref.Schema, ref.Table)
		if err != nil {
			return fmt.Errorf("leer las claves de %s: %w", ref.Completo(), err)
		}
		fks, err := dump.ClavesForaneas(ctx, sesion.db, *detalle)
		if err != nil {
			return fmt.Errorf("escribir las claves de %s: %w", ref.Completo(), err)
		}
		claves = append(claves, fks...)
	}
	if len(claves) == 0 {
		return nil
	}
	if err := dump.Seccion(w, "Claves foráneas"); err != nil {
		return err
	}
	for _, s := range claves {
		if _, err := fmt.Fprintf(w, "%s;\n", s); err != nil {
			return err
		}
	}
	return nil
}

// datos escribe los INSERTs de cada tabla, en el orden de dependencias.
func (d *Dumps) datos(
	ctx context.Context, sesion *openSession, p plan, w io.Writer, runID string,
) (int, error) {
	if err := dump.Seccion(w, "Datos"); err != nil {
		return 0, err
	}
	total := 0
	for _, ref := range p.tablas {
		if _, err := fmt.Fprintf(w, "-- %s\n", ref.Completo()); err != nil {
			return total, err
		}
		n, err := d.exports.volcar(ctx, TableExport{
			RunID:  runID,
			Schema: ref.Schema,
			Table:  ref.Table,
			Format: export.SQL,
		}, export.Options{}, w, 0)
		if err != nil {
			return total, err
		}
		total += n
		if _, err := io.WriteString(w, "\n"); err != nil {
			return total, err
		}
	}
	return total, nil
}

// motorDe es «PostgreSQL 18.3», para el encabezado.
func motorDe(sesion *openSession) string {
	if sesion.server != nil && sesion.server.Version != "" {
		return sesion.server.Version
	}
	return sesion.db.Kind().Label()
}
