import { useId, useState } from "react";
import styles from "./InfoHint.module.css";

/**
 * Un signo de pregunta que explica un campo al pasar el mouse o al enfocarlo.
 *
 * Existe para lo que no cabe en una etiqueta pero hace falta antes de escribir:
 * qué formato acepta una ruta, qué pasa si se deja vacío, por qué una opción es
 * mejor que otra. Un `title` nativo haría casi lo mismo, pero tarda un segundo
 * largo en aparecer, no se puede leer con el teclado y se dibuja con los colores
 * del sistema operativo en vez de los del tema.
 *
 * Responde también al foco, no solo al mouse: una ayuda que solo aparece al
 * pasar el puntero no existe para quien navega con el teclado.
 */
export function InfoHint({ label, children }: { label: string; children: React.ReactNode }) {
  const [abierto, setAbierto] = useState(false);
  const id = useId();

  return (
    <span className={styles.wrap}>
      <button
        type="button"
        className={styles.boton}
        aria-label={label}
        aria-expanded={abierto}
        aria-describedby={abierto ? id : undefined}
        onMouseEnter={() => setAbierto(true)}
        onMouseLeave={() => setAbierto(false)}
        onFocus={() => setAbierto(true)}
        onBlur={() => setAbierto(false)}
        // El clic lo fija: un globo que se va al mover el mouse es inútil si lo
        // que explica hay que leerlo mientras se escribe en el campo de al lado.
        onClick={() => setAbierto((v) => !v)}
      >
        ?
      </button>
      {abierto ? (
        <span className={styles.globo} id={id} role="tooltip">
          {children}
        </span>
      ) : null}
    </span>
  );
}
