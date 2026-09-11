import { useEffect, useRef, useState } from "react";
import type { TransitionEvent } from "react";
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

/** Tope de espera para la salida, por si `transitionend` no llega. */
const TOPE_DE_SALIDA = 220;

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

  // La salida se dibuja antes de avisar: quien muestra el toast lo saca de su
  // lista al recibir `onDismiss`, y un elemento que ya no está no puede irse
  // suave. Es el mismo arreglo que en Dialog. El toast es el único lugar
  // donde la animación es funcional y no decorativa: aparece sin que nadie lo
  // pida y en el borde de la vista, y el movimiento es lo que hace que se vea.
  const [saliendo, setSaliendo] = useState(false);
  const tope = useRef<ReturnType<typeof setTimeout> | null>(null);
  function terminar() {
    if (tope.current) clearTimeout(tope.current);
    tope.current = null;
    descartar.current(toast.id);
  }
  function irse() {
    if (saliendo) return;
    // Con la ventana minimizada no hay salida que dibujar, y los
    // temporizadores se estiran: se va en el acto. Ver Dialog.
    if (document.hidden) {
      terminar();
      return;
    }
    setSaliendo(true);
    tope.current = setTimeout(terminar, TOPE_DE_SALIDA);
  }
  // El temporizador de los ocho segundos llama a la versión más reciente,
  // por lo mismo que `descartar`: no se reinicia con cada render.
  const irseAhora = useRef(irse);
  useEffect(() => {
    irseAhora.current = irse;
  });
  useEffect(
    () => () => {
      if (tope.current) clearTimeout(tope.current);
    },
    [],
  );

  useEffect(() => {
    if (!seVaSolo) return;
    const t = setTimeout(() => irseAhora.current(), DURACION_CONFIRMACION);
    return () => clearTimeout(t);
  }, [seVaSolo, toast.id]);

  function alTerminarLaTransicion(e: TransitionEvent<HTMLDivElement>) {
    if (saliendo && e.target === e.currentTarget && e.propertyName === "opacity") terminar();
  }

  return (
    <div
      className={cx(styles.toast, TONE[toast.tone], saliendo && styles.saliendo)}
      // Los errores interrumpen al lector de pantalla; el resto espera turno.
      role={toast.tone === "error" ? "alert" : "status"}
      onTransitionEnd={alTerminarLaTransicion}
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
        onClick={irse}
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
