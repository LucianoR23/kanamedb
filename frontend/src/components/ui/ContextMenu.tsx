import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { CSSProperties } from "react";
import { cx } from "../../lib/cx";
import styles from "./ContextMenu.module.css";

export interface MenuAction {
  kind?: "action";
  id: string;
  label: string;
  /** Atajo mostrado a la derecha. */
  hint?: string;
  destructive?: boolean;
  disabled?: boolean;
  /** Por qué está deshabilitado: "solo lectura", "sin PK". Se muestra en vez
   *  del atajo, para que el usuario no tenga que adivinar. */
  disabledReason?: string;
  onSelect: () => void;
}

export interface MenuSeparator {
  kind: "separator";
  id: string;
}

export interface MenuLabel {
  kind: "label";
  id: string;
  label: string;
}

/**
 * Una entrada que abre otro menú al costado: «Mover a carpeta ▸».
 *
 * Existe para las listas que no se conocen de antemano —las carpetas las
 * inventa quien usa la app— y que no caben desplegadas en el menú principal
 * sin taparlo. Un nivel: lo que hay adentro son acciones, separadores y
 * rótulos, no otro submenú.
 */
export interface MenuSubmenu {
  kind: "submenu";
  id: string;
  label: string;
  entries: readonly MenuLeaf[];
  disabled?: boolean;
  disabledReason?: string;
}

/** Lo que puede haber dentro de un submenú: todo menos otro submenú. */
export type MenuLeaf = MenuAction | MenuSeparator | MenuLabel;

export type MenuEntry = MenuAction | MenuSeparator | MenuLabel | MenuSubmenu;

export interface MenuAnchor {
  x: number;
  y: number;
}

export function ContextMenu({
  anchor,
  entries,
  onClose,
}: {
  /** `null` cierra el menú. */
  anchor: MenuAnchor | null;
  entries: readonly MenuEntry[];
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<MenuAnchor | null>(anchor);

  // Reposicionar si el menú se sale de la ventana. Se hace antes de pintar
  // para que no se vea saltar.
  useLayoutEffect(() => {
    if (!anchor) {
      setPos(null);
      return;
    }
    const el = ref.current;
    if (!el) {
      setPos(anchor);
      return;
    }
    const { width, height } = el.getBoundingClientRect();
    setPos({
      x: Math.max(4, Math.min(anchor.x, window.innerWidth - width - 4)),
      y: Math.max(4, Math.min(anchor.y, window.innerHeight - height - 4)),
    });
  }, [anchor]);

  useEffect(() => {
    if (!anchor) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    const onDown = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) onClose();
    };
    window.addEventListener("keydown", onKey);
    window.addEventListener("mousedown", onDown);
    window.addEventListener("resize", onClose);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("mousedown", onDown);
      window.removeEventListener("resize", onClose);
    };
  }, [anchor, onClose]);

  if (!anchor) return null;

  return (
    <div
      ref={ref}
      role="menu"
      className={styles.menu}
      style={{
        left: pos?.x ?? anchor.x,
        top: pos?.y ?? anchor.y,
        // Hasta medir, invisible pero con layout, para no mostrar el salto.
        visibility: pos ? "visible" : "hidden",
      }}
    >
      <Entradas entries={entries} onClose={onClose} />
    </div>
  );
}

/**
 * Las entradas de un menú, el principal o uno desplegado al costado.
 *
 * Lleva el estado de qué submenú está abierto, y es uno solo: pasar el mouse
 * por otra entrada lo cierra, como en cualquier menú del sistema. Sin eso,
 * dos desplegados a la vez se tapan entre sí.
 */
function Entradas({
  entries,
  onClose,
}: {
  entries: readonly MenuEntry[];
  onClose: () => void;
}) {
  const [abierto, setAbierto] = useState<string | null>(null);

  return (
    <>
      {entries.map((e) => {
        // Pasar por encima de cualquier otra entrada cierra el submenú abierto,
        // también por un separador o un rótulo: si no, quedaba colgado hasta
        // llegar a la próxima acción.
        if (e.kind === "separator") {
          return (
            <div
              key={e.id}
              className={styles.separator}
              role="separator"
              onMouseEnter={() => setAbierto(null)}
            />
          );
        }
        if (e.kind === "label") {
          return (
            <div key={e.id} className={styles.sectionLabel} onMouseEnter={() => setAbierto(null)}>
              {e.label}
            </div>
          );
        }
        if (e.kind === "submenu") {
          return (
            <Submenu
              key={e.id}
              entry={e}
              abierto={abierto === e.id}
              onAbrir={() => setAbierto(e.id)}
              onCerrar={() => setAbierto(null)}
              onClose={onClose}
            />
          );
        }
        return (
          <button
            key={e.id}
            type="button"
            role="menuitem"
            disabled={e.disabled}
            className={cx(styles.item, e.destructive && styles.destructive)}
            onMouseEnter={() => setAbierto(null)}
            onClick={() => {
              e.onSelect();
              onClose();
            }}
          >
            {e.label}
            <span className={styles.spacer} />
            {e.disabled && e.disabledReason ? (
              <span className={styles.reason}>{e.disabledReason}</span>
            ) : e.hint ? (
              <span className={styles.hint}>{e.hint}</span>
            ) : null}
          </button>
        );
      })}
    </>
  );
}

function Submenu({
  entry,
  abierto,
  onAbrir,
  onCerrar,
  onClose,
}: {
  entry: MenuSubmenu;
  abierto: boolean;
  onAbrir: () => void;
  /** Cierra solo el desplegado; el menú queda. */
  onCerrar: () => void;
  /** Cierra el menú entero: se eligió algo. */
  onClose: () => void;
}) {
  const disparador = useRef<HTMLButtonElement>(null);
  const flyout = useRef<HTMLDivElement>(null);
  const [ajuste, setAjuste] = useState<CSSProperties>({});
  // Si se abrió con → el foco tiene que pasar al primer ítem; con el mouse no,
  // porque robarle el foco a lo que se está mirando descoloca.
  const porTeclado = useRef(false);

  useEffect(() => {
    if (!abierto || !porTeclado.current) return;
    porTeclado.current = false;
    flyout.current?.querySelector<HTMLElement>("[role=menuitem]:not(:disabled)")?.focus();
  }, [abierto]);

  // El desplegado sale hacia la derecha, que es donde hay lugar casi siempre.
  // Cuando no lo hay, va a la izquierda; y si se pasa por abajo, sube lo justo.
  // Se mide después de pintar y se corrige antes de que se vea.
  useLayoutEffect(() => {
    if (!abierto) {
      setAjuste({});
      return;
    }
    const el = flyout.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const next: CSSProperties = {};
    if (r.right > window.innerWidth - 4) {
      next.left = "auto";
      next.right = "100%";
    }
    const sobra = r.bottom - (window.innerHeight - 4);
    if (sobra > 0) next.marginTop = -sobra;
    setAjuste(next);
  }, [abierto]);

  const puede = !entry.disabled;

  return (
    <div className={styles.subWrap} onMouseEnter={puede ? onAbrir : undefined}>
      <button
        ref={disparador}
        type="button"
        role="menuitem"
        aria-haspopup="menu"
        aria-expanded={abierto}
        disabled={entry.disabled}
        className={cx(styles.item, abierto && styles.itemAbierto)}
        onClick={puede ? onAbrir : undefined}
        onKeyDown={(e) => {
          if ((e.key === "ArrowRight" || e.key === "Enter" || e.key === " ") && puede) {
            e.preventDefault();
            porTeclado.current = true;
            onAbrir();
          } else if (e.key === "ArrowLeft" && abierto) {
            e.preventDefault();
            onCerrar();
          }
        }}
      >
        {entry.label}
        <span className={styles.spacer} />
        {entry.disabled && entry.disabledReason ? (
          <span className={styles.reason}>{entry.disabledReason}</span>
        ) : (
          <span className={styles.chevron} aria-hidden="true">
            ▸
          </span>
        )}
      </button>
      {abierto ? (
        <div
          ref={flyout}
          role="menu"
          className={cx(styles.menu, styles.flyout)}
          style={ajuste}
          onKeyDown={(e) => {
            // ← vuelve al disparador con el desplegado cerrado. Escape sigue
            // cerrando el menú entero, que es lo que hace en el resto de la app.
            if (e.key === "ArrowLeft") {
              e.preventDefault();
              e.stopPropagation();
              onCerrar();
              disparador.current?.focus();
            }
          }}
        >
          <Entradas entries={entry.entries} onClose={onClose} />
        </div>
      ) : null}
    </div>
  );
}
