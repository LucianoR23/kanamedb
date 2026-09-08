import type { ReactNode } from "react";
import { cx } from "../../lib/cx";
import styles from "./Badge.module.css";

/** Entornos de conexión. El color es el mismo que marca el borde de la ventana
 *  y la barra de estado, para que no haya duda de contra qué estás escribiendo. */
export type Environment = "local" | "dev" | "staging" | "production";

const ENV_STYLE: Record<Environment, string | undefined> = {
  local: styles.local,
  dev: styles.dev,
  staging: styles.staging,
  production: styles.production,
};

const ENV_LABEL: Record<Environment, string> = {
  local: "Local",
  dev: "Dev",
  staging: "Staging",
  production: "Production",
};

export function EnvBadge({ env }: { env: Environment }) {
  return (
    <span className={cx(styles.badge, styles.env, ENV_STYLE[env])}>
      {ENV_LABEL[env]}
    </span>
  );
}

export type BadgeTone = "neutral" | "warning" | "danger" | "info";

const TONE: Record<BadgeTone, string | undefined> = {
  neutral: styles.neutral,
  warning: cx(styles.mono, styles.warning),
  danger: cx(styles.mono, styles.danger),
  info: styles.info,
};

export function Badge({
  tone = "neutral",
  children,
}: {
  tone?: BadgeTone;
  children: ReactNode;
}) {
  return <span className={cx(styles.badge, TONE[tone])}>{children}</span>;
}
