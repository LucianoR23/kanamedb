import { Glyph } from "./Glyph";
import type { ObjectKind } from "./Glyph";
import { cx } from "../../lib/cx";
import styles from "./TreeRow.module.css";

const DEPTH = [styles.depth0, styles.depth1, styles.depth2, styles.depth3];

interface TreeRowProps {
  label: string;
  kind: ObjectKind;
  /** 0 para esquemas, 1 para objetos dentro de un esquema. */
  depth?: number;
  selected?: boolean;
  /** Filas expandibles muestran el chevron; `undefined` no lo muestra. */
  expanded?: boolean;
  /** Conteo de filas, tipo de objeto, lo que la fila quiera decir a la derecha. */
  meta?: string;
  /** Explicación del `meta` al pasar por encima. Algunos son ambiguos por lo
   *  cortos que son —«sin analizar» se lee como «vacía»— y la fila no tiene
   *  lugar para más texto. */
  metaTitle?: string;
  /** Tiene cambios pendientes en el changeset. */
  dirty?: boolean;
  loading?: boolean;
  onClick?: () => void;
  onContextMenu?: (e: React.MouseEvent) => void;
}

export function TreeRow({
  label,
  kind,
  depth = 0,
  selected = false,
  expanded,
  meta,
  metaTitle,
  dirty = false,
  loading = false,
  onClick,
  onContextMenu,
}: TreeRowProps) {
  const isSchema = kind === "schema";
  return (
    <div
      role="treeitem"
      tabIndex={0}
      aria-selected={selected}
      aria-expanded={expanded}
      aria-level={depth + 1}
      className={cx(
        styles.row,
        DEPTH[Math.min(depth, DEPTH.length - 1)],
        selected && styles.selected,
        loading && styles.loading,
      )}
      onClick={onClick}
      onContextMenu={onContextMenu}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          onClick?.();
        }
      }}
    >
      <span
        className={cx(styles.caret, expanded && styles.open)}
        aria-hidden="true"
      >
        {expanded === undefined ? "" : "▶"}
      </span>
      {isSchema ? null : <Glyph kind={kind} muted={loading} />}
      <span className={isSchema ? styles.labelUi : styles.labelMono}>{label}</span>
      <span className={styles.spacer} />
      {loading ? <span className={styles.loadingText}>cargando…</span> : null}
      {dirty && !loading ? (
        <span className={styles.dirty} title="Cambios pendientes" />
      ) : null}
      {meta && !loading ? (
        <span className={styles.meta} {...(metaTitle ? { title: metaTitle } : {})}>
          {meta}
        </span>
      ) : null}
    </div>
  );
}
