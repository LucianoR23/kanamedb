import { useState } from "react";
import { Button, Dialog, SearchInput } from "../components/ui";
import { cx } from "../lib/cx";
import styles from "./mobile.module.css";

export interface Opcion {
  value: string;
  /** Qué mostrar en vez del valor: «es igual a» por `eq`. */
  label?: string;
  /** Etiqueta chica a la derecha: el tipo de la columna. */
  tag?: string;
}

/**
 * Un desplegable del teléfono (M08 B): el campo muestra lo elegido y al
 * tocarlo se abre una hoja con la lista, con buscador cuando la lista es
 * larga. Reemplaza al Combobox de escritorio, que es un campo de texto con
 * una lista flotante: en el teléfono la lista flotante queda debajo del
 * teclado y el campo de texto invita a tipear lo que no hay. La lista es
 * cerrada por construcción: lo que no está no se puede elegir.
 */
export function Selector({
  valor,
  opciones,
  titulo,
  ariaLabel,
  ui = false,
  placeholder,
  onChange,
}: {
  valor: string;
  opciones: readonly Opcion[];
  /** El título de la hoja: «Columna», «Operador». */
  titulo: string;
  ariaLabel: string;
  /** Las opciones son palabras de la app (operadores), no identificadores. */
  ui?: boolean;
  placeholder?: string;
  onChange: (v: string) => void;
}) {
  const [abierto, setAbierto] = useState(false);
  const [filtro, setFiltro] = useState("");

  const elegida = opciones.find((o) => o.value === valor);
  const etiqueta = elegida ? (elegida.label ?? elegida.value) : "";
  const q = filtro.trim().toLowerCase();
  const visibles = q ? opciones.filter((o) => (o.label ?? o.value).toLowerCase().includes(q)) : opciones;
  const conBuscador = opciones.length > 7;

  function cerrar() {
    setAbierto(false);
    setFiltro("");
  }

  return (
    <>
      <button
        type="button"
        className={cx(styles.selectorDisparador, ui && styles.selectorUi, !etiqueta && styles.selectorVacio)}
        onClick={() => setAbierto(true)}
        aria-label={ariaLabel}
        aria-haspopup="dialog"
      >
        <span>{etiqueta || placeholder || "elegir"}</span>
        {elegida?.tag ? <span className={styles.selectorTag}>{elegida.tag}</span> : null}
        <span className={styles.selectorCaret} aria-hidden="true">
          ▼
        </span>
      </button>

      {abierto ? (
        <Dialog
          open
          title={titulo}
          onClose={cerrar}
          footer={<Button onClick={cerrar}>Cancelar</Button>}
        >
          <div className={styles.columna}>
            {conBuscador ? (
              <SearchInput
                className={styles.busqueda}
                value={filtro}
                onChange={(e) => setFiltro(e.target.value)}
                placeholder={`Buscar en ${titulo.toLowerCase()}`}
                aria-label={`Buscar en ${titulo.toLowerCase()}`}
                autoFocus
                autoCapitalize="off"
                autoCorrect="off"
              />
            ) : null}
            <div className={styles.selectorLista} role="listbox" aria-label={titulo}>
              {visibles.length === 0 ? <div className={styles.selectorNada}>Nada coincide.</div> : null}
              {visibles.map((o) => {
                const es = o.value === valor;
                return (
                  <button
                    key={o.value}
                    type="button"
                    role="option"
                    aria-selected={es}
                    className={cx(styles.selectorOpcion, ui && styles.selectorOpcionUi, es && styles.selectorElegida)}
                    onClick={() => {
                      onChange(o.value);
                      cerrar();
                    }}
                  >
                    <span>{o.label ?? o.value}</span>
                    {o.tag ? <span className={styles.selectorTag}>{o.tag}</span> : null}
                    {es ? (
                      <span className={styles.selectorTilde} aria-hidden="true">
                        ✓
                      </span>
                    ) : null}
                  </button>
                );
              })}
            </div>
          </div>
        </Dialog>
      ) : null}
    </>
  );
}
