import { useEffect, useLayoutEffect, useRef, useState } from "react";
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

export type MenuEntry = MenuAction | MenuSeparator | MenuLabel;

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
      {entries.map((e) => {
        if (e.kind === "separator") {
          return <div key={e.id} className={styles.separator} role="separator" />;
        }
        if (e.kind === "label") {
          return (
            <div key={e.id} className={styles.sectionLabel}>
              {e.label}
            </div>
          );
        }
        return (
          <button
            key={e.id}
            type="button"
            role="menuitem"
            disabled={e.disabled}
            className={cx(styles.item, e.destructive && styles.destructive)}
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
    </div>
  );
}
