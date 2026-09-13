import type { Column } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import { cx } from "../lib/cx";
import { useMantener } from "./useMantener";
import styles from "./mobile.module.css";

export type Valor = string | null;

/** Cuántas columnas muestra una tarjeta antes de «y N más». */
const MAX_CAMPOS = 6;

/**
 * Una fila como tarjeta: nombre de columna y valor, las primeras seis. Es la
 * unidad de la interfaz del teléfono en lugar de la celda de la grilla. Los
 * valores son datos no confiables: van como texto, nunca como HTML.
 *
 * Tocar la tarjeta es de quien la usa (`onClick`); mantener apretado un campo
 * avisa cuál (`onMantener`), y el dueño decide qué mostrar —la hoja con el
 * valor entero y «Copiar»—. La hoja no vive acá porque la tarjeta suele ser un
 * botón, y un diálogo adentro de un botón le manda los clics al botón.
 */
export function Tarjeta({
  columnas,
  fila,
  clave,
  onClick,
  onMantener,
}: {
  columnas: Column[];
  fila: Valor[];
  /** Nombres de las columnas de la clave, para marcarlas. */
  clave?: ReadonlySet<string>;
  onClick?: () => void;
  /** Mantener apretado un campo, por índice de columna. */
  onMantener?: (i: number) => void;
}) {
  const visibles = columnas.slice(0, MAX_CAMPOS);
  const resto = columnas.length - visibles.length;
  const mantener = useMantener((el) => {
    const i = Number(el.dataset["mantener"]);
    if (Number.isInteger(i) && onMantener) onMantener(i);
  });
  const contenido = (
    <>
      <dl className={styles.campos} {...(onMantener ? mantener : {})}>
        {visibles.map((c, i) => (
          <Campo key={c.name} indice={i} nombre={c.name} valor={fila[i] ?? null} esClave={clave?.has(c.name) ?? false} />
        ))}
      </dl>
      {resto > 0 ? <span className={styles.nota}>y {resto} más</span> : null}
    </>
  );
  if (!onClick) {
    return <div className={styles.tarjeta}>{contenido}</div>;
  }
  return (
    <button type="button" className={styles.tarjeta} onClick={onClick}>
      {contenido}
    </button>
  );
}

function Campo({ indice, nombre, valor, esClave }: { indice: number; nombre: string; valor: Valor; esClave: boolean }) {
  return (
    <>
      <dt className={cx(esClave && styles.pk)} title={nombre} data-mantener={indice}>
        {nombre}
      </dt>
      <dd data-mantener={indice}>
        {valor === null ? <span className={styles.nulo}>NULL</span> : valor === "" ? <span className={styles.nulo}>vacío</span> : valor}
      </dd>
    </>
  );
}
