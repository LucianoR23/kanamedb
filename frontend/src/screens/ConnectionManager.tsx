import { useState } from "react";
import type { ConnectionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Environment } from "../../bindings/github.com/LucianoR23/kanamedb/internal/connection";
import {
  Badge,
  Button,
  ContextMenu,
  Dialog,
  EnvBadge,
  Field,
  Input,
  SearchInput,
} from "../components/ui";
import type { MenuAnchor, MenuEntry, MenuLeaf } from "../components/ui";
import { DevSignature } from "../components/DevSignature";
import { SIN_CARPETA, carpetasDe } from "../lib/carpetas";
import { cx } from "../lib/cx";
import { nombreDeMotor } from "../lib/motor";
import styles from "./ConnectionManager.module.css";

/** El orden de los entornos dentro de una carpeta: del más inofensivo al que
 *  más cuidado pide. Así un proyecto se lee como su pipeline. */
const ORDEN_ENV: Record<string, number> = {
  [Environment.Local]: 0,
  [Environment.Dev]: 1,
  [Environment.Staging]: 2,
  [Environment.Production]: 3,
};

/** Cómo se dice el entorno en una fila de 36 píxeles. */
const ENV_CORTO: Record<string, string> = {
  [Environment.Local]: "local",
  [Environment.Dev]: "dev",
  [Environment.Staging]: "staging",
  [Environment.Production]: "prod",
};

/** Qué carpetas están plegadas. Se guarda en esta máquina y no en la libreta:
 *  es cómo se mira la lista, no qué hay en ella. */
const CLAVE_PLEGADAS = "kaname.conexiones.carpetasPlegadas";

function leerPlegadas(): ReadonlySet<string> {
  try {
    const crudo = localStorage.getItem(CLAVE_PLEGADAS);
    const lista: unknown = crudo ? JSON.parse(crudo) : [];
    return new Set(
      Array.isArray(lista) ? lista.filter((x): x is string => typeof x === "string") : [],
    );
  } catch {
    return new Set();
  }
}

function guardarPlegadas(plegadas: ReadonlySet<string>) {
  try {
    localStorage.setItem(CLAVE_PLEGADAS, JSON.stringify([...plegadas]));
  } catch {
    /* Sin almacenamiento, el plegado dura lo que la ventana. No es grave. */
  }
}

interface Grupo {
  carpeta: string;
  filas: ConnectionView[];
}

/**
 * Agrupa por carpeta —alfabético, sin carpeta al final— y dentro de cada una
 * ordena por entorno y después por nombre.
 *
 * Una carpeta es un proyecto, y adentro conviven su local, su dev y su
 * producción: por eso el entorno ya no es el grupo sino una marca en la fila.
 */
function agrupar(vistas: readonly ConnectionView[]): Grupo[] {
  const porCarpeta = new Map<string, ConnectionView[]>();
  for (const v of vistas) {
    const filas = porCarpeta.get(v.connection.folder);
    if (filas) filas.push(v);
    else porCarpeta.set(v.connection.folder, [v]);
  }
  const nombres = [...porCarpeta.keys()].sort((a, b) => {
    if (a === SIN_CARPETA) return 1;
    if (b === SIN_CARPETA) return -1;
    return a.localeCompare(b, undefined, { sensitivity: "base" });
  });
  return nombres.map((carpeta) => ({
    carpeta,
    filas: (porCarpeta.get(carpeta) ?? []).sort((a, b) => {
      const ea = ORDEN_ENV[a.connection.environment] ?? 9;
      const eb = ORDEN_ENV[b.connection.environment] ?? 9;
      if (ea !== eb) return ea - eb;
      return a.connection.name.localeCompare(b.connection.name, undefined, { sensitivity: "base" });
    }),
  }));
}

type Filter = "all" | "production";

interface Props {
  connections: readonly ConnectionView[];
  onNew: () => void;
  /** Atajo: elegir un archivo de SQLite y abrirlo sin pasar por el formulario. */
  onOpenFile: () => void;
  onEdit: (view: ConnectionView) => void;
  /** Cambia el modo de solo lectura de una conexión.
   *
   *  Es el único interruptor de protecciones que se adelantó a esta
   *  iteración. Lo primero que puede escribir en la base es el editor SQL,
   *  que llega ahora; la tab Safety completa recién está en la 9, y hasta
   *  entonces la única forma de activarlo era editar connections.toml. */
  onToggleReadOnly: (view: ConnectionView, readOnly: boolean) => void;
  onConnect: (view: ConnectionView) => void;
  onDuplicate: (id: string) => void;
  /** Cambia la carpeta de una conexión. Vacío la saca de la que tenga. */
  onMoveToFolder: (id: string, folder: string) => void;
  /** Exporta esas conexiones a un archivo para compartir, sin secretos.
   *  `sugerido` es el nombre de archivo que propone el selector. */
  onExport: (ids: string[], sugerido: string) => void;
  /** Importa conexiones de un archivo compartido. */
  onImport: () => void;
  onDelete: (id: string) => void;
  onAbout: () => void;
  onSettings: () => void;
  /** S20: comparar esquemas. Con una conexión, esa queda como origen. */
  onCompare: (sourceId: string | null) => void;
  /** Error de la última acción, si hubo. */
  error?: string | null;
}

export function ConnectionManager({
  connections,
  onNew,
  onOpenFile,
  onEdit,
  onToggleReadOnly,
  onConnect,
  onDuplicate,
  onMoveToFolder,
  onExport,
  onImport,
  onDelete,
  onAbout,
  onSettings,
  onCompare,
  error,
}: Props) {
  const [selectedId, setSelectedId] = useState<string | null>(
    connections[0]?.connection.id ?? null,
  );
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<Filter>("all");
  const [menu, setMenu] = useState<MenuAnchor | null>(null);
  // El menú de una cabecera de carpeta: dónde y de cuál.
  const [menuCarpeta, setMenuCarpeta] = useState<{ anchor: MenuAnchor; carpeta: string } | null>(
    null,
  );
  const [confirmDelete, setConfirmDelete] = useState<ConnectionView | null>(null);
  const [plegadas, setPlegadas] = useState<ReadonlySet<string>>(leerPlegadas);
  // «Nueva carpeta…» desde el menú: a qué conexión se le pone, y el nombre.
  const [nuevaCarpeta, setNuevaCarpeta] = useState<{ view: ConnectionView; nombre: string } | null>(
    null,
  );

  function plegar(carpeta: string) {
    const next = new Set(plegadas);
    if (next.has(carpeta)) next.delete(carpeta);
    else next.add(carpeta);
    guardarPlegadas(next);
    setPlegadas(next);
  }

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
      c.connection.folder.toLowerCase().includes(q) ||
      c.connection.host.toLowerCase().includes(q) ||
      c.connection.database.toLowerCase().includes(q)
    );
  });

  const selected =
    visibles.find((c) => c.connection.id === selectedId) ?? visibles[0] ?? null;

  const carpetas = carpetasDe(connections);
  const grupos = agrupar(visibles);
  // Con una sola carpeta —o ninguna— el rótulo «Sin carpeta» sobre todas las
  // conexiones no separa nada: se muestra solo cuando hay de qué separar.
  const conCabeceras = carpetas.length > 0;
  // Buscando o filtrando, todo a la vista: un plegado que esconde lo que se
  // está buscando se lee como «no está».
  const respetarPlegado = query === "" && filter === "all";

  /** Adónde se puede mover una conexión: las otras carpetas, afuera de la
   *  suya si tiene, y una nueva. Una carpeta nace con su primera conexión
   *  adentro, así que «nueva» es mover, no crear en el vacío. */
  function destinosDeCarpeta(view: ConnectionView): MenuLeaf[] {
    const actual = view.connection.folder;
    const destinos: MenuLeaf[] = carpetas
      .filter((c) => c !== actual)
      .map((c) => ({
        // Con su propio prefijo: una carpeta que se llame «out» o «new» no
        // puede chocar con las entradas fijas de abajo.
        id: `move:to:${c}`,
        label: c,
        onSelect: () => onMoveToFolder(view.connection.id, c),
      }));
    if (actual !== SIN_CARPETA) {
      destinos.push({
        id: "move:out",
        label: `Sacar de «${actual}»`,
        onSelect: () => onMoveToFolder(view.connection.id, SIN_CARPETA),
      });
    }
    if (destinos.length > 0) destinos.push({ kind: "separator", id: "move:sep" });
    destinos.push({
      id: "move:new",
      label: "Nueva carpeta…",
      onSelect: () => setNuevaCarpeta({ view, nombre: "" }),
    });
    return destinos;
  }

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
          kind: "submenu",
          id: "move",
          label: "Mover a carpeta",
          // Mover pasa por la validación del store, igual que guardar: una
          // conexión rota primero se arregla.
          disabled: (selected.problems?.length ?? 0) > 0,
          disabledReason: "mal configurada",
          entries: destinosDeCarpeta(selected),
        },
        {
          id: "copy",
          label: "Copiar la URI",
          onSelect: () => void navigator.clipboard.writeText(selected.uri),
        },
        {
          id: "export",
          label: "Exportar para compartir…",
          onSelect: () => onExport([selected.connection.id], selected.connection.name),
        },
        {
          id: "compare",
          label: "Comparar contra otra…",
          // Con una sola conexión no hay «otra»: la pantalla solo podría decir
          // «son la misma». Mismo criterio que el botón de la barra.
          disabled: connections.length < 2,
          disabledReason: "hace falta otra conexión",
          onSelect: () => onCompare(selected.connection.id),
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

  /** El menú de una carpeta: exportarla, y plegarla o desplegarla.
   *
   *  Se exporta lo que la carpeta MUESTRA: con un filtro puesto, la cabecera
   *  dice cuántas hay a la vista, y el archivo lleva esas mismas. */
  function entradasDeCarpeta(carpeta: string): MenuEntry[] {
    const ids = visibles
      .filter((c) => c.connection.folder === carpeta)
      .map((c) => c.connection.id);
    return [
      {
        id: "folder:export",
        label: `Exportar carpeta… (${ids.length})`,
        onSelect: () => onExport(ids, carpeta),
      },
      {
        id: "folder:fold",
        label: plegadas.has(carpeta) ? "Desplegar" : "Plegar",
        onSelect: () => plegar(carpeta),
      },
    ];
  }

  return (
    <div className={styles.screen}>
      <header className={styles.titlebar}>
        <span className={styles.wordmark}>KANAME</span>
        <span className={styles.divider} />
        <span className={styles.section}>Conexiones</span>
        <span className={styles.spacer} />
        {/* El atajo va antes y en secundario: abrir un archivo es lo rápido,
            pero crear una conexión sigue siendo lo que esta pantalla hace. */}
        {/* S20. Con dos conexiones o más: comparar necesita dos, y un botón
            que abre una pantalla para decir «no hay con qué» no ayuda. */}
        {connections.length >= 2 ? (
          <Button size="sm" onClick={() => onCompare(selected?.connection.id ?? null)}>
            Comparar esquemas…
          </Button>
        ) : null}
        <Button size="sm" onClick={onImport}>
          Importar…
        </Button>
        <Button size="sm" onClick={onOpenFile}>
          Abrir archivo SQLite…
        </Button>
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
              grupos.map((g) => {
                const plegada = respetarPlegado && plegadas.has(g.carpeta);
                const rotulo = g.carpeta === SIN_CARPETA ? "Sin carpeta" : g.carpeta;
                return (
                  <div key={g.carpeta} className={styles.group}>
                    {conCabeceras ? (
                      <div
                        className={styles.groupHead}
                        onContextMenu={(e) => {
                          if (g.carpeta === SIN_CARPETA) return;
                          e.preventDefault();
                          setMenuCarpeta({ anchor: { x: e.clientX, y: e.clientY }, carpeta: g.carpeta });
                        }}
                      >
                        <button
                          type="button"
                          className={styles.groupToggle}
                          aria-expanded={!plegada}
                          onClick={() => plegar(g.carpeta)}
                        >
                          <span className={styles.groupChevron} aria-hidden="true">
                            {plegada ? "▸" : "▾"}
                          </span>
                          <span
                            className={cx(
                              styles.groupLabel,
                              g.carpeta === SIN_CARPETA && styles.groupLabelDim,
                            )}
                          >
                            {rotulo}
                          </span>
                          <span className={styles.groupRule} />
                          <span className={styles.groupCount}>{g.filas.length}</span>
                        </button>
                        {g.carpeta !== SIN_CARPETA ? (
                          <button
                            type="button"
                            className={styles.groupMore}
                            aria-label={`Acciones de la carpeta ${g.carpeta}`}
                            onClick={(e) => {
                              const r = e.currentTarget.getBoundingClientRect();
                              setMenuCarpeta({ anchor: { x: r.left, y: r.bottom + 2 }, carpeta: g.carpeta });
                            }}
                          >
                            ⋯
                          </button>
                        ) : null}
                      </div>
                    ) : null}
                    {plegada
                      ? null
                      : g.filas.map((c) => (
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
                                {c.connection.engine === "sqlite"
                                  ? c.connection.database
                                  : `${c.connection.host}:${c.connection.port}`}
                              </div>
                            </div>
                            {(c.problems?.length ?? 0) > 0 ? (
                              <Badge tone="danger">rota</Badge>
                            ) : null}
                            {c.connection.safety.readOnly ? (
                              <Badge tone="neutral">RO</Badge>
                            ) : null}
                            {!c.hasPassword && c.connection.engine !== "sqlite" ? (
                              <span className={styles.noKey} title="Sin contraseña en esta máquina">
                                sin clave
                              </span>
                            ) : null}
                            <span className={styles.rowEnv}>
                              {ENV_CORTO[c.connection.environment] ?? c.connection.environment}
                            </span>
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
        <button type="button" className={styles.statusLink} onClick={onSettings}>
          ajustes
        </button>
        <button type="button" className={styles.statusLink} onClick={onAbout}>
          about
        </button>
      </footer>

      <ContextMenu anchor={menu} entries={menuEntries} onClose={() => setMenu(null)} />
      <ContextMenu
        anchor={menuCarpeta?.anchor ?? null}
        entries={menuCarpeta ? entradasDeCarpeta(menuCarpeta.carpeta) : []}
        onClose={() => setMenuCarpeta(null)}
      />

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

      <Dialog
        open={nuevaCarpeta !== null}
        title={`Nueva carpeta para ${nuevaCarpeta?.view.connection.name ?? ""}`}
        onClose={() => setNuevaCarpeta(null)}
        footer={
          <>
            <Button onClick={() => setNuevaCarpeta(null)}>Cancelar</Button>
            <Button
              variant="primary"
              disabled={(nuevaCarpeta?.nombre.trim() ?? "") === ""}
              onClick={() => {
                if (!nuevaCarpeta) return;
                onMoveToFolder(nuevaCarpeta.view.connection.id, nuevaCarpeta.nombre.trim());
                setNuevaCarpeta(null);
              }}
            >
              Mover
            </Button>
          </>
        }
      >
        <div className={styles.dialogBody}>
          <Field label="Nombre">
            <Input
              value={nuevaCarpeta?.nombre ?? ""}
              autoFocus
              placeholder="el proyecto, por ejemplo"
              onChange={(e) => {
                const nombre = e.currentTarget.value;
                setNuevaCarpeta((prev) => (prev ? { ...prev, nombre } : prev));
              }}
              onKeyDown={(e) => {
                if (e.key !== "Enter" || !nuevaCarpeta || nuevaCarpeta.nombre.trim() === "") return;
                e.preventDefault();
                onMoveToFolder(nuevaCarpeta.view.connection.id, nuevaCarpeta.nombre.trim());
                setNuevaCarpeta(null);
              }}
            />
          </Field>
          <p className={styles.dialogNote}>
            La carpeta existe mientras alguna conexión la tenga. Las demás se mueven desde su
            menú, o se elige la carpeta al editarlas.
          </p>
        </div>
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
            <Row label="Motor" value={nombreDeMotor(c.engine)} />
            {c.engine === "sqlite" ? (
              /* Un archivo: el permiso lo da el sistema de archivos. No hay
                 host, usuario, contraseña ni TLS que mostrar. */
              <Row label="Archivo" value={c.database} />
            ) : (
              <>
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
              </>
            )}
            <Row label="Carpeta" value={c.folder || "sin carpeta"} muted={c.folder === ""} />
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
            Solo lectura se cambia acá. Las otras tres se editan por ahora en
            connections.toml —los valores se guardan y se respetan— y van a tener su tab
            en el editor.
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
