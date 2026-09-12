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
	"regexp"
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

	// SinObjetos pide no comparar vistas, funciones y demás objetos, porque la
	// lista de algún lado no se pudo leer entera. Compararlos igual sobre una
	// lista parcial daba «existe solo en el destino» por cada objeto que el
	// origen no llegó a listar (C-22 de la auditoría del 2026-09-11).
	SinObjetos bool
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
		"Los índices y las restricciones, salvo la CLAVE PRIMARIA: el resto vive en "+
			"el detalle de cada tabla, que es una consulta por tabla. La clave primaria "+
			"sí está en el catálogo y sí se compara.",
		"Los datos. Esta pantalla no lee una sola fila de ninguna de las dos bases.",
		"De las claves foráneas, DEFERRABLE y MATCH: dos claves que difieren solo en eso se "+
			"ven iguales, y la que se crea no lleva la propiedad.",
		"El orden de las columnas y el particionado de las tablas.",
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

	// Un solo esquema de cada lado con distinto nombre se empareja por
	// posición: en MySQL y MariaDB el esquema se llama como la base, así que
	// `shop_dev` contra `shop_prod` —el caso normal de dev → prod— salía como
	// «el esquema no existe en el destino» y «existe solo en el destino», con
	// cero tablas comparadas (C-07 de la auditoría del 2026-09-11). Se
	// compara con el nombre del DESTINO, que es donde las sentencias van a
	// correr, y se dice.
	if len(origen.Schemas) == 1 && len(destino.Schemas) == 1 &&
		origen.Schemas[0].Name != destino.Schemas[0].Name {
		so, sd := origen.Schemas[0], destino.Schemas[0]
		res.NoComparado = append(res.NoComparado, fmt.Sprintf(
			"Los nombres de los esquemas: se comparó `%s` del origen con `%s` del destino, "+
				"que es el único de cada lado. Las sentencias nombran `%s`.",
			so.Name, sd.Name, sd.Name))
		res.Diferencias = append(res.Diferencias, compararEsquema(renombrado(so, sd.Name), sd, opts)...)
		return res
	}

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
				Resumen: "el esquema no existe en el destino · " + cuenta(len(so.Tables), "tabla", "tablas"),
				Origen:  cuenta(len(so.Tables), "tabla", "tablas") + ", " + cuenta(len(so.Objects), "objeto", "objetos"),
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
				Resumen:      "el esquema existe solo en el destino · " + cuenta(len(sd.Tables), "tabla", "tablas"),
				Destino:      cuenta(len(sd.Tables), "tabla", "tablas") + ", " + cuenta(len(sd.Objects), "objeto", "objetos"),
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

// renombrado devuelve el esquema como si se llamara `nombre`, con las claves
// foráneas que apuntaban a sí mismo apuntando al nombre nuevo. Sin esto, la
// clave que falta en `shop_prod` se creaba con `REFERENCES shop_dev.productos`:
// falla, o peor, crea una clave entre bases (review del 2026-09-12).
func renombrado(s schema.Schema, nombre string) schema.Schema {
	viejo := s.Name
	s.Name = nombre
	tablas := make([]schema.Table, len(s.Tables))
	for i, t := range s.Tables {
		fks := make([]schema.ForeignKey, len(t.ForeignKeys))
		for j, fk := range t.ForeignKeys {
			if fk.Schema == viejo {
				fk.Schema = nombre
			}
			if fk.RefSchema == viejo {
				fk.RefSchema = nombre
			}
			fks[j] = fk
		}
		t.ForeignKeys = fks
		tablas[i] = t
	}
	s.Tables = tablas
	objetos := make([]schema.Object, len(s.Objects))
	for i, o := range s.Objects {
		if o.Schema == viejo {
			o.Schema = nombre
		}
		objetos[i] = o
	}
	s.Objects = objetos
	return s
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
			out = append(out, tablaSoloEnOrigen(origen.Name, o, opts))
		case !hayOrigen && hayDestino:
			out = append(out, Diferencia{
				ID:           idDe("table", origen.Name, nombre),
				Lado:         SoloEnDestino,
				Clase:        ClaseTabla,
				Schema:       origen.Name,
				Objeto:       nombre,
				Resumen:      "la tabla existe solo en el destino",
				Destino:      cuenta(len(d.Columns), "columna", "columnas"),
				Riesgo:       RiesgoAlto,
				Nota:         "Puede tener filas. Borrarla las pierde, y desde acá no hay forma de saber cuántas son.",
				SinSentencia: "Borrar no se genera nunca. Si de verdad querés soltarla, escribí el DROP en el editor.",
			})
		default:
			out = append(out, compararTabla(origen.Name, o, d, opts)...)
		}
	}

	if !opts.SinObjetos {
		out = append(out, compararObjetos(origen, destino)...)
	}
	return out
}

// tablaSoloEnOrigen arma el CREATE TABLE completo para una tabla que falta.
func tablaSoloEnOrigen(esquema string, t schema.Table, opts Opciones) Diferencia {
	d := Diferencia{
		ID:      idDe("table", esquema, t.Name),
		Lado:    SoloEnOrigen,
		Clase:   ClaseTabla,
		Schema:  esquema,
		Objeto:  t.Name,
		Resumen: "la tabla no existe en el destino · " + cuenta(len(t.Columns), "columna", "columnas"),
		Origen:  resumenDeTabla(t),
		Riesgo:  RiesgoBajo,
		Nota:    "Tabla nueva: crearla no puede romper nada de lo que ya está.",
	}

	// Entre motores distintos NO se genera el CREATE TABLE.
	//
	// Los tipos se copiarían tal cual los escribió el motor de origen, y el
	// destino los rendea con su sintaxis: un `timestamp with time zone` o un
	// `jsonb` de Postgres dentro de un CREATE TABLE de MySQL es una sentencia
	// que no corre. Suprimir la comparación de tipos sin suprimir esto dejaba la
	// mitad del problema, que es peor que ninguna: parece que funciona.
	if !opts.MismoMotor {
		d.Nota = "Los dos lados son motores distintos, así que los tipos de las columnas " +
			"no se pueden copiar de uno al otro tal cual."
		d.SinSentencia = "Entre motores distintos, la definición de la tabla se escribe a mano."
		return d
	}

	cols := make([]change.Column, 0, len(t.Columns))
	var numeradas []string
	for _, c := range t.Columns {
		cols = append(cols, change.Column{
			Name:     c.Name,
			DataType: c.DataType,
			Nullable: c.Nullable,
		})
		if c.AutoIncrement {
			numeradas = append(numeradas, c.Name)
		}
	}
	// La clave primaria viaja en `Names`, que es lo que el renderizador
	// convierte en `PRIMARY KEY (…)`, en el ORDEN de la clave y no en el de
	// las columnas. Sin esto la tabla se creaba SIN clave —y `HasPrimaryKey`
	// es lo que habilita editar la grilla, así que la tabla nueva quedaba de
	// solo lectura— y la comparación siguiente no lo notaba.
	clave := clavePrimariaDe(t)

	d.Cambio = &change.Change{
		Type:    change.CreateTable,
		Schema:  esquema,
		Table:   t.Name,
		Columns: cols,
		Names:   clave,
		Source:  "drift",
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
	// Una columna que se numera sola —identity, serial, AUTO_INCREMENT— se
	// crea como una columna común: el modelo de cambios no sabe escribirla.
	// Callarlo escondía la deriva adentro de la corrección: el primer INSERT
	// en el destino fallaba por `id` nulo y la comparación siguiente decía
	// «alineadas» (C-08). Hasta que el CreateTable la sepa escribir, se dice y
	// se sube el riesgo.
	if len(numeradas) > 0 {
		d.Riesgo = RiesgoMedio
		d.Nota += " " + cuentaDe(len(numeradas),
			"La columna "+strings.Join(numeradas, ", ")+" se numera sola en el origen y acá se "+
				"crea sin eso: el primer INSERT sin ese valor va a fallar. Agregale el "+
				"autoincremento a mano después de crearla.",
			"Las columnas "+strings.Join(numeradas, ", ")+" se numeran solas en el origen y acá "+
				"se crean sin eso: el primer INSERT sin esos valores va a fallar. Agregales el "+
				"autoincremento a mano después de crearlas.")
	}

	// Las claves foráneas de una tabla nueva tampoco viajan: el CREATE TABLE
	// sale sin ellas y las claves solo se comparan entre tablas que están de
	// los dos lados. No se pierden —la comparación siguiente, con la tabla ya
	// creada, las reporta como «falta en el destino»— pero convergen en dos
	// pasadas y no en una, y eso hay que decirlo: una tabla «igual» que se
	// crea sin sus claves es otra diferencia escondida adentro de la
	// corrección.
	if len(t.ForeignKeys) > 0 {
		d.Nota += " " + cuentaDe(len(t.ForeignKeys),
			"Su clave foránea no va en este CREATE TABLE: aparece en la comparación siguiente, "+
				"cuando la tabla exista de los dos lados.",
			"Sus %d claves foráneas no van en este CREATE TABLE: aparecen en la comparación "+
				"siguiente, cuando la tabla exista de los dos lados.")
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
			}

			switch {
			case !opts.MismoMotor:
				// Mismo motivo que el CREATE TABLE: el tipo lo escribió el otro
				// motor y no se puede copiar tal cual.
				dif.Nota = "Los dos lados son motores distintos y el tipo está escrito con la " +
					"sintaxis del origen."
				dif.SinSentencia = "Entre motores distintos, la columna se agrega a mano con el tipo que corresponda."

			case !o.Nullable:
				// Una columna NOT NULL nueva NECESITA un valor por defecto, y el
				// catálogo no dice cuál es: solo dice que hay uno.
				//
				// Antes se generaba igual «con el aviso», y eso era una promesa
				// falsa: `change.Validate` rechaza ese addColumn, así que quien
				// lo mandaba al changeset recibía un error de validación y no
				// tenía por dónde seguir. Una operación que el resto del sistema
				// va a rechazar no es una operación.
				dif.Riesgo = RiesgoMedio
				dif.Nota = "La columna es NOT NULL, así que hace falta un valor por defecto para " +
					"las filas que ya están — y el catálogo dice que el origen tiene uno, no cuál es."
				dif.SinSentencia = "Falta el valor por defecto: agregala desde el editor de estructura, " +
					"que te deja escribirlo."

			default:
				dif.Cambio = &change.Change{
					Type:   change.AddColumn,
					Schema: esquema,
					Table:  origen.Name,
					Column: &change.Column{
						Name:     o.Name,
						DataType: o.DataType,
						Nullable: o.Nullable,
					},
					Source: "drift",
				}
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

	out = append(out, compararClavePrimaria(esquema, origen, destino)...)
	out = append(out, compararForaneas(esquema, origen, destino)...)
	return out
}

// compararClavePrimaria compara la clave como CONJUNTO ordenado, a nivel
// tabla.
//
// Antes se comparaba columna por columna y salía un ADD PRIMARY KEY por cada
// una: con `(order_id, line_no)` en el origen y sin clave en el destino, dos
// sentencias, la primera con una clave equivocada que en MySQL —sin DDL
// transaccional— quedaba confirmada antes de que la segunda fallara (C-09 de
// la auditoría del 2026-09-11).
func compararClavePrimaria(esquema string, origen, destino schema.Table) []Diferencia {
	co, cd := clavePrimariaDe(origen), clavePrimariaDe(destino)
	if strings.Join(co, "\x00") == strings.Join(cd, "\x00") {
		return nil
	}
	d := Diferencia{
		ID:      idDe("pk", esquema, origen.Name),
		Lado:    Distinto,
		Clase:   ClaseColumna,
		Schema:  esquema,
		Objeto:  origen.Name,
		Origen:  clavePrimariaComoTexto(co),
		Destino: clavePrimariaComoTexto(cd),
		Riesgo:  RiesgoMedio,
	}
	switch {
	case len(co) > 0 && len(cd) == 0:
		d.Resumen = "tiene clave primaria en el origen y no en el destino · " + clavePrimariaComoTexto(co)
		d.Nota = "Crear la clave exige que no haya repetidos ni NULL, y el servidor construye " +
			"un índice único mientras tanto. Una tabla sin clave primaria además no se puede " +
			"editar desde la grilla."
		d.Cambio = &change.Change{
			Type: change.AddPrimaryKey, Schema: esquema, Table: origen.Name,
			Names: append([]string(nil), co...), Source: "drift",
		}
	case len(co) == 0:
		d.Resumen = "tiene clave primaria en el destino y no en el origen"
		d.Nota = "Soltar una clave primaria es borrar una restricción, y puede haber cosas " +
			"que dependan de ella."
		d.SinSentencia = "Borrar no se genera nunca."
	default:
		d.Resumen = "la clave primaria es distinta · " + clavePrimariaComoTexto(co) + " contra " + clavePrimariaComoTexto(cd)
		d.Nota = "Cambiarla es soltar la que hay y crear la nueva; las claves foráneas que " +
			"apunten a la vieja dejan de poder hacerlo."
		d.SinSentencia = "Reemplazar una clave primaria son dos operaciones y una borra: se " +
			"arma a mano en el editor de estructura."
	}
	return []Diferencia{d}
}

// clavePrimariaDe son las columnas de la clave, en el orden de la tabla.
func clavePrimariaDe(t schema.Table) []string {
	var out []string
	for _, c := range t.Columns {
		if c.PrimaryKey {
			out = append(out, c.Name)
		}
	}
	return out
}

func clavePrimariaComoTexto(cols []string) string {
	if len(cols) == 0 {
		return "sin clave"
	}
	return "(" + strings.Join(cols, ", ") + ")"
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
			// La columna viaja COMPLETA —tipo y nulabilidad—: en MySQL la
			// nulabilidad es parte de la definición y MODIFY la reescribe
			// entera; con solo el tipo, un `int` → `bigint` en la clave le
			// quitaba el NOT NULL (C-20).
			Cambio: &change.Change{
				Type:     change.SetColumnType,
				Schema:   esquema,
				Table:    tabla,
				Column:   &change.Column{Name: origen.Name, DataType: origen.DataType, Nullable: origen.Nullable},
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
			// Con el tipo: MySQL no tiene DROP NOT NULL y reescribe la
			// definición con MODIFY, así que sin el tipo «no supo escribir» la
			// operación (C-20).
			d.Cambio = &change.Change{
				Type: change.DropNotNull, Schema: esquema, Table: tabla,
				Column: &change.Column{Name: origen.Name, DataType: origen.DataType, Nullable: true}, Source: "drift",
			}
		} else {
			d.Resumen = "es NOT NULL en el origen y acepta NULL en el destino"
			d.Riesgo = RiesgoMedio
			d.Nota = "El servidor va a recorrer la tabla para verificar que no haya ningún NULL, " +
				"y falla si encuentra uno. Conviene rellenarlos antes."
			d.Cambio = &change.Change{
				Type: change.SetNotNull, Schema: esquema, Table: tabla,
				Column: &change.Column{Name: origen.Name, DataType: origen.DataType}, Source: "drift",
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
		// La etiqueta sale de la clave y no del índice: el índice es la firma
		// técnica, y esto se lee en una lista.
		etiquetada := o
		if !hayOrigen {
			etiquetada = d
		}
		objeto := origen.Name + " (" + strings.Join(etiquetada.Columns, ", ") + ") → " + etiquetada.RefTable

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
					Type:   change.AddForeignKey,
					Schema: esquema,
					Table:  origen.Name,
					// El NOMBRE viaja cuando lo eligió alguien; uno que inventó
					// el motor —`fk_t_0`, `t_ibfk_1`— no, porque en el destino
					// el número sería otro. Las claves se emparejan por lo que
					// hacen, así que la comparación converge igual.
					Name:      nombreDeForanea(o),
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
			// Se comparan las ACCIONES: columnas y destino ya coinciden por
			// construcción del índice, y el destino se comparó sin distinguir
			// mayúsculas.
			if o.OnDelete != d.OnDelete || o.OnUpdate != d.OnUpdate {
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

// indexarForaneas identifica cada clave por lo que HACE —columnas locales,
// tabla y columnas destino— y no por su nombre.
//
// El nombre no sirve para emparejar: SQLite no lo devuelve y se sintetiza a
// partir del `id` posicional del pragma, y MySQL nombra las automáticas
// `<tabla>_ibfk_N`, también ordinal. Con dos claves creadas en distinto orden a
// cada lado, la comparación no convergía y cada apply agregaba una clave
// duplicada (C-06 de la auditoría del 2026-09-11). El nombre queda para la
// sentencia, cuando es uno que alguien eligió.
//
// La tabla destino se compara sin distinguir mayúsculas: SQLite guarda el
// texto tal cual se escribió en REFERENCES, y MySQL en Windows pliega los
// nombres (C-24).
func indexarForaneas(fks []schema.ForeignKey) map[string]schema.ForeignKey {
	m := make(map[string]schema.ForeignKey, len(fks))
	for _, f := range fks {
		clave := firmaDeForanea(f)
		if _, repetida := m[clave]; repetida {
			// Dos claves iguales en la misma tabla: legal y raro. Se
			// distinguen por nombre para no perder ninguna.
			clave += "#" + f.Name
		}
		m[clave] = f
	}
	return m
}

func firmaDeForanea(f schema.ForeignKey) string {
	return strings.Join(f.Columns, ",") + "→" + strings.ToLower(f.RefTable) +
		"(" + strings.Join(f.RefColumns, ",") + ")"
}

// nombreSintetizado reconoce los nombres que un motor inventa para una clave
// sin nombre: `fk_<tabla>_<n>` de la introspección de SQLite y
// `<tabla>_ibfk_<n>` de MySQL. Copiarlos al destino no tiene sentido: allá el
// número sería otro.
var nombreSintetizado = regexp.MustCompile(`^(fk_.+_\d+|.+_ibfk_\d+)$`)

// nombreDeForanea es el nombre que viaja en la sentencia: el del origen si lo
// eligió alguien, y vacío si lo inventó el motor.
func nombreDeForanea(f schema.ForeignKey) string {
	if nombreSintetizado.MatchString(f.Name) {
		return ""
	}
	return f.Name
}

func indexarObjetos(os []schema.Object) map[string]schema.Object {
	m := make(map[string]schema.Object, len(os))
	for _, o := range os {
		// La identidad ENTERA: clase, tabla y argumentos, además del nombre.
		// Una vista y una función pueden llamarse igual; un trigger
		// `set_updated_at` vive en cinco tablas; `calcular(int)` y
		// `calcular(text)` son dos funciones. Con solo clase y nombre, el
		// último ganaba en el mapa y una tabla nueva sin su trigger salía
		// «alineada» (C-18 de la auditoría del 2026-09-11).
		m[claveDeObjeto(o)] = o
	}
	return m
}

func claveDeObjeto(o schema.Object) string {
	clave := string(o.Kind) + ":" + o.Name
	if o.Table != "" {
		clave = string(o.Kind) + ":" + o.Table + "." + o.Name
	}
	if o.Args != "" {
		clave += "(" + o.Args + ")"
	}
	return clave
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

// cuenta escribe «1 tabla» o «3 tablas»: un «1 columnas» se lee como un error de
// programa y hace dudar del resto del texto.
func cuenta(n int, uno, varios string) string {
	if n == 1 {
		return "1 " + uno
	}
	return fmt.Sprintf("%d %s", n, varios)
}

// cuentaDe elige una frase entera según el número; la de varios lleva el %d.
func cuentaDe(n int, una, varias string) string {
	if n == 1 {
		return una
	}
	return fmt.Sprintf(varias, n)
}
