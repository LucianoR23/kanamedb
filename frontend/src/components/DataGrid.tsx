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
import type { Edicion } from "../lib/edicion";
import { CellEditor } from "./ui";
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

/**
 * Lo que la grilla necesita para editar. Todo el estado vive afuera: la grilla
 * dibuja y avisa, no decide.
 */
export interface GridEdit {
  estado: Edicion;
  /** La celda que tiene el editor abierto. */
  editando: CellRef | null;
  /** Cambia con cada apertura del editor. Es la `key` del editor: sin ella,
   *  cerrar y volver a abrir la misma celda en el mismo tick reutilizaba la
   *  instancia ya cerrada, y lo que se escribía después no se confirmaba. */
  editorKey: number;
  /** Doble clic, o Enter sobre la celda seleccionada. */
  onEditar: (ref: CellRef) => void;
  onConfirmar: (ref: CellRef, valor: string) => void;
  onCancelar: () => void;
  /** Clic derecho sobre una celda. */
  onCellMenu?: (ref: CellRef, e: React.MouseEvent) => void;
  /** Recibe el elemento de la grilla, para poder enfocarla desde afuera:
   *  después de «Agregar fila», el Enter tiene que abrir el editor y no
   *  volver a apretar el botón. */
  gridRef?: React.RefObject<HTMLDivElement | null>;
}

/** Cómo está una celda: leída tal cual, editada, o —en una fila nueva— sin
 *  cargar, con el valor por defecto de la tabla. */
export type EstadoCelda = "normal" | "editada" | "porDefecto";
/** Cómo está una fila: leída, con celdas editadas, marcada para borrar, o nueva. */
export type EstadoFila = "normal" | "editada" | "borrada" | "nueva";

export function DataGrid({
  result,
  selection,
  onSelect,
  onOpenCell,
  sort,
  onSort,
  onColumnMenu,
  keys,
  edit,
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
  /** Con esto la grilla se puede editar. Sin esto es la de solo lectura de S07. */
  edit?: GridEdit;
}) {
  const scrollRef = useRef<HTMLDivElement | null>(null);

  const columns = result.columns ?? [];
  // Go construye cada fila con make, así que nunca devuelve una fila nula. El
  // generador de bindings no puede saberlo y la tipa como anulable, así que se
  // normaliza acá y no con un `?? []` repetido en cada uso.
  const rows: (string | null)[][] = (result.rows ?? []).map((f) => f ?? []);
  // Las filas nuevas van al final, después de las leídas, y se numeran con «+».
  // No se mezclan en `rows`: una celda sin cargar de una fila nueva no es NULL
  // ni vacía, es «por defecto» —el valor lo va a poner la base— y eso no se
  // puede representar con un string sin inventar un centinela que choque con
  // un valor real.
  const nuevas = edit?.estado.nuevas ?? [];
  const total = rows.length + nuevas.length;

  // Qué mostrar en (fila, col) y en qué estado. Una sola función para que la
  // celda y el editor no puedan discrepar sobre cuál es el valor vigente.
  function celdaEn(fila: number, col: number): { valor: string | null; estado: EstadoCelda } {
    if (fila >= rows.length) {
      const n = nuevas[fila - rows.length];
      if (n && n.has(col)) return { valor: n.get(col) ?? null, estado: "normal" };
      return { valor: null, estado: "porDefecto" };
    }
    const editada = edit?.estado.celdas.get(fila);
    if (editada && editada.has(col)) return { valor: editada.get(col) ?? null, estado: "editada" };
    return { valor: rows[fila]?.[col] ?? null, estado: "normal" };
  }
  function estadoDeFila(fila: number): EstadoFila {
    if (fila >= rows.length) return "nueva";
    if (edit?.estado.borradas.has(fila)) return "borrada";
    if (edit?.estado.celdas.has(fila)) return "editada";
    return "normal";
  }

  // Elegir una celda también enfoca la grilla, sin desplazarla: es lo que hace
  // que el Enter siguiente abra el editor en vez de perderse.
  function seleccionar(ref: CellRef) {
    scrollRef.current?.focus({ preventScroll: true });
    onSelect(ref);
  }

  const table = useTable({
    features,
    // El ancho se aplica mientras se arrastra, no al soltar.
    //
    // El default de TanStack es "onEnd", que evita recalcular en cada píxel.
    // Acá no hace falta ese cuidado: el ancho vive en una sola cadena de
    // grid-template-columns y moverlo es un reflow de la grilla, no volver a
    // renderizar las filas. Y ajustar una columna a ojo sin ver el resultado
    // hasta soltar significa soltar, mirar, y volver a agarrar.
    columnResizeMode: "onChange",
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

  // Se recorren los headers y no las columnas porque el tirador de
  // redimensionado cuelga del header, no de la columna.
  const headers = table.getHeaderGroups()[0]?.headers ?? [];

  // Todas las columnas conservan su ancho y una columna de relleno se come lo
  // que sobra.
  //
  // Antes se estiraba la última, y con un resultado de una sola columna
  // numérica el valor quedaba pegado al borde derecho de la pantalla, a medio
  // metro de su encabezado. Una columna no tiene por qué medir lo que mide la
  // ventana solo porque es la última.
  const template = [
    `${GUTTER_W}px`,
    ...headers.map((h) => `${h.column.getSize()}px`),
    "minmax(0, 1fr)",
  ].join(" ");

  const virtual = useVirtualizer({
    count: total,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_H,
    // Suficientes filas de más para que un desplazamiento con la rueda no
    // muestre huecos en blanco antes de que React alcance a pintar.
    overscan: 12,
  });

  return (
    <div
      className={styles.scroller}
      ref={(el) => {
        scrollRef.current = el;
        if (edit?.gridRef) edit.gridRef.current = el;
      }}
      role="grid"
      aria-rowcount={total}
      // Enfocable para que las teclas —Enter para editar, Escape— lleguen a
      // quien escucha arriba. Las celdas son divs y no se enfocan solas.
      tabIndex={0}
    >
      <div className={styles.header} style={{ gridTemplateColumns: template }} role="row">
        <div className={styles.gutterHead} role="columnheader">
          #
        </div>
        {headers.map((h, i) => {
          const meta = columns[i];
          if (!meta) return null;
          const ordenada = sort?.column === meta.name;
          const clave = keys?.[meta.name];
          return (
            <div
              key={h.id}
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
              <span className={cx(styles.tag, clave && styles[`tag_${clave}`])}>
                {clave === "pk" ? "PK" : clave === "fk" ? "FK" : (TAG[meta.class] ?? "···")}
              </span>
              <span className={styles.headName}>{meta.name}</span>
              <span className={styles.spacer} />
              {ordenada ? (
                <span className={styles.sortGlyph}>{sort?.descending ? "▼" : "▲"}</span>
              ) : null}
              {/* El tirador se lleva el clic para que arrastrar el borde no
                  ordene la columna sin querer. */}
              <span
                className={cx(styles.resizer, h.column.getIsResizing() && styles.resizerOn)}
                onMouseDown={h.getResizeHandler()}
                onTouchStart={h.getResizeHandler()}
                onClick={(e) => e.stopPropagation()}
                onDoubleClick={(e) => {
                  e.stopPropagation();
                  h.column.resetSize();
                }}
                role="separator"
                aria-orientation="vertical"
                aria-label={`Ancho de ${meta.name}`}
              />
            </div>
          );
        })}
        <div className={styles.filler} aria-hidden="true" />
      </div>

      <div className={styles.body} style={{ height: virtual.getTotalSize() }}>
        {virtual.getVirtualItems().map((v) => {
          const estadoFila = estadoDeFila(v.index);
          return (
            <div
              key={v.key}
              role="row"
              aria-rowindex={v.index + 1}
              className={cx(
                styles.row,
                v.index % 2 === 1 && estadoFila === "normal" && styles.rowStriped,
                estadoFila === "borrada" && styles.rowBorrada,
                estadoFila === "nueva" && styles.rowNueva,
              )}
              style={{ gridTemplateColumns: template, transform: `translateY(${v.start}px)` }}
            >
              <div
                className={cx(
                  styles.gutter,
                  estadoFila === "editada" && styles.gutterEditada,
                  estadoFila === "borrada" && styles.gutterBorrada,
                  estadoFila === "nueva" && styles.gutterNueva,
                )}
              >
                {estadoFila === "nueva" ? "+" : estadoFila === "borrada" ? "−" : v.index + 1}
              </div>
              {headers.map((h, i) => {
                const meta = columns[i];
                if (!meta) return null;
                const ref = { row: v.index, col: i };
                const { valor, estado } = celdaEn(v.index, i);
                const puesta = selection?.row === v.index && selection.col === i;
                const editando = edit?.editando?.row === v.index && edit.editando.col === i;
                // Doble clic: con edición abre el editor; sin ella, el visor.
                const dobleClic = edit
                  ? estadoFila === "borrada"
                    ? undefined
                    : () => edit.onEditar(ref)
                  : onOpenCell
                    ? () => onOpenCell(ref)
                    : undefined;
                return (
                  <Celda
                    key={h.id}
                    valor={valor}
                    estado={estado}
                    borrada={estadoFila === "borrada"}
                    align={alineacionDe(meta)}
                    selected={puesta}
                    editor={
                      editando && edit ? (
                        <CellEditor
                          key={edit.editorKey}
                          initial={valor ?? ""}
                          align={alineacionDe(meta)}
                          onCommit={(texto) => edit.onConfirmar(ref, texto)}
                          onCancel={edit.onCancelar}
                        />
                      ) : null
                    }
                    onClick={() => seleccionar(ref)}
                    {...(dobleClic ? { onDoubleClick: dobleClic } : {})}
                    {...(edit?.onCellMenu
                      ? {
                          onContextMenu: (e: React.MouseEvent) => {
                            e.preventDefault();
                            seleccionar(ref);
                            edit.onCellMenu?.(ref, e);
                          },
                        }
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
  estado,
  borrada,
  align,
  selected,
  editor,
  onClick,
  onDoubleClick,
  onContextMenu,
}: {
  valor: string | null;
  estado: EstadoCelda;
  /** La fila entera está marcada para borrar. */
  borrada: boolean;
  align: "left" | "right";
  selected: boolean;
  /** El editor en línea, cuando esta celda es la que se está editando. */
  editor: React.ReactNode;
  onClick: () => void;
  onDoubleClick?: () => void;
  onContextMenu?: (e: React.MouseEvent) => void;
}) {
  const porDefecto = estado === "porDefecto";
  const esNulo = valor === null && !porDefecto;
  // El valor entero al pasar el mouse. Un texto largo se recorta con puntos
  // suspensivos, y sin esto la única forma de leerlo sería abrir el visor de
  // celda, que es demasiado para confirmar de un vistazo qué dice.
  //
  // En NULL no va: "[null]" no está recortado, es todo el valor, y un globo
  // que repite lo que ya se lee es ruido. En «por defecto» va la explicación.
  const titulo = porDefecto
    ? { title: "Sin valor: la base pone el suyo por defecto" }
    : esNulo
      ? {}
      : { title: valor ?? "" };
  return (
    <div
      role="gridcell"
      className={cx(
        styles.cell,
        selected && styles.cellSelected,
        esNulo && styles.cellNull,
        porDefecto && styles.cellPorDefecto,
        estado === "editada" && styles.cellEditada,
        borrada && styles.cellBorrada,
        editor !== null && styles.cellEditando,
      )}
      style={{ textAlign: align }}
      onClick={onClick}
      {...(onDoubleClick ? { onDoubleClick } : {})}
      {...(onContextMenu ? { onContextMenu } : {})}
      {...titulo}
    >
      {editor ?? (porDefecto ? "[default]" : esNulo ? "[null]" : valor)}
    </div>
  );
}
