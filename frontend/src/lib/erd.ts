import dagre from "@dagrejs/dagre";
import type { Edge, Node } from "@xyflow/react";
import type {
  ForeignKey,
  Snapshot,
  Table,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";

/* Las medidas del nodo están acá y no solo en el CSS porque dagre necesita el
   tamaño ANTES de que exista el DOM: acomoda un grafo, no una pantalla. Si los
   dos números se separan, el layout deja huecos o superpone tarjetas. El CSS las
   lee de estas mismas constantes a través de variables. */

/** Ancho de toda tarjeta. Fijo a propósito: un diagrama generado con tarjetas
 *  de anchos distintos se lee peor que uno parejo, aunque recorte algún nombre. */
export const NODO_ANCHO = 252;
/** Alto del encabezado de la tarjeta. */
export const NODO_CABECERA = 26;
/** Alto de cada fila de columna. */
export const NODO_FILA = 21;
/** Alto de la fila «+ N más». */
export const NODO_RESTO = 19;

export interface ErdColumna {
  name: string;
  dataType: string;
  /** "PK", "FK", "UQ" o "". */
  key: string;
}

/** Lo que la tarjeta necesita saber. Es `Record<string, unknown>` para xyflow,
 *  que exige que los datos de un nodo sean indexables. */
export interface DatosDeNodo extends Record<string, unknown> {
  schema: string;
  name: string;
  rowEstimate: number;
  columnas: ErdColumna[];
  /** Cuántas columnas quedaron fuera de la tarjeta. */
  ocultas: number;
  /** Está seleccionada. */
  activa: boolean;
  /** Está del otro lado de una relación con la seleccionada. */
  vecina: boolean;
  /** Se muestran los tipos al lado de cada columna. */
  tipos: boolean;
}

export interface DatosDeArista extends Record<string, unknown> {
  fk: ForeignKey;
  /** Fila de la columna de origen dentro de la tarjeta, o -1 si no se ve.
   *  Sirve para que la línea salga de la columna y no del medio del nodo. */
  filaOrigen: number;
  filaDestino: number;
  activa: boolean;
}

export type NodoErd = Node<DatosDeNodo, "tabla">;
export type AristaErd = Edge<DatosDeArista, "relacion">;

/** Cuánto mide de alto una tarjeta con estas columnas. */
export function altoDeNodo(columnas: number, ocultas: number): number {
  return NODO_CABECERA + columnas * NODO_FILA + (ocultas > 0 ? NODO_RESTO : 0) + 2;
}

export interface OpcionesDelGrafo {
  /** Todas las columnas, o solo las que son clave. */
  todasLasColumnas: boolean;
  tipos: boolean;
  /** Tablas escondidas del diagrama, por «esquema.tabla». */
  ocultas: ReadonlySet<string>;
  seleccionada: string | null;
}

export interface Grafo {
  nodos: NodoErd[];
  aristas: AristaErd[];
  /** Claves que apuntan fuera del esquema dibujado. No son aristas —el otro
   *  extremo no está en pantalla— pero el panel las cuenta: una relación que
   *  desaparece sin decir nada hace creer que la tabla no tiene ninguna. */
  fueraDelEsquema: number;
}

/** Identificador de una tabla en el grafo. */
export const idDeTabla = (esquema: string, tabla: string) => `${esquema}.${tabla}`;

/**
 * Arma nodos y aristas a partir del esquema ya introspectado.
 *
 * No consulta nada: las claves foráneas viajan en el snapshot justamente para
 * que dibujar el diagrama no cueste un viaje a la base por tabla.
 */
export function construirGrafo(
  snapshot: Snapshot | null,
  esquema: string,
  opts: OpcionesDelGrafo,
): Grafo {
  const tablas = (snapshot?.schemas ?? []).find((s) => s.name === esquema)?.tables ?? [];
  const visibles = tablas.filter((t) => !opts.ocultas.has(idDeTabla(esquema, t.name)));
  const enPantalla = new Set(visibles.map((t) => idDeTabla(esquema, t.name)));

  // Quiénes están relacionadas con la seleccionada, para resaltarlas. Se calcula
  // antes de armar los nodos porque la relación es simétrica: la tabla apuntada
  // también es vecina, y sus claves están declaradas del otro lado.
  const vecinas = new Set<string>();
  if (opts.seleccionada) {
    for (const t of visibles) {
      const yo = idDeTabla(esquema, t.name);
      for (const fk of t.foreignKeys ?? []) {
        const otro = idDeTabla(fk.refSchema, fk.refTable);
        if (yo === opts.seleccionada) vecinas.add(otro);
        if (otro === opts.seleccionada) vecinas.add(yo);
      }
    }
  }

  const nodos: NodoErd[] = visibles.map((t) => {
    const todas = columnasDe(t);
    const mostradas = opts.todasLasColumnas ? todas : todas.filter((c) => c.key !== "");
    const id = idDeTabla(esquema, t.name);
    return {
      id,
      type: "tabla",
      position: { x: 0, y: 0 },
      data: {
        schema: esquema,
        name: t.name,
        rowEstimate: t.rowEstimate,
        columnas: mostradas,
        ocultas: todas.length - mostradas.length,
        activa: id === opts.seleccionada,
        vecina: vecinas.has(id),
        tipos: opts.tipos,
      },
      // El tamaño va en el nodo y no solo en el CSS: xyflow lo usa para el
      // encuadre y dagre para acomodar, y ninguno de los dos mide el DOM.
      width: NODO_ANCHO,
      height: altoDeNodo(mostradas.length, todas.length - mostradas.length),
    };
  });

  const filaDe = new Map<string, Map<string, number>>();
  for (const n of nodos) {
    const m = new Map<string, number>();
    n.data.columnas.forEach((c, i) => m.set(c.name, i));
    filaDe.set(n.id, m);
  }

  const aristas: AristaErd[] = [];
  let fueraDelEsquema = 0;
  for (const t of visibles) {
    const origen = idDeTabla(esquema, t.name);
    for (const fk of t.foreignKeys ?? []) {
      const destino = idDeTabla(fk.refSchema, fk.refTable);
      if (!enPantalla.has(destino)) {
        fueraDelEsquema++;
        continue;
      }
      // Una tabla puede apuntarse a sí misma —una jerarquía, por ejemplo—. La
      // arista existe igual; xyflow la dibuja como un bucle.
      const primeraLocal = (fk.columns ?? [])[0] ?? "";
      const primeraRemota = (fk.refColumns ?? [])[0] ?? "";
      aristas.push({
        id: `${origen}:${fk.name}`,
        type: "relacion",
        source: origen,
        target: destino,
        data: {
          fk,
          filaOrigen: filaDe.get(origen)?.get(primeraLocal) ?? -1,
          filaDestino: filaDe.get(destino)?.get(primeraRemota) ?? -1,
          activa: origen === opts.seleccionada || destino === opts.seleccionada,
        },
      });
    }
  }

  return { nodos, aristas, fueraDelEsquema };
}

function columnasDe(t: Table): ErdColumna[] {
  return (t.columns ?? []).map((c) => ({
    name: c.name,
    dataType: c.dataType,
    key: c.primaryKey ? "PK" : c.foreignKey ? "FK" : "",
  }));
}

/**
 * Acomoda el grafo con dagre.
 *
 * De izquierda a derecha porque las tarjetas son altas y angostas: apiladas en
 * columnas entran en pantalla, apiladas en filas obligan a scrollear a lo ancho
 * desde la segunda tabla.
 *
 * dagre trabaja con el CENTRO del nodo y xyflow con su esquina superior
 * izquierda. Olvidar la conversión desplaza todo medio nodo y se ve como si el
 * layout estuviera mal calculado.
 */
export function acomodar(nodos: NodoErd[], aristas: AristaErd[]): NodoErd[] {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({ rankdir: "LR", ranksep: 96, nodesep: 36, marginx: 24, marginy: 24 });

  for (const n of nodos) {
    g.setNode(n.id, { width: n.width ?? NODO_ANCHO, height: n.height ?? 100 });
  }
  for (const a of aristas) {
    // Los bucles rompen el algoritmo de capas: una tabla que se apunta a sí
    // misma no tiene un "antes" y un "después".
    if (a.source !== a.target) g.setEdge(a.source, a.target);
  }

  dagre.layout(g);

  return nodos.map((n) => {
    const p = g.node(n.id);
    if (!p) return n;
    return {
      ...n,
      position: {
        x: p.x - (n.width ?? NODO_ANCHO) / 2,
        y: p.y - (n.height ?? 100) / 2,
      },
    };
  });
}
