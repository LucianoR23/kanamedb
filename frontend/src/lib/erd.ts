import dagre from "@dagrejs/dagre";
import type { Edge, Node } from "@xyflow/react";
import { ReferenceAction } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import type { Estado, Pendientes } from "./erdStaged";
import { claveColumna, vacio as sinPendientes } from "./erdStaged";
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
  /** Qué le va a pasar cuando se aplique el changeset, si algo. */
  estado?: Estado | undefined;
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

  /** La tabla tiene cambios pendientes, o todavía no existe. */
  estado?: Estado | "nueva" | undefined;

  /** El modo edición cambia lo que un clic significa, así que los nodos
   *  necesitan saberlo para ofrecer los conectores por columna. */
  editando: boolean;
  herramienta: Herramienta;
}

/** Qué hace un clic en el lienzo. */
export type Herramienta = "select" | "column" | "table" | "relate" | "drop";

export interface DatosDeArista extends Record<string, unknown> {
  fk: ForeignKey;
  /** Fila de la columna de origen dentro de la tarjeta, o -1 si no se ve.
   *  Sirve para que la línea salga de la columna y no del medio del nodo. */
  filaOrigen: number;
  filaDestino: number;
  activa: boolean;

  /** Cuántas claves unen este mismo par de tablas, y cuál de ellas es esta.
   *
   *  Dos claves entre las mismas dos tablas suelen apuntar a la MISMA columna
   *  del otro lado —`autor` y `revisor` van los dos a `id`—, así que salen de
   *  filas distintas y convergen en el mismo punto: en el último tramo se
   *  superponen y parecen una sola. Con estos dos números cada una se corre un
   *  poco y las dos se ven. */
  paralelas: number;
  indiceParalela: number;

  /** Qué le va a pasar cuando se aplique el changeset, si algo. */
  estado?: Estado | undefined;
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
  /** Lo que el changeset va a hacer, para pintarlo antes de que pase. */
  pendientes?: Pendientes;
  editando?: boolean;
  herramienta?: Herramienta;
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
  const pend = opts.pendientes ?? sinPendientes();
  const editando = opts.editando ?? false;
  const herramienta = opts.herramienta ?? "select";

  const tablas = (snapshot?.schemas ?? []).find((s) => s.name === esquema)?.tables ?? [];
  const visibles = tablas.filter((t) => !opts.ocultas.has(idDeTabla(esquema, t.name)));
  const enPantalla = new Set(visibles.map((t) => idDeTabla(esquema, t.name)));
  for (const t of pend.nuevasTablas) enPantalla.add(t.id);

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
    const id = idDeTabla(esquema, t.name);
    // Las columnas del catálogo más las que el changeset va a agregar: una
    // columna nueva tiene que verse en la tarjeta antes de existir, o el
    // diagrama muestra un pasado que ya nadie quiere.
    const todas = [
      ...columnasDe(t).map((c) => ({
        ...c,
        ...(pend.columnas.get(claveColumna(id, c.name))
          ? { estado: pend.columnas.get(claveColumna(id, c.name)) }
          : {}),
      })),
      ...(pend.nuevasColumnas.get(id) ?? []).map((c) => ({ ...c, estado: "agregada" as Estado })),
    ];
    const mostradas = opts.todasLasColumnas
      ? todas
      : todas.filter((c) => c.key !== "" || c.estado !== undefined);
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
        ...(pend.tablas.get(id) ? { estado: pend.tablas.get(id) } : {}),
        editando,
        herramienta,
      },
      // El tamaño va en el nodo y no solo en el CSS: xyflow lo usa para el
      // encuadre y dagre para acomodar, y ninguno de los dos mide el DOM.
      width: NODO_ANCHO,
      height: altoDeNodo(mostradas.length, todas.length - mostradas.length),
    };
  });

  // Las tablas que el changeset crea todavía no están en el catálogo, así que
  // se agregan acá: sin esto, la clave nueva que apunta a una tabla nueva no
  // tendría de dónde salir.
  for (const t of pend.nuevasTablas) {
    if (opts.ocultas.has(t.id)) continue;
    nodos.push({
      id: t.id,
      type: "tabla",
      position: { x: 0, y: 0 },
      data: {
        schema: t.schema,
        name: t.name,
        rowEstimate: -1,
        columnas: t.columnas,
        ocultas: 0,
        activa: t.id === opts.seleccionada,
        vecina: vecinas.has(t.id),
        tipos: opts.tipos,
        estado: "nueva",
        editando,
        herramienta,
      },
      width: NODO_ANCHO,
      height: altoDeNodo(t.columnas.length, 0),
    });
  }

  const filaDe = new Map<string, Map<string, number>>();
  for (const n of nodos) {
    const m = new Map<string, number>();
    n.data.columnas.forEach((c, i) => m.set(c.name, i));
    filaDe.set(n.id, m);
  }

  const aristas: AristaErd[] = [];
  let fueraDelEsquema = 0;
  // Cuántas claves van ya entre cada par de tablas, para poder correr las que se
  // superpondrían. La clave del mapa no distingue dirección: dos tablas unidas
  // en los dos sentidos también se pisan.
  const porPar = new Map<string, number>();

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
      const par = [origen, destino].sort().join("|");
      const indice = porPar.get(par) ?? 0;
      porPar.set(par, indice + 1);

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
          paralelas: 1,
          indiceParalela: indice,
        },
      });
    }
  }

  // Las claves que el changeset va a crear se dibujan antes de existir, y las
  // que va a borrar se marcan sobre la que todavía está. Ver una relación
  // aparecer al soltar el arrastre es lo que hace que dibujar sirva.
  for (const k of pend.nuevasClaves) {
    if (!enPantalla.has(k.origen) || !enPantalla.has(k.destino)) continue;
    aristas.push({
      id: `nueva:${k.changeId}`,
      type: "relacion",
      source: k.origen,
      target: k.destino,
      data: {
        fk: {
          name: k.nombre || "(sin nombre todavía)",
          schema: k.origen.split(".")[0] ?? "",
          table: k.origen.split(".").slice(1).join("."),
          columns: [k.columna],
          refSchema: k.destino.split(".")[0] ?? "",
          refTable: k.destino.split(".").slice(1).join("."),
          refColumns: [k.refColumna],
          onDelete: ReferenceAction.NoAction,
          onUpdate: ReferenceAction.NoAction,
          deferrable: "",
          optional: false,
        },
        filaOrigen: filaDe.get(k.origen)?.get(k.columna) ?? -1,
        filaDestino: filaDe.get(k.destino)?.get(k.refColumna) ?? -1,
        activa: false,
        paralelas: 1,
        indiceParalela: 0,
        estado: "agregada",
      },
    });
  }
  for (const a of aristas) {
    if (a.data && pend.clavesBorradas.has(a.data.fk.name)) a.data.estado = "borrando";
  }

  // El total por par se sabe recién cuando se recorrieron todas, así que se
  // completa al final.
  for (const a of aristas) {
    const par = [a.source, a.target].sort().join("|");
    if (a.data) a.data.paralelas = porPar.get(par) ?? 1;
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
