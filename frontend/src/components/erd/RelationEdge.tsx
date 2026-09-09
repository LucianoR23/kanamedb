import { getBezierPath, Position, useInternalNode } from "@xyflow/react";
import type { EdgeProps, InternalNode, Node } from "@xyflow/react";
import type { AristaErd } from "../../lib/erd";
import { NODO_CABECERA, NODO_FILA } from "../../lib/erd";
import styles from "./RelationEdge.module.css";

/** Los tres marcadores de pata de gallo, uno por color. Se declaran una vez en
 *  el canvas; acá solo se elige cuál usar. */
export const MARCADOR = {
  normal: "kn-muchos",
  activo: "kn-muchos-activo",
  cascada: "kn-muchos-cascada",
} as const;

/**
 * Una clave foránea en el diagrama.
 *
 * Calcula su propia geometría en vez de usar la que le pasa xyflow. Con
 * conectores fijos, una tabla arrastrada a la izquierda de otra deja la línea
 * saliendo por el lado equivocado y cruzando la tarjeta entera. Leyendo las
 * posiciones acá, la línea elige el lado que mira al otro nodo y se reacomoda
 * sola mientras se arrastra.
 *
 * Sale de la FILA de la columna, no del medio de la tarjeta: en una tabla con
 * tres claves foráneas, tres líneas naciendo del mismo punto no dicen cuál es
 * cuál.
 */
export function RelationEdge({ id, source, target, data }: EdgeProps<AristaErd>) {
  const origen = useInternalNode(source);
  const destino = useInternalNode(target);
  if (!origen || !destino || !data) return null;

  const fk = data.fk;
  const tono = data.activa ? "activo" : fk.onDelete === "cascade" ? "cascada" : "normal";
  const clase = data.activa
    ? styles.activa
    : fk.onDelete === "cascade"
      ? styles.cascada
      : styles.normal;

  const d =
    source === target
      ? bucle(origen, data.filaOrigen)
      : entreNodos(origen, destino, data.filaOrigen, data.filaDestino);

  return (
    <>
      {/* Una línea de 1,4px es casi imposible de acertar con el mouse. Esta va
          transparente y ancha, solo para que el hover y el clic tengan blanco. */}
      <path d={d} className={styles.zonaClic} />
      <path
        id={id}
        d={d}
        className={clase}
        strokeDasharray={fk.optional ? "5 4" : undefined}
        markerStart={`url(#${MARCADOR[tono]})`}
      >
        <title>{`${fk.name}\n${(fk.columns ?? []).join(", ")} → ${fk.refTable}.${(fk.refColumns ?? []).join(", ")}\nal borrar: ${fk.onDelete}`}</title>
      </path>
    </>
  );
}

interface Ancla {
  x: number;
  y: number;
  lado: Position;
}

/** Dónde nace o muere la línea en una tarjeta. */
function ancla(n: InternalNode<Node>, fila: number, derecha: boolean): Ancla {
  const { x, y } = n.internals.positionAbsolute;
  const ancho = n.measured?.width ?? n.width ?? 0;
  const alto = n.measured?.height ?? n.height ?? 0;
  // La fila de la columna cuando está a la vista; el medio de la tarjeta cuando
  // las columnas están plegadas y no hay ninguna fila que señalar.
  const cy = fila >= 0 ? y + NODO_CABECERA + fila * NODO_FILA + NODO_FILA / 2 : y + alto / 2;
  return {
    x: derecha ? x + ancho : x,
    y: cy,
    lado: derecha ? Position.Right : Position.Left,
  };
}

function entreNodos(
  origen: InternalNode<Node>,
  destino: InternalNode<Node>,
  filaOrigen: number,
  filaDestino: number,
): string {
  const centro = (n: InternalNode<Node>) =>
    n.internals.positionAbsolute.x + (n.measured?.width ?? n.width ?? 0) / 2;
  // El destino a la derecha: la línea sale por la derecha del origen y entra por
  // la izquierda del destino. Al revés, al revés.
  const aLaDerecha = centro(destino) >= centro(origen);
  const a = ancla(origen, filaOrigen, aLaDerecha);
  const b = ancla(destino, filaDestino, !aLaDerecha);

  const [d] = getBezierPath({
    sourceX: a.x,
    sourceY: a.y,
    sourcePosition: a.lado,
    targetX: b.x,
    targetY: b.y,
    targetPosition: b.lado,
  });
  return d;
}

/**
 * Una tabla que se apunta a sí misma —una jerarquía, un árbol de categorías—.
 *
 * Un bezier de un punto a sí mismo es un punto: no se ve nada. Se dibuja a mano
 * saliendo y volviendo por la derecha.
 */
function bucle(n: InternalNode<Node>, fila: number): string {
  const a = ancla(n, fila, true);
  const salida = 46;
  return `M ${a.x} ${a.y} C ${a.x + salida} ${a.y - 26}, ${a.x + salida} ${a.y + 26}, ${a.x} ${a.y + 2}`;
}
