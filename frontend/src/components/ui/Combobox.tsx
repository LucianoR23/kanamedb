import { useEffect, useLayoutEffect, useRef, useState } from "react";
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
 */
export function Combobox({
  value,
  options,
  placeholder,
  ariaLabel,
  disabled = false,
  onChange,
}: {
  value: string;
  options: readonly ComboOption[];
  placeholder?: string;
  ariaLabel: string;
  disabled?: boolean;
  onChange: (v: string) => void;
}) {
  const [abierto, setAbierto] = useState(false);
  const [filtro, setFiltro] = useState("");
  const [resaltado, setResaltado] = useState(0);
  const caja = useRef<HTMLDivElement>(null);
  const lista = useRef<HTMLDivElement>(null);

  // Cerrar al hacer clic afuera. Sin esto el desplegable queda abierto tapando
  // el resto del formulario.
  useEffect(() => {
    if (!abierto) return;
    const fuera = (e: MouseEvent) => {
      if (caja.current && !caja.current.contains(e.target as Node)) setAbierto(false);
    };
    document.addEventListener("mousedown", fuera);
    return () => document.removeEventListener("mousedown", fuera);
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

      {abierto ? (
        <div className={styles.lista} role="listbox" ref={lista}>
          {visibles.length === 0 ? (
            <div className={styles.vacio}>
              Ningún tipo coincide. Lo que escribas se usa igual: la base lo va a validar.
            </div>
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
        </div>
      ) : null}
    </div>
  );
}
