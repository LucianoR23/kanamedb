import { useEffect, useState } from "react";
import { Class } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import type { Column, Items } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import * as QueriesSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import { Badge, Button, Dialog, Textarea } from "../components/ui";
import { cx } from "../lib/cx";
import styles from "./CellViewer.module.css";

/**
 * S09 Cell viewer.
 *
 * Iteración 2: solo lectura. Los botones de escritura —Set null, Revert, Stage
 * change— llegan con la grilla editable de la Iteración 7; se muestran
 * deshabilitados y no ocultos, porque el diálogo se diseñó con ellos y sacarlos
 * dejaría un pie vacío.
 */
/** Lo que hace falta para poder editar desde el visor. Sin esto, mira y nada
 *  más: es lo que pasa con el resultado de una consulta. */
export interface EdicionDeCelda {
  /** El valor que quedaría, que puede no ser el leído si ya se editó. */
  valor: string | null;
  /** Ya hay una edición sin preparar sobre esta celda. */
  editado: boolean;
  onCambiar: (v: string | null) => void;
  /** Volver al valor que trajo la base. */
  onRevertir: () => void;
}

export function CellViewer({
  open,
  columns,
  row,
  index,
  rowNumber,
  source,
  modoInicial,
  edicion,
  onIndexChange,
  onClose,
}: {
  open: boolean;
  columns: readonly Column[];
  row: readonly (string | null)[];
  index: number;
  rowNumber: number;
  /** Tabla o consulta de la que salió la fila, para el encabezado. */
  source: string;
  /** Con qué modo abrir. «fila» es el ítem «Ver la fila como JSON». */
  modoInicial?: "celda" | "fila";
  /** Sin esto el visor es de solo lectura. */
  edicion?: EdicionDeCelda;
  onIndexChange: (i: number) => void;
  onClose: () => void;
}) {
  const [modo, setModo] = useState(0);
  // Los elementos de un array los parte Go: el literal de Postgres tiene
  // comas adentro de comillas y escapes, y partirlo acá sería lógica sin tests.
  const [items, setItems] = useState<Items | null>(null);
  const [borrador, setBorrador] = useState<string | null>(null);
  const [fila, setFila] = useState<string | null>(null);

  const col = columns[index];
  const valorLeido = row[index] ?? null;
  const valor = edicion ? edicion.valor : valorLeido;

  useEffect(() => {
    // El borrador arranca en lo que hay cada vez que se cambia de celda.
    setBorrador(null);
  }, [index]);

  useEffect(() => {
    if (!open || col?.class !== Class.ClassArray || valor === null) {
      setItems(null);
      return;
    }
    let vivo = true;
    void QueriesSvc.ArrayItems(valor).then((r) => {
      if (vivo) setItems(r.ok ? r.items : null);
    });
    return () => {
      vivo = false;
    };
  }, [open, col?.class, valor]);

  useEffect(() => {
    if (!open) return;
    let vivo = true;
    void QueriesSvc.RowJSON([...columns], [...row])
      .then((t) => {
        if (vivo) setFila(t);
      })
      .catch(() => {
        if (vivo) setFila(null);
      });
    return () => {
      vivo = false;
    };
  }, [open, columns, row]);

  if (!col) return null;

  const modos = modosDe(col.class, valor, items, fila, columns.length);
  const inicial = modoInicial === "fila" ? modos.findIndex((m) => m.id === "fila") : 0;
  const activo = Math.min(modo === 0 && inicial > 0 ? inicial : modo, modos.length - 1);

  return (
    <Dialog
      open={open}
      title="Visor de celda"
      size="xl"
      onClose={onClose}
      footer={
        edicion ? (
          <>
            <span className={styles.footHint}>
              {borrador !== null
                ? "Sin preparar todavía: «Usar este valor» lo lleva a la celda."
                : edicion.editado
                  ? "Esta celda ya tiene una edición sin preparar."
                  : "Editá el valor acá cuando no entre en una celda de una línea."}
            </span>
            <span className={styles.grow} />
            <Button
              disabled={!edicion.editado && borrador === null}
              onClick={() => {
                setBorrador(null);
                edicion.onRevertir();
              }}
            >
              Volver al valor leído
            </Button>
            <Button
              disabled={borrador === null || borrador === valor}
              variant="primary"
              onClick={() => {
                if (borrador === null) return;
                edicion.onCambiar(borrador);
                setBorrador(null);
              }}
            >
              Usar este valor
            </Button>
          </>
        ) : (
          <>
            <span className={styles.footHint}>
              El resultado de una consulta no se edita: puede venir de varias tablas.
            </span>
            <span className={styles.grow} />
          </>
        )
      }
    >
      <div className={styles.head}>
        <span className={styles.path}>
          {source}.{col.name}
        </span>
        <span className={styles.typeChip}>{col.dataType}</span>
        <span className={styles.rowNo}>fila {rowNumber}</span>
        {valor === null ? <Badge tone="neutral">null</Badge> : null}
      </div>

      <div className={styles.body}>
        <aside className={styles.side}>
          <div className={styles.sideLabel}>Celda</div>
          <div className={styles.sideList}>
            {columns.map((c, i) => (
              <button
                type="button"
                key={c.name + i}
                className={cx(styles.sideItem, i === index && styles.sideItemOn)}
                onClick={() => {
                  onIndexChange(i);
                  setModo(0);
                }}
              >
                <span className={styles.sideTag}>{TAG[c.class] ?? "···"}</span>
                <span className={styles.sideName}>{c.name}</span>
              </button>
            ))}
          </div>
          <dl className={styles.meta}>
            <MetaRow label="tamaño" value={tamanoDe(valor)} />
            <MetaRow label="codificación" value={codificacionDe(col.class, valor)} />
            <MetaRow label="tipo" value={col.dataType} />
          </dl>
        </aside>

        <section className={styles.main}>
          <div className={styles.modes}>
            {modos.map((m, i) => (
              <button
                type="button"
                key={m.id}
                className={cx(styles.mode, i === activo && styles.modeOn)}
                onClick={() => setModo(i)}
              >
                {m.label}
              </button>
            ))}
            <span className={styles.grow} />
            <span className={styles.viewMeta}>{modos[activo]?.meta ?? ""}</span>
            <button
              type="button"
              className={styles.link}
              onClick={() => void navigator.clipboard.writeText(valor ?? "")}
              disabled={valor === null}
            >
              Copiar
            </button>
          </div>

          <div className={styles.content}>
            {/* El editor reemplaza al modo que muestra el valor tal cual —«Texto»
                para lo común, «Crudo» para lo que tiene una vista formateada—.
                En los demás modos se sigue mirando: editar un array por su
                lista de elementos, o la fila entera, es otra cosa. */}
            {edicion && esModoDeValor(modos[activo]?.id) ? (
              <Textarea
                className={styles.editor}
                value={borrador ?? valor ?? ""}
                aria-label="Valor de la celda"
                spellCheck={false}
                onChange={(e) => setBorrador(e.target.value)}
              />
            ) : (
              (modos[activo]?.render() ?? null)
            )}
          </div>
        </section>
      </div>
    </Dialog>
  );
}

/** Los modos que muestran el valor tal cual, que son los que se pueden editar. */
const esModoDeValor = (id: string | undefined) => id === "text" || id === "raw";

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

interface Modo {
  id: string;
  label: string;
  meta: string;
  render: () => React.ReactNode;
}

/**
 * Los modos de vista dependen del tipo, como en el diseño.
 *
 * No está el modo "Items" de los arrays: descomponer un literal de array de
 * Postgres —`{a,"b,c",NULL}`— tiene casos borde con comillas y escapes, y
 * escribirlo acá sería lógica sin tests. El array se muestra crudo hasta que la
 * Iteración 7 necesite editarlo elemento por elemento, y ahí el parseo va en Go
 * con sus pruebas. Mostrar el literal es correcto; mostrarlo mal partido, no.
 */
function modosDe(
  clase: string,
  valor: string | null,
  items: Items | null,
  fila: string | null,
  columnas: number,
): Modo[] {
  const modoFila: Modo[] =
    fila === null || columnas < 2
      ? []
      : [
          {
            id: "fila",
            label: "La fila",
            meta: `${columnas} columnas · JSON`,
            render: () => <pre className={styles.pre}>{fila}</pre>,
          },
        ];
  return [...modosDeCelda(clase, valor, items), ...modoFila];
}

function modosDeCelda(clase: string, valor: string | null, items: Items | null): Modo[] {
  if (valor === null) {
    return [
      {
        id: "null",
        label: "Valor",
        meta: "null",
        render: () => (
          <div className={styles.nullBox}>
            <span className={styles.nullTag}>[null]</span>
            <span className={styles.nullNote}>sin valor almacenado</span>
            <p className={styles.nullExplain}>
              Esta celda no tiene valor. Una cadena vacía sería un valor distinto: en la grilla se
              ve como celda en blanco, sin la marca <code>[null]</code>.
            </p>
          </div>
        ),
      },
    ];
  }

  const crudo: Modo = {
    id: "raw",
    label: "Crudo",
    meta: `${valor.length} caracteres`,
    render: () => <pre className={styles.pre}>{valor}</pre>,
  };

  if (clase === Class.ClassJSON) {
    let formateado: string | null = null;
    try {
      formateado = JSON.stringify(JSON.parse(valor), null, 2);
    } catch {
      // Un JSON que no parsea es un dato válido igual: la columna puede ser
      // json y contener algo que otro cliente escribió mal. Se cae al modo
      // crudo en vez de mostrar un error, que no ayudaría a leerlo.
      formateado = null;
    }
    if (formateado === null) return [crudo];
    return [
      {
        id: "fmt",
        label: "Formateado",
        meta: `${formateado.split("\n").length} líneas · JSON válido`,
        render: () => <pre className={styles.pre}>{formateado}</pre>,
      },
      crudo,
    ];
  }

  if (clase === Class.ClassArray && items !== null) {
    const n = items.values?.length ?? 0;
    return [
      {
        id: "items",
        label: "Elementos",
        meta: `${n} ${n === 1 ? "elemento" : "elementos"}`,
        render: () => (
          <ol className={styles.items}>
            {(items.values ?? []).map((v, i) => (
              <li key={i} className={styles.item}>
                <span className={styles.itemNo}>{i + 1}</span>
                {items.nulls?.[i] ? (
                  <span className={styles.itemNull}>[null]</span>
                ) : v === "" ? (
                  <span className={styles.itemVacio}>cadena vacía</span>
                ) : (
                  <span className={styles.itemValor}>{v}</span>
                )}
              </li>
            ))}
            {n === 0 ? <li className={styles.itemVacio}>el array está vacío</li> : null}
          </ol>
        ),
      },
      crudo,
    ];
  }

  if (clase === Class.ClassBinary) {
    // Postgres devuelve bytea en formato hex: \x seguido de dos dígitos por
    // byte. Cortarlo de a dos no tiene casos borde.
    const hex = valor.startsWith("\\x") ? valor.slice(2) : valor;
    const bytes = hex.match(/.{1,2}/g) ?? [];
    return [
      {
        id: "hex",
        label: "Hex",
        meta: `${bytes.length} bytes`,
        render: () => (
          <div className={styles.hex}>
            {agruparEnLineas(bytes, 16).map((linea, i) => (
              <div className={styles.hexLine} key={i}>
                <span className={styles.hexOff}>{(i * 16).toString(16).padStart(8, "0")}</span>
                <span className={styles.hexBytes}>{linea.join(" ")}</span>
              </div>
            ))}
          </div>
        ),
      },
      crudo,
    ];
  }

  return [
    {
      id: "text",
      label: "Texto",
      meta: `${valor.split("\n").length} líneas · ${valor.length} caracteres`,
      render: () => <div className={styles.text}>{valor}</div>,
    },
    crudo,
  ];
}

function agruparEnLineas<T>(xs: T[], n: number): T[][] {
  const out: T[][] = [];
  for (let i = 0; i < xs.length; i += n) out.push(xs.slice(i, i + n));
  return out;
}

/** Bytes reales de la representación, no caracteres: un carácter puede pesar más de uno. */
function tamanoDe(valor: string | null): string {
  if (valor === null) return "—";
  const n = new TextEncoder().encode(valor).length;
  return n < 1024 ? `${n} B` : `${(n / 1024).toFixed(1)} kB`;
}

function codificacionDe(clase: string, valor: string | null): string {
  if (valor === null) return "—";
  return clase === Class.ClassBinary ? "hex" : "utf-8";
}

function MetaRow({ label, value }: { label: string; value: string }) {
  return (
    <div className={styles.metaRow}>
      <dt className={styles.metaKey}>{label}</dt>
      <dd className={styles.metaValue}>{value}</dd>
    </div>
  );
}
