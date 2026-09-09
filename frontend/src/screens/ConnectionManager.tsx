import { useState } from "react";
import type { ConnectionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Environment } from "../../bindings/github.com/LucianoR23/kanamedb/internal/connection";
import {
  Badge,
  Button,
  ContextMenu,
  Dialog,
  EnvBadge,
  SearchInput,
} from "../components/ui";
import type { MenuAnchor, MenuEntry } from "../components/ui";
import { DevSignature } from "../components/DevSignature";
import { cx } from "../lib/cx";
import styles from "./ConnectionManager.module.css";

const GROUPS: { env: Environment; label: string }[] = [
  { env: Environment.Local, label: "Local" },
  { env: Environment.Dev, label: "Dev" },
  { env: Environment.Staging, label: "Staging" },
  { env: Environment.Production, label: "Production" },
];

type Filter = "all" | "production";

interface Props {
  connections: readonly ConnectionView[];
  onNew: () => void;
  onEdit: (view: ConnectionView) => void;
  /** Cambia el modo de solo lectura de una conexión.
   *
   *  Es el único interruptor de protecciones que se adelantó a esta
   *  iteración. Lo primero que puede escribir en la base es el editor SQL,
   *  que llega ahora; la tab Safety completa recién está en la 5, y hasta
   *  entonces la única forma de activarlo era editar connections.toml. */
  onToggleReadOnly: (view: ConnectionView, readOnly: boolean) => void;
  onConnect: (view: ConnectionView) => void;
  onDuplicate: (id: string) => void;
  onDelete: (id: string) => void;
  onAbout: () => void;
  /** Error de la última acción, si hubo. */
  error?: string | null;
}

export function ConnectionManager({
  connections,
  onNew,
  onEdit,
  onToggleReadOnly,
  onConnect,
  onDuplicate,
  onDelete,
  onAbout,
  error,
}: Props) {
  const [selectedId, setSelectedId] = useState<string | null>(
    connections[0]?.connection.id ?? null,
  );
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [menu, setMenu] = useState<MenuAnchor | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<ConnectionView | null>(null);

  const produccion = connections.filter(
    (c) => c.connection.environment === Environment.Production,
  ).length;

  const visibles = connections.filter((c) => {
    if (filter === "production" && c.connection.environment !== Environment.Production) {
      return false;
    }
    if (query === "") return true;
    const q = query.toLowerCase();
    return (
      c.connection.name.toLowerCase().includes(q) ||
      c.connection.host.toLowerCase().includes(q) ||
      c.connection.database.toLowerCase().includes(q)
    );
  });

  const selected =
    visibles.find((c) => c.connection.id === selectedId) ?? visibles[0] ?? null;

  const menuEntries: MenuEntry[] = selected
    ? [
        {
          id: "connect",
          label: "Conectar",
          hint: "↵",
          disabled: (selected.problems?.length ?? 0) > 0,
          disabledReason: "mal configurada",
          onSelect: () => onConnect(selected),
        },
        { id: "edit", label: "Editar…", onSelect: () => onEdit(selected) },
        { kind: "separator", id: "s1" },
        {
          id: "duplicate",
          label: "Duplicar",
          onSelect: () => onDuplicate(selected.connection.id),
        },
        {
          id: "copy",
          label: "Copiar la URI",
          onSelect: () => void navigator.clipboard.writeText(selected.uri),
        },
        { kind: "separator", id: "s2" },
        {
          id: "delete",
          label: "Borrar conexión…",
          destructive: true,
          onSelect: () => setConfirmDelete(selected),
        },
      ]
    : [];

  return (
    <div className={styles.screen}>
      <header className={styles.titlebar}>
        <span className={styles.wordmark}>KANAME</span>
        <span className={styles.divider} />
        <span className={styles.section}>Conexiones</span>
        <span className={styles.spacer} />
        <Button variant="primary" size="sm" onClick={onNew}>
          Nueva conexión
        </Button>
      </header>

      {error ? (
        <div className={styles.banner} role="alert">
          {error}
        </div>
      ) : null}

      <div className={styles.body}>
        <aside className={styles.list}>
          <div className={styles.listHead}>
            <SearchInput
              placeholder="Filtrar conexiones…"
              value={query}
              onChange={(e) => setQuery(e.currentTarget.value)}
            />
            <div className={styles.filters}>
              <button
                type="button"
                className={cx(styles.filter, filter === "all" && styles.filterOn)}
                onClick={() => setFilter("all")}
              >
                Todas {connections.length}
              </button>
              <button
                type="button"
                className={cx(styles.filter, filter === "production" && styles.filterOn)}
                onClick={() => setFilter("production")}
              >
                Producción {produccion}
              </button>
            </div>
          </div>

          <div className={styles.groups} role="listbox" aria-label="Conexiones">
            {visibles.length === 0 ? (
              <p className={styles.emptyList}>
                {connections.length === 0
                  ? "Todavía no hay ninguna conexión."
                  : "Ninguna conexión coincide con el filtro."}
              </p>
            ) : (
              GROUPS.map((g) => {
                const filas = visibles.filter((c) => c.connection.environment === g.env);
                if (filas.length === 0) return null;
                return (
                  <div key={g.env} className={styles.group}>
                    <div className={cx(styles.groupHead, styles[`env_${g.env}`])}>
                      <span className={styles.groupLabel}>{g.label}</span>
                      <span className={styles.groupRule} />
                      <span className={styles.groupCount}>{filas.length}</span>
                    </div>
                    {filas.map((c) => (
                      <div
                        key={c.connection.id}
                        role="option"
                        tabIndex={0}
                        aria-selected={c.connection.id === selected?.connection.id}
                        className={cx(
                          styles.row,
                          styles[`env_${c.connection.environment}`],
                          c.connection.id === selected?.connection.id && styles.rowOn,
                        )}
                        onClick={() => setSelectedId(c.connection.id)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") onConnect(c);
                        }}
                        onContextMenu={(e) => {
                          e.preventDefault();
                          setSelectedId(c.connection.id);
                          setMenu({ x: e.clientX, y: e.clientY });
                        }}
                      >
                        <span className={styles.rowDot} />
                        <div className={styles.rowText}>
                          <div className={styles.rowName}>{c.connection.name}</div>
                          <div className={styles.rowHost}>
                            {c.connection.host}:{c.connection.port}
                          </div>
                        </div>
                        {(c.problems?.length ?? 0) > 0 ? (
                          <Badge tone="danger">rota</Badge>
                        ) : null}
                        {c.connection.safety.readOnly ? (
                          <Badge tone="neutral">RO</Badge>
                        ) : null}
                        {!c.hasPassword ? (
                          <span className={styles.noKey} title="Sin contraseña en esta máquina">
                            sin clave
                          </span>
                        ) : null}
                      </div>
                    ))}
                  </div>
                );
              })
            )}
          </div>

          <div className={styles.listFoot}>
            <span className={styles.spacer} />
            <span>
              {connections.length} {connections.length === 1 ? "conexión" : "conexiones"}
            </span>
          </div>
        </aside>

        <main className={styles.detail}>
          {selected ? (
            <Detail
              view={selected}
              onToggleReadOnly={onToggleReadOnly}
              onEdit={() => onEdit(selected)}
              onConnect={() => onConnect(selected)}
              onMenu={(e) => setMenu({ x: e.clientX, y: e.clientY })}
            />
          ) : (
            <div className={styles.detailEmpty}>
              <p className={styles.emptyTitle}>Ninguna conexión seleccionada</p>
            </div>
          )}
        </main>
      </div>

      <footer className={styles.statusbar}>
        <span>
          {selected
            ? `${selected.connection.name} seleccionada · sin conectar`
            : "sin conexiones"}
        </span>
        <span className={styles.spacer} />
        <DevSignature />
        <span className={styles.statusSep} />
        <button type="button" className={styles.statusLink} onClick={onAbout}>
          about
        </button>
      </footer>

      <ContextMenu anchor={menu} entries={menuEntries} onClose={() => setMenu(null)} />

      <Dialog
        open={confirmDelete !== null}
        title={`¿Borrar ${confirmDelete?.connection.name ?? ""}?`}
        onClose={() => setConfirmDelete(null)}
        footer={
          <>
            <Button onClick={() => setConfirmDelete(null)}>Cancelar</Button>
            <Button
              variant="danger"
              onClick={() => {
                if (confirmDelete) onDelete(confirmDelete.connection.id);
                setConfirmDelete(null);
              }}
            >
              Borrar
            </Button>
          </>
        }
      >
        Se borra la conexión y <strong>también su contraseña del keychain</strong>. Eso
        último no se puede deshacer. La base de datos no se toca.
      </Dialog>
    </div>
  );
}

function Detail({
  view,
  onToggleReadOnly,
  onEdit,
  onConnect,
  onMenu,
}: {
  view: ConnectionView;
  onToggleReadOnly: (view: ConnectionView, readOnly: boolean) => void;
  onEdit: () => void;
  onConnect: () => void;
  onMenu: (e: React.MouseEvent) => void;
}) {
  const c = view.connection;
  const production = c.environment === Environment.Production;
  const problems = view.problems ?? [];

  return (
    <>
      <div className={cx(styles.detailHead, production && styles.detailHeadProd)}>
        <div className={styles.detailTitleRow}>
          <div className={styles.detailTitleText}>
            <div className={styles.detailTitle}>
              <span className={styles.detailName}>{c.name}</span>
              <EnvBadge env={c.environment as "local" | "dev" | "staging" | "production"} />
              {c.safety.readOnly ? <Badge tone="neutral">Solo lectura</Badge> : null}
            </div>
            <div className={styles.detailUri}>{view.uri}</div>
          </div>
          <div className={styles.detailActions}>
            <Button
              variant={production ? "danger" : "primary"}
              size="lg"
              disabled={problems.length > 0}
              onClick={onConnect}
            >
              Conectar
            </Button>
            <Button size="lg" onClick={onEdit}>
              Editar
            </Button>
            <Button size="lg" aria-label="Más acciones" onClick={onMenu}>
              ⋯
            </Button>
          </div>
        </div>

        {problems.length > 0 ? (
          <div className={cx(styles.notice, styles.noticeDanger)}>
            <strong>Esta conexión está mal configurada.</strong>
            <ul className={styles.noticeList}>
              {problems.map((p) => (
                <li key={p.field}>{p.message}</li>
              ))}
            </ul>
          </div>
        ) : null}

        {production ? (
          <div className={cx(styles.notice, styles.noticeProd)}>
            Producción. Escribir exige tipear el nombre de la base, y las sentencias
            destructivas se listan con las filas afectadas antes de correr.
          </div>
        ) : null}

        {(view.warnings ?? []).length > 0 ? (
          <div className={cx(styles.notice, styles.noticeWarn)}>
            <ul className={styles.noticeList}>
              {(view.warnings ?? []).map((w) => (
                <li key={w.field}>{w.message}</li>
              ))}
            </ul>
          </div>
        ) : null}
      </div>

      <div className={styles.detailBody}>
        <section>
          <h2 className={styles.sectionLabel}>Conexión</h2>
          <dl className={styles.table}>
            <Row label="Motor" value="PostgreSQL" />
            <Row label="Host" value={`${c.host}:${c.port}`} />
            <Row label="Base" value={c.database} />
            <Row label="Usuario" value={c.user} />
            <Row
              label="Contraseña"
              value={
                view.hasPassword
                  ? `keychain · ${view.keychainRef}`
                  : "sin guardar en esta máquina"
              }
              muted={!view.hasPassword}
            />
            <Row label="TLS" value={c.sslMode} />
          </dl>
        </section>

        <section>
          <h2 className={styles.sectionLabel}>Protecciones</h2>
          <ul className={styles.safety}>
            <SafetyRow
              on={c.safety.readOnly}
              label="Abrir en solo lectura"
              onToggle={(v) => onToggleReadOnly(view, v)}
            />
            <SafetyRow
              on={!c.safety.allowApplyWithoutPreview}
              label="Exigir preview de SQL antes de aplicar"
            />
            <SafetyRow
              on={production || !c.safety.allowWriteWithoutConfirmation}
              label="Tipear el nombre de la base para confirmar escrituras"
              locked={production}
            />
            <SafetyRow on={c.safety.blockDropTruncate} label="Bloquear DROP y TRUNCATE" />
          </ul>
          <p className={styles.safetyNote}>
            Solo lectura se cambia acá. Las otras tres se configuran en la tab Safety del
            editor, que llega en la Iteración 5; hasta entonces se editan en
            connections.toml, y los valores ya se guardan y se respetan.
          </p>
        </section>
      </div>
    </>
  );
}

function Row({ label, value, muted = false }: { label: string; value: string; muted?: boolean }) {
  return (
    <div className={styles.tableRow}>
      <dt className={styles.tableKey}>{label}</dt>
      <dd className={cx(styles.tableValue, muted && styles.tableValueMuted)}>{value}</dd>
    </div>
  );
}

/**
 * Una protección.
 *
 * Con `onToggle` es un interruptor de verdad; sin él, un punto de estado. La
 * distinción es deliberada: un switch dibujado que no responde al clic se lee
 * como una app rota, no como una función pendiente. La forma del control es una
 * promesa, y no se hace ninguna que no se pueda cumplir.
 */
function SafetyRow({
  on,
  label,
  locked = false,
  onToggle,
}: {
  on: boolean;
  label: string;
  locked?: boolean;
  onToggle?: (value: boolean) => void;
}) {
  const cuerpo = (
    <>
      <span className={cx(styles.safetyDot, on && styles.safetyDotOn)} aria-hidden="true" />
      <span className={cx(styles.safetyLabel, !on && styles.safetyLabelOff)}>{label}</span>
      <span className={styles.srOnly}>{on ? "activa" : "inactiva"}</span>
      {locked ? <span className={styles.safetyLocked}>no se puede apagar</span> : null}
    </>
  );
  if (!onToggle || locked) {
    return <li className={styles.safetyRow}>{cuerpo}</li>;
  }
  return (
    <li>
      <button
        type="button"
        role="switch"
        aria-checked={on}
        className={cx(styles.safetyRow, styles.safetyRowButton)}
        onClick={() => onToggle(!on)}
      >
        {cuerpo}
      </button>
    </li>
  );
}
