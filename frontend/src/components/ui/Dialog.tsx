import { createContext, useContext, useEffect, useLayoutEffect, useRef, useState } from "react";
import type { ComponentProps, ReactNode, TransitionEvent } from "react";
import { EnvBadge } from "./Badge";
import { Button } from "./Button";
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

/** Tope de espera para la salida, por si `transitionend` no llega —un
 *  diálogo que se cierra mientras la pestaña está oculta, por ejemplo—. Más
 *  que `--dur`, menos que lo que alguien notaría. */
const TOPE_DE_SALIDA = 220;

/** Cuánto se espera, después de avisar, a que el padre cierre de verdad. Si
 *  no lo hizo —un `onClose` que no cierra— el diálogo vuelve a verse en vez
 *  de quedar abierto e invisible tapando la aplicación. */
const ESPERA_AL_PADRE = 50;

/** Cómo se sale de un diálogo: se dibuja la salida y DESPUÉS se hace lo que
 *  cierra. Ver `salir` en Dialog. */
type Salir = (despues: () => void) => void;

const ContextoSalir = createContext<Salir | null>(null);

/**
 * DialogClose es un botón del pie que cierra el diálogo con la misma salida
 * que Esc, ✕ y el clic en el velo.
 *
 * Es para «Cancelar» y «Cerrar»: son la persona pidiendo cerrar, y no tienen
 * por qué salir distinto que Esc. Es un componente y no un hook a propósito:
 * el pie lo escribe la pantalla que abre el diálogo, que está FUERA de él, y
 * un hook ahí no vería el contexto; el componente corre donde se monta, que
 * es adentro. Lo que cierra el padre por su cuenta —guardó, aplicó— no pasa
 * por acá y desaparece en el acto. Fuera de un Dialog, hace lo que se le
 * pasa y nada más.
 */
export function DialogClose({
  onClose,
  ...rest
}: Omit<ComponentProps<typeof Button>, "onClick"> & { onClose: () => void }) {
  const salir = useContext(ContextoSalir);
  return <Button {...rest} onClick={() => (salir ? salir(onClose) : onClose())} />;
}

interface DialogProps {
  open: boolean;
  title: string;
  /** Marca el diálogo como escritura en producción: franja roja y etiqueta. */
  production?: boolean;
  /** Aparece y desaparece sin transición. Es para las confirmaciones
   *  destructivas: un modal rojo que entra suave se lee como menos serio que
   *  uno que aparece. Es el único lugar donde la ausencia de movimiento es la
   *  decisión de diseño. */
  abrupto?: boolean;
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
  abrupto = false,
  size = "md",
  onClose,
  footer,
  children,
}: DialogProps) {
  const ref = useRef<HTMLDialogElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  // Saliendo: la persona pidió cerrar y el diálogo se está yendo. Casi todos
  // los diálogos se desmontan cuando el padre recibe `onClose`, y un elemento
  // desmontado no puede animar nada: la salida se dibuja ANTES de avisar, y
  // el aviso llega cuando terminó. Lo que cierra el padre por su cuenta
  // —guardó, aplicó— desaparece en el acto, que es lo que corresponde a algo
  // que se acaba de accionar.
  const [saliendo, setSaliendo] = useState(false);
  const pendiente = useRef<(() => void) | null>(null);
  const tope = useRef<ReturnType<typeof setTimeout> | null>(null);
  const vuelta = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Producción aparece, no llega, sin que cada pantalla tenga que pedirlo:
  // es la confirmación que tiene que verse seria, y una clave de host que
  // cambió —que también entra por acá— es una alarma.
  const sinTransicion = abrupto || production;

  // <dialog> nativo solo por el foco atrapado y la capa superior que da el
  // navegador; toda la piel es nuestra. Sin él habría que reimplementar el
  // ciclo de foco a mano y saldría peor.
  //
  // Con efecto de layout y no uno común: abrir y cerrar tocan el DOM y tienen
  // que pasar antes de pintar. Al cerrar, además, es lo que garantiza que
  // cuando `saliendo` se limpie el diálogo ya esté cerrado, y no haya un
  // frame en que el panel empiece a volver.
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (open && !el.open) el.showModal();
    if (!open && el.open) el.close();
    limpiar();
    setSaliendo(false);
  }, [open]);

  useEffect(() => limpiar, []);

  function limpiar() {
    if (tope.current) clearTimeout(tope.current);
    if (vuelta.current) clearTimeout(vuelta.current);
    tope.current = vuelta.current = null;
    pendiente.current = null;
  }

  /** Dibuja la salida y después hace lo que cierra. */
  function salir(despues: () => void) {
    // Con la ventana minimizada no hay nada que dibujar, y además el
    // navegador congela las transiciones y estira los temporizadores hasta
    // un minuto: cerrar en el acto es lo único que no deja el diálogo
    // colgado esperando una salida que nadie ve.
    if (sinTransicion || document.hidden) {
      despues();
      return;
    }
    if (saliendo) return;
    setSaliendo(true);
    pendiente.current = despues;
    tope.current = setTimeout(terminar, TOPE_DE_SALIDA);
  }

  function terminar() {
    if (tope.current) clearTimeout(tope.current);
    tope.current = null;
    const despues = pendiente.current;
    pendiente.current = null;
    despues?.();
    // `saliendo` queda puesto hasta que el padre cierre —lo limpia el efecto
    // de arriba— o desmonte. Si no hizo ninguna de las dos, el diálogo
    // vuelve: uno abierto e invisible sería la aplicación bloqueada.
    vuelta.current = setTimeout(() => {
      vuelta.current = null;
      if (ref.current?.open) setSaliendo(false);
    }, ESPERA_AL_PADRE);
  }

  /** Lo que pide la persona: Esc, ✕ o el clic en el velo. */
  function cerrar() {
    if (onClose) salir(onClose);
  }

  function alTerminarLaTransicion(e: TransitionEvent<HTMLDivElement>) {
    if (saliendo && pendiente.current && e.target === panel.current && e.propertyName === "opacity") {
      terminar();
    }
  }

  return (
    <dialog
      ref={ref}
      className={cx(styles.scrim, sinTransicion && styles.abrupto, saliendo && styles.saliendo)}
      aria-label={title}
      onCancel={(e) => {
        e.preventDefault();
        cerrar();
      }}
      onClick={(e) => {
        // Clic en el scrim, fuera del panel.
        if (e.target === ref.current) cerrar();
      }}
    >
      <ContextoSalir.Provider value={salir}>
        <div
          ref={panel}
          className={cx(styles.dialog, production && styles.production, styles[`size_${size}`])}
          onClick={(e) => e.stopPropagation()}
          onTransitionEnd={alTerminarLaTransicion}
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
                onClick={cerrar}
              >
                ✕
              </button>
            ) : null}
          </div>
          <div className={styles.body}>{children}</div>
          {footer ? <div className={styles.footer}>{footer}</div> : null}
        </div>
      </ContextoSalir.Provider>
    </dialog>
  );
}
