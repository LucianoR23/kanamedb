import type { Column } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import { cx } from "../lib/cx";
import { useMantener } from "./useMantener";
import styles from "./mobile.module.css";

export type Valor = string | null;

/** Cuántas columnas muestra una tarjeta antes de «y N más». */
const MAX_CAMPOS = 6;

/**
 * M07: una fila como tarjeta, columna → valor en dos columnas, las primeras
 * seis. Es la unidad de la interfaz del teléfono en lugar de la celda de la
 * grilla. La clave primaria va en su color, NULL en itálica gris, la cadena
 * vacía dice «vacío». Los valores son datos no confiables: van como texto,
 * nunca como HTML.
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
  const manejadores = onMantener ? mantener : {};
  const contenido = visibles.map((c, i) => {
    const v = fila[i] ?? null;
    const pk = clave?.has(c.name) ?? false;
    return (
      <Campo key={c.name} indice={i} nombre={c.name} valor={v} pk={pk} />
    );
  });
  const mas = resto > 0 ? <span className={styles.mas}>y {resto} más</span> : null;

  if (!onClick) {
    return (
      <div className={styles.filaTarjeta} {...manejadores}>
        {contenido}
        {mas}
      </div>
    );
  }
  return (
    <button type="button" className={styles.filaTarjeta} onClick={onClick} {...manejadores}>
      {contenido}
      {mas}
    </button>
  );
}

function Campo({ indice, nombre, valor, pk }: { indice: number; nombre: string; valor: Valor; pk: boolean }) {
  return (
    <>
      <span className={cx(styles.k, pk && styles.kPk)} title={nombre} data-mantener={indice}>
        {nombre}
      </span>
      <span
        className={cx(styles.v, pk && styles.vPk, valor === null && styles.nulo, valor === "" && styles.vacioValor)}
        data-mantener={indice}
      >
        {valor === null ? "NULL" : valor === "" ? "vacío" : valor}
      </span>
    </>
  );
}
