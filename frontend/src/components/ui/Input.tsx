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

/**
 * Elegir UNA cosa entre pocas, todas a la vista.
 *
 * Existe porque faltaba y se estaba usando `<input type="radio">` crudo, que es
 * justo lo que CLAUDE.md prohíbe: el nativo se pinta con los colores del sistema
 * operativo, así que ignora el tema y los acentos de entorno — en el tema claro
 * queda un círculo azul de Windows adentro de una paleta que no es ésa.
 *
 * Va con `role="radiogroup"` y botones de verdad, no con `<label>` alrededor de
 * un input escondido: así las flechas del teclado las maneja el navegador sobre
 * elementos enfocables reales y el estado vive en `aria-checked`, que es lo que
 * lee un lector de pantalla.
 *
 * Para más de cinco o seis opciones está `Combobox`: una lista larga de radios
 * es una lista que nadie lee.
 */
export function RadioGroup<T extends string>({
  value,
  onChange,
  options,
  label,
  disabled = false,
}: {
  value: T;
  onChange: (next: T) => void;
  options: readonly { readonly value: T; readonly label: string }[];
  /** Qué se está eligiendo. No se dibuja: lo anuncia el lector de pantalla. */
  label: string;
  disabled?: boolean;
}) {
  return (
    <div role="radiogroup" aria-label={label} className={styles.radios}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={value === o.value}
          disabled={disabled}
          className={styles.radio}
          onClick={() => onChange(o.value)}
        >
          <span className={styles.punto} aria-hidden="true" />
          {o.label}
        </button>
      ))}
    </div>
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
