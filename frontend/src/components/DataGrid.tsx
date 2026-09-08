import { useRef } from "react";
import {
  useTable,
  columnPinningFeature,
  columnResizingFeature,
  columnSizingFeature,
  columnVisibilityFeature,
  coreCellsFeature,
  coreColumnsFeature,
  coreHeadersFeature,
  coreRowModelsFeature,
  coreRowsFeature,
  coreTablesFeature,
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Class } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import type { Column, Result } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import { cx } from "../lib/cx";
import styles from "./DataGrid.module.css";

/** Una celda del resultado, por posición. */
export interface CellRef {
  row: number;
  col: number;
}

export interface SortState {
  column: string;
  descending: boolean;
}

/** Alto de fila y del encabezado, en píxeles. Viene del diseño de S07. */
const ROW_H = 25;
/** Ancho del canal de números de fila. */
const GUTTER_W = 38;

/**
 * Las features de TanStack que usamos, y solo esas.
 *
 * v9 obliga a declararlas: lo que no se pide no se incluye en el bundle. No
 * están rowSorting ni columnFiltering a propósito — ordenar y filtrar son SQL,
 * no estado del cliente, porque la grilla muestra una página de un resultado
 * que puede tener millones de filas. Ordenar en el navegador ordenaría solo lo
 * que ya se trajo, que es la clase de mentira difícil de notar.
 */
const features = {
  coreTablesFeature,
  coreColumnsFeature,
  coreHeadersFeature,
  coreRowsFeature,
  coreCellsFeature,
  coreRowModelsFeature,
  columnSizingFeature,
  columnResizingFeature,
  columnVisibilityFeature,
  columnPinningFeature,
};

/** Etiqueta corta por tipo, como en el encabezado del diseño. */
const TAG: Record<string, string> = {
  [Class.ClassNumber]: "NUM",
  [Class.ClassText]: "TXT",
  [Class.ClassBool]: "BOO",
  [Class.ClassTemporal]: "TS",
  [Class.ClassJSON]: "JSN",
  [Class.ClassBinary]: "BIN",
  [Class.ClassEnum]: "ENM",
  [Class.ClassArray]: "ARR",
  [Class.ClassOther]: "···",
};

/**
 * Ancho por defecto según el tipo.
 *
 * Son anchos de arranque, no definitivos: la columna se puede redimensionar y
 * TanStack guarda el tamaño. Salen del tipo y no del contenido porque el
 * contenido todavía no llegó cuando hay que dibujar el encabezado, y porque
 * medir la página traída daría un ancho que cambia al cargar más filas.
 */
const WIDTH: Record<string, number> = {
  [Class.ClassNumber]: 96,
  [Class.ClassBool]: 72,
  [Class.ClassTemporal]: 164,
  [Class.ClassJSON]: 220,
  [Class.ClassBinary]: 140,
  [Class.ClassEnum]: 124,
  [Class.ClassArray]: 160,
  [Class.ClassText]: 200,
  [Class.ClassOther]: 160,
};

function anchoDe(c: Column): number {
  return WIDTH[c.class] ?? 160;
}

/** Los números se leen alineados a la derecha; el resto, a la izquierda. */
function alineacionDe(c: Column): "left" | "right" {
  return c.class === Class.ClassNumber ? "right" : "left";
}

export function DataGrid({
  result,
  selection,
  onSelect,
  onOpenCell,
  sort,
  onSort,
  onColumnMenu,
  keys,
}: {
  result: Result;
  selection: CellRef | null;
  onSelect: (ref: CellRef) => void;
  onOpenCell?: (ref: CellRef) => void;
  sort?: SortState | null;
  onSort?: (column: string) => void;
  onColumnMenu?: (column: string, index: number, e: React.MouseEvent) => void;
  /** Qué columnas son clave, por nombre.
   *
   *  El resultado de una consulta no lo sabe: `select 1 as id` devuelve un
   *  entero que no es clave de nada. Lo sabe el esquema, y solo cuando se está
   *  mirando una tabla concreta — por eso viene de afuera y es opcional, en vez
   *  de deducirse del nombre de la columna. */
  keys?: Readonly<Record<string, "pk" | "fk">>;
}) {
  const scrollRef = useRef<HTMLDivElement | null>(null);

  const columns = result.columns ?? [];
  // Go construye cada fila con make, así que nunca devuelve una fila nula. El
  // generador de bindings no puede saberlo y la tipa como anulable, así que se
  // normaliza acá y no con un `?? []` repetido en cada uso.
  const rows: (string | null)[][] = (result.rows ?? []).map((f) => f ?? []);

  const table = useTable({
    features,
    data: rows,
    columns: columns.map((c, i) => ({
      id: `${i}:${c.name}`,
      accessorFn: (fila: (string | null)[]) => fila[i] ?? null,
      header: c.name,
      size: anchoDe(c),
      minSize: 48,
    })),
    getRowId: (_fila, i) => String(i),
  });

  const visibles = table.getVisibleLeafColumns();

  // Todas las columnas conservan su ancho y una columna de relleno se come lo
  // que sobra.
  //
  // Antes se estiraba la última, y con un resultado de una sola columna
  // numérica el valor quedaba pegado al borde derecho de la pantalla, a medio
  // metro de su encabezado. Una columna no tiene por qué medir lo que mide la
  // ventana solo porque es la última.
  const template = [
    `${GUTTER_W}px`,
    ...visibles.map((c) => `${c.getSize()}px`),
    "minmax(0, 1fr)",
  ].join(" ");

  const virtual = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_H,
    // Suficientes filas de más para que un desplazamiento con la rueda no
    // muestre huecos en blanco antes de que React alcance a pintar.
    overscan: 12,
  });

  return (
    <div className={styles.scroller} ref={scrollRef} role="grid" aria-rowcount={rows.length}>
      <div className={styles.header} style={{ gridTemplateColumns: template }} role="row">
        <div className={styles.gutterHead} role="columnheader">
          #
        </div>
        {visibles.map((col, i) => {
          const meta = columns[i];
          if (!meta) return null;
          const ordenada = sort?.column === meta.name;
          return (
            <div
              key={col.id}
              role="columnheader"
              aria-sort={ordenada ? (sort?.descending ? "descending" : "ascending") : "none"}
              className={cx(styles.headCell, ordenada && styles.headCellSorted)}
              title={`${meta.name} · ${meta.dataType}`}
              onClick={() => onSort?.(meta.name)}
              {...(onColumnMenu
                ? {
                    onContextMenu: (e: React.MouseEvent) => {
                      e.preventDefault();
                      onColumnMenu(meta.name, i, e);
                    },
                  }
                : {})}
            >
              <span className={cx(styles.tag, keys?.[meta.name] && styles[`tag_${keys[meta.name]}`])}>
                {keys?.[meta.name] === "pk"
                  ? "PK"
                  : keys?.[meta.name] === "fk"
                    ? "FK"
                    : (TAG[meta.class] ?? "···")}
              </span>
              <span className={styles.headName}>{meta.name}</span>
              <span className={styles.spacer} />
              {ordenada ? (
                <span className={styles.sortGlyph}>{sort?.descending ? "▼" : "▲"}</span>
              ) : null}
            </div>
          );
        })}
        <div className={styles.filler} aria-hidden="true" />
      </div>

      <div className={styles.body} style={{ height: virtual.getTotalSize() }}>
        {virtual.getVirtualItems().map((v) => {
          const fila = rows[v.index] ?? [];
          return (
            <div
              key={v.key}
              role="row"
              aria-rowindex={v.index + 1}
              className={cx(styles.row, v.index % 2 === 1 && styles.rowStriped)}
              style={{ gridTemplateColumns: template, transform: `translateY(${v.start}px)` }}
            >
              <div className={styles.gutter}>{v.index + 1}</div>
              {visibles.map((col, i) => {
                const meta = columns[i];
                if (!meta) return null;
                const valor = fila[i] ?? null;
                const puesta = selection?.row === v.index && selection.col === i;
                return (
                  <Celda
                    key={col.id}
                    valor={valor}
                    align={alineacionDe(meta)}
                    selected={puesta}
                    onClick={() => onSelect({ row: v.index, col: i })}
                    {...(onOpenCell
                      ? { onDoubleClick: () => onOpenCell({ row: v.index, col: i }) }
                      : {})}
                  />
                );
              })}
              <div className={styles.filler} aria-hidden="true" />
            </div>
          );
        })}
      </div>
    </div>
  );
}

/**
 * Una celda.
 *
 * NULL lleva marca —`[null]`, en itálica y apagado— y la cadena vacía no lleva
 * ninguna: celda en blanco. Esa asimetría es la que hace la distinción legible,
 * y es la razón de que el valor llegue como `string | null` desde Go.
 *
 * La primera versión dibujaba la cadena vacía como `""` y estaba mal por dos
 * motivos. Uno: dos textos grises casi iguales no se distinguen de un vistazo.
 * Dos, y peor: `""` es un valor posible. Una celda que contiene literalmente
 * dos comillas se habría visto idéntica a una vacía, o sea que la marca
 * inventaba una ambigüedad nueva para resolver otra.
 */
function Celda({
  valor,
  align,
  selected,
  onClick,
  onDoubleClick,
}: {
  valor: string | null;
  align: "left" | "right";
  selected: boolean;
  onClick: () => void;
  onDoubleClick?: () => void;
}) {
  const esNulo = valor === null;
  return (
    <div
      role="gridcell"
      className={cx(styles.cell, selected && styles.cellSelected, esNulo && styles.cellNull)}
      style={{ textAlign: align }}
      onClick={onClick}
      {...(onDoubleClick ? { onDoubleClick } : {})}
    >
      {esNulo ? "[null]" : valor}
    </div>
  );
}
