import type { ReactNode } from "react";
import { InfoHint } from "../components/ui";
import { cx } from "../lib/cx";
import styles from "./ConnectionEditor.module.css";

/**
 * Etiqueta + control + error, en la grilla de dos columnas del editor de
 * conexión. Lo comparten las pestañas del editor —General, Túnel SSH, TLS—,
 * y por eso vive en su propio archivo: dentro de ConnectionEditor.tsx una
 * pestaña que lo importara importaría al editor entero.
 */
export function Field({
  label,
  error,
  compact = false,
  align = "center",
  hint,
  children,
}: {
  label: string;
  error?: string | undefined;
  compact?: boolean;
  align?: "center" | "start";
  /** Ayuda que se abre al pasar el mouse o al enfocar. Para lo que no cabe en
   *  la etiqueta pero hace falta ANTES de escribir. */
  hint?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div className={cx(styles.field, compact && styles.fieldCompact)}>
      <label className={cx(styles.label, align === "start" && styles.labelTop)}>
        {label}
        {hint ? <InfoHint label={`Ayuda sobre ${label}`}>{hint}</InfoHint> : null}
      </label>
      <div className={styles.fieldBody}>
        {children}
        {error ? (
          <div className={styles.fieldError} role="alert">
            {error}
          </div>
        ) : null}
      </div>
    </div>
  );
}
