import type { ButtonHTMLAttributes, ReactNode } from "react";
import { cx } from "../../lib/cx";
import styles from "./Button.module.css";

export type ButtonVariant =
  | "primary"
  | "secondary"
  | "ghost"
  | "danger"
  | "dangerOutline";

export type ButtonSize = "sm" | "md" | "lg";

const VARIANTS: Record<ButtonVariant, string | undefined> = {
  primary: styles.primary,
  secondary: styles.secondary,
  ghost: styles.ghost,
  danger: styles.danger,
  dangerOutline: styles.dangerOutline,
};

const SIZES: Record<ButtonSize, string | undefined> = {
  sm: styles.sm,
  md: "",
  lg: styles.lg,
};

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant;
  size?: ButtonSize;
  /** Muestra un spinner y deshabilita el botón. */
  loading?: boolean;
  children?: ReactNode;
}

export function Button({
  variant = "secondary",
  size = "md",
  loading = false,
  disabled,
  className,
  children,
  ...rest
}: ButtonProps) {
  return (
    <button
      type="button"
      {...rest}
      disabled={disabled === true || loading}
      aria-busy={loading || undefined}
      className={cx(styles.button, VARIANTS[variant], SIZES[size], className)}
    >
      {loading ? <span className={styles.spinner} aria-hidden="true" /> : null}
      {children}
    </button>
  );
}

/** Ficha de atajo con la misma altura que un botón, para poner al lado. */
export function ShortcutChip({ children }: { children: ReactNode }) {
  return <span className={styles.shortcut}>{children}</span>;
}
