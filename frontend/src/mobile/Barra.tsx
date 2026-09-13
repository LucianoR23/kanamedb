import type { ReactNode } from "react";
import type { SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { cx } from "../lib/cx";
import styles from "./mobile.module.css";

/**
 * La barra de arriba de una pantalla del teléfono (M05): título, subtítulo
 * en mono —lo que entra o sale de una base—, «‹» para volver si hay adónde,
 * y las acciones a la derecha. En producción se lava de rojo entera: no es
 * un chip, es el ambiente.
 */
export function Barra({
  titulo,
  mono = false,
  subtitulo,
  subtituloAcento = false,
  chip,
  atras,
  derecha,
  prod = false,
}: {
  titulo: string;
  /** El título es un identificador (nombre de tabla) y va en mono. */
  mono?: boolean;
  subtitulo?: string;
  subtituloAcento?: boolean;
  /** Algo al lado del título: la etiqueta de entorno. */
  chip?: ReactNode;
  /** Acepta `undefined` explícito: mientras se guarda, el volver se apaga. */
  atras?: (() => void) | undefined;
  derecha?: ReactNode;
  prod?: boolean;
}) {
  return (
    <header className={cx(styles.barra, atras && styles.barraConAtras, prod && styles.barraProd)}>
      {atras ? (
        <button type="button" className={styles.plano} onClick={atras} aria-label="Volver">
          ‹
        </button>
      ) : null}
      <div className={styles.tituloCaja}>
        <div className={styles.tituloFila}>
          <span className={cx(styles.titulo, mono && styles.tituloMono)}>{titulo}</span>
          {chip}
        </div>
        {subtitulo ? <span className={cx(styles.subtitulo, subtituloAcento && styles.subtituloAcento)}>{subtitulo}</span> : null}
      </div>
      {derecha}
    </header>
  );
}

/** La barra de una pestaña de la sesión: nombre de la sesión y `describe`. */
export function BarraDeSesion({
  sesion,
  titulo,
  mono = false,
  atras,
  derecha,
}: {
  sesion: SessionView;
  titulo?: string;
  mono?: boolean;
  atras?: () => void;
  derecha?: ReactNode;
}) {
  return (
    <Barra
      titulo={titulo ?? sesion.name}
      mono={mono}
      subtitulo={sesion.describe + (sesion.readOnly ? " · solo lectura" : "")}
      prod={sesion.environment === "production"}
      {...(atras ? { atras } : {})}
      {...(derecha ? { derecha } : {})}
    />
  );
}
