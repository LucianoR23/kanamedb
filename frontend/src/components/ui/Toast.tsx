import { useEffect, useRef } from "react";
import { cx } from "../../lib/cx";
import styles from "./Toast.module.css";

export type ToastTone = "success" | "error" | "warning" | "info";

export interface ToastItem {
  id: string;
  tone: ToastTone;
  title: string;
  /** Lo que dijo el motor: SQLSTATE, tiempos, nombre de la base. */
  detail?: string;
  action?: { label: string; onClick: () => void };
}

const TONE: Record<ToastTone, string | undefined> = {
  success: styles.success,
  error: styles.error,
  warning: styles.warning,
  info: styles.info,
};

/** Cuánto dura una confirmación antes de irse sola. */
const DURACION_CONFIRMACION = 8000;

export function Toast({
  toast,
  onDismiss,
}: {
  toast: ToastItem;
  onDismiss: (id: string) => void;
}) {
  // Los de éxito e información se van solos: son confirmaciones, y una
  // confirmación que se queda hasta que alguien la cierre se vuelve un cartel.
  // Los de error y advertencia se quedan: un error de SQL que desaparece antes
  // de que lo leas es peor que no mostrarlo. La regla vive acá y no en quien
  // muestra el toast, para que el próximo no nazca sin ella.
  //
  // El temporizador depende del toast y no de `onDismiss`: quien lo muestra
  // suele pasar una función nueva en cada render, y atarse a ella reiniciaría
  // los ocho segundos con cada cambio de estado de la pantalla.
  const seVaSolo = toast.tone === "success" || toast.tone === "info";
  const descartar = useRef(onDismiss);
  useEffect(() => {
    descartar.current = onDismiss;
  });
  useEffect(() => {
    if (!seVaSolo) return;
    const t = setTimeout(() => descartar.current(toast.id), DURACION_CONFIRMACION);
    return () => clearTimeout(t);
  }, [seVaSolo, toast.id]);

  return (
    <div
      className={cx(styles.toast, TONE[toast.tone])}
      // Los errores interrumpen al lector de pantalla; el resto espera turno.
      role={toast.tone === "error" ? "alert" : "status"}
    >
      <div className={styles.content}>
        <div className={styles.title}>{toast.title}</div>
        {toast.detail ? <div className={styles.detail}>{toast.detail}</div> : null}
        {toast.action ? (
          <button type="button" className={styles.action} onClick={toast.action.onClick}>
            {toast.action.label}
          </button>
        ) : null}
      </div>
      <button
        type="button"
        className={styles.close}
        aria-label="Descartar"
        onClick={() => onDismiss(toast.id)}
      >
        ✕
      </button>
    </div>
  );
}

/** Los de error y advertencia no se van solos; los de éxito e información sí.
 *  Ver Toast. */
export function ToastStack({
  toasts,
  onDismiss,
}: {
  toasts: readonly ToastItem[];
  onDismiss: (id: string) => void;
}) {
  if (toasts.length === 0) return null;
  return (
    <div className={styles.stack}>
      {toasts.map((t) => (
        <Toast key={t.id} toast={t} onDismiss={onDismiss} />
      ))}
    </div>
  );
}
