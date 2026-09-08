import { useEffect, useRef, useState } from "react";
import styles from "./CopyButton.module.css";

/**
 * Copia un texto al portapapeles y lo dice.
 *
 * El "Copiado" es la mitad del componente: sin confirmación, quien aprieta no
 * sabe si funcionó y aprieta de nuevo. Vuelve a "Copiar" solo, porque un botón
 * que se queda diciendo "Copiado" miente en cuanto el portapapeles cambia.
 */
export function CopyButton({
  text,
  label = "Copiar",
  title,
}: {
  text: string;
  label?: string;
  title?: string;
}) {
  const [copiado, setCopiado] = useState(false);
  const timer = useRef<number | null>(null);

  // El temporizador se limpia al desmontar: sin esto, cerrar el diálogo justo
  // después de copiar deja un setState apuntando a un componente que ya no está.
  useEffect(() => {
    return () => {
      if (timer.current !== null) window.clearTimeout(timer.current);
    };
  }, []);

  return (
    <button
      type="button"
      className={styles.boton}
      {...(title ? { title } : {})}
      onClick={() => {
        void navigator.clipboard.writeText(text).then(() => {
          setCopiado(true);
          if (timer.current !== null) window.clearTimeout(timer.current);
          timer.current = window.setTimeout(() => setCopiado(false), 1800);
        });
      }}
    >
      {copiado ? "Copiado" : label}
    </button>
  );
}
