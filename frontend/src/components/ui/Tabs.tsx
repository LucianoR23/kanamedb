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
          className={cx(styles.pill, it.id === activeId && styles.pillActive)}
          onClick={() => onSelect(it.id)}
        >
          {it.label}
        </button>
      ))}
    </div>
  );
}
