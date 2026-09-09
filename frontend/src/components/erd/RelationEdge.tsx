import { EdgeLabelRenderer, getBezierPath, Position, useInternalNode } from "@xyflow/react";
import type { EdgeProps, InternalNode, Node } from "@xyflow/react";
import type { AristaErd, DatosDeArista } from "../../lib/erd";
import { NODO_CABECERA, NODO_FILA } from "../../lib/erd";
import { cx } from "../../lib/cx";
import styles from "./RelationEdge.module.css";

/**
 * Cuánto se retira el arranque de la línea del borde de la tarjeta.
 *
 * Es el largo de la pata de gallo. La línea no llega hasta la caja: termina en
 * el vértice de la pata, y los tres dedos hacen el último tramo. Sin este
 * retiro, la línea corre por debajo del marcador y toca la caja igual, que es
 * exactamente lo que se ve como «la línea se conecta al recuadro».
 *
 * Tiene que coincidir con la geometría del marcador en ErdScreen.
 */
export const RETIRO_PATA = 12;

/** Cuánto se separan entre sí dos claves que unen el mismo par de tablas. */
const SEPARACION_PARALELAS = 11;

/** Los tres marcadores de pata de gallo, uno por color. Se declaran una vez en
 *  el canvas; acá solo se elige cuál usar. */
export const MARCADOR = {
  normal: "kn-muchos",
  activo: "kn-muchos-activo",
  cascada: "kn-muchos-cascada",
  nueva: "kn-muchos-nueva",
  borrando: "kn-muchos-borrando",
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
  // El estado pendiente manda sobre todo lo demás: que una relación esté por
  // aparecer o por desaparecer importa más que su acción al borrar.
  const tono: keyof typeof MARCADOR =
    data.estado === "agregada"
      ? "nueva"
      : data.estado === "borrando"
        ? "borrando"
        : data.activa
          ? "activo"
          : fk.onDelete === "cascade"
            ? "cascada"
            : "normal";
  const clase =
    data.estado === "agregada"
      ? styles.nueva
      : data.estado === "borrando"
        ? styles.borrando
        : data.activa
          ? styles.activa
          : fk.onDelete === "cascade"
            ? styles.cascada
            : styles.normal;
  const punteada = data.estado === "borrando" ? "6 4" : fk.optional ? "5 4" : undefined;

  const corrimiento = corrimientoParalelo(data);
  const trazo =
    source === target
      ? bucle(origen, data.filaOrigen, data.filaDestino, corrimiento)
      : entreNodos(origen, destino, data.filaOrigen, data.filaDestino, corrimiento);

  const columnas = fk.columns ?? [];
  const remotas = fk.refColumns ?? [];
  const compuesta = columnas.length > 1;
  const etiqueta =
    data.estado === "agregada"
      ? "clave nueva"
      : data.estado === "borrando"
        ? "se borra"
        : compuesta
          ? columnas.join(" + ")
          : "";
  const detalle = `${fk.name}\n${columnas.join(", ")} → ${fk.refTable}.${remotas.join(", ")}\nal borrar: ${fk.onDelete}`;

  return (
    <>
      {/* Una línea de 1,4px es casi imposible de acertar con el mouse. Esta va
          transparente y ancha, solo para que el hover y el clic tengan blanco. */}
      <path d={trazo.d} className={styles.zonaClic} />
      <path
        id={id}
        d={trazo.d}
        className={clase}
        strokeDasharray={punteada}
        markerStart={`url(#${MARCADOR[tono]})`}
      >
        <title>{detalle}</title>
      </path>

      {/* Una clave compuesta se dibuja con UNA línea, igual que una de una sola
          columna: sin decirlo, son indistinguibles. La etiqueta es lo único que
          revela que esa relación empareja dos columnas y no una. */}
      {etiqueta ? (
        <EdgeLabelRenderer>
          <div
            className={cx(
              styles.etiqueta,
              data.activa && styles.etiquetaActiva,
              data.estado === "agregada" && styles.etiquetaNueva,
              data.estado === "borrando" && styles.etiquetaBorrando,
            )}
            style={{ transform: `translate(-50%, -50%) translate(${trazo.cx}px, ${trazo.cy}px)` }}
            title={detalle}
          >
            {etiqueta}
          </div>
        </EdgeLabelRenderer>
      ) : null}
    </>
  );
}

/**
 * Cuánto se corre esta clave para no taparse con las otras del mismo par.
 *
 * Dos claves entre las mismas dos tablas casi siempre apuntan a la misma columna
 * del otro lado —`autor` y `revisor` van los dos a `id`—, así que convergen en
 * un punto y el último tramo queda superpuesto. Se reparten alrededor del
 * centro: con dos, una para cada lado.
 */
function corrimientoParalelo(data: DatosDeArista): number {
  if (data.paralelas < 2) return 0;
  return (data.indiceParalela - (data.paralelas - 1) / 2) * SEPARACION_PARALELAS;
}

/** El trazo y dónde poner su etiqueta. */
interface Trazo {
  d: string;
  cx: number;
  cy: number;
}

interface Ancla {
  x: number;
  y: number;
  lado: Position;
}

interface Caja {
  x: number;
  y: number;
  ancho: number;
  alto: number;
}

function caja(n: InternalNode<Node>): Caja {
  const { x, y } = n.internals.positionAbsolute;
  return {
    x,
    y,
    ancho: n.measured?.width ?? n.width ?? 0,
    alto: n.measured?.height ?? n.height ?? 0,
  };
}

/**
 * Dónde nace o muere la línea en una tarjeta.
 *
 * Por los costados apunta a la FILA de la columna, que es lo que distingue tres
 * claves foráneas de la misma tabla. Por arriba y por abajo no puede: ahí la
 * coordenada que manda es la horizontal y la fila no tiene dónde expresarse, así
 * que sale del centro.
 */
function ancla(n: InternalNode<Node>, fila: number, lado: Position): Ancla {
  const c = caja(n);
  const filaY =
    fila >= 0 ? c.y + NODO_CABECERA + fila * NODO_FILA + NODO_FILA / 2 : c.y + c.alto / 2;

  switch (lado) {
    case Position.Left:
      return { x: c.x, y: filaY, lado };
    case Position.Right:
      return { x: c.x + c.ancho, y: filaY, lado };
    case Position.Top:
      return { x: c.x + c.ancho / 2, y: c.y, lado };
    default:
      return { x: c.x + c.ancho / 2, y: c.y + c.alto, lado };
  }
}

/** Corre un ancla a lo largo del borde en el que está, sin despegarla. */
function correr(a: Ancla, cuanto: number): Ancla {
  if (cuanto === 0) return a;
  const horizontal = a.lado === Position.Top || a.lado === Position.Bottom;
  return horizontal ? { ...a, x: a.x + cuanto } : { ...a, y: a.y + cuanto };
}

/** Aparta un ancla del borde, para dejarle lugar al marcador. */
function retirar(a: Ancla, cuanto: number): Ancla {
  switch (a.lado) {
    case Position.Left:
      return { ...a, x: a.x - cuanto };
    case Position.Right:
      return { ...a, x: a.x + cuanto };
    case Position.Top:
      return { ...a, y: a.y - cuanto };
    default:
      return { ...a, y: a.y + cuanto };
  }
}

/**
 * Por qué lados conviene unir dos tarjetas.
 *
 * Se compara la separación ENTRE LAS CAJAS, no entre sus centros. Dos tablas
 * apiladas una arriba de la otra tienen centros casi alineados en horizontal, y
 * unirlas por los costados obliga a la línea a salir, bajar por afuera y volver:
 * es el rodeo feo que hacía `envios`. Si se solapan en horizontal y hay hueco en
 * vertical, la línea corta derecho por arriba o por abajo.
 *
 * Un hueco negativo significa que las cajas se solapan en ese eje. Comparar los
 * dos huecos con signo elige bien también cuando se solapan en los dos.
 */
function ladosEntre(a: Caja, b: Caja): [Position, Position] {
  const huecoX = Math.max(a.x - (b.x + b.ancho), b.x - (a.x + a.ancho));
  const huecoY = Math.max(a.y - (b.y + b.alto), b.y - (a.y + a.alto));

  if (huecoY > huecoX) {
    return b.y > a.y ? [Position.Bottom, Position.Top] : [Position.Top, Position.Bottom];
  }
  return b.x >= a.x ? [Position.Right, Position.Left] : [Position.Left, Position.Right];
}

function entreNodos(
  origen: InternalNode<Node>,
  destino: InternalNode<Node>,
  filaOrigen: number,
  filaDestino: number,
  corrimiento: number,
): Trazo {
  const [ladoA, ladoB] = ladosEntre(caja(origen), caja(destino));
  // El origen se retira para dejarle lugar al marcador, y las dos puntas se
  // corren a lo largo de su borde cuando hay más de una clave entre estas dos
  // tablas: correr una sola punta las cruzaría en vez de separarlas.
  const a = retirar(correr(ancla(origen, filaOrigen, ladoA), corrimiento), RETIRO_PATA);
  const b = correr(ancla(destino, filaDestino, ladoB), corrimiento);

  const [d, centroX, centroY] = getBezierPath({
    sourceX: a.x,
    sourceY: a.y,
    sourcePosition: a.lado,
    targetX: b.x,
    targetY: b.y,
    targetPosition: b.lado,
  });
  return { d, cx: centroX, cy: centroY };
}

/**
 * Una tabla que se apunta a sí misma —una jerarquía, un árbol de categorías—.
 *
 * Une las DOS filas involucradas, no un punto consigo mismo: en `categorias`,
 * `padre_id` sale y `id` entra, que son filas distintas de la misma tarjeta. La
 * primera versión salía y volvía casi al mismo punto, así que la punta de la
 * flecha caía encima de la línea de vuelta y se veía corrida.
 *
 * Cuando las dos filas coinciden —o ninguna está a la vista— se separan a mano:
 * un lazo de altura cero no se ve.
 */
function bucle(
  n: InternalNode<Node>,
  filaOrigen: number,
  filaDestino: number,
  corrimiento: number,
): Trazo {
  const a = retirar(correr(ancla(n, filaOrigen, Position.Right), corrimiento), RETIRO_PATA);
  const b = correr(ancla(n, filaDestino, Position.Right), corrimiento);

  const desde = a.y;
  const hasta = Math.abs(b.y - a.y) < 8 ? b.y + 18 : b.y;

  // El ancho del lazo crece con la separación vertical para que no quede
  // aplastado cuando las filas están lejos, pero con tope: un lazo de doscientos
  // píxeles se lleva por delante la tabla de al lado.
  const salida = Math.min(72, 34 + Math.abs(hasta - desde) * 0.25);

  return {
    d: `M ${a.x} ${desde} C ${a.x + salida} ${desde}, ${b.x + salida} ${hasta}, ${b.x} ${hasta}`,
    cx: Math.max(a.x, b.x) + salida * 0.62,
    cy: (desde + hasta) / 2,
  };
}
