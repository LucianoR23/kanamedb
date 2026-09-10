// Package dump arma el volcado de una base: la estructura, los datos, o los dos.
//
// Es la mitad de lo que la gente llama «backup», y la mitad que se puede hacer
// bien. La palabra «backup» no se usa para lo que genera Kaname: un backup es
// una copia de la que se puede restaurar TODO, y esto no lo es —ver `Coverage`,
// que dice con nombre y apellido qué queda afuera—. Para lo que genera
// `pg_dump`, la palabra sí vale.
//
// Acá no hay conexión ni motor: entra lo que la introspección ya sabe y sale
// texto. La parte que habla con la base vive en `internal/service`.
package dump

import "sort"

// Ref identifica una tabla dentro de la base.
type Ref struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
}

// Arista es una dependencia: `De` necesita que `A` ya exista.
type Arista struct {
	De Ref
	A  Ref
}

// Orden decide en qué orden se escriben las tablas para que los datos se puedan
// volver a meter.
//
// El plan decía que este orden «ya lo sabemos calcular, es el mismo del
// changeset». No lo era: el changeset ordena por FASE —primero las tablas,
// después las claves— y eso funciona justamente porque las claves van aparte.
// Un volcado de DATOS no tiene esa salida: las filas de la tabla hija no entran
// antes que las de la madre, así que hace falta un orden topológico de verdad.
//
// Devuelve las tablas ordenadas y los CICLOS que encontró. Un ciclo no es un
// error ni se rompe por las buenas: dos tablas que se apuntan entre sí no
// tienen ningún orden que funcione, y la única salida honesta es decirlo para
// que el volcado avise —y para que el script difiera las restricciones—.
func Orden(tablas []Ref, aristas []Arista) (orden []Ref, ciclos [][]Ref) {
	// El orden de entrada se normaliza para que dos corridas sobre la misma
	// base den el mismo archivo: un volcado que cambia de orden solo porque el
	// catálogo devolvió las filas distinto no se puede comparar con el de ayer.
	pendientes := append([]Ref(nil), tablas...)
	sort.Slice(pendientes, func(i, j int) bool {
		if pendientes[i].Schema != pendientes[j].Schema {
			return pendientes[i].Schema < pendientes[j].Schema
		}
		return pendientes[i].Table < pendientes[j].Table
	})

	existe := make(map[Ref]bool, len(pendientes))
	for _, t := range pendientes {
		existe[t] = true
	}

	// Cuántas dependencias sin resolver tiene cada tabla, y quién la espera.
	//
	// Las aristas hacia una tabla que no se está volcando se ignoran: apuntar a
	// algo de otro esquema que no entra en este archivo no ordena nada acá, y
	// contarla dejaría la tabla trabada para siempre.
	faltan := make(map[Ref]int, len(pendientes))
	esperan := make(map[Ref][]Ref, len(pendientes))
	vistas := make(map[Arista]bool, len(aristas))
	for _, a := range aristas {
		if !existe[a.De] || !existe[a.A] {
			continue
		}
		// Una tabla que se apunta a sí misma —un `padre_id` que referencia la
		// misma tabla— es legal y NO es un ciclo entre tablas: las filas se
		// insertan en la misma tabla y el orden entre tablas no cambia.
		if a.De == a.A {
			continue
		}
		// Dos claves distintas entre las mismas dos tablas son una sola
		// dependencia. Contarlas dos veces también funciona —se suma dos y se
		// resta dos— pero solo mientras las DOS mitades cuenten igual: dedupar
		// una sola dejaría el contador sin llegar nunca a cero y la tabla
		// parecería atrapada en un ciclo que no existe. Se dedupa acá, arriba
		// de las dos, para que no haya forma de que se separen.
		if vistas[a] {
			continue
		}
		vistas[a] = true
		faltan[a.De]++
		esperan[a.A] = append(esperan[a.A], a.De)
	}

	listas := make([]Ref, 0, len(pendientes))
	for _, t := range pendientes {
		if faltan[t] == 0 {
			listas = append(listas, t)
		}
	}

	orden = make([]Ref, 0, len(pendientes))
	for len(listas) > 0 {
		// Se saca siempre la primera: `listas` arranca ordenada y lo que se
		// agrega va al final, así que el resultado es estable.
		t := listas[0]
		listas = listas[1:]
		orden = append(orden, t)

		siguientes := esperan[t]
		sort.Slice(siguientes, func(i, j int) bool {
			if siguientes[i].Schema != siguientes[j].Schema {
				return siguientes[i].Schema < siguientes[j].Schema
			}
			return siguientes[i].Table < siguientes[j].Table
		})
		for _, s := range siguientes {
			faltan[s]--
			if faltan[s] == 0 {
				listas = append(listas, s)
			}
		}
	}

	if len(orden) == len(pendientes) {
		return orden, nil
	}

	// Lo que quedó sin salir está en algún ciclo, o depende de uno. Se agrupan
	// en componentes para poder NOMBRARLOS: «hay un ciclo» no sirve para
	// decidir nada; «pedidos ↔ clientes» sí.
	atrapadas := make([]Ref, 0, len(pendientes)-len(orden))
	for _, t := range pendientes {
		if faltan[t] > 0 {
			atrapadas = append(atrapadas, t)
		}
	}
	ciclos = componentes(atrapadas, vistas)

	// Las atrapadas van igual al final, en orden estable: el volcado las
	// escribe y avisa, en vez de dejarlas afuera en silencio.
	orden = append(orden, atrapadas...)
	return orden, ciclos
}

// componentes agrupa las tablas trabadas en conjuntos conectados entre sí.
//
// No se calculan las componentes fuertemente conexas exactas: alcanza con
// agrupar lo que está conectado, porque lo que se hace con esto es nombrarlo en
// un aviso. Distinguir «está en el ciclo» de «depende del ciclo» pediría
// Tarjan para una frase que se lee igual.
func componentes(atrapadas []Ref, aristas map[Arista]bool) [][]Ref {
	enGrupo := make(map[Ref]bool, len(atrapadas))
	vecinos := make(map[Ref][]Ref, len(atrapadas))
	for _, t := range atrapadas {
		enGrupo[t] = true
	}
	for a := range aristas {
		if !enGrupo[a.De] || !enGrupo[a.A] {
			continue
		}
		vecinos[a.De] = append(vecinos[a.De], a.A)
		vecinos[a.A] = append(vecinos[a.A], a.De)
	}

	visto := make(map[Ref]bool, len(atrapadas))
	var out [][]Ref
	for _, t := range atrapadas {
		if visto[t] {
			continue
		}
		var grupo []Ref
		pila := []Ref{t}
		visto[t] = true
		for len(pila) > 0 {
			u := pila[len(pila)-1]
			pila = pila[:len(pila)-1]
			grupo = append(grupo, u)
			for _, v := range vecinos[u] {
				if !visto[v] {
					visto[v] = true
					pila = append(pila, v)
				}
			}
		}
		sort.Slice(grupo, func(i, j int) bool {
			if grupo[i].Schema != grupo[j].Schema {
				return grupo[i].Schema < grupo[j].Schema
			}
			return grupo[i].Table < grupo[j].Table
		})
		out = append(out, grupo)
	}
	return out
}
