import { useEffect, useRef } from "react";
import type { MouseEvent, PointerEvent } from "react";

/** Cuánto hay que sostener el dedo. Lo que usa Android para sus propios menús. */
const DEMORA_MS = 450;
/** Cuánto se puede mover el dedo antes de que sea un scroll y no un toque. */
const TOLERANCIA_PX = 10;
/** Cuánto se espera el click después de soltar, antes de dejar de tragarlo. */
const CLICK_MS = 700;

/**
 * Se come el próximo click, venga de donde venga. La espera arranca cuando el
 * dedo se levanta —el click llega después de soltar, y el dedo puede quedarse
 * apoyado un rato largo— y se rinde a los CLICK_MS de eso, o si no llegó
 * ninguno.
 */
function tragarElProximoClick() {
  let t: number | null = null;
  const limpiar = () => {
    window.removeEventListener("click", tragar, true);
    window.removeEventListener("pointerup", solto, true);
    window.removeEventListener("pointercancel", solto, true);
    if (t !== null) window.clearTimeout(t);
  };
  const tragar = (e: Event) => {
    limpiar();
    e.preventDefault();
    e.stopPropagation();
  };
  const solto = () => {
    if (t === null) t = window.setTimeout(limpiar, CLICK_MS);
  };
  window.addEventListener("click", tragar, true);
  window.addEventListener("pointerup", solto, true);
  // Si el sistema se queda con el gesto, no hay click que esperar.
  window.addEventListener("pointercancel", solto, true);
}

/**
 * Mantener apretado, con delegación: los manejadores van en UN contenedor y
 * el objetivo es el elemento con `data-mantener` más cercano al dedo. Una
 * tarjeta tiene seis campos y no hace falta un temporizador por cada uno.
 *
 * Cancela si el dedo se mueve —eso es un scroll—, si se levanta antes o si el
 * navegador cancela el puntero. Cuando dispara, el `click` que el navegador
 * pueda mandar al soltar se traga en captura **en `window`**, no en el
 * contenedor: en Android el click se dirige a lo que haya debajo del dedo al
 * soltar, y para entonces ya está abierta la hoja con el valor, así que el
 * click caería en su fondo y la cerraría en el acto. Mantener apretado un
 * campo no puede además abrir la fila ni cerrar lo que abrió. Y el menú
 * contextual del WebView —que en Android es la selección de texto— se anula,
 * porque acá el gesto ya tiene dueño.
 */
export function useMantener(onLargo: (objetivo: HTMLElement) => void) {
  const timer = useRef<number | null>(null);
  const origen = useRef<{ x: number; y: number } | null>(null);

  useEffect(() => cancelar, []);

  function cancelar() {
    if (timer.current !== null) {
      window.clearTimeout(timer.current);
      timer.current = null;
    }
    origen.current = null;
  }

  return {
    onPointerDown(e: PointerEvent<HTMLElement>) {
      if (e.button !== 0) return;
      const objetivo = (e.target as HTMLElement).closest<HTMLElement>("[data-mantener]");
      if (!objetivo) return;
      cancelar();
      origen.current = { x: e.clientX, y: e.clientY };
      timer.current = window.setTimeout(() => {
        timer.current = null;
        origen.current = null;
        tragarElProximoClick();
        onLargo(objetivo);
      }, DEMORA_MS);
    },
    onPointerMove(e: PointerEvent<HTMLElement>) {
      const o = origen.current;
      if (!o) return;
      if (Math.abs(e.clientX - o.x) > TOLERANCIA_PX || Math.abs(e.clientY - o.y) > TOLERANCIA_PX) cancelar();
    },
    onPointerUp: cancelar,
    onPointerCancel: cancelar,
    onPointerLeave: cancelar,
    onContextMenu(e: MouseEvent<HTMLElement>) {
      if ((e.target as HTMLElement).closest("[data-mantener]")) e.preventDefault();
    },
  };
}
