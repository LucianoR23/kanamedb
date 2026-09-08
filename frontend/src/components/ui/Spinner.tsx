import { cx } from "../../lib/cx";
import styles from "./Spinner.module.css";

/**
 * El indicador de que algo está en curso.
 *
 * Vive acá y no en cada pantalla porque ya estaba copiado en tres lugares con
 * sus propios keyframes: tres definiciones del mismo giro que se separan en
 * cuanto alguien toca una.
 */
export function Spinner({ size = "md" }: { size?: "sm" | "md" }) {
  return <span className={cx(styles.spinner, size === "sm" && styles.sm)} aria-hidden="true" />;
}
