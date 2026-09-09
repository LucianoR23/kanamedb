package mysql

import (
	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/schema"
)

// columnTypes son los tipos que se pueden elegir para una columna.
//
// Acá SÍ es una lista escrita a mano, al revés que en Postgres, y el motivo es
// que el conjunto de tipos de MySQL es CERRADO: no hay extensiones que agreguen
// los suyos, no hay enums con nombre —el ENUM de MySQL declara sus valores en
// la columna— y no hay dominios. Leerlo del catálogo daría exactamente esta
// misma lista con una consulta de más.
//
// Van primero los que se usan todos los días. Un selector ordenado
// alfabéticamente pone `bigint` entre `binary` y `bit`, que es correcto y
// molesto.
func columnTypes(k engine.Kind) []schema.TypeOption {
	tipos := []schema.TypeOption{
		{Name: "int", Comment: "entero de 4 bytes"},
		{Name: "bigint", Comment: "entero de 8 bytes"},
		{Name: "varchar", AcceptsModifier: true, Comment: "texto con largo máximo; hace falta el largo"},
		{Name: "text", Comment: "texto largo, hasta 64 KB"},
		{Name: "boolean", Comment: "alias de tinyint(1)"},
		{Name: "datetime", AcceptsModifier: true, Comment: "fecha y hora, sin zona"},
		{Name: "timestamp", AcceptsModifier: true, Comment: "fecha y hora, se guarda en UTC"},
		{Name: "date"},
		{Name: "time", AcceptsModifier: true},
		{Name: "decimal", AcceptsModifier: true, Comment: "exacto; para dinero"},
		{Name: "double", Comment: "coma flotante; NO para dinero"},
		{Name: "float"},
		{Name: "json"},
		{Name: "char", AcceptsModifier: true, Comment: "largo fijo, se rellena con espacios"},
		{Name: "tinyint", AcceptsModifier: true},
		{Name: "smallint"},
		{Name: "mediumint"},
		{Name: "mediumtext", Comment: "hasta 16 MB"},
		{Name: "longtext", Comment: "hasta 4 GB"},
		{Name: "blob", Comment: "binario"},
		{Name: "varbinary", AcceptsModifier: true},
		{Name: "binary", AcceptsModifier: true},
		{Name: "enum", AcceptsModifier: true, Comment: "los valores van en el paréntesis: enum('a','b')"},
		{Name: "set", AcceptsModifier: true},
		{Name: "year"},
		{Name: "bit", AcceptsModifier: true},
	}

	// Los que existen en uno y no en el otro. Ofrecer un tipo que el motor no
	// tiene sería un error que aparece recién al aplicar.
	if k == engine.MariaDB {
		tipos = append(tipos,
			schema.TypeOption{Name: "uuid", Comment: "solo MariaDB, desde 10.7"},
			schema.TypeOption{Name: "inet4", Comment: "solo MariaDB"},
			schema.TypeOption{Name: "inet6", Comment: "solo MariaDB"},
		)
	} else {
		tipos = append(tipos,
			schema.TypeOption{Name: "vector", AcceptsModifier: true, Comment: "solo MySQL, desde 9.0"},
		)
	}

	for i := range tipos {
		tipos[i].BuiltIn = true
		tipos[i].Kind = "base"
	}
	return tipos
}
