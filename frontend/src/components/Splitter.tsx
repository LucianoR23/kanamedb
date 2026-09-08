import { useEffect, useRef, useState } from "react";
import { cx } from "../lib/cx";
import styles from "./Splitter.module.css";

interface SplitterProps {
  /** Ancho actual del pane, en píxeles. */
  size: number;
  onResize: (next: number) => void;
  min: number;
  max: number;
  /** "left": el pane está a la izquierda y crece hacia la derecha. */
  side: "left" | "right";
  label: string;
}

/** Manija de redimensionado. Arrastrable con el mouse y con las flechas del
 *  teclado, porque un pane que solo se puede mover arrastrando no es usable
 *  sin mouse. */
export function Splitter({ size, onResize, min, max, side, label }: SplitterProps) {
  const [dragging, setDragging] = useState(false);
  const start = useRef({ x: 0, size: 0 });

  useEffect(() => {
    if (!dragging) return;

    const onMove = (e: MouseEvent) => {
      const delta = e.clientX - start.current.x;
      const next = side === "left" ? start.current.size + delta : start.current.size - delta;
      onResize(Math.max(min, Math.min(max, next)));
    };
    const onUp = () => setDragging(false);

    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    // Sin esto, arrastrar rápido sobre el webview selecciona texto y deja el
    // cursor cambiado a mitad de gesto.
    const prevCursor = document.body.style.cursor;
    document.body.style.cursor = "col-resize";
    return () => {
      window.removeEventListener("mousemove", onMove);
      window.removeEventListener("mouseup", onUp);
      document.body.style.cursor = prevCursor;
    };
  }, [dragging, min, max, side, onResize]);

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      aria-valuenow={size}
      aria-valuemin={min}
      aria-valuemax={max}
      tabIndex={0}
      className={cx(styles.handle, dragging && styles.dragging)}
      onMouseDown={(e) => {
        e.preventDefault();
        start.current = { x: e.clientX, size };
        setDragging(true);
      }}
      onKeyDown={(e) => {
        const step = e.shiftKey ? 40 : 8;
        const dir = side === "left" ? 1 : -1;
        if (e.key === "ArrowLeft") {
          e.preventDefault();
          onResize(Math.max(min, Math.min(max, size - step * dir)));
        }
        if (e.key === "ArrowRight") {
          e.preventDefault();
          onResize(Math.max(min, Math.min(max, size + step * dir)));
        }
      }}
    />
  );
}
