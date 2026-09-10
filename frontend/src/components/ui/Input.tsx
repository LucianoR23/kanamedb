import { useId } from "react";
import type { InputHTMLAttributes, ReactNode, TextareaHTMLAttributes } from "react";
import { cx } from "../../lib/cx";
import styles from "./Input.module.css";

interface InputProps extends Omit<InputHTMLAttributes<HTMLInputElement>, "size"> {
  invalid?: boolean;
}

export function Input({ invalid = false, className, ...rest }: InputProps) {
  return (
    <input
      {...rest}
      aria-invalid={invalid || undefined}
      className={cx(styles.control, invalid && styles.invalid, className)}
    />
  );
}

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  invalid?: boolean;
}

/**
 * El mismo control que Input, pero de varias líneas.
 *
 * Existe porque el visor de celda edita valores que no entran en una línea —un
 * JSON, un texto largo—, que es justamente para lo que sirve abrir el visor en
 * vez de escribir en la celda.
 */
export function Textarea({ invalid = false, className, ...rest }: TextareaProps) {
  return (
    <textarea
      {...rest}
      aria-invalid={invalid || undefined}
      className={cx(styles.control, styles.area, invalid && styles.invalid, className)}
    />
  );
}

/** Etiqueta + control + mensaje de error, en la grilla de dos columnas de S00. */
export function Field({
  label,
  error,
  children,
}: {
  label: string;
  error?: string;
  children: ReactNode;
}) {
  const id = useId();
  return (
    <>
      <div className={styles.field}>
        <label className={styles.label} htmlFor={id}>
          {label}
        </label>
        <div id={id}>{children}</div>
      </div>
      {error ? (
        <div className={styles.field}>
          <span className={styles.error} role="alert">
            {error}
          </span>
        </div>
      ) : null}
    </>
  );
}

export function SearchInput({
  hint,
  className,
  ...rest
}: InputHTMLAttributes<HTMLInputElement> & { hint?: string }) {
  return (
    <div className={cx(styles.search, className)}>
      <span className={styles.searchIcon} aria-hidden="true">
        ⌕
      </span>
      <input type="search" {...rest} className={styles.searchInput} />
      {hint ? <span className={styles.searchHint}>{hint}</span> : null}
    </div>
  );
}

/** Disparador de select. El desplegable en sí lo pone quien lo usa, con
 *  ContextMenu — el <select> nativo ignora el tema en Windows. */
export function SelectTrigger({
  value,
  disabled = false,
  onClick,
}: {
  value: string;
  disabled?: boolean;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      className={styles.select}
      disabled={disabled}
      onClick={onClick}
    >
      <span>{value}</span>
      <span className={styles.caret} aria-hidden="true">
        ▼
      </span>
    </button>
  );
}

export function Toggle({
  checked,
  onChange,
  label,
  disabled = false,
}: {
  checked: boolean;
  onChange: (next: boolean) => void;
  label: string;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      disabled={disabled}
      className={styles.toggle}
      onClick={() => onChange(!checked)}
    >
      <span className={styles.knob} />
    </button>
  );
}

export function Checkbox({
  checked,
  onChange,
  children,
  disabled = false,
}: {
  checked: boolean;
  onChange: (next: boolean) => void;
  children: ReactNode;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      disabled={disabled}
      className={styles.checkbox}
      onClick={() => onChange(!checked)}
    >
      <span className={styles.box} aria-hidden="true">
        ✓
      </span>
      {children}
    </button>
  );
}
