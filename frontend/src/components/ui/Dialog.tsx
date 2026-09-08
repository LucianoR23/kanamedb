import { useEffect, useRef } from "react";
import type { ReactNode } from "react";
import { EnvBadge } from "./Badge";
import { cx } from "../../lib/cx";
import styles from "./Dialog.module.css";

/** Cuánto espacio necesita el contenido.
 *
 * Existe porque los diálogos de este proyecto no son todos del mismo tamaño: el
 * de verificar una clave de host tiene que mostrar una huella completa sin
 * cortarla, y el visor de celda muestra un JSON entero. Sin esto, el contenido
 * pedía más ancho del que el diálogo permitía y aparecía una barra de scroll
 * horizontal — que en una huella que se compara carácter por carácter no es una
 * molestia, es un impedimento.
 *
 * Los tres se topan contra el ancho de la ventana: en una angosta el diálogo se
 * achica en vez de desbordar. */
export type DialogSize = "md" | "lg" | "xl";

interface DialogProps {
  open: boolean;
  title: string;
  /** Marca el diálogo como escritura en producción: franja roja y etiqueta. */
  production?: boolean;
  size?: DialogSize;
  /** Sin `onClose` el diálogo no se puede descartar — para confirmaciones que
   *  exigen una respuesta explícita. */
  onClose?: () => void;
  footer?: ReactNode;
  children: ReactNode;
}

export function Dialog({
  open,
  title,
  production = false,
  size = "md",
  onClose,
  footer,
  children,
}: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);

  // <dialog> nativo solo por el foco atrapado y la capa superior que da el
  // navegador; toda la piel es nuestra. Sin él habría que reimplementar el
  // ciclo de foco a mano y saldría peor.
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (open && !el.open) el.showModal();
    if (!open && el.open) el.close();
  }, [open]);

  return (
    <dialog
      ref={ref}
      className={styles.scrim}
      aria-label={title}
      onCancel={(e) => {
        e.preventDefault();
        onClose?.();
      }}
      onClick={(e) => {
        // Clic en el scrim, fuera del panel.
        if (e.target === ref.current) onClose?.();
      }}
    >
      <div
        className={cx(styles.dialog, production && styles.production, styles[`size_${size}`])}
        onClick={(e) => e.stopPropagation()}
      >
        {production ? <div className={styles.prodStripe} /> : null}
        <div className={styles.header}>
          {production ? <EnvBadge env="production" /> : null}
          <span className={styles.title}>{title}</span>
          <span className={styles.spacer} />
          {onClose ? (
            <button
              type="button"
              className={styles.close}
              aria-label="Cerrar"
              onClick={onClose}
            >
              ✕
            </button>
          ) : null}
        </div>
        <div className={styles.body}>{children}</div>
        {footer ? <div className={styles.footer}>{footer}</div> : null}
      </div>
    </dialog>
  );
}
