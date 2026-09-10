package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	sqldriver "github.com/go-sql-driver/mysql"

	"github.com/LucianoR23/kanamedb/internal/schema"
)

// Dependents lista las vistas que usan a este objeto.
//
// MySQL 8.0.13 y posteriores tienen `information_schema.VIEW_TABLE_USAGE`, que
// dice qué vista usa qué tabla o vista. MariaDB NO la tiene, y ahí la respuesta
// honesta es «no se puede saber» y no una lista vacía: una lista vacía
// significa «no depende nada de esto», que es justo lo que alguien mira antes
// de apretar un botón destructivo.
//
// Aunque la vista exista, la respuesta puede ser INCOMPLETA sin decirlo: el
// servidor filtra sus filas por privilegio, así que una vista sobre la que
// este usuario no tiene `SHOW VIEW` no aparece, y no aparece SIN ERROR. Por eso
// se comprueba primero si esta conexión puede ver todo; si no, lo que se
// encuentre se devuelve igual pero marcado como incompleto.
func Dependents(ctx context.Context, db *sql.DB, o schema.Object) (schema.Dependents, error) {
	var out schema.Dependents

	// `VIEW_TABLE_USAGE` responde por tablas y por vistas: son los dos lados de
	// «qué usa una vista». Lo que no cubre en NINGÚN motor de esta familia es
	// el cuerpo de una rutina o de un trigger — el catálogo no lo guarda, y
	// deducirlo leyendo su texto sería adivinar.
	if o.Kind != schema.ObjView && o.Kind != schema.ObjectKind("table") {
		out.Unknown = true
		out.Reason = "Este motor solo registra de qué dependen las vistas: de una rutina o un trigger no guarda nada."
		return out, nil
	}

	const q = `SELECT VIEW_SCHEMA, VIEW_NAME
	           FROM information_schema.VIEW_TABLE_USAGE
	           WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
	             AND NOT (VIEW_SCHEMA = ? AND VIEW_NAME = ?)`
	filas, err := db.QueryContext(ctx, q, o.Schema, o.Name, o.Schema, o.Name)
	if err != nil {
		// La vista no existe en MariaDB ni en MySQL anteriores a 8.0.13. No es
		// un fallo: es que este servidor no sabe contestar.
		if noExisteLaVista(err) {
			out.Unknown = true
			out.Reason = "Este servidor no tiene `information_schema.VIEW_TABLE_USAGE`, que es donde se registra qué vista usa qué."
			return out, nil
		}
		return out, fmt.Errorf("leer las dependencias de %s: %w", o.Completo(), err)
	}
	defer filas.Close()

	for filas.Next() {
		dep := schema.Object{Kind: schema.ObjView}
		if err := filas.Scan(&dep.Schema, &dep.Name); err != nil {
			return out, fmt.Errorf("leer las dependencias de %s: %w", o.Completo(), err)
		}
		out.Objects = append(out.Objects, dep)
	}
	if err := filas.Err(); err != nil {
		return out, fmt.Errorf("leer las dependencias de %s: %w", o.Completo(), err)
	}

	// La lista puede estar recortada por privilegios y el servidor no lo dice.
	if !veTodasLasVistas(ctx, db) {
		out.Unknown = true
		out.Reason = "Esta conexión no tiene `SHOW VIEW` sobre todas las bases, y el servidor esconde " +
			"—sin avisar— las vistas que no puede ver. Puede haber más de las que están acá."
	}
	return out, nil
}

// veTodasLasVistas dice si esta conexión puede ver TODAS las vistas del
// servidor.
//
// Hace falta porque `VIEW_TABLE_USAGE` no falla cuando no alcanza el permiso:
// devuelve menos filas. Y menos filas, cuando llega a cero, se lee como «no
// depende nada de esto» — que es la peor forma de equivocarse acá.
//
// Se mira el privilegio GLOBAL y no el de la base del objeto a propósito: una
// vista de OTRA base puede depender de esta tabla, así que tener todo sobre la
// propia no alcanza para afirmar que la lista está completa.
func veTodasLasVistas(ctx context.Context, db *sql.DB) bool {
	filas, err := db.QueryContext(ctx, "SHOW GRANTS FOR CURRENT_USER")
	if err != nil {
		// Sin poder comprobarlo, no se afirma que la lista esté completa.
		return false
	}
	defer filas.Close()

	for filas.Next() {
		var linea string
		if err := filas.Scan(&linea); err != nil {
			return false
		}
		mayus := strings.ToUpper(linea)
		if !strings.Contains(mayus, "ON *.*") {
			continue
		}
		if strings.Contains(mayus, "ALL PRIVILEGES") || strings.Contains(mayus, "SHOW VIEW") {
			return true
		}
	}
	return false
}

// noExisteLaVista reconoce el error de una tabla de information_schema ausente.
//
// Se mira el NÚMERO del error y no su texto: los servidores traducen sus
// mensajes según `lc_messages`, así que contra uno en otro idioma la
// comparación por texto fallaba y el camino honesto de «no se puede saber» se
// degradaba en un error rojo. 1109 es el de MariaDB —comprobado— y 1146 el de
// MySQL viejo. El texto queda solo como respaldo.
func noExisteLaVista(err error) bool {
	var me *sqldriver.MySQLError
	if errors.As(err, &me) {
		return me.Number == 1109 || me.Number == 1146
	}
	t := strings.ToLower(err.Error())
	return strings.Contains(t, "view_table_usage") &&
		(strings.Contains(t, "doesn't exist") || strings.Contains(t, "unknown table"))
}
