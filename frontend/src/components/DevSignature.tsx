import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { Browser } from "@wailsio/runtime";
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

const TIPEO = 90;
const BORRADO = 50;
const PAUSA_LLENO = 2500;
const PAUSA_VACIO = 350;

/**
 * Quién hizo esto, al pie de S25 About.
 *
 * Va acá y en ningún otro lado a propósito. En el chrome principal sería
 * publicidad, y además rompería la regla de movimiento: un typewriter en bucle
 * anima CONTENIDO, para siempre, y eso es exactamente lo que no se hace en una
 * herramienta que se mira ocho horas. About es un diálogo que se abre a
 * propósito, se mira y se cierra; ahí no compite con nada.
 *
 * Los enlaces NO son `<a href>`. Dentro de un webview eso no abre el navegador
 * del sistema: según cómo esté configurado WebView2, navega el webview en el
 * lugar —te reemplaza la aplicación por una página— o abre una ventana pelada.
 * `Browser.OpenURL` se lo entrega al sistema operativo, que es lo único
 * aceptable acá: la aplicación no carga contenido remoto en su propio proceso.
 */
export function DevSignature() {
  return (
    <div className={styles.firma}>
      <span className={styles.hecho}>Hecho por</span>
      <Wordmark />
      <span className={styles.separador} />
      <button
        type="button"
        className={styles.repo}
        title={REPO}
        onClick={() => void Browser.OpenURL(REPO)}
      >
        Código en GitHub
      </button>
    </div>
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
        <span>{fuerte}</span>
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
