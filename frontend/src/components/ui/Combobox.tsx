import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { CSSProperties } from "react";
import { createPortal } from "react-dom";
import { cx } from "../../lib/cx";
import styles from "./Combobox.module.css";

export interface ComboOption {
  /** Lo que se guarda y lo que se muestra. */
  value: string;
  /** Etiqueta chica a la derecha: "enum", "de esta base". */
  tag?: string;
  /** Explicación al pasar por encima. */
  title?: string;
  /** Separa el bloque anterior del siguiente. */
  group?: string;
}

/** Cuánto puede medir la lista, y cuánto es tan poco que conviene ir arriba. */
const ALTO_MAXIMO = 260;
const ALTO_MINIMO = 120;
const SEPARACION = 3;

/**
 * Un desplegable con búsqueda que además acepta lo que se escriba.
 *
 * Las dos mitades importan. La lista evita el error de tipeo, que es el motivo
 * de existir; y aceptar texto libre evita el callejón sin salida cuando lo que
 * hace falta no está listado —una extensión instalada después de conectar, un
 * modificador con una forma rara—. Una lista cerrada sería más prolija y a veces
 * impediría hacer algo perfectamente válido.
 *
 * No usa `<select>`: el nativo ignora el tema en Windows y no busca. Ver
 * CLAUDE.md.
 *
 * La lista se dibuja en un portal, fuera del árbol donde está el campo, y se
 * posiciona con coordenadas de pantalla. No es un capricho: dentro de un diálogo
 * el cuerpo tiene `overflow: auto`, así que una lista posicionada en su interior
 * —por más `absolute` que sea— cuenta como contenido, estira el alto del diálogo
 * y aparece una segunda barra de desplazamiento. El portal la saca de ese
 * contenedor: flota por encima, con su propio scroll, y el diálogo no se entera.
 */
export function Combobox({
  value,
  options,
  placeholder,
  ariaLabel,
  disabled = false,
  vacio = "Nada coincide. Lo que escribas se usa igual: la base lo va a validar.",
  onChange,
}: {
  value: string;
  options: readonly ComboOption[];
  placeholder?: string;
  ariaLabel: string;
  disabled?: boolean;
  /** Qué decir cuando el filtro no deja nada. */
  vacio?: string;
  onChange: (v: string) => void;
}) {
  const [abierto, setAbierto] = useState(false);
  const [filtro, setFiltro] = useState("");
  const [resaltado, setResaltado] = useState(0);
  const [posicion, setPosicion] = useState<CSSProperties | null>(null);
  const [destino, setDestino] = useState<HTMLElement | null>(null);
  const caja = useRef<HTMLDivElement>(null);
  const lista = useRef<HTMLDivElement>(null);

  // Cerrar al hacer clic afuera. Con la lista en un portal, "afuera" son DOS
  // cosas: ni el campo ni la lista. Mirar solo el campo cerraría el desplegable
  // al hacer clic en una de sus opciones, que es justo para lo que está.
  useEffect(() => {
    if (!abierto) return;
    const fuera = (e: MouseEvent) => {
      const t = e.target as Node;
      if (caja.current?.contains(t)) return;
      if (lista.current?.contains(t)) return;
      setAbierto(false);
    };
    document.addEventListener("mousedown", fuera);
    return () => document.removeEventListener("mousedown", fuera);
  }, [abierto]);

  // Seguir al campo mientras esté abierto. Al estar en coordenadas de pantalla,
  // la lista no se mueve sola cuando el diálogo o la página hacen scroll: sin
  // esto quedaría flotando sobre un campo que ya no está ahí.
  useLayoutEffect(() => {
    if (!abierto) return;
    setDestino(contenedorDe(caja.current));
    const ubicar = () => setPosicion(medir(caja.current));
    ubicar();
    // `true` para escuchar también el scroll de contenedores internos, que no
    // burbujea.
    window.addEventListener("scroll", ubicar, true);
    window.addEventListener("resize", ubicar);
    return () => {
      window.removeEventListener("scroll", ubicar, true);
      window.removeEventListener("resize", ubicar);
    };
  }, [abierto]);

  const visibles = options.filter((o) =>
    o.value.toLowerCase().includes(filtro.trim().toLowerCase()),
  );

  // Mantener a la vista lo resaltado al moverse con el teclado.
  useLayoutEffect(() => {
    if (!abierto) return;
    lista.current?.querySelector('[data-resaltado="1"]')?.scrollIntoView({ block: "nearest" });
  }, [abierto, resaltado]);

  function elegir(v: string) {
    onChange(v);
    setAbierto(false);
    setFiltro("");
  }

  return (
    <div className={styles.caja} ref={caja}>
      <input
        type="text"
        role="combobox"
        aria-expanded={abierto}
        aria-label={ariaLabel}
        autoComplete="off"
        spellCheck={false}
        disabled={disabled}
        className={styles.entrada}
        value={abierto ? filtro : value}
        placeholder={placeholder}
        onFocus={() => {
          setAbierto(true);
          setFiltro("");
          setResaltado(0);
        }}
        onChange={(e) => {
          setFiltro(e.currentTarget.value);
          setResaltado(0);
          // Lo escrito ES el valor: si nadie elige de la lista, vale igual.
          onChange(e.currentTarget.value);
        }}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown" || e.key === "ArrowUp") {
            e.preventDefault();
            setAbierto(true);
            setResaltado((i) => {
              const paso = e.key === "ArrowDown" ? 1 : -1;
              const n = visibles.length;
              return n === 0 ? 0 : (i + paso + n) % n;
            });
          } else if (e.key === "Enter" && abierto && visibles[resaltado]) {
            e.preventDefault();
            elegir(visibles[resaltado].value);
          } else if (e.key === "Escape" && abierto) {
            e.preventDefault();
            setAbierto(false);
          }
        }}
      />

      {abierto && posicion && destino
        ? createPortal(
            <div className={styles.lista} role="listbox" ref={lista} style={posicion}>
              {visibles.length === 0 ? (
                <div className={styles.vacio}>{vacio}</div>
              ) : (
                visibles.map((o, i) => (
                  <button
                    key={o.value}
                    type="button"
                    role="option"
                    aria-selected={o.value === value}
                    data-resaltado={i === resaltado ? "1" : "0"}
                    className={cx(styles.opcion, i === resaltado && styles.resaltada)}
                    {...(o.title ? { title: o.title } : {})}
                    onMouseEnter={() => setResaltado(i)}
                    onClick={() => elegir(o.value)}
                  >
                    <span className={styles.valor}>{o.value}</span>
                    {o.tag ? <span className={styles.tag}>{o.tag}</span> : null}
                  </button>
                ))
              )}
            </div>,
            destino,
          )
        : null}
    </div>
  );
}

/**
 * Dentro de qué elemento se dibuja la lista.
 *
 * Un `<dialog>` abierto con `showModal()` vive en la **capa superior** del
 * navegador: todo lo que está fuera de él queda debajo y además inerte, sin
 * recibir clics, por más `z-index` que se le ponga. Un portal a `<body>` haría
 * desaparecer la lista justo donde más se usa —los tres diálogos de edición—,
 * así que cuando el campo está adentro de un diálogo la lista va adentro del
 * mismo diálogo, pero colgada de él y no del cuerpo con scroll.
 */
function contenedorDe(el: HTMLElement | null): HTMLElement {
  return el?.closest("dialog") ?? document.body;
}

/**
 * Dónde y de qué tamaño va la lista.
 *
 * Se abre hacia abajo salvo que abajo no entre nada: ahí va arriba, que es lo
 * que hace cualquier desplegable y lo que espera quien lo usa. El alto máximo
 * sale del espacio que de verdad hay, así que la lista nunca se sale de la
 * ventana ni tapa el borde.
 */
function medir(el: HTMLElement | null): CSSProperties | null {
  if (!el) return null;
  const r = el.getBoundingClientRect();
  const abajo = window.innerHeight - r.bottom - SEPARACION * 2;
  const arriba = r.top - SEPARACION * 2;
  const haciaArriba = abajo < ALTO_MINIMO && arriba > abajo;

  const alto = Math.max(0, Math.min(ALTO_MAXIMO, haciaArriba ? arriba : abajo));
  return {
    position: "fixed",
    left: r.left,
    width: r.width,
    maxHeight: alto,
    ...(haciaArriba
      ? { bottom: window.innerHeight - r.top + SEPARACION }
      : { top: r.bottom + SEPARACION }),
  };
}
