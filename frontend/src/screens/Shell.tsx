import { useEffect, useState } from "react";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import type { SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Snapshot } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import { Splitter } from "../components/Splitter";
import {
  Badge,
  Button,
  EnvBadge,
  PillTabs,
  SearchInput,
  ShortcutChip,
  Spinner,
  TabStrip,
} from "../components/ui";
import type { TabItem } from "../components/ui";
import type { $Object as DBObject } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import { glifoDe, idDe as idDeObjeto } from "../lib/objetos";
import { ObjectScreen } from "./ObjectScreen";
import { SchemaTree } from "./SchemaTree";
import { SqlEditorScreen } from "./SqlEditorScreen";
import { TableDataScreen } from "./TableDataScreen";
import { ErdScreen } from "./ErdScreen";
import { PendingChanges } from "./PendingChanges";
import { DevSignature } from "../components/DevSignature";
import { cx } from "../lib/cx";
import { DumpDialog } from "./DumpDialog";
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
  // La barra lateral se pliega, no solo se angosta. Con el diagrama abierto, sus
  // 260 píxeles son la diferencia entre ver el esquema entero y no.
  const [sidebarOpen, setSidebarOpen] = useState(true);
  const [nav, setNav] = useState<string>("objects");
  const [query, setQuery] = useState("");

  const [session, setSession] = useState<SessionView | null>(null);
  const [volcando, setVolcando] = useState(false);
  // El identificador es de ESTA apertura: cancelar tiene que cortar este
  // volcado y no otra cosa que esté corriendo.
  const [runIDVolcado] = useState(() => `dump:${Date.now()}`);
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [schemaError, setSchemaError] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [tabs, setTabs] = useState<TabItem[]>([]);

  const [activeTab, setActiveTab] = useState<string | null>(null);

  // Sube cada vez que hay que releer la base. Las pestañas abiertas lo miran y
  // se recargan solas: sin esto, «Refrescar» releía el árbol y dejaba la
  // pestaña de la tabla mostrando el esquema de antes, y la única salida era
  // cerrarla y volver a abrirla.
  const [recarga, setRecarga] = useState(0);

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
      if (refresh) setRecarga((n) => n + 1);
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
    setEsquemaSel(schema);
    setTabs((prev) =>
      prev.some((t) => t.id === id) ? prev : [...prev, { id, label: table, kind: "table" }],
    );
    setActiveTab(id);
  }

  // Los objetos abiertos, por id de pestaña. El OBJETO entero se guarda y no
  // solo su nombre: la clase y la firma son parte de su identidad —dos
  // sobrecargas de una función se llaman igual— y volver a buscarlo en el
  // snapshot por nombre elegiría cualquiera de las dos.
  const [objetos, setObjetos] = useState<Record<string, DBObject>>({});

  // El esquema de lo último que se eligió en el árbol. Ver esquemaPrincipal().
  const [esquemaSel, setEsquemaSel] = useState<string | null>(null);

  function openObject(o: DBObject) {
    const id = `objeto:${idDeObjeto(o)}`;
    setSelected(idDeObjeto(o));
    setEsquemaSel(o.schema);
    setObjetos((prev) => (prev[id] ? prev : { ...prev, [id]: o }));
    setTabs((prev) =>
      prev.some((t) => t.id === id) ? prev : [...prev, { id, label: o.name, kind: glifoDe(o.kind) }],
    );
    setActiveTab(id);
  }

  const ID_CAMBIOS = "cambios:";

  function openCambios() {
    setTabs((prev) =>
      prev.some((t) => t.id === ID_CAMBIOS)
        ? prev
        : [...prev, { id: ID_CAMBIOS, label: "Cambios pendientes", kind: "query" }],
    );
    setActiveTab(ID_CAMBIOS);
  }

  // El diagrama es una pestaña por esquema: dos esquemas son dos diagramas
  // distintos y mezclarlos en uno solo daría un dibujo que nadie pidió.
  // A qué tabla ir cuando se abre el diagrama desde otra pantalla. El contador
  // es lo que distingue "pedilo de nuevo" de "ya está pedido": sin él, volver a
  // «Ver en el diagrama» sobre la misma tabla no haría nada.
  const [erdFoco, setErdFoco] = useState<{ tabla: string; pedido: number } | null>(null);

  // Cuántos cambios hay sin aplicar. Se relee cuando algo los toca; no se
  // consulta en cada render porque cruza el puente a Go.
  const [pendientes, setPendientes] = useState(0);

  function openErd(schema: string, tabla?: string) {
    if (tabla) setErdFoco((prev) => ({ tabla, pedido: (prev?.pedido ?? 0) + 1 }));
    const id = `erd:${schema}`;
    setTabs((prev) =>
      prev.some((t) => t.id === id) ? prev : [...prev, { id, label: `ERD · ${schema}`, kind: "erd" }],
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

  // Qué objeto representa el id de una pestaña. Se deriva del id en vez de
  // guardarlo aparte: dos fuentes para el mismo dato se desincronizan.
  function objetoDe(id: string): { schema: string; table: string } | null {
    if (!id.startsWith("tabla:")) return null;
    const resto = id.slice("tabla:".length);
    const punto = resto.indexOf(".");
    if (punto < 0) return null;
    return { schema: resto.slice(0, punto), table: resto.slice(punto + 1) };
  }

  /** El esquema que dibuja una pestaña de diagrama, o null si no lo es. */
  function esquemaDeErd(id: string): string | null {
    return id.startsWith("erd:") ? id.slice("erd:".length) : null;
  }

  /** El esquema del que conviene abrir el diagrama.
   *
   *  El de lo último que se eligió en el árbol —tabla u objeto— y si no hay
   *  nada, el primero QUE TENGA TABLAS. El snapshot pone `public` primero
   *  porque es donde está casi todo, pero en una base donde no se usa queda
   *  vacío y el diagrama abría en blanco: elegir el primero a secas es correcto
   *  y molesto.
   *
   *  El esquema se GUARDA al elegir en vez de deducirse del id seleccionado.
   *  Se deducía partiendo por el primer punto, y eso funcionaba mientras el id
   *  fuera siempre `esquema.tabla`; el id de un objeto es
   *  `view:public.v_ventas`, así que elegir una vista dejaba el esquema en
   *  «view:public» y «Diagrama» abría una pestaña vacía contra un esquema que
   *  no existe. */
  function esquemaPrincipal(): string {
    if (esquemaSel) return esquemaSel;
    const conTablas = (snapshot?.schemas ?? []).find((sc) => (sc.tables ?? []).length > 0);
    return conTablas?.name ?? snapshot?.schemas?.[0]?.name ?? "public";
  }

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
        {pendientes > 0 ? (
          /* La `key` es el contador a propósito: al cambiar, React remonta el
           * botón y la animación de destello vuelve a correr. Es la única
           * señal de que la edición llegó a algún lado — el diálogo se cierra
           * y lo único que pasa es que este número sube, en la otra punta de
           * la pantalla. */
          <Button
            key={pendientes}
            size="sm"
            variant="secondary"
            className={styles.destello}
            onClick={openCambios}
          >
            {pendientes} {pendientes === 1 ? "cambio" : "cambios"} sin aplicar
          </Button>
        ) : null}
        <Button
          size="sm"
          onClick={() => openErd(esquemaPrincipal())}
          disabled={!session?.connected || totalTablas === 0}
          title={totalTablas === 0 ? "No hay tablas para dibujar" : "Ver el esquema como diagrama"}
        >
          Diagrama
        </Button>
        <Button size="sm" onClick={openQuery} disabled={!session?.connected}>
          Nueva consulta
        </Button>
        <Button
          size="sm"
          onClick={() => setVolcando(true)}
          disabled={!session?.connected || totalTablas === 0}
          title={totalTablas === 0 ? "No hay tablas para volcar" : "Volcar la estructura, los datos, o los dos"}
        >
          Volcar…
        </Button>
        <Button size="sm" onClick={() => void load(true)} loading={loading}>
          Refrescar
        </Button>
        <span className={styles.divider} />
        <ShortcutChip>Ctrl K</ShortcutChip>
      </header>

      <div className={styles.body}>
        {sidebarOpen ? null : (
          <button
            type="button"
            className={styles.borde}
            title="Mostrar los objetos"
            aria-label="Mostrar los objetos"
            onClick={() => setSidebarOpen(true)}
          >
            &gt;
          </button>
        )}

        <aside
          className={styles.sidebar}
          style={{ width: sidebarWidth }}
          hidden={!sidebarOpen}
        >
          <div className={styles.sidebarNav}>
            <PillTabs
              items={NAV}
              activeId={nav}
              onSelect={setNav}
              ariaLabel="Secciones de la barra lateral"
            />
            <span className={styles.spacer} />
            <Button
              variant="ghost"
              size="sm"
              aria-label="Plegar la barra lateral"
              title="Plegar la barra lateral"
              onClick={() => setSidebarOpen(false)}
            >
              &lt;
            </Button>
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
              <p className={styles.cargando}>
                <Spinner size="sm" />
                Leyendo el catálogo…
              </p>
            ) : snapshot ? (
              <SchemaTree
                snapshot={snapshot}
                query={query}
                selected={selected}
                onSelect={openTable}
                onSelectObject={openObject}
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

        {sidebarOpen ? (
          <Splitter
            size={sidebarWidth}
            onResize={setSidebarWidth}
            min={SIDEBAR.min}
            max={SIDEBAR.max}
            side="left"
            label="Ancho de la barra lateral"
          />
        ) : null}

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
          {/* Se montan TODAS las pestañas y se esconden las inactivas.
           *
           * Antes se montaba solo la activa, así que cambiar de pestaña
           * desmontaba la anterior y se perdía lo que hubiera escrito en el
           * editor. Perder texto tipeado por navegar es de lo peor que puede
           * hacer una aplicación: no hay deshacer que lo recupere.
           *
           * El costo es tener varios editores y varias grillas vivas a la vez.
           * Es aceptable para un puñado de pestañas y es el precio de que una
           * pestaña conserve su estado —texto, resultado, scroll, deshacer—
           * como lo conserva en cualquier editor. */}
          <div className={styles.mainBody}>
            {tabs.length === 0 ? (
              <div className={styles.empty}>
                <p className={styles.emptyTitle}>Nada abierto</p>
                <p className={styles.emptyHint}>
                  Elegí una tabla del árbol, mirá el esquema entero con «Diagrama», o abrí una
                  consulta con «Nueva consulta».
                </p>
              </div>
            ) : null}
            {tabs.map((t) => {
              const obj = objetoDe(t.id);
              const erd = esquemaDeErd(t.id);
              return (
                <div
                  key={t.id}
                  className={cx(styles.pane, t.id !== activeTab && styles.paneHidden)}
                >
                  {obj ? (
                    <TableDataScreen
                      tabId={t.id}
                      schema={obj.schema}
                      table={obj.table}
                      readOnly={session?.readOnly ?? false}
                      engine={session?.server?.engine ?? ""}
                      snapshot={snapshot}
                      recarga={recarga}
                      onShowInErd={openErd}
                      onRevisar={openCambios}
                      onStaged={() => {
                        void SessionSvc.Changeset().then((v) => setPendientes(v.summary.total));
                      }}
                    />
                  ) : objetos[t.id] ? (
                    <ObjectScreen objeto={objetos[t.id]!} recarga={recarga} />
                  ) : t.id === ID_CAMBIOS ? (
                    <PendingChanges
                      active={t.id === activeTab}
                      onApplied={(schemaChanged) => {
                        // Un apply de puras filas no toca el árbol: se recargan
                        // las pestañas y el catálogo no se vuelve a inspeccionar.
                        if (schemaChanged) void load(true);
                        else setRecarga((n) => n + 1);
                      }}
                      onCount={setPendientes}
                      onOpenTable={openTable}
                    />
                  ) : erd ? (
                    <ErdScreen
                      snapshot={snapshot}
                      schema={erd}
                      foco={erdFoco}
                      onOpenTable={openTable}
                      readOnly={session?.readOnly ?? false}
                      onStaged={() => {
                        void SessionSvc.Changeset().then((v) => setPendientes(v.summary.total));
                      }}
                      onRevisar={openCambios}
                    />
                  ) : (
                    <SqlEditorScreen
                      tabId={t.id}
                      active={t.id === activeTab}
                      snapshot={snapshot}
                      readOnly={session?.readOnly ?? false}
                      statementTimeoutSeconds={session?.statementTimeoutSeconds ?? 0}
                      rowLimit={session?.rowLimit ?? 0}
                      connectionLabel={session?.describe ?? ""}
                      engine={session?.server?.engine ?? ""}
                    />
                  )}
                </div>
              );
            })}
          </div>
        </main>

        {railOpen ? null : (
          <button
            type="button"
            className={styles.borde}
            title="Mostrar la conexión"
            aria-label="Mostrar la conexión"
            onClick={() => setRailOpen(true)}
          >
            &lt;
          </button>
        )}

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
        {/* La firma va acá y no en la esquina: «Desconectar» es la acción y no
         *  se le mueve el rincón, que es donde la mano ya la busca. */}
        <DevSignature />
        <span className={styles.statusSep} />
        {session?.readOnly ? (
          <span className={styles.statusDim}>solo lectura</span>
        ) : null}
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

      {volcando ? (
        <DumpDialog
          open
          schema={esquemaPrincipal()}
          runID={runIDVolcado}
          onClose={() => setVolcando(false)}
        />
      ) : null}
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
