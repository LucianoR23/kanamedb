package schema

import (
	"strings"
	"testing"
)

func snap(esquemas ...Schema) Snapshot {
	return Snapshot{Database: "shop", Schemas: esquemas}
}

func nombresDeEsquemas(s Snapshot) string {
	n := make([]string, 0, len(s.Schemas))
	for _, sc := range s.Schemas {
		n = append(n, sc.Name)
	}
	return strings.Join(n, ",")
}

func nombresDeTablas(sc Schema) string {
	n := make([]string, 0, len(sc.Tables))
	for _, t := range sc.Tables {
		n = append(n, t.Name)
	}
	return strings.Join(n, ",")
}

// `public` es donde está casi todo lo que le interesa a quien abre la app;
// mandarlo a su lugar alfabético es correcto y molesto.
func TestNormalizeDejaPublicPrimero(t *testing.T) {
	casos := []struct {
		nombre  string
		entrada []Schema
		quiere  string
	}{
		{
			"public en el medio",
			[]Schema{{Name: "audit"}, {Name: "public"}, {Name: "reporting"}},
			"public,audit,reporting",
		},
		{
			"public al final",
			[]Schema{{Name: "a"}, {Name: "b"}, {Name: "public"}},
			"public,a,b",
		},
		{
			"sin public",
			[]Schema{{Name: "zeta"}, {Name: "Alfa"}, {Name: "beta"}},
			"Alfa,beta,zeta",
		},
		{
			"public solo",
			[]Schema{{Name: "public"}},
			"public",
		},
		{
			"nada",
			nil,
			"",
		},
	}
	for _, tc := range casos {
		t.Run(tc.nombre, func(t *testing.T) {
			s := snap(tc.entrada...)
			s.Normalize()
			if got := nombresDeEsquemas(s); got != tc.quiere {
				t.Errorf("orden = %q, se esperaba %q", got, tc.quiere)
			}
		})
	}
}

// El árbol compara snapshots para detectar cambios: normalizar dos veces no
// puede dar resultados distintos.
func TestNormalizeEsIdempotente(t *testing.T) {
	s := snap(
		Schema{Name: "reporting", Tables: []Table{{Name: "Zeta"}, {Name: "alfa"}}},
		Schema{Name: "public", Tables: []Table{{Name: "orders"}, {Name: "Customers"}}},
	)
	s.Normalize()
	primera := nombresDeEsquemas(s) + "|" + nombresDeTablas(s.Schemas[0])
	s.Normalize()
	segunda := nombresDeEsquemas(s) + "|" + nombresDeTablas(s.Schemas[0])
	if primera != segunda {
		t.Errorf("normalizar dos veces dio distinto:\n %q\n %q", primera, segunda)
	}
}

func TestNormalizeOrdenaTablasSinDistinguirMayusculas(t *testing.T) {
	s := snap(Schema{Name: "public", Tables: []Table{
		{Name: "zeta"}, {Name: "Alfa"}, {Name: "beta"}, {Name: "ALFA_2"},
	}})
	s.Normalize()
	if got, quiere := nombresDeTablas(s.Schemas[0]), "Alfa,ALFA_2,beta,zeta"; got != quiere {
		t.Errorf("orden = %q, se esperaba %q", got, quiere)
	}
}

// -1 es "el planificador no tiene estimación", que es distinto de "cero filas".
// Confundirlos haría que el árbol muestre 0 para una tabla con millones.
func TestHasRowEstimateDistingueDesconocidoDeCero(t *testing.T) {
	casos := map[int64]bool{
		-1:    false,
		0:     true,
		1:     true,
		12481: true,
		-999:  false,
	}
	for valor, quiere := range casos {
		if got := (Table{RowEstimate: valor}).HasRowEstimate(); got != quiere {
			t.Errorf("HasRowEstimate() con %d = %v, se esperaba %v", valor, got, quiere)
		}
	}
}

func TestTotalTablesSumaTodosLosEsquemas(t *testing.T) {
	s := snap(
		Schema{Name: "public", Tables: []Table{{Name: "a"}, {Name: "b"}}},
		Schema{Name: "audit", Tables: []Table{{Name: "c"}}},
		Schema{Name: "vacio"},
	)
	if got := s.TotalTables(); got != 3 {
		t.Errorf("TotalTables() = %d, se esperaba 3", got)
	}
	if got := snap().TotalTables(); got != 0 {
		t.Errorf("TotalTables() sin esquemas = %d, se esperaba 0", got)
	}
}

func TestFindSchema(t *testing.T) {
	s := snap(Schema{Name: "public"}, Schema{Name: "audit"})

	if sc, ok := s.FindSchema("audit"); !ok || sc.Name != "audit" {
		t.Errorf("FindSchema(audit) = %+v, %v", sc, ok)
	}
	if _, ok := s.FindSchema("no_existe"); ok {
		t.Error("FindSchema() encontró un esquema que no está")
	}
	// Los nombres de esquema en Postgres distinguen mayúsculas.
	if _, ok := s.FindSchema("PUBLIC"); ok {
		t.Error("FindSchema() no debería ignorar mayúsculas: Postgres no lo hace")
	}
}

// Un esquema llamado literalmente distinto de "public" pero que empieza igual
// no debe recibir el trato especial.
func TestSoloElEsquemaPublicExactoVaPrimero(t *testing.T) {
	s := snap(Schema{Name: "public_backup"}, Schema{Name: "alfa"}, Schema{Name: "public"})
	s.Normalize()
	if got, quiere := nombresDeEsquemas(s), "public,alfa,public_backup"; got != quiere {
		t.Errorf("orden = %q, se esperaba %q", got, quiere)
	}
}
