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

export function Toast({
  toast,
  onDismiss,
}: {
  toast: ToastItem;
  onDismiss: (id: string) => void;
}) {
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

/** Los toasts nunca se autodescartan solos acá: un error de SQL que desaparece
 *  antes de que lo leas es peor que no mostrarlo. Quien los use decide. */
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
