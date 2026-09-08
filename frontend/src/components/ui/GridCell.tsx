import { cx } from "../../lib/cx";
import styles from "./GridCell.module.css";

/** Marcador de que una celda es NULL. `null` de JS es ambiguo cruzando el
 *  puente con Go, y una cadena vacía es un valor legítimo y distinto. */
export const NULL_VALUE = Symbol("sql-null");
export type CellValue = string | typeof NULL_VALUE;

export type CellState = "normal" | "modified" | "deleted";

export function GridCell({
  value,
  state = "normal",
  /** El motor lo genera al insertar: [default], now(), nextval(...). */
  generated = false,
}: {
  value: CellValue;
  state?: CellState;
  generated?: boolean;
}) {
  const isNull = value === NULL_VALUE;
  return (
    <span
      className={cx(
        styles.cell,
        isNull && styles.null,
        generated && styles.generated,
        state === "modified" && styles.modified,
        state === "deleted" && styles.deleted,
      )}
      title={isNull ? "NULL" : value === "" ? "cadena vacía" : value}
    >
      {isNull ? "[null]" : value}
    </span>
  );
}

export type ColumnTag = "PK" | "FK" | "ENM" | "NUM" | "TS" | "TXT" | "JSON" | "BIN";

export function GridHeaderCell({
  name,
  tag,
  sort,
}: {
  name: string;
  tag?: ColumnTag;
  sort?: "asc" | "desc";
}) {
  return (
    <div className={styles.headerCell}>
      {tag ? (
        <span
          className={cx(
            styles.typeTag,
            tag === "PK" && styles.tagPk,
            tag === "FK" && styles.tagFk,
          )}
        >
          {tag}
        </span>
      ) : null}
      <span className={styles.colName}>{name}</span>
      {sort ? (
        <>
          <span style={{ flex: 1 }} />
          <span className={styles.sort} aria-hidden="true">
            {sort === "asc" ? "▲" : "▼"}
          </span>
        </>
      ) : null}
    </div>
  );
}

export type RowState = "normal" | "modified" | "added" | "deleted";

/** Columna de números de fila. El signo dice qué le va a pasar a la fila. */
export function GridGutter({ n, state = "normal" }: { n: number; state?: RowState }) {
  const label = state === "added" ? "+" : state === "deleted" ? "−" : String(n);
  return (
    <div
      className={cx(
        styles.gutter,
        state === "modified" && styles.gutterModified,
        state === "added" && styles.gutterAdded,
        state === "deleted" && styles.gutterDeleted,
      )}
    >
      {label}
    </div>
  );
}

export function GridRow({
  template,
  state = "normal",
  children,
}: {
  /** grid-template-columns, con el ancho del gutter primero. */
  template: string;
  state?: RowState;
  children: React.ReactNode;
}) {
  return (
    <div
      className={cx(
        styles.row,
        state === "added" && styles.rowAdded,
        state === "deleted" && styles.rowDeleted,
      )}
      style={{ gridTemplateColumns: template }}
    >
      {children}
    </div>
  );
}
