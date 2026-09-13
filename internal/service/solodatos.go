package service

import (
	"fmt"

	"github.com/LucianoR23/kanamedb/internal/engine"
	"github.com/LucianoR23/kanamedb/internal/query"
)

// En el teléfono, datos sí y esquema no. El changeset lo cumple en Stage
// (ErrSchemaLocked); el editor SQL, acá: la misma regla, por el mismo camino
// que «Bloquear DROP y TRUNCATE», mirando el verbo de cada sentencia antes
// de correr ninguna. Es tan fuerte como esa protección y no más: reconoce la
// sentencia por su primera palabra, como el resto de las de Safety.

// verbosDeEsquema son las sentencias que cambian la estructura o los permisos.
// VACUUM, ANALYZE, SET y las de transacción no están: no tocan el esquema.
var verbosDeEsquema = map[string]bool{
	"CREATE": true, "ALTER": true, "DROP": true, "TRUNCATE": true,
	"RENAME": true, "COMMENT": true, "GRANT": true, "REVOKE": true,
	"REINDEX": true,
}

// esquemaEnElTelefono rechaza una sentencia de esquema con soloDatos.
func esquemaEnElTelefono(sql string, d query.Dialect) *engine.Failure {
	cmd := query.Command(sql, d)
	if !verbosDeEsquema[cmd] {
		return nil
	}
	return &engine.Failure{
		Kind:    engine.FailurePermission,
		Message: fmt.Sprintf("En el teléfono no se cambia el esquema, y esta sentencia es un %s.", cmd),
		Hint: "No se ejecutó ninguna sentencia del lote. Los cambios de estructura se hacen desde " +
			"Kaname en la PC.",
	}
}
