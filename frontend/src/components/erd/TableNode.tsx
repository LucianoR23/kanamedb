import { Handle, Position } from "@xyflow/react";
import type { NodeProps } from "@xyflow/react";
import { Glyph } from "../ui";
import { cx } from "../../lib/cx";
import type { NodoErd } from "../../lib/erd";
import styles from "./TableNode.module.css";

/**
 * Una tabla en el diagrama.
 *
 * Es un componente de React que se dibuja como DOM, no como canvas, y esa es la
 * razón por la que xyflow ganó sobre las alternativas: la tarjeta usa los mismos
 * tokens que el resto de la aplicación, así que el tema claro de la Iteración 9
 * la va a alcanzar sin escribir una línea. En un canvas habría que releer los
 * colores desde JavaScript y repintar a mano.
 *
 * Los conectores están escondidos y son los cuatro lados: la arista elige cuál
 * usar según dónde quedó cada tabla, para que la línea salga siempre por el lado
 * que mira al otro nodo.
 */
export function TableNode({ data }: NodeProps<NodoErd>) {
  const n = (x: number) => x.toLocaleString("es", { useGrouping: true });

  return (
    <div className={cx(styles.card, data.activa && styles.activa, data.vecina && styles.vecina)}>
      <Handle type="source" position={Position.Left} id="l" className={styles.puerto} />
      <Handle type="source" position={Position.Right} id="r" className={styles.puerto} />
      <Handle type="target" position={Position.Left} id="l" className={styles.puerto} />
      <Handle type="target" position={Position.Right} id="r" className={styles.puerto} />

      <div className={styles.cabecera}>
        <Glyph kind="table" />
        <span className={styles.nombre} title={`${data.schema}.${data.name}`}>
          {data.name}
        </span>
        <span className={styles.filas}>
          {data.rowEstimate >= 0 ? `≈ ${n(data.rowEstimate)}` : ""}
        </span>
      </div>

      {data.columnas.map((c) => (
        <div key={c.name} className={styles.fila}>
          <span
            className={cx(
              styles.clave,
              c.key === "PK" && styles.pk,
              c.key === "FK" && styles.fk,
            )}
          >
            {c.key}
          </span>
          <span className={cx(styles.col, c.key && styles.colClave)} title={c.name}>
            {c.name}
          </span>
          {data.tipos ? <span className={styles.tipo}>{c.dataType}</span> : null}
        </div>
      ))}

      {data.ocultas > 0 ? <div className={styles.resto}>+ {data.ocultas} más</div> : null}
    </div>
  );
}
