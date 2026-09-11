import { useSyncExternalStore } from "react";
import { Theme } from "../../bindings/github.com/LucianoR23/kanamedb/internal/config";
import type { Config } from "../../bindings/github.com/LucianoR23/kanamedb/internal/config";
import * as SettingsSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/settings";
import type { SettingsView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";

/**
 * Lo que las preferencias cambian en la pantalla, de S23.
 *
 * Todo pasa por atributos y variables de la raíz del documento, y no por estado
 * de React, por una razón concreta: el tema lo consume CSS —`[data-theme]`— y el
 * cuerpo del editor lo consume CodeMirror, que arma su hoja de estilos una sola
 * vez al montarse. Con estado de React habría que volver a renderizar la
 * aplicación entera para cambiar un color, y recrear el editor para cambiarle el
 * cuerpo de letra, perdiendo el cursor y el deshacer.
 *
 * El módulo no llama a Go. Recibe lo que ya se leyó y lo pinta.
 */

/** El tema elegido. Se guarda para poder repintar si cambia el del sistema. */
let elegido: Theme = Theme.ThemeDark;

/**
 * El tema del sistema operativo.
 *
 * Se consulta por «claro» y no por «oscuro» porque oscuro es el default de esta
 * aplicación: en un navegador que no soporte la consulta —o en un entorno de
 * prueba sin `matchMedia`— `matches` es false y queda oscuro, que es lo que la
 * app hacía antes de que existiera este archivo.
 */
const sistemaEnClaro =
  typeof window !== "undefined" && typeof window.matchMedia === "function"
    ? window.matchMedia("(prefers-color-scheme: light)")
    : null;

// Alguien puede cambiar el tema del sistema con la aplicación abierta. Con
// «El del sistema» elegido, eso tiene que verse sin reiniciar.
sistemaEnClaro?.addEventListener("change", () => pintarTema());

function pintarTema() {
  const raiz = document.documentElement;
  const claro =
    elegido === Theme.ThemeLight ||
    (elegido === Theme.ThemeSystem && sistemaEnClaro?.matches === true);

  // Oscuro NO pone atributo: es lo que manda `:root` en tokens.css. Ponerle
  // `data-theme="dark"` obligaría a duplicar la paleta entera en un bloque que
  // hoy no existe, y a mantener las dos iguales para siempre.
  if (claro) raiz.setAttribute("data-theme", "light");
  else raiz.removeAttribute("data-theme");
}

/** Qué tema se está mostrando de verdad. La pantalla lo dice cuando el elegido
 *  es «el del sistema», que si no es adivinar. */
export function temaEfectivo(): "claro" | "oscuro" {
  return document.documentElement.getAttribute("data-theme") === "light" ? "claro" : "oscuro";
}

/**
 * El interlineado que le corresponde a un cuerpo de letra.
 *
 * CodeMirror mide alturas en píxeles para virtualizar, así que necesita un
 * número y no un múltiplo. 1.54 es la proporción que ya tenía el editor —13 px
 * con 20 px de línea— y se conserva para que cambiar el tamaño no cambie de paso
 * la densidad.
 */
function interlineado(px: number): number {
  return Math.round(px * 1.54);
}

/** Aplica todo lo que se ve. */
export function aplicar(c: Config) {
  elegido = c.theme || Theme.ThemeDark;
  pintarTema();

  const raiz = document.documentElement;
  // Un tamaño fuera de rango no se pinta: Go ya lo normaliza al guardar, pero
  // esto también corre con lo que devuelve una lectura fallida, y un `NaN` en
  // una variable CSS deja el editor sin tamaño en vez de con el de antes.
  const px = Number(c.editor?.fontSize);
  if (Number.isFinite(px) && px > 0) {
    raiz.style.setProperty("--sql-font-size", `${px}px`);
    raiz.style.setProperty("--sql-line-height", `${interlineado(px)}px`);
  }

  actual = c;
  for (const f of oyentes) f();
}

/* ------------------------------------------------- lo que no es CSS */

/**
 * Las preferencias que un componente necesita leer, no solo pintar.
 *
 * El tema y el cuerpo de letra se resuelven con CSS y no hace falta que React
 * se entere. Los números de línea y el ajuste del editor SÍ: son extensiones de
 * CodeMirror, y cambiarlas es reconfigurar el editor.
 *
 * Va por un store de módulo con `useSyncExternalStore` y no por props, y no es
 * por comodidad: pasarlas como props obligaría a enhebrarlas por el Shell y por
 * la pantalla del editor —dos componentes que no tienen nada que ver con esto—
 * solo para que lleguen al fondo. Una preferencia de la aplicación es global por
 * definición.
 */
let actual: Config | null = null;
const oyentes = new Set<() => void>();

function suscribir(f: () => void) {
  oyentes.add(f);
  return () => oyentes.delete(f);
}

function leer(): Config | null {
  return actual;
}

/** Las preferencias de ahora, o null mientras no se leyeron. */
export function usarPreferencias(): Config | null {
  return useSyncExternalStore(suscribir, leer, leer);
}

/**
 * Lee las preferencias del disco y las aplica.
 *
 * Devuelve la vista entera —con la ruta del archivo y el problema de lectura, si
 * lo hubo— porque la pantalla de ajustes necesita eso además de los valores.
 * Quien solo quiere que la aplicación se pinte bien ignora el resultado.
 */
export async function cargar(): Promise<SettingsView> {
  const v = await SettingsSvc.Get();
  aplicar(v.config);
  return v;
}
