import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { Browser } from "@wailsio/runtime";
import { cx } from "../lib/cx";
import styles from "./DevSignature.module.css";

/** A dónde llevan los dos enlaces. Sin parámetros, y eso no es un detalle: un
 *  `?ref=kaname` convertiría el clic en telemetría —el servidor se enteraría de
 *  que alguien corre Kaname y qué versión— que es justo lo que prohíbe la regla
 *  de no phone-home. La URL va pelada. */
const PORTFOLIO = "https://portfolio.lemydev.com/";
const REPO = "https://github.com/LucianoR23/kanamedb";

/** Lo que teclea el wordmark. Cada texto lleva exactamente UN punto, que es el
 *  separador entre la parte fuerte y la apagada. */
const TEXTOS = ["luciano.rodriguez", "lemy.dev"] as const;
/** El corto, para la variante quieta: en una barra de 10,5px no entra el largo. */
const CORTO = TEXTOS[1];

const TIPEO = 90;
const BORRADO = 50;
const PAUSA_LLENO = 2500;
const PAUSA_VACIO = 350;

/**
 * Quién hizo esto, y dónde está el código.
 *
 * Dos variantes, y la diferencia no es estética:
 *
 * - **`quieta`** es la que va en la barra de estado, o sea en pantallas que se
 *   miran horas. Ahí no se anima NADA. Un texto que se teclea solo en el borde
 *   de la vista mientras leés un plan de ejecución es exactamente lo que la
 *   regla de movimiento existe para evitar, y además es lo que haría que se
 *   leyera como publicidad: lo que llama la atención es el movimiento, no el
 *   hecho de estar ahí.
 * - **`animada`** es la de About, que es un destino: se abre a propósito, se
 *   mira y se cierra, y no compite con nada.
 *
 * Los enlaces NO son `<a href>`. Dentro de un webview eso no abre el navegador
 * del sistema: según cómo esté configurado WebView2, navega el webview en el
 * lugar —te reemplaza la aplicación por una página— o abre una ventana pelada.
 * `Browser.OpenURL` se lo entrega al sistema operativo, que es lo único
 * aceptable acá: la aplicación no carga contenido remoto en su propio proceso.
 */
export function DevSignature({
  variant = "quieta",
}: {
  variant?: "quieta" | "animada";
}) {
  const animada = variant === "animada";
  return (
    <div className={cx(styles.firma, animada ? styles.enPanel : styles.enBarra)}>
      {animada ? <span className={styles.hecho}>Hecho por</span> : null}
      {animada ? <Wordmark /> : <WordmarkQuieto />}
      {animada ? <span className={styles.separador} /> : null}
      <button
        type="button"
        className={styles.repo}
        title={`Código en GitHub · ${REPO}`}
        aria-label="Ver el código en GitHub"
        onClick={() => void Browser.OpenURL(REPO)}
      >
        <MarcaGitHub />
        {animada ? <span>Código en GitHub</span> : null}
      </button>
    </div>
  );
}

/** El wordmark sin nada que se mueva. */
function WordmarkQuieto() {
  return (
    <button
      type="button"
      className={styles.marca}
      title={`Hecho por Luciano Rodríguez · ${PORTFOLIO}`}
      onClick={() => void Browser.OpenURL(PORTFOLIO)}
    >
      <span className={styles.fuerte}>{CORTO.slice(0, CORTO.indexOf("."))}</span>
      <span className={styles.puntoQuieto}>.</span>
      <span className={styles.apagado}>{CORTO.slice(CORTO.indexOf(".") + 1)}</span>
    </button>
  );
}

/**
 * El wordmark que se teclea solo.
 *
 * Se detiene cuando la ventana no está visible. En una página web eso es
 * cortesía; acá es necesario, porque esta ventana queda abierta horas y un
 * temporizador que redibuja cada noventa milisegundos para nadie es batería
 * regalada.
 */
function Wordmark() {
  const menosMovimiento = usaMenosMovimiento();
  const visible = ventanaVisible();
  const [encima, setEncima] = useState(false);

  const [mostrado, setMostrado] = useState<string>(TEXTOS[0]);
  const [puntoKey, setPuntoKey] = useState(0);

  const iTexto = useRef(0);
  const iChar = useRef(TEXTOS[0].length);
  const fase = useRef<"tipeando" | "lleno" | "borrando" | "vacio">("lleno");
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (menosMovimiento) {
      setMostrado(TEXTOS[0]);
      return;
    }
    // Quieto mientras no se vea, y quieto mientras el puntero esté encima: si
    // alguien se detuvo a leerlo es porque lo va a clickear, y un texto que se
    // borra debajo del cursor es hostil.
    if (!visible || encima) {
      if (timer.current) clearTimeout(timer.current);
      return;
    }

    const paso = () => {
      const actual = TEXTOS[iTexto.current] ?? "";

      if (fase.current === "tipeando") {
        iChar.current++;
        const siguiente = actual.slice(0, iChar.current);
        setMostrado(siguiente);
        if (siguiente.endsWith(".")) setPuntoKey((k) => k + 1);
        if (iChar.current >= actual.length) {
          fase.current = "lleno";
          timer.current = setTimeout(paso, PAUSA_LLENO);
        } else {
          // El jitter es lo que lo hace parecer tecleado y no generado.
          timer.current = setTimeout(paso, TIPEO + (Math.random() * 40 - 20));
        }
        return;
      }
      if (fase.current === "lleno") {
        fase.current = "borrando";
        timer.current = setTimeout(paso, 0);
        return;
      }
      if (fase.current === "borrando") {
        iChar.current--;
        setMostrado(actual.slice(0, iChar.current));
        if (iChar.current <= 0) {
          fase.current = "vacio";
          timer.current = setTimeout(paso, PAUSA_VACIO);
        } else {
          timer.current = setTimeout(paso, BORRADO);
        }
        return;
      }
      iTexto.current = (iTexto.current + 1) % TEXTOS.length;
      fase.current = "tipeando";
      timer.current = setTimeout(paso, 0);
    };

    timer.current = setTimeout(paso, PAUSA_LLENO);
    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, [menosMovimiento, visible, encima]);

  // Al pasar por encima se completa: hace falta poder leer entero lo que se va
  // a clickear.
  useEffect(() => {
    if (!encima || menosMovimiento) return;
    const actual = TEXTOS[iTexto.current] ?? "";
    setMostrado(actual);
    iChar.current = actual.length;
    fase.current = "lleno";
  }, [encima, menosMovimiento]);

  const punto = mostrado.indexOf(".");
  const fuerte = punto >= 0 ? mostrado.slice(0, punto) : mostrado;
  const apagado = punto >= 0 ? mostrado.slice(punto + 1) : "";

  return (
    <button
      type="button"
      className={styles.marca}
      // El texto cicla entre dos cosas, así que el destino tiene que estar
      // escrito: si no, se puede hacer clic mientras dice una y esperar otra.
      title={PORTFOLIO}
      onMouseEnter={() => setEncima(true)}
      onMouseLeave={() => setEncima(false)}
      onFocus={() => setEncima(true)}
      onBlur={() => setEncima(false)}
      onClick={() => void Browser.OpenURL(PORTFOLIO)}
    >
      <span className={styles.oculto}>lemy.dev</span>
      <span className={styles.texto} aria-hidden="true">
        <span className={styles.fuerte}>{fuerte}</span>
        {punto >= 0 ? (
          <span key={puntoKey} className={styles.punto}>
            .
          </span>
        ) : null}
        <span className={styles.apagado}>{apagado}</span>
        <span className={styles.cursor}>|</span>
      </span>
    </button>
  );
}

/** La marca de GitHub. Va como SVG y no por `Glyph` porque `Glyph` son
 *  etiquetas de texto, y esto es una forma concreta de un tercero. */
function MarcaGitHub() {
  return (
    <svg
      viewBox="0 0 16 16"
      width="12"
      height="12"
      fill="currentColor"
      aria-hidden="true"
      focusable="false"
      className={styles.gh}
    >
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  );
}

/** El interruptor del sistema. Se lee en vivo: alguien puede cambiarlo con la
 *  aplicación abierta, y esta ventana queda abierta mucho tiempo. */
function usaMenosMovimiento(): boolean {
  return useSyncExternalStore(
    (avisar) => {
      const mq = window.matchMedia("(prefers-reduced-motion: reduce)");
      mq.addEventListener("change", avisar);
      return () => mq.removeEventListener("change", avisar);
    },
    () => window.matchMedia("(prefers-reduced-motion: reduce)").matches,
    () => false,
  );
}

function ventanaVisible(): boolean {
  return useSyncExternalStore(
    (avisar) => {
      document.addEventListener("visibilitychange", avisar);
      return () => document.removeEventListener("visibilitychange", avisar);
    },
    () => document.visibilityState === "visible",
    () => true,
  );
}
