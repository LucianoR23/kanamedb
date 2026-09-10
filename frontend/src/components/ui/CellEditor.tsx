import { useEffect, useRef, useState } from "react";
import styles from "./CellEditor.module.css";

/**
 * El editor que se abre ADENTRO de una celda de la grilla.
 *
 * Es un campo de texto sin borde propio que ocupa la celda entera: el borde lo
 * pone la celda, en acento, para que se vea dónde se está escribiendo. Enter
 * confirma, Escape cancela, y perder el foco confirma también — cerrar sin
 * guardar lo que se escribió sería peor que guardar de más, porque lo guardado
 * se ve y se puede deshacer.
 *
 * No sabe de NULL: un campo de texto vacío es la cadena vacía. NULL se pone
 * desde el menú de la celda, que es la única forma de que «nada» y «vacío» no
 * se confundan.
 */
export function CellEditor({
  initial,
  align = "left",
  onCommit,
  onCancel,
}: {
  initial: string;
  align?: "left" | "right";
  onCommit: (value: string) => void;
  onCancel: () => void;
}) {
  const [value, setValue] = useState(initial);
  const ref = useRef<HTMLInputElement>(null);
  // Para que el blur que sigue a Escape no confirme lo que se acaba de cancelar.
  const cerrado = useRef(false);
  // Si no se escribió nada, cerrar es cancelar. Importa en una celda NULL o
  // «por defecto»: el editor arranca vacío, y confirmar ese vacío la
  // convertiría en cadena vacía sin que nadie haya tecleado. Escribir y
  // borrar todo sí cuenta: eso es querer la cadena vacía.
  const tocado = useRef(false);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.focus();
    el.select();
  }, []);

  function confirmar() {
    if (cerrado.current) return;
    cerrado.current = true;
    if (tocado.current) onCommit(value);
    else onCancel();
  }
  function cancelar() {
    if (cerrado.current) return;
    cerrado.current = true;
    onCancel();
  }

  return (
    <input
      ref={ref}
      className={styles.editor}
      style={{ textAlign: align }}
      value={value}
      spellCheck={false}
      autoComplete="off"
      aria-label="Valor de la celda"
      onChange={(e) => {
        tocado.current = true;
        setValue(e.target.value);
      }}
      onBlur={confirmar}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          confirmar();
        } else if (e.key === "Escape") {
          e.preventDefault();
          cancelar();
        }
        // Que la grilla no reciba las flechas ni el Tab mientras se escribe.
        e.stopPropagation();
      }}
      onClick={(e) => e.stopPropagation()}
      onDoubleClick={(e) => e.stopPropagation()}
    />
  );
}
