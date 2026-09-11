// Package drift compara dos esquemas y dice en qué se diferencian.
//
// Es el núcleo de S20: elegir dos conexiones —típicamente dev y producción—,
// ver qué tiene una y la otra no, y obtener las sentencias que alinearían al
// destino. Compara CATÁLOGOS, nunca datos: no lee una sola fila de ninguna de
// las dos bases.
//
// # Cuatro reglas, y las cuatro son de seguridad
//
// **La comparación tiene dirección.** Origen → destino. No es simétrica, y eso
// no es un detalle de implementación: lo que existe solo en el origen se crea
// en el destino, y lo que existe solo en el destino NO se borra. Ver la regla
// siguiente.
//
// **Nunca se genera una sentencia que borre algo.** Una tabla o una columna que
// está solo en el destino puede tener datos que nadie quiere perder —y desde
// acá no hay forma de saberlo—, así que se reporta la diferencia y se deja
// explícito que no hay sentencia. Quien de verdad quiera borrarla lo escribe en
// el editor, que es donde se piensa antes de apretar.
//
// **Una diferencia que no se sabe escribir sigue siendo una diferencia.** Se
// reporta con el motivo en `SinSentencia`. Callarla la convertiría en un
// esquema que parece alineado y no lo está, que es peor que no comparar.
//
// **Lo que no se comparó se dice.** El snapshot trae los objetos que no son
// tablas por NOMBRE, no por definición: dos vistas homónimas con cuerpos
// distintos se ven iguales desde acá. `Resultado.NoComparado` lo deja escrito,
// porque un «no hay diferencias» sobre algo que no se miró es una mentira.
package drift

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/change"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Lado dice de qué lado está la diferencia.
type Lado string

const (
	// SoloEnOrigen: existe en el origen y no en el destino.
	SoloEnOrigen Lado = "onlyInSource"
	// Distinto: existe en los dos y no son iguales.
	Distinto Lado = "different"
	// SoloEnDestino: existe en el destino y no en el origen.
	SoloEnDestino Lado = "onlyInTarget"
)

// Clase es qué tipo de cosa es lo que difiere.
type Clase string

const (
	ClaseTabla   Clase = "table"
	ClaseColumna Clase = "column"
	ClaseForanea Clase = "foreignKey"
	ClaseObjeto  Clase = "object"
	ClaseEsquema Clase = "schema"
)

// Riesgo es cuánto cuidado pide aplicar esta diferencia.
type Riesgo string

const (
	// RiesgoBajo: aditivo y reversible. Crear una tabla que no existía.
	RiesgoBajo Riesgo = "low"
	// RiesgoMedio: puede bloquear, reescribir o fallar con los datos que hay.
	RiesgoMedio Riesgo = "medium"
	// RiesgoAlto: pierde datos, o no se puede generar.
	RiesgoAlto Riesgo = "high"
)

// Diferencia es una cosa que no coincide entre los dos esquemas.
type Diferencia struct {
	// ID es estable entre corridas: la pantalla lo usa para saber qué está
	// seleccionado, y un índice se correría al cambiar un filtro.
	ID string `json:"id"`

	Lado   Lado   `json:"side"`
	Clase  Clase  `json:"kind"`
	Schema string `json:"schema"`

	// Objeto es cómo se lo nombra: `orders`, `orders.note`.
	Objeto string `json:"object"`

	// Resumen es la diferencia en una línea.
	Resumen string `json:"summary"`

	// Origen y Destino son cómo está cada lado. Vacío significa «no está».
	Origen  string `json:"source"`
	Destino string `json:"target"`

	Riesgo Riesgo `json:"risk"`

	// Nota es qué hay que tener en cuenta antes de aplicarla.
	Nota string `json:"note,omitempty"`

	// Cambio es la operación que alinearía el destino con el origen.
	//
	// Nil cuando no hay sentencia, y entonces `SinSentencia` dice por qué. Son
	// excluyentes: o hay una operación o hay un motivo.
	Cambio *change.Change `json:"change,omitempty"`

	// SinSentencia explica por qué esta diferencia no se puede —o no se debe—
	// alinear automáticamente.
	SinSentencia string `json:"noStatement,omitempty"`
}

// Resultado es la comparación completa.
type Resultado struct {
	Diferencias []Diferencia `json:"differences"`

	// NoComparado son las cosas que esta comparación NO miró, en castellano.
	//
	// Existe para que la pantalla no pueda decir «no hay diferencias» sobre algo
	// que nadie comparó. Es la misma lección que el panel de dependientes: una
	// lista vacía y un «no sé» son la misma lista y significan lo contrario.
	NoComparado []string `json:"notCompared,omitempty"`
}

// Cuenta devuelve cuántas diferencias hay de cada lado.
func (r Resultado) Cuenta() (soloOrigen, distintas, soloDestino int) {
	for _, d := range r.Diferencias {
		switch d.Lado {
		case SoloEnOrigen:
			soloOrigen++
		case Distinto:
			distintas++
		case SoloEnDestino:
			soloDestino++
		}
	}
	return
}

// Opciones ajusta qué se compara.
type Opciones struct {
	// MismoMotor dice si los dos lados corren el mismo motor.
	//
	// Cuando NO lo es, los tipos no se comparan. No es una limitación técnica
	// sino la única respuesta honesta: cada motor escribe el mismo tipo
	// distinto —`character varying(255)` contra `varchar(255)`, `bigint` contra
	// `bigint(20)`— así que compararlos daría cientos de «el tipo difiere» que
	// no son diferencias, cada uno con una sentencia que además estaría mal.
	// Ruido con esa forma no molesta: esconde las diferencias de verdad.
	//
	// Lo que sí se compara igual es qué tablas y qué columnas existen de cada
	// lado, que es lo que uno mira cuando está migrando de un motor a otro.
	MismoMotor bool
}

// Comparar produce las diferencias que llevarían el destino al estado del
// origen.
//
// Los dos snapshots tienen que venir normalizados —`Normalize()`— o el orden de
// los esquemas puede cambiar el orden de la salida. No cambia el resultado, pero
// sí hace ruido al mirar dos corridas seguidas.
func Comparar(origen, destino schema.Snapshot, opts Opciones) Resultado {
	var res Resultado

	// Los objetos que no son tablas viajan por nombre, sin definición. Se dice
	// SIEMPRE, aunque no haya ninguno: lo que hay que comunicar no es «encontré
	// esto», es «esto no lo miré».
	res.NoComparado = append(res.NoComparado,
		"El cuerpo de las vistas, funciones, triggers y tipos: el catálogo los "+
			"trae por nombre y la definición se pide de a una. Dos objetos con el "+
			"mismo nombre y distinto contenido se ven iguales desde acá.",
		"Los índices y las restricciones: viven en el detalle de cada tabla, que "+
			"es una consulta por tabla.",
		"Los datos. Esta pantalla no lee una sola fila de ninguna de las dos bases.",
	)
	if !opts.MismoMotor {
		res.NoComparado = append(res.NoComparado,
			"Los TIPOS de las columnas: los dos lados son motores distintos y cada uno "+
				"escribe el mismo tipo a su manera —`character varying(255)` contra "+
				"`varchar(255)`—, así que compararlos daría diferencias que no lo son. "+
				"Qué tablas y qué columnas existe de cada lado sí se comparó.")
	}

	porNombreOrigen := indexarEsquemas(origen)
	porNombreDestino := indexarEsquemas(destino)

	for _, nombre := range nombresOrdenados(porNombreOrigen, porNombreDestino) {
		so, hayOrigen := porNombreOrigen[nombre]
		sd, hayDestino := porNombreDestino[nombre]

		switch {
		case hayOrigen && !hayDestino:
			res.Diferencias = append(res.Diferencias, Diferencia{
				ID:      "schema:" + nombre,
				Lado:    SoloEnOrigen,
				Clase:   ClaseEsquema,
				Schema:  nombre,
				Objeto:  nombre,
				Resumen: fmt.Sprintf("el esquema no existe en el destino · %d tablas", len(so.Tables)),
				Origen:  fmt.Sprintf("%d tablas, %d objetos", len(so.Tables), len(so.Objects)),
				Riesgo:  RiesgoBajo,
				Nota: "Crear el esquema no está entre las operaciones que Kaname sabe " +
					"escribir, así que tampoco se generan las tablas de adentro: " +
					"llevarían un esquema que no existe.",
				SinSentencia: "Kaname no sabe escribir CREATE SCHEMA.",
			})
			continue

		case !hayOrigen && hayDestino:
			res.Diferencias = append(res.Diferencias, Diferencia{
				ID:           "schema:" + nombre,
				Lado:         SoloEnDestino,
				Clase:        ClaseEsquema,
				Schema:       nombre,
				Objeto:       nombre,
				Resumen:      fmt.Sprintf("el esquema existe solo en el destino · %d tablas", len(sd.Tables)),
				Destino:      fmt.Sprintf("%d tablas, %d objetos", len(sd.Tables), len(sd.Objects)),
				Riesgo:       RiesgoAlto,
				Nota:         "Puede tener datos que no están en ningún otro lado.",
				SinSentencia: "Borrar no se genera nunca.",
			})
			continue
		}

		res.Diferencias = append(res.Diferencias, compararEsquema(so, sd, opts)...)
	}
	return res
}

// compararEsquema compara las tablas y los objetos de un esquema que está en
// los dos lados.
func compararEsquema(origen, destino schema.Schema, opts Opciones) []Diferencia {
	var out []Diferencia

	to := indexarTablas(origen.Tables)
	td := indexarTablas(destino.Tables)

	for _, nombre := range nombresOrdenados(to, td) {
		o, hayOrigen := to[nombre]
		d, hayDestino := td[nombre]

		switch {
		case hayOrigen && !hayDestino:
			out = append(out, tablaSoloEnOrigen(origen.Name, o))
		case !hayOrigen && hayDestino:
			out = append(out, Diferencia{
				ID:           idDe("table", origen.Name, nombre),
				Lado:         SoloEnDestino,
				Clase:        ClaseTabla,
				Schema:       origen.Name,
				Objeto:       nombre,
				Resumen:      "la tabla existe solo en el destino",
				Destino:      fmt.Sprintf("%d columnas", len(d.Columns)),
				Riesgo:       RiesgoAlto,
				Nota:         "Puede tener filas. Borrarla las pierde, y desde acá no hay forma de saber cuántas son.",
				SinSentencia: "Borrar no se genera nunca. Si de verdad querés soltarla, escribí el DROP en el editor.",
			})
		default:
			out = append(out, compararTabla(origen.Name, o, d, opts)...)
		}
	}

	out = append(out, compararObjetos(origen, destino)...)
	return out
}

// tablaSoloEnOrigen arma el CREATE TABLE completo para una tabla que falta.
func tablaSoloEnOrigen(esquema string, t schema.Table) Diferencia {
	cols := make([]change.Column, 0, len(t.Columns))
	for _, c := range t.Columns {
		cols = append(cols, change.Column{
			Name:     c.Name,
			DataType: c.DataType,
			Nullable: c.Nullable,
		})
	}

	d := Diferencia{
		ID:      idDe("table", esquema, t.Name),
		Lado:    SoloEnOrigen,
		Clase:   ClaseTabla,
		Schema:  esquema,
		Objeto:  t.Name,
		Resumen: fmt.Sprintf("la tabla no existe en el destino · %d columnas", len(t.Columns)),
		Origen:  resumenDeTabla(t),
		Riesgo:  RiesgoBajo,
		Nota:    "Tabla nueva: crearla no puede romper nada de lo que ya está.",
		Cambio: &change.Change{
			Type:    change.CreateTable,
			Schema:  esquema,
			Table:   t.Name,
			Columns: cols,
			Source:  "drift",
		},
	}

	// Los DEFAULT no viajan en el snapshot —solo si la columna tiene uno— así
	// que la tabla se crearía sin ellos. Decirlo es obligatorio: una tabla que
	// se crea «igual» y pierde los defaults es una diferencia nueva escondida
	// adentro de la corrección.
	if algunaTieneDefault(t.Columns) {
		d.Riesgo = RiesgoMedio
		d.Nota += " Ojo: el catálogo dice QUÉ columnas tienen valor por defecto pero no CUÁL es, " +
			"así que la tabla se crea sin ellos."
	}
	return d
}

// compararTabla compara columna por columna y las claves foráneas.
func compararTabla(esquema string, origen, destino schema.Table, opts Opciones) []Diferencia {
	var out []Diferencia

	co := indexarColumnas(origen.Columns)
	cd := indexarColumnas(destino.Columns)

	for _, nombre := range nombresOrdenados(co, cd) {
		o, hayOrigen := co[nombre]
		d, hayDestino := cd[nombre]
		objeto := origen.Name + "." + nombre

		switch {
		case hayOrigen && !hayDestino:
			dif := Diferencia{
				ID:      idDe("column", esquema, objeto),
				Lado:    SoloEnOrigen,
				Clase:   ClaseColumna,
				Schema:  esquema,
				Objeto:  objeto,
				Resumen: fmt.Sprintf("la columna no existe en el destino · %s", resumenDeColumna(o)),
				Origen:  resumenDeColumna(o),
				Riesgo:  RiesgoBajo,
				Nota:    "Agregar una columna que acepta NULL es solo metadatos: no reescribe la tabla.",
				Cambio: &change.Change{
					Type:   change.AddColumn,
					Schema: esquema,
					Table:  origen.Name,
					Column: &change.Column{
						Name:     o.Name,
						DataType: o.DataType,
						Nullable: o.Nullable,
					},
					Source: "drift",
				},
			}
			if !o.Nullable {
				// Agregar NOT NULL sin default falla si la tabla tiene filas, y
				// el default no está en el snapshot. Se genera igual porque es
				// lo que pidió el origen, pero con el aviso.
				dif.Riesgo = RiesgoMedio
				dif.Nota = "La columna es NOT NULL y el catálogo no dice cuál es su valor por defecto. " +
					"Si la tabla del destino tiene filas, la sentencia va a fallar hasta que le des uno."
			}
			out = append(out, dif)

		case !hayOrigen && hayDestino:
			out = append(out, Diferencia{
				ID:           idDe("column", esquema, objeto),
				Lado:         SoloEnDestino,
				Clase:        ClaseColumna,
				Schema:       esquema,
				Objeto:       objeto,
				Resumen:      "la columna existe solo en el destino",
				Destino:      resumenDeColumna(d),
				Riesgo:       RiesgoAlto,
				Nota:         "Puede tener datos. Borrarla los pierde.",
				SinSentencia: "Borrar no se genera nunca.",
			})

		default:
			out = append(out, compararColumna(esquema, origen.Name, o, d, opts)...)
		}
	}

	out = append(out, compararForaneas(esquema, origen, destino)...)
	return out
}

// compararColumna mira tipo y nulabilidad, que es lo que el snapshot trae.
func compararColumna(esquema, tabla string, origen, destino schema.Column, opts Opciones) []Diferencia {
	var out []Diferencia
	objeto := tabla + "." + origen.Name

	// Entre motores distintos los tipos no se comparan. Ver Opciones.
	if opts.MismoMotor && !mismoTipo(origen.DataType, destino.DataType) {
		out = append(out, Diferencia{
			ID:      idDe("columnType", esquema, objeto),
			Lado:    Distinto,
			Clase:   ClaseColumna,
			Schema:  esquema,
			Objeto:  objeto,
			Resumen: fmt.Sprintf("el tipo difiere · %s contra %s", origen.DataType, destino.DataType),
			Origen:  origen.DataType,
			Destino: destino.DataType,
			Riesgo:  RiesgoMedio,
			Nota: "Cambiar el tipo puede reescribir la tabla entera y tomarse un bloqueo " +
				"exclusivo mientras lo hace. Y si los datos del destino no entran en el tipo " +
				"nuevo, falla.",
			Cambio: &change.Change{
				Type:     change.SetColumnType,
				Schema:   esquema,
				Table:    tabla,
				Column:   &change.Column{Name: origen.Name, DataType: origen.DataType},
				DataType: origen.DataType,
				Source:   "drift",
			},
		})
	}

	if origen.Nullable != destino.Nullable {
		d := Diferencia{
			ID:      idDe("columnNull", esquema, objeto),
			Lado:    Distinto,
			Clase:   ClaseColumna,
			Schema:  esquema,
			Objeto:  objeto,
			Origen:  nulabilidad(origen.Nullable),
			Destino: nulabilidad(destino.Nullable),
		}
		if origen.Nullable {
			d.Resumen = "acepta NULL en el origen y no en el destino"
			d.Riesgo = RiesgoBajo
			d.Nota = "Soltar el NOT NULL no toca ninguna fila: es solo metadatos."
			d.Cambio = &change.Change{
				Type: change.DropNotNull, Schema: esquema, Table: tabla,
				Column: &change.Column{Name: origen.Name}, Source: "drift",
			}
		} else {
			d.Resumen = "es NOT NULL en el origen y acepta NULL en el destino"
			d.Riesgo = RiesgoMedio
			d.Nota = "El servidor va a recorrer la tabla para verificar que no haya ningún NULL, " +
				"y falla si encuentra uno. Conviene rellenarlos antes."
			d.Cambio = &change.Change{
				Type: change.SetNotNull, Schema: esquema, Table: tabla,
				Column: &change.Column{Name: origen.Name}, Source: "drift",
			}
		}
		out = append(out, d)
	}

	// El DEFAULT se compara por PRESENCIA y no por valor: el snapshot dice si
	// hay uno, no cuál. Con eso alcanza para avisar que difieren, y no alcanza
	// para escribir la sentencia — así que no se escribe.
	if origen.HasDefault != destino.HasDefault {
		d := Diferencia{
			ID:      idDe("columnDefault", esquema, objeto),
			Lado:    Distinto,
			Clase:   ClaseColumna,
			Schema:  esquema,
			Objeto:  objeto,
			Origen:  conDefault(origen.HasDefault),
			Destino: conDefault(destino.HasDefault),
			Riesgo:  RiesgoBajo,
		}
		if origen.HasDefault {
			d.Resumen = "tiene valor por defecto en el origen y no en el destino"
			d.Nota = "El catálogo dice que hay un default, no cuál es."
			d.SinSentencia = "No se sabe qué valor poner: la definición del default no viaja en el snapshot."
		} else {
			d.Resumen = "no tiene valor por defecto en el origen y el destino sí"
			d.Nota = "Sacarlo no toca las filas que ya están: el default solo se aplica a las nuevas."
			d.Cambio = &change.Change{
				Type: change.DropDefault, Schema: esquema, Table: tabla,
				Column: &change.Column{Name: origen.Name}, Source: "drift",
			}
		}
		out = append(out, d)
	}
	return out
}

// compararForaneas compara las claves foráneas salientes de una tabla.
func compararForaneas(esquema string, origen, destino schema.Table) []Diferencia {
	var out []Diferencia

	fo := indexarForaneas(origen.ForeignKeys)
	fd := indexarForaneas(destino.ForeignKeys)

	for _, nombre := range nombresOrdenados(fo, fd) {
		o, hayOrigen := fo[nombre]
		d, hayDestino := fd[nombre]
		objeto := origen.Name + " → " + nombre

		switch {
		case hayOrigen && !hayDestino:
			out = append(out, Diferencia{
				ID:      idDe("fk", esquema, origen.Name+"."+nombre),
				Lado:    SoloEnOrigen,
				Clase:   ClaseForanea,
				Schema:  esquema,
				Objeto:  objeto,
				Resumen: "la clave foránea no existe en el destino",
				Origen:  resumenDeForanea(o),
				Riesgo:  RiesgoMedio,
				Nota: "El servidor verifica las filas existentes al crearla, y falla si alguna " +
					"apunta a algo que no está.",
				Cambio: &change.Change{
					Type:      change.AddForeignKey,
					Schema:    esquema,
					Table:     origen.Name,
					Names:     append([]string(nil), o.Columns...),
					RefSchema: o.RefSchema,
					RefTable:  o.RefTable,
					RefNames:  append([]string(nil), o.RefColumns...),
					OnDelete:  string(o.OnDelete),
					OnUpdate:  string(o.OnUpdate),
					Source:    "drift",
				},
			})

		case !hayOrigen && hayDestino:
			out = append(out, Diferencia{
				ID:           idDe("fk", esquema, origen.Name+"."+nombre),
				Lado:         SoloEnDestino,
				Clase:        ClaseForanea,
				Schema:       esquema,
				Objeto:       objeto,
				Resumen:      "la clave foránea existe solo en el destino",
				Destino:      resumenDeForanea(d),
				Riesgo:       RiesgoMedio,
				Nota:         "Alguien la creó directamente en el destino. Decidilo a propósito.",
				SinSentencia: "Borrar no se genera nunca.",
			})

		default:
			if resumenDeForanea(o) != resumenDeForanea(d) {
				out = append(out, Diferencia{
					ID:      idDe("fk", esquema, origen.Name+"."+nombre),
					Lado:    Distinto,
					Clase:   ClaseForanea,
					Schema:  esquema,
					Objeto:  objeto,
					Resumen: "la clave foránea apunta a otro lado o tiene otras acciones",
					Origen:  resumenDeForanea(o),
					Destino: resumenDeForanea(d),
					Riesgo:  RiesgoMedio,
					Nota: "Cambiarla es soltar la que hay y crear la nueva, y crear una clave " +
						"foránea verifica todas las filas.",
					SinSentencia: "Reemplazar una clave foránea son dos operaciones y una borra: " +
						"se arma a mano en el editor de estructura.",
				})
			}
		}
	}
	return out
}

// compararObjetos compara vistas, funciones, triggers y tipos POR NOMBRE.
//
// Por nombre y nada más: el snapshot no trae la definición. Que dos objetos
// homónimos puedan ser distintos está dicho en `NoComparado`, y esto solo
// reporta los que están de un lado y no del otro.
func compararObjetos(origen, destino schema.Schema) []Diferencia {
	var out []Diferencia

	oo := indexarObjetos(origen.Objects)
	od := indexarObjetos(destino.Objects)

	for _, clave := range nombresOrdenados(oo, od) {
		o, hayOrigen := oo[clave]
		d, hayDestino := od[clave]
		if hayOrigen && hayDestino {
			continue
		}

		obj := o
		lado := SoloEnOrigen
		if !hayOrigen {
			obj = d
			lado = SoloEnDestino
		}

		dif := Diferencia{
			ID:     idDe("object", origen.Name, clave),
			Lado:   lado,
			Clase:  ClaseObjeto,
			Schema: origen.Name,
			Objeto: obj.Name,
			Riesgo: RiesgoBajo,
		}
		if lado == SoloEnOrigen {
			dif.Resumen = fmt.Sprintf("%s: no existe en el destino", string(obj.Kind))
			dif.Origen = string(obj.Kind)
			dif.Nota = "Su definición se lee de a una y no viaja en la comparación."
			dif.SinSentencia = "Para crearlo hace falta su definición, que se pide aparte. " +
				"Abrilo en el origen y copiá el texto."
		} else {
			dif.Resumen = fmt.Sprintf("%s: existe solo en el destino", string(obj.Kind))
			dif.Destino = string(obj.Kind)
			dif.Riesgo = RiesgoMedio
			dif.Nota = "Alguien lo creó directamente en el destino."
			dif.SinSentencia = "Borrar no se genera nunca."
		}
		out = append(out, dif)
	}
	return out
}

/* ------------------------------------------------------------- ayudantes */

func indexarEsquemas(s schema.Snapshot) map[string]schema.Schema {
	m := make(map[string]schema.Schema, len(s.Schemas))
	for _, sc := range s.Schemas {
		m[sc.Name] = sc
	}
	return m
}

func indexarTablas(ts []schema.Table) map[string]schema.Table {
	m := make(map[string]schema.Table, len(ts))
	for _, t := range ts {
		m[t.Name] = t
	}
	return m
}

func indexarColumnas(cs []schema.Column) map[string]schema.Column {
	m := make(map[string]schema.Column, len(cs))
	for _, c := range cs {
		m[c.Name] = c
	}
	return m
}

func indexarForaneas(fks []schema.ForeignKey) map[string]schema.ForeignKey {
	m := make(map[string]schema.ForeignKey, len(fks))
	for _, f := range fks {
		// La clave es el NOMBRE de la restricción cuando lo hay. Sin nombre,
		// las columnas: dos claves distintas de la misma tabla no pueden tener
		// las mismas columnas de origen.
		clave := f.Name
		if clave == "" {
			clave = strings.Join(f.Columns, ",")
		}
		m[clave] = f
	}
	return m
}

func indexarObjetos(os []schema.Object) map[string]schema.Object {
	m := make(map[string]schema.Object, len(os))
	for _, o := range os {
		// La clase entra en la clave: una vista y una función pueden llamarse
		// igual, y compararlas entre sí no tiene sentido.
		m[string(o.Kind)+":"+o.Name] = o
	}
	return m
}

// nombresOrdenados devuelve la unión de las claves de dos mapas, ordenada.
//
// Ordenada porque la salida se muestra en una lista y se compara entre corridas:
// un orden de mapa haría que dos comparaciones idénticas se vean distintas.
func nombresOrdenados[T any](a, b map[string]T) []string {
	vistos := make(map[string]bool, len(a)+len(b))
	for k := range a {
		vistos[k] = true
	}
	for k := range b {
		vistos[k] = true
	}
	out := make([]string, 0, len(vistos))
	for k := range vistos {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func idDe(prefijo, esquema, objeto string) string {
	return prefijo + ":" + esquema + "." + objeto
}

// mismoTipo compara dos tipos como los escribe el motor.
//
// Se normalizan los espacios y las mayúsculas, y nada más: `varchar(255)` y
// `character varying(255)` son el mismo tipo en Postgres y esta función dice que
// no. Es a propósito. Los dos lados de una comparación salen de la MISMA
// introspección —el mismo motor escribe los dos textos— así que si difieren, es
// porque difieren. Adivinar equivalencias acá sería esconder diferencias reales
// entre motores distintos, que es justo lo que se está buscando.
func mismoTipo(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}

func nulabilidad(nullable bool) string {
	if nullable {
		return "acepta NULL"
	}
	return "NOT NULL"
}

func conDefault(tiene bool) string {
	if tiene {
		return "con valor por defecto"
	}
	return "sin valor por defecto"
}

func resumenDeColumna(c schema.Column) string {
	partes := []string{c.DataType, nulabilidad(c.Nullable)}
	if c.HasDefault {
		partes = append(partes, "con default")
	}
	if c.PrimaryKey {
		partes = append(partes, "clave primaria")
	}
	return strings.Join(partes, ", ")
}

func resumenDeTabla(t schema.Table) string {
	lineas := make([]string, 0, len(t.Columns))
	for _, c := range t.Columns {
		lineas = append(lineas, c.Name+" "+resumenDeColumna(c))
	}
	return strings.Join(lineas, "\n")
}

func resumenDeForanea(f schema.ForeignKey) string {
	s := fmt.Sprintf("(%s) → %s.%s(%s)",
		strings.Join(f.Columns, ", "), f.RefSchema, f.RefTable, strings.Join(f.RefColumns, ", "))
	if f.OnDelete != "" {
		s += " on delete " + string(f.OnDelete)
	}
	if f.OnUpdate != "" {
		s += " on update " + string(f.OnUpdate)
	}
	return s
}

func algunaTieneDefault(cs []schema.Column) bool {
	for _, c := range cs {
		if c.HasDefault {
			return true
		}
	}
	return false
}
