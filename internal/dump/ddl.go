package dump

import (
	"context"
	"fmt"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Renderizador es lo único que `DDLDeTabla` necesita de un motor.
//
// Es la interfaz de `engine.Conn` recortada a un método para que este paquete
// no dependa de `engine` —que depende de `schema`, de `query` y de `change`— y
// para que el test pueda darle un renderizador de mentira.
type Renderizador interface {
	RenderDDL(ctx context.Context, c change.Change) (change.Statement, error)
	// AutoIncrement dice cómo escribe este motor una columna que se numera
	// sola, y si puede.
	AutoIncrement(col schema.DetailColumn) (tipo string, puede bool)
}

// DDLDeTabla escribe la tabla tal como está, en sentencias.
//
// El DDL NO se arma acá: se arman `change.Change` desde lo que leyó la
// introspección y los renderiza el motor, con EL MISMO `RenderDDL` que usa el
// changeset. Escribir un segundo generador sería tener dos verdades sobre cómo
// se escribe una columna en cada motor, y la segunda se atrasaría sin que nadie
// lo note — que es exactamente el defecto por el que se rechazó Atlas.
//
// Tiene una consecuencia que conviene ver de frente: **lo que el changeset no
// sabe expresar, esto no lo escribe**. Por eso `Cobertura` no es opcional. La
// lista de acá y la de `Objects` son las dos mitades de la misma frase.
//
// El orden es el que hace que el archivo se pueda correr de arriba abajo:
// primero la tabla con sus columnas, después lo que se le cuelga. Las claves
// foráneas NO van acá —van al final de todo el volcado, con `ClavesForaneas`—
// porque apuntan a tablas que pueden no existir todavía.
func DDLDeTabla(ctx context.Context, r Renderizador, d schema.TableDetail) ([]string, []schema.Object, error) {
	var out []string
	// Lo que esta función NO pudo escribir. Se devuelve en vez de comentarse,
	// porque un comentario que dice «la cobertura la nombra» no la nombra: hay
	// que devolverla para que alguien la ponga ahí.
	var fuera []schema.Object
	agregar := func(c change.Change) error {
		st, err := r.RenderDDL(ctx, c)
		if err != nil {
			return err
		}
		if st.SQL != "" {
			out = append(out, st.SQL)
		}
		return nil
	}

	columnas := make([]change.Column, 0, len(d.Columns))
	for _, c := range d.Columns {
		// Una columna generada NO se escribe como columna común: su valor lo
		// calcula el motor y el catálogo no nos da la expresión. Se saltea, y
		// se nombra.
		if c.Generated != "" {
			fuera = append(fuera, columnaFuera(d, c))
			continue
		}
		col := change.Column{
			Name:     c.Name,
			DataType: c.DataType,
			Nullable: c.Nullable,
			Default:  c.Default,
			Comment:  c.Comment,
		}
		// Una columna que se numera sola no se escribe como una común: el
		// `nextval(…)` de un serial apunta a una secuencia que este archivo no
		// crea, así que copiarlo tal cual daba un volcado que NO se puede
		// correr. Cada motor dice cómo se escribe, o que no puede.
		if seNumeraSola(c) {
			tipo, puede := r.AutoIncrement(c)
			if !puede {
				fuera = append(fuera, columnaFuera(d, c))
				col.Default = ""
			} else {
				col.DataType = tipo
				col.Default = ""
			}
		}
		columnas = append(columnas, col)
	}
	if len(columnas) == 0 {
		return nil, fuera, fmt.Errorf(
			"la tabla %s.%s no tiene ninguna columna que se pueda escribir", d.Schema, d.Name)
	}

	// La clave primaria va ADENTRO del CREATE TABLE —`Names`—, no en un ALTER
	// aparte. No es prolijidad: SQLite no tiene `ALTER TABLE … ADD PRIMARY
	// KEY`, así que la forma separada deja al volcado sin clave en uno de los
	// cuatro motores. Adentro funciona en los cuatro y es una sentencia menos.
	if err := agregar(change.Change{
		Type: change.CreateTable, Schema: d.Schema, Table: d.Name,
		Columns: columnas, Names: clavePrimaria(d), Source: "dump",
	}); err != nil {
		return nil, fuera, err
	}

	for _, ix := range d.Indexes {
		// El índice que hace cumplir la clave primaria ya está: crearlo de
		// nuevo daría un error al correr el archivo.
		if ix.Primary {
			continue
		}
		tipo := change.AddIndex
		if ix.Unique {
			tipo = change.AddUnique
		}
		if err := agregar(change.Change{
			Type: tipo, Schema: d.Schema, Table: d.Name,
			Name: ix.Name, Names: ix.Columns, Source: "dump",
		}); err != nil {
			return nil, fuera, err
		}
	}

	for _, ck := range d.Checks {
		if err := agregar(change.Change{
			Type: change.AddCheck, Schema: d.Schema, Table: d.Name,
			Name: ck.Name, Expression: ck.Expression, Source: "dump",
		}); err != nil {
			return nil, fuera, err
		}
	}

	if d.Comment != "" {
		if err := agregar(change.Change{
			Type: change.SetTableComment, Schema: d.Schema, Table: d.Name,
			Comment: d.Comment, Source: "dump",
		}); err != nil {
			return nil, fuera, err
		}
	}
	return out, fuera, nil
}

// seNumeraSola dice si la columna toma su valor de un contador del motor.
//
// Las dos formas: `Identity` puesto —que es como llegan la identity de
// Postgres, el AUTO_INCREMENT de MySQL y MariaDB y el AUTOINCREMENT de
// SQLite— y el default `nextval(` de un `serial` de Postgres.
func seNumeraSola(c schema.DetailColumn) bool {
	return c.Identity != "" || strings.HasPrefix(c.Default, "nextval(")
}

// columnaFuera nombra una columna que el volcado no pudo escribir como era.
func columnaFuera(d schema.TableDetail, c schema.DetailColumn) schema.Object {
	return schema.Object{
		Kind: schema.ObjColumn, Schema: d.Schema, Name: d.Name + "." + c.Name,
	}
}

// ClavesForaneas escribe las claves de una tabla, para el final del volcado.
//
// Van separadas de la tabla y no pegadas a ella por una razón que no es de
// estilo: una clave apunta a otra tabla, y si esa tabla todavía no existe el
// archivo falla al correrse. Al final, todas las tablas ya están — y encima
// deja de importar el ciclo, que es lo único que el orden de inserción no
// puede resolver.
func ClavesForaneas(ctx context.Context, r Renderizador, d schema.TableDetail) ([]string, error) {
	var out []string
	for _, fk := range d.ForeignKeys {
		st, err := r.RenderDDL(ctx, change.Change{
			Type: change.AddForeignKey, Schema: d.Schema, Table: d.Name,
			Name: fk.Name, Names: fk.Columns,
			RefSchema: fk.RefSchema, RefTable: fk.RefTable, RefNames: fk.RefColumns,
			OnDelete: string(fk.OnDelete), OnUpdate: string(fk.OnUpdate),
			Source: "dump",
		})
		if err != nil {
			return nil, err
		}
		if st.SQL != "" {
			out = append(out, st.SQL)
		}
	}
	return out, nil
}

// clavePrimaria saca las columnas de la PK del índice que la implementa.
func clavePrimaria(d schema.TableDetail) []string {
	for _, ix := range d.Indexes {
		if ix.Primary {
			return ix.Columns
		}
	}
	// Sin índice marcado como primario, se cae a las columnas: algunos motores
	// no marcan el índice pero sí la columna.
	var out []string
	for _, c := range d.Columns {
		if c.PrimaryKey {
			out = append(out, c.Name)
		}
	}
	return out
}
