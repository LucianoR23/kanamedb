package dump

import (
	"strings"
	"testing"
)

func t1(nombres ...string) []Ref {
	out := make([]Ref, 0, len(nombres))
	for _, n := range nombres {
		out = append(out, Ref{Schema: "public", Table: n})
	}
	return out
}

func dep(de, a string) Arista {
	return Arista{De: Ref{Schema: "public", Table: de}, A: Ref{Schema: "public", Table: a}}
}

func nombres(refs []Ref) string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.Table)
	}
	return strings.Join(out, ",")
}

// TestLaMadreVaAntesQueLaHija es la razón de existir del orden: las filas de
// `pedidos` no entran antes que las de `clientes`.
func TestLaMadreVaAntesQueLaHija(t *testing.T) {
	orden, ciclos := Orden(
		t1("pedidos", "clientes", "items"),
		[]Arista{dep("pedidos", "clientes"), dep("items", "pedidos")},
	)
	if len(ciclos) != 0 {
		t.Fatalf("ciclos donde no hay: %v", ciclos)
	}
	if got := nombres(orden); got != "clientes,pedidos,items" {
		t.Errorf("orden = %q, quería clientes,pedidos,items", got)
	}
}

// TestElOrdenEsEstable: dos corridas sobre la misma base tienen que dar el
// mismo archivo, o no se puede comparar el volcado de hoy con el de ayer. El
// catálogo no promete ningún orden.
func TestElOrdenEsEstable(t *testing.T) {
	// VARIAS raíces: tres tablas sin dependencias y una que depende de `a`.
	//
	// Con una sola raíz el caso no probaría nada, porque lo que sale después
	// se ordena al desencolar. Lo que decide el orden ENTRE raíces es la
	// normalización de la entrada, y eso es lo que hay que fijar: el catálogo
	// no promete ningún orden y dos corridas tienen que dar el mismo archivo.
	aristas := []Arista{dep("d", "a")}
	primero := nombres(must(Orden(t1("c", "b", "a", "d"), aristas)))
	segundo := nombres(must(Orden(t1("b", "a", "c", "d"), aristas)))
	if primero != segundo {
		t.Errorf("dos corridas con la misma base dieron %q y %q", primero, segundo)
	}
	// Y a igualdad de dependencias, alfabético.
	if primero != "a,b,c,d" {
		t.Errorf("orden = %q, quería a,b,c,d", primero)
	}

	// Lo mismo sin ninguna dependencia, que es el caso más común de todos:
	// un esquema sin claves foráneas.
	if got := nombres(must(Orden(t1("z", "m", "a"), nil))); got != "a,m,z" {
		t.Errorf("sin dependencias el orden es %q y quería a,m,z", got)
	}
}

// TestUnCicloSeNombraYNoSeRompe.
//
// Dos tablas que se apuntan entre sí no tienen ningún orden que funcione.
// Inventar uno sería mentir: se dice cuáles son, y las tablas salen igual para
// que el volcado exista.
func TestUnCicloSeNombraYNoSeRompe(t *testing.T) {
	orden, ciclos := Orden(
		t1("pedidos", "clientes", "sola"),
		[]Arista{dep("pedidos", "clientes"), dep("clientes", "pedidos")},
	)
	if len(ciclos) != 1 {
		t.Fatalf("ciclos = %v, quería uno", ciclos)
	}
	if got := nombres(ciclos[0]); got != "clientes,pedidos" {
		t.Errorf("el ciclo es %q y quería clientes,pedidos", got)
	}
	// Ninguna tabla se pierde: las trabadas van al final, no afuera.
	if len(orden) != 3 {
		t.Errorf("salieron %d tablas de 3: %q", len(orden), nombres(orden))
	}
	if !strings.Contains(nombres(orden), "sola") {
		t.Errorf("la tabla sin dependencias se perdió: %q", nombres(orden))
	}
}

// TestUnaClaveHaciaSiMismaNoEsUnCiclo.
//
// Un `padre_id` que apunta a la misma tabla es lo más común del mundo —un árbol
// de categorías— y no ordena nada entre tablas: las filas entran en la misma
// tabla. Contarlo como ciclo dejaría todos esos volcados con un aviso falso.
func TestUnaClaveHaciaSiMismaNoEsUnCiclo(t *testing.T) {
	orden, ciclos := Orden(t1("categorias"), []Arista{dep("categorias", "categorias")})
	if len(ciclos) != 0 {
		t.Errorf("una clave hacia sí misma se contó como ciclo: %v", ciclos)
	}
	if nombres(orden) != "categorias" {
		t.Errorf("orden = %q", nombres(orden))
	}
}

// TestDosClavesEntreLasMismasTablasSonUnaDependencia.
//
// Una tabla puede referenciar a otra dos veces —`envio_id` y `factura_id` a
// `direcciones`—. Contarlas como dos dejaría el contador de pendientes en 1
// para siempre y la tabla parecería atrapada en un ciclo que no existe.
func TestDosClavesEntreLasMismasTablasSonUnaDependencia(t *testing.T) {
	orden, ciclos := Orden(
		t1("ventas", "direcciones"),
		[]Arista{dep("ventas", "direcciones"), dep("ventas", "direcciones")},
	)
	if len(ciclos) != 0 {
		t.Fatalf("ciclo inventado por una clave repetida: %v", ciclos)
	}
	if got := nombres(orden); got != "direcciones,ventas" {
		t.Errorf("orden = %q, quería direcciones,ventas", got)
	}
}

// TestUnaClaveHaciaAfueraDelVolcadoNoOrdenaNada.
//
// Si se vuelca un esquema y una tabla apunta a otro que no entra en el archivo,
// esa dependencia no se puede resolver acá. Contarla dejaría la tabla trabada
// para siempre y el volcado la reportaría como parte de un ciclo.
func TestUnaClaveHaciaAfueraDelVolcadoNoOrdenaNada(t *testing.T) {
	orden, ciclos := Orden(
		t1("pedidos"),
		[]Arista{{De: Ref{"public", "pedidos"}, A: Ref{"otro", "clientes"}}},
	)
	if len(ciclos) != 0 {
		t.Errorf("una clave hacia afuera se contó como ciclo: %v", ciclos)
	}
	if nombres(orden) != "pedidos" {
		t.Errorf("orden = %q", nombres(orden))
	}
}

// TestUnaCadenaLargaSaleEnOrden comprueba que no se resuelva de a un nivel: la
// última de una cadena de cinco tiene que quedar última.
func TestUnaCadenaLargaSaleEnOrden(t *testing.T) {
	orden, _ := Orden(
		t1("e", "d", "c", "b", "a"),
		[]Arista{dep("b", "a"), dep("c", "b"), dep("d", "c"), dep("e", "d")},
	)
	if got := nombres(orden); got != "a,b,c,d,e" {
		t.Errorf("orden = %q, quería a,b,c,d,e", got)
	}
}

// TestDosCiclosSeparadosSeNombranPorSeparado.
func TestDosCiclosSeparadosSeNombranPorSeparado(t *testing.T) {
	_, ciclos := Orden(
		t1("a", "b", "x", "y"),
		[]Arista{dep("a", "b"), dep("b", "a"), dep("x", "y"), dep("y", "x")},
	)
	if len(ciclos) != 2 {
		t.Fatalf("ciclos = %v, quería dos grupos separados", ciclos)
	}
	if nombres(ciclos[0]) != "a,b" || nombres(ciclos[1]) != "x,y" {
		t.Errorf("los grupos salieron %q y %q", nombres(ciclos[0]), nombres(ciclos[1]))
	}
}

func must(orden []Ref, _ [][]Ref) []Ref { return orden }
