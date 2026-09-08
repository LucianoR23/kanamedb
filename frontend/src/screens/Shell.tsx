import { useEffect, useState } from "react";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import type { SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Snapshot } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import { Splitter } from "../components/Splitter";
import { Badge, Button, EnvBadge, PillTabs, SearchInput, ShortcutChip, TabStrip } from "../components/ui";
import type { TabItem } from "../components/ui";
import { SchemaTree } from "./SchemaTree";
import { SqlEditorScreen } from "./SqlEditorScreen";
import { TableDataScreen } from "./TableDataScreen";
import { cx } from "../lib/cx";
import styles from "./Shell.module.css";

const SIDEBAR = { min: 200, max: 480, initial: 272 };
const RAIL = { min: 220, max: 480, initial: 268 };

const NAV = [
  { id: "objects", label: "Objects" },
  { id: "queries", label: "Queries" },
  { id: "history", label: "History" },
] as const;

/**
 * S05 Workspace shell.
 *
 * Iteración 1: el chrome más el árbol de esquema con las tablas de Postgres.
 * Las pestañas de documento, la grilla y el panel de cambios llegan en las
 * Iteraciones 2 y 5.
 */
export function Shell({
  onOpenAbout,
  onDisconnect,
}: {
  onOpenAbout: () => void;
  onDisconnect: () => void;
}) {
  const [sidebarWidth, setSidebarWidth] = useState(SIDEBAR.initial);
  const [railWidth, setRailWidth] = useState(RAIL.initial);
  const [railOpen, setRailOpen] = useState(true);
  const [nav, setNav] = useState<string>("objects");
  const [query, setQuery] = useState("");

  const [session, setSession] = useState<SessionView | null>(null);
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [schemaError, setSchemaError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [tabs, setTabs] = useState<TabItem[]>([]);
  const [activeTab, setActiveTab] = useState<string | null>(null);

  async function load(refresh: boolean) {
    setLoading(true);
    setSchemaError(null);
    try {
      const [s, snap] = await Promise.all([
        SessionSvc.Current(),
        SessionSvc.Schema(refresh),
      ]);
      setSession(s);
      setSnapshot(snap);
    } catch (err) {
      setSchemaError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load(false);
  }, []);

  const env = session?.environment ?? "";
  const envClass = env ? styles[`env_${env}`] : undefined;
  const totalTablas = (snapshot?.schemas ?? []).reduce(
    (n, sc) => n + (sc.tables ?? []).length,
    0,
  );

  function openTable(schema: string, table: string) {
    const id = `tabla:${schema}.${table}`;
    setSelected(`${schema}.${table}`);
    setTabs((prev) =>
      prev.some((t) => t.id === id) ? prev : [...prev, { id, label: table, kind: "table" }],
    );
    setActiveTab(id);
  }

  // Cada consulta nueva es su propia pestaña con su propio identificador de
  // ejecución, para que cancelar en una no corte la de otra.
  function openQuery() {
    const n = tabs.filter((t) => t.id.startsWith("sql:")).length + 1;
    const id = `sql:${Date.now().toString(36)}`;
    setTabs((prev) => [...prev, { id, label: `Consulta ${n}`, kind: "query" }]);
    setActiveTab(id);
  }

  // De dónde salen los props de la pestaña activa. Se deriva del id en vez de
  // guardarlo aparte: dos fuentes para el mismo dato se desincronizan.
  const activa = tabs.find((t) => t.id === activeTab) ?? null;
  const esTabla = activa?.id.startsWith("tabla:") ?? false;
  const objeto = esTabla ? (activa?.id.slice("tabla:".length) ?? "") : "";
  const punto = objeto.indexOf(".");
  const esquemaActivo = punto >= 0 ? objeto.slice(0, punto) : "";
  const tablaActiva = punto >= 0 ? objeto.slice(punto + 1) : "";

  return (
    <div className={cx(styles.shell, envClass)}>
      <header className={styles.titlebar}>
        <span className={styles.wordmark}>KANAME</span>
        <span className={styles.divider} />
        {session?.connected ? (
          <>
            <span className={styles.envDot} />
            <span className={styles.dbName}>{session.server?.currentDatabase}</span>
            <EnvBadge env={session.environment as "local" | "dev" | "staging" | "production"} />
            <span className={styles.describe}>{session.describe}</span>
          </>
        ) : (
          <span className={styles.describe}>Sin conexión</span>
        )}
        <span className={styles.spacer} />
        {session?.readOnly ? <Badge tone="neutral">Solo lectura</Badge> : null}
        <Button size="sm" onClick={openQuery} disabled={!session?.connected}>
          Nueva consulta
        </Button>
        <Button size="sm" onClick={() => void load(true)} loading={loading}>
          Refrescar
        </Button>
        <span className={styles.divider} />
        <ShortcutChip>Ctrl K</ShortcutChip>
      </header>

      <div className={styles.body}>
        <aside className={styles.sidebar} style={{ width: sidebarWidth }}>
          <div className={styles.sidebarNav}>
            <PillTabs
              items={NAV}
              activeId={nav}
              onSelect={setNav}
              ariaLabel="Secciones de la barra lateral"
            />
          </div>
          <div className={styles.sidebarSearch}>
            <SearchInput
              placeholder="Filtrar objetos…"
              hint="Ctrl F"
              value={query}
              disabled={nav !== "objects"}
              onChange={(e) => setQuery(e.currentTarget.value)}
            />
          </div>
          <div className={styles.sidebarBody}>
            {nav !== "objects" ? (
              <p className={styles.emptySmall}>
                {nav === "queries"
                  ? "Las consultas guardadas llegan en la Iteración 9."
                  : "El historial llega en la Iteración 9."}
              </p>
            ) : schemaError ? (
              <p className={styles.emptySmall}>No se pudo leer el esquema. {schemaError}</p>
            ) : loading && !snapshot ? (
              <p className={styles.emptySmall}>Leyendo el catálogo…</p>
            ) : snapshot ? (
              <SchemaTree
                snapshot={snapshot}
                query={query}
                selected={selected}
                onSelect={openTable}
              />
            ) : (
              <p className={styles.emptySmall}>
                El árbol de esquema aparece cuando hay una conexión abierta.
              </p>
            )}
          </div>
          <div className={styles.sidebarFoot}>
            <span className={styles.spacer} />
            <span>
              {snapshot ? `${totalTablas} ${totalTablas === 1 ? "tabla" : "tablas"}` : "—"}
            </span>
          </div>
        </aside>

        <Splitter
          size={sidebarWidth}
          onResize={setSidebarWidth}
          min={SIDEBAR.min}
          max={SIDEBAR.max}
          side="left"
          label="Ancho de la barra lateral"
        />

        <main className={styles.main}>
          <TabStrip
            tabs={tabs}
            activeId={activeTab}
            {...(env ? { env: env as "local" | "dev" | "staging" | "production" } : {})}
            onSelect={setActiveTab}
            onNew={openQuery}
            onClose={(id) => {
              setTabs((prev) => prev.filter((t) => t.id !== id));
              setActiveTab((prev) => (prev === id ? null : prev));
            }}
          />
          <div className={styles.mainBody}>
            {!activa ? (
              <div className={styles.empty}>
                <p className={styles.emptyTitle}>Nada abierto</p>
                <p className={styles.emptyHint}>
                  Elegí una tabla del árbol, o abrí una consulta con «Nueva consulta».
                </p>
              </div>
            ) : esTabla ? (
              <TableDataScreen
                key={activa.id}
                tabId={activa.id}
                schema={esquemaActivo}
                table={tablaActiva}
                readOnly={session?.readOnly ?? false}
              />
            ) : (
              <SqlEditorScreen
                key={activa.id}
                tabId={activa.id}
                snapshot={snapshot}
                readOnly={session?.readOnly ?? false}
                statementTimeoutSeconds={session?.statementTimeoutSeconds ?? 0}
                rowLimit={session?.rowLimit ?? 0}
                connectionLabel={session?.describe ?? ""}
              />
            )}
          </div>
        </main>

        {railOpen ? (
          <>
            <Splitter
              size={railWidth}
              onResize={setRailWidth}
              min={RAIL.min}
              max={RAIL.max}
              side="right"
              label="Ancho del panel derecho"
            />
            <aside className={styles.rail} style={{ width: railWidth }}>
              <div className={styles.railHead}>
                <span className={styles.railTitle}>Conexión</span>
                <span className={styles.spacer} />
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label="Cerrar el panel"
                  onClick={() => setRailOpen(false)}
                >
                  ✕
                </Button>
              </div>
              <div className={styles.railBody}>
                {session?.connected && session.server ? (
                  <dl className={styles.info}>
                    <InfoRow label="Servidor" value={session.server.display} />
                    <InfoRow label="Usuario" value={session.server.currentUser} />
                    <InfoRow label="Codificación" value={session.server.encoding} />
                    <InfoRow label="Zona horaria" value={session.server.timeZone} />
                    <InfoRow label="Latencia" value={`${session.server.latencyMs} ms`} />
                    {session.server.inRecovery ? (
                      <InfoRow label="Rol" value="réplica" />
                    ) : null}
                  </dl>
                ) : (
                  <p className={styles.emptySmall}>Sin conexión.</p>
                )}

                {session?.readOnlyReason ? (
                  <p className={styles.readOnlyNote}>{session.readOnlyReason}</p>
                ) : null}
              </div>
            </aside>
          </>
        ) : null}
      </div>

      <footer className={cx(styles.statusbar, envClass)}>
        {session?.connected ? (
          <>
            <span className={styles.statusDot} />
            <span>{session.describe}</span>
            <span className={styles.statusDim}>{session.server?.display}</span>
          </>
        ) : (
          <span>sin conexión</span>
        )}
        <span className={styles.spacer} />
        {session?.readOnly ? (
          <span className={styles.statusDim}>solo lectura</span>
        ) : null}
        {railOpen ? null : (
          <button type="button" className={styles.statusLink} onClick={() => setRailOpen(true)}>
            show connection
          </button>
        )}
        <button
          type="button"
          className={styles.statusLink}
          onClick={() => {
            void SessionSvc.Disconnect().then(onDisconnect);
          }}
        >
          desconectar
        </button>
        <button type="button" className={styles.statusLink} onClick={onOpenAbout}>
          about
        </button>
      </footer>
    </div>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className={styles.infoRow}>
      <dt className={styles.infoKey}>{label}</dt>
      <dd className={styles.infoValue}>{value}</dd>
    </div>
  );
}
