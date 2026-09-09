import { Glyph } from "./Glyph";
import type { ObjectKind } from "./Glyph";
import type { Environment } from "./Badge";
import { cx } from "../../lib/cx";
import styles from "./Tabs.module.css";

const ENV_RULE: Record<Environment, string | undefined> = {
  local: styles.envLocal,
  dev: styles.envDev,
  staging: styles.envStaging,
  production: styles.envProduction,
};

export interface TabItem {
  id: string;
  label: string;
  kind?: ObjectKind;
  /** Tiene cambios sin aplicar. */
  dirty?: boolean;
}

export function TabStrip({
  tabs,
  activeId,
  env,
  onSelect,
  onClose,
  onNew,
}: {
  tabs: readonly TabItem[];
  activeId: string | null;
  /** Pinta la línea superior con el color del entorno. */
  env?: Environment;
  onSelect?: (id: string) => void;
  onClose?: (id: string) => void;
  onNew?: () => void;
}) {
  return (
    <div
      role="tablist"
      className={cx(styles.strip, env && ENV_RULE[env])}
    >
      {tabs.map((t) => (
        <div
          key={t.id}
          role="tab"
          tabIndex={0}
          aria-selected={t.id === activeId}
          className={cx(styles.tab, t.id === activeId && styles.active)}
          onClick={() => onSelect?.(t.id)}
          // Botón del medio para cerrar, como en cualquier navegador o editor.
          // `onAuxClick` y no `onMouseUp` porque es el evento que existe para
          // esto; el `preventDefault` del `onMouseDown` es lo que evita que
          // Windows entre en modo autoscroll y deje el cursor de las flechitas
          // dando vueltas.
          onMouseDown={(e) => {
            if (e.button === 1) e.preventDefault();
          }}
          onAuxClick={(e) => {
            if (e.button !== 1 || !onClose) return;
            e.preventDefault();
            onClose(t.id);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === " ") {
              e.preventDefault();
              onSelect?.(t.id);
            }
          }}
        >
          {t.kind ? <Glyph kind={t.kind} /> : null}
          {t.label}
          {t.dirty ? (
            <span className={styles.dirty} title="Cambios sin aplicar" />
          ) : null}
          {onClose ? (
            <button
              type="button"
              className={styles.close}
              aria-label={`Cerrar ${t.label}`}
              onClick={(e) => {
                e.stopPropagation();
                onClose(t.id);
              }}
            >
              ✕
            </button>
          ) : null}
        </div>
      ))}
      {onNew ? (
        <button
          type="button"
          className={styles.new}
          aria-label="Nueva pestaña"
          onClick={onNew}
        >
          ＋
        </button>
      ) : null}
    </div>
  );
}

export interface PillItem {
  id: string;
  label: string;
  /** Deshabilita la pestaña. Para secciones que existen en el diseño y todavía
   *  no se construyeron: se ven, no se pueden elegir, y `title` explica por qué. */
  disabled?: boolean;
  title?: string;
  /** Cuántos elementos hay detrás de la pestaña. Se muestra apagado al lado
   *  del texto: sin el número hay que entrar a cada una para descubrir cuáles
   *  tienen algo. Cero se muestra igual — decir "0 triggers" es informacion.
   *
   *  Acepta `undefined` explícito porque el proyecto compila con
   *  `exactOptionalPropertyTypes`: quien todavía no sabe el número lo pasa sin
   *  tener que armar el objeto de dos formas distintas. */
  count?: number | undefined;
}

export function PillTabs({
  items,
  activeId,
  onSelect,
  ariaLabel,
}: {
  items: readonly PillItem[];
  activeId: string;
  onSelect: (id: string) => void;
  ariaLabel: string;
}) {
  return (
    <div role="tablist" aria-label={ariaLabel} className={styles.pills}>
      {items.map((it) => (
        <button
          key={it.id}
          type="button"
          role="tab"
          aria-selected={it.id === activeId}
          disabled={it.disabled ?? false}
          {...(it.title ? { title: it.title } : {})}
          className={cx(styles.pill, it.id === activeId && styles.pillActive)}
          onClick={() => onSelect(it.id)}
        >
          {it.label}
          {it.count === undefined ? null : (
            <span className={styles.pillCount}>{it.count}</span>
          )}
        </button>
      ))}
    </div>
  );
}
