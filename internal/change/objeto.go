package change

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// PuedeReemplazarEnElLugar dice si este motor sabe reemplazar esta clase de
// objeto sin borrarla antes.
//
// La diferencia no es de comodidad: **reemplazar en el lugar no puede romper
// nada**. Si el CREATE OR REPLACE falla —porque el SQL está mal, o porque en
// Postgres cambió la lista de columnas de una vista— el objeto sigue exactamente
// como estaba y el error se ve. Borrar y crear es lo contrario: rompe lo que
// dependa del objeto, y en MySQL y MariaDB, donde cada sentencia de DDL hace
// commit sola, un CREATE que falla después de un DROP que funcionó deja el
// objeto PERDIDO — sin transacción que lo devuelva.
//
// Por eso el valor por defecto de la pantalla es reemplazar, y borrar y crear es
// una escalada que se pide.
//
// Está acá y no en `engine.Caps` porque depende del motor Y de la clase de
// objeto, que es una combinación y no una capacidad suelta.
func PuedeReemplazarEnElLugar(motor string, k schema.ObjectKind) bool {
	switch motor {
	case "postgres":
		switch k {
		case schema.ObjView, schema.ObjFunction, schema.ObjProcedure:
			return true
		case schema.ObjTrigger:
			// `CREATE OR REPLACE TRIGGER` existe desde PostgreSQL 14, que es la
			// versión mínima que este proyecto soporta.
			return true
		}
		// Una vista materializada NO: `CREATE OR REPLACE MATERIALIZED VIEW` no
		// existe, y recrearla además pierde sus filas hasta el próximo REFRESH.
		// Una secuencia tampoco, y recrearla le devuelve el valor inicial: la
		// próxima fila puede recibir un id que ya se usó.
		// Un enum se cambia con ALTER TYPE, no reescribiendo su CREATE.
		return false

	case "mysql", "mariadb":
		// Las dos tienen `CREATE OR REPLACE VIEW`. Para rutinas y triggers solo
		// MariaDB, y no se aprovecha: distinguirlas acá haría que la misma
		// pantalla se comportara distinto contra dos motores que se presentan
		// como compatibles, y el caso que importa —perder el objeto si el
		// CREATE falla— se cubre igual avisando.
		return k == schema.ObjView

	case "sqlite":
		// SQLite no tiene OR REPLACE para nada de esto. A cambio, su DDL sí es
		// transaccional, así que un CREATE que falla después del DROP se
		// revierte entero.
		return false
	}
	return false
}

// dropDeObjeto escribe el DROP que precede al CREATE cuando hay que recrear.
//
// `nombre` ya viene citado y completo —con la firma, si es una función
// sobrecargada— y `sufijo` es lo que algunos motores exigen después: Postgres
// pide `DROP TRIGGER x ON tabla`, y los demás no.
func dropDeObjeto(k schema.ObjectKind, nombre, sufijo string) (string, error) {
	var que string
	switch k {
	case schema.ObjView:
		que = "VIEW"
	case schema.ObjMatView:
		que = "MATERIALIZED VIEW"
	case schema.ObjFunction:
		que = "FUNCTION"
	case schema.ObjProcedure:
		que = "PROCEDURE"
	case schema.ObjTrigger:
		que = "TRIGGER"
	case schema.ObjSequence:
		que = "SEQUENCE"
	case schema.ObjEnum, schema.ObjDomain, schema.ObjComposite:
		que = "TYPE"
	default:
		return "", fmt.Errorf("no se sabe borrar %s", k)
	}
	return "DROP " + que + " " + nombre + sufijo, nil
}

// empiezaConCreate comprueba que la definición sea un CREATE.
//
// Es lo mínimo que se puede comprobar sin parsear SQL, y alcanza para el error
// que de verdad pasa: pegar en el editor un SELECT —o el cuerpo suelto de una
// función— y guardarlo. Con Recreate puesto eso sería un DROP seguido de una
// sentencia que no crea nada, y el objeto se pierde sin que ningún motor se
// queje: un SELECT es SQL perfectamente válida.
//
// NO se valida más que esto a propósito. Decidir si un CREATE entero es
// correcto es el trabajo del servidor, y adivinarlo acá terminaría rechazando
// SQL válida que Kaname no entendió.
var empiezaConCreate = regexp.MustCompile(`(?is)^\s*CREATE\s`)

// ObjetoAReemplazar valida y arma las partes de un replaceObject.
//
// Devuelve el DROP —vacío si no hace falta— y la definición ya lista. El
// renderizado por motor es el mismo en los cuatro: lo único que cambia es cómo
// se cita el nombre, y eso lo pone quien llama.
func ObjetoAReemplazar(c Change, motor, nombre, sufijoDrop string) (drop, definicion string, err error) {
	if err := c.Validate(); err != nil {
		return "", "", err
	}
	// Lo que el motor no sabe reemplazar en el lugar se exige recrear, y no se
	// intenta igual. Sin este candado, un `CREATE MATERIALIZED VIEW` sobre una
	// que ya existe falla con «already exists» —sin daño, pero con un mensaje
	// que no dice qué hacer—, y el arreglo sería el mismo que esta línea
	// obliga a elegir antes: con la lista de dependientes a la vista.
	if !c.Recreate && !PuedeReemplazarEnElLugar(motor, c.ObjectKind) {
		// Antes de mandar a recrear hay que saber BORRAR: para una política o
		// una extensión no se sabe, y el consejo llevaba derecho a un segundo
		// error distinto dos líneas después.
		if _, err := dropDeObjeto(c.ObjectKind, "x", ""); err != nil {
			return "", "", fmt.Errorf("Kaname todavía no sabe reemplazar %s", c.ObjectKind)
		}
		return "", "", fmt.Errorf(
			"este motor no sabe reemplazar %s en el lugar: hay que borrarla y volver a crearla",
			c.ObjectKind)
	}
	def := strings.TrimSpace(c.Definition)
	if !empiezaConCreate.MatchString(def) {
		return "", "", fmt.Errorf(
			"la definición de %s no empieza con CREATE, así que no crea nada: "+
				"guardarla dejaría el objeto borrado", c.Target())
	}
	if !c.Recreate {
		return "", def, nil
	}
	drop, err = dropDeObjeto(c.ObjectKind, nombre, sufijoDrop)
	if err != nil {
		return "", "", err
	}
	return drop, def, nil
}

// soloCreate encuentra el CREATE inicial de una definición, sin tocar nada más.
var soloCreate = regexp.MustCompile(`(?is)^(\s*CREATE\s+)(OR\s+REPLACE\s+)?`)

// ConOrReplace devuelve la definición en la forma que se puede volver a correr
// sobre el objeto que ya existe.
//
// Se usa al LEER, no al ejecutar, y esa diferencia es toda la justificación
// para tocar el texto. La definición que Kaname muestra en el editor tiene que
// ser la que se va a correr —esa es la promesa— y `CREATE VIEW x` sobre una
// vista que existe falla con «already exists». Así que la forma re-ejecutable
// se elige cuando se arma lo que se muestra, con el resultado a la vista, y
// nadie reescribe después lo que la persona escribió.
//
// La sustitución es deliberadamente mínima: solo el `CREATE` del principio, y
// si ya dice OR REPLACE no se toca. Lo que sigue —el `ALGORITHM=UNDEFINED
// DEFINER=…` de MySQL, el cuerpo entero— queda intacto, que es lo único
// posible sin parsear SQL de verdad.
func ConOrReplace(definicion string) string {
	m := soloCreate.FindStringSubmatchIndex(definicion)
	if m == nil {
		return definicion
	}
	// Ya la tiene.
	if m[4] != -1 {
		return definicion
	}
	return definicion[:m[3]] + "OR REPLACE " + definicion[m[3]:]
}
