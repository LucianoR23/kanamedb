import { useState } from "react";
import { Splitter } from "../components/Splitter";
import { Button, PillTabs, SearchInput, ShortcutChip, TabStrip } from "../components/ui";
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
 * Iteración 0: solo el chrome. El árbol, las tabs, la grilla y el panel de
 * cambios llegan cuando exista una conexión — Iteraciones 1, 2 y 5.
 */
export function Shell({ onOpenAbout }: { onOpenAbout: () => void }) {
  const [sidebarWidth, setSidebarWidth] = useState(SIDEBAR.initial);
  const [railWidth, setRailWidth] = useState(RAIL.initial);
  const [railOpen, setRailOpen] = useState(true);
  const [nav, setNav] = useState<string>("objects");

  return (
    <div className={styles.shell}>
      <header className={styles.titlebar}>
        <span className={styles.wordmark}>KANAME</span>
        <span className={styles.divider} />
        <span className={styles.disconnected}>
          <span className={styles.dot} />
          Sin conexión
        </span>
        <span className={styles.spacer} />
        <Button size="sm" disabled>
          New query
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
            <SearchInput placeholder="Filtrar objetos…" hint="Ctrl F" disabled />
          </div>
          <div className={styles.sidebarBody} role="tree" aria-label="Objetos">
            <p className={styles.emptySmall}>
              El árbol de esquema aparece cuando hay una conexión abierta.
            </p>
          </div>
          <div className={styles.sidebarFoot}>
            <span className={styles.spacer} />
            <span>sin objetos</span>
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
          <TabStrip tabs={[]} activeId={null} />
          <div className={styles.mainBody}>
            <div className={styles.empty}>
              <p className={styles.emptyTitle}>Sin conexión</p>
              <p className={styles.emptyHint}>
                Kaname todavía no se conecta a ninguna base. El gestor de
                conexiones llega en la próxima iteración; por ahora esto es el
                esqueleto de la ventana.
              </p>
              <Button onClick={onOpenAbout}>Acerca de Kaname</Button>
            </div>
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
                <span className={styles.railTitle}>Pending changes</span>
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
                <p className={styles.emptySmall}>Sin cambios pendientes.</p>
              </div>
            </aside>
          </>
        ) : null}
      </div>

      <footer className={styles.statusbar}>
        <span>sin conexión</span>
        <span className={styles.spacer} />
        {railOpen ? null : (
          <button
            type="button"
            className={styles.statusLink}
            onClick={() => setRailOpen(true)}
          >
            show changes
          </button>
        )}
        <button type="button" className={styles.statusLink} onClick={onOpenAbout}>
          about
        </button>
      </footer>
    </div>
  );
}
