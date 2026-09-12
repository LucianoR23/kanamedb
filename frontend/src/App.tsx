import { useCallback, useEffect, useState } from "react";
import * as AppInfo from "../bindings/github.com/LucianoR23/kanamedb/internal/appinfo/service";
import * as Connections from "../bindings/github.com/LucianoR23/kanamedb/internal/service/connections";
import { PasswordAction } from "../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { ConnectionView, ImportPreview } from "../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { About } from "./screens/About";
import { Settings } from "./screens/Settings";
import { CompareScreen } from "./screens/CompareScreen";
import { ConnectionEditor } from "./screens/ConnectionEditor";
import { ConnectionManager } from "./screens/ConnectionManager";
import { ImportConnectionsDialog } from "./screens/ImportConnectionsDialog";
import { Shell } from "./screens/Shell";
import { Welcome } from "./screens/Welcome";
import { useConectar } from "./lib/useConectar";
import { elegirArchivoSQLite } from "./lib/archivoSQLite";
import { carpetasDe } from "./lib/carpetas";
import { textoDe } from "./lib/dialogos";
import { elegirDestinoLibreta, elegirLibreta, nombreDeArchivoCompartido } from "./lib/libreta";
import { ToastStack } from "./components/ui";
import type { ToastItem } from "./components/ui";
import { cargar as cargarPreferencias } from "./lib/preferencias";
import styles from "./App.module.css";

type Screen = "loading" | "welcome" | "manager" | "shell" | "about" | "settings" | "compare";

interface EditorState {
  view: ConnectionView;
  isNew: boolean;
}

export default function App() {
  const [screen, setScreen] = useState<Screen>("loading");
  const [connections, setConnections] = useState<ConnectionView[]>([]);
  const [editor, setEditor] = useState<EditorState | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [connectionsPath, setConnectionsPath] = useState("");

  // Un slice nil de Go cruza el puente como null. Se normaliza acá, una sola
  // vez, para que ninguna pantalla tenga que acordarse.
  const reload = useCallback(async () => {
    const list = (await Connections.List()) ?? [];
    setConnections(list);
    return list;
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        // Las preferencias van en el mismo viaje que lo demás y NO se esperan
        // aparte: el tema se pinta en cuanto llegan, y hacer que la primera
        // pantalla espere por ellas sería un rato de ventana vacía por un color.
        // Si no se pueden leer, la aplicación arranca con lo de siempre —tema
        // oscuro, cuerpo 13— y los ajustes explican el problema cuando se abran.
        void cargarPreferencias().catch(() => {});
        const [raw, info] = await Promise.all([Connections.List(), AppInfo.Get()]);
        if (cancelled) return;
        const list = raw ?? [];
        setConnections(list);
        setConnectionsPath(info.paths.connections);
        setScreen(list.length === 0 ? "welcome" : "manager");
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
        setScreen("welcome");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  async function openNew() {
    setError(null);
    try {
      setEditor({ view: await Connections.Draft(), isNew: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  /**
   * El atajo "Abrir archivo SQLite…" de S01 y S02.
   *
   * Termina en el editor y no en la base: el borrador ya viene válido —motor,
   * ruta y nombre puestos— así que es un clic para conectar. Abrir el editor y
   * no conectar de una es a propósito: la conexión se guarda en la libreta, y
   * escribir ahí algo que la persona no vio es cómo la lista se llena de
   * entradas que nadie creó a sabiendas.
   */
  async function abrirArchivoSQLite() {
    setError(null);
    try {
      const ruta = await elegirArchivoSQLite();
      if (!ruta) return;
      setEditor({ view: await Connections.DraftSQLite(ruta), isNew: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  // Un aviso de algo que salió bien: «exportadas en…», «se agregaron…». Es un
  // toast y no el banner rojo porque no es un error; se va solo porque es de
  // éxito (la regla vive en Toast).
  const [aviso, setAviso] = useState<ToastItem | null>(null);

  // La vista previa de un archivo compartido, esperando decisión.
  const [importacion, setImportacion] = useState<{
    preview: ImportPreview;
    importing: boolean;
    error: string | null;
  } | null>(null);

  /** Exporta conexiones a un archivo para compartir. Sin secretos: eso lo
   *  garantiza Go, acá solo se elige adónde. */
  async function exportarConexiones(ids: string[], sugerido: string) {
    setError(null);
    try {
      const ruta = await elegirDestinoLibreta(nombreDeArchivoCompartido(sugerido));
      if (!ruta) return;
      const info = await Connections.ExportConnections(ids, ruta);
      setAviso({
        // Un id por aviso, no por tipo: dos exportaciones seguidas son dos
        // toasts, y el segundo tiene que arrancar sus ocho segundos de cero.
        id: `export-${Date.now()}`,
        tone: "success",
        title: `${info.count === 1 ? "Conexión exportada" : `${info.count} conexiones exportadas`} · sin contraseñas`,
        detail: info.path,
      });
    } catch (err) {
      setError(textoDe(err));
    }
  }

  /** Elige un archivo compartido y muestra qué trae. Todavía no agrega nada. */
  async function importarConexiones() {
    setError(null);
    try {
      const ruta = await elegirLibreta();
      if (!ruta) return;
      const preview = await Connections.PreviewImport(ruta);
      setImportacion({ preview, importing: false, error: null });
    } catch (err) {
      setError(textoDe(err));
    }
  }

  async function confirmarImportacion(include: number[]) {
    if (!importacion) return;
    setImportacion({ ...importacion, importing: true, error: null });
    let nuevas: ConnectionView[];
    try {
      nuevas =
        (await Connections.ImportConnections(
          importacion.preview.path,
          importacion.preview.fingerprint,
          include,
        )) ?? [];
    } catch (err) {
      setImportacion({ ...importacion, importing: false, error: textoDe(err) });
      return;
    }
    // Desde acá la libreta ya cambió: un fallo al releerla no puede devolver
    // el diálogo con el botón habilitado, que importaría lo mismo otra vez.
    setImportacion(null);
    setError(null);
    try {
      await reload();
    } catch (err) {
      setError(textoDe(err));
    }
    setScreen("manager");
    setAviso({
      id: `import-${Date.now()}`,
      tone: "success",
      title:
        nuevas.length === 1 ? "Se agregó 1 conexión" : `Se agregaron ${nuevas.length} conexiones`,
      detail: "Sin contraseña: se pide al conectar.",
    });
  }

  // Conectar —inspección del bastión, TOFU, conexión— vive en el hook, que
  // comparten esta interfaz y la del teléfono. Acá solo se dice a dónde ir.
  const { connect, dialogos: dialogosDeConexion } = useConectar({
    onStart: () => setError(null),
    onConnected: () => setScreen("shell"),
    onEdit: (view) => setEditor({ view, isNew: false }),
    onError: setError,
  });

  /**
   * De dónde se entró a About o a Ajustes, para saber a dónde vuelve «Volver».
   *
   * Antes About volvía siempre al gestor de conexiones, así que abrirla con una
   * sesión abierta te dejaba afuera del workspace —con las pestañas, el árbol y
   * el changeset como estaban del lado de Go, pero sin forma de volver a ellos
   * salvo reconectando—. Son pantallas que se abren, se miran y se cierran: el
   * lugar al que vuelven es aquel del que se salió.
   */
  const [desdeDonde, setDesdeDonde] = useState<"welcome" | "manager" | "shell">("manager");

  function abrirPantallaDeLaApp(cual: "about" | "settings" | "compare") {
    if (screen === "welcome" || screen === "manager" || screen === "shell") {
      setDesdeDonde(screen);
    }
    setScreen(cual);
  }

  // S20: la conexión que queda como origen al abrir la comparación, si hubo.
  const [origenDeComparacion, setOrigenDeComparacion] = useState<string | null>(null);

  function abrirComparacion(sourceId: string | null) {
    setOrigenDeComparacion(sourceId);
    abrirPantallaDeLaApp("compare");
  }

  function volver() {
    if (desdeDonde === "shell") {
      setScreen("shell");
      return;
    }
    setScreen(connections.length === 0 ? "welcome" : "manager");
  }

  async function run(action: () => Promise<unknown>) {
    setError(null);
    try {
      await action();
      const list = await reload();
      if (list.length === 0) setScreen("welcome");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  if (screen === "loading") {
    return <div style={{ padding: 24, color: "var(--text-3)" }}>Cargando…</div>;
  }

  // About y Ajustes abiertas DESDE el workspace se dibujan encima, con el Shell
  // montado debajo. Ver App.module.css: reemplazarlo perdía las pestañas y el
  // texto sin guardar de cualquier editor, sin preguntar.
  const encimaDelShell =
    desdeDonde === "shell" &&
    (screen === "about" || screen === "settings" || screen === "compare");

  if (screen === "shell" || encimaDelShell) {
    return (
      <>
        {/* `inert` mientras hay algo encima: sin eso el tabulador entra al
            workspace tapado y el foco se va a controles que no se ven. */}
        <div className={styles.debajo} inert={encimaDelShell}>
          <Shell
            onOpenAbout={() => abrirPantallaDeLaApp("about")}
            onOpenSettings={() => abrirPantallaDeLaApp("settings")}
            onCompare={abrirComparacion}
            onDisconnect={(motivo) => {
              setScreen(connections.length === 0 ? "welcome" : "manager");
              if (motivo) {
                setAviso({
                  id: `cierre-${Date.now()}`,
                  tone: "info",
                  title: "Sesión cerrada",
                  detail: motivo,
                });
              }
            }}
          />
        </div>
        {encimaDelShell ? (
          <div className={styles.overlay} data-overlay-app="">
            {screen === "about" ? (
              <About onBack={volver} />
            ) : screen === "settings" ? (
              <Settings onBack={volver} />
            ) : (
              <CompareScreen
                connections={connections}
                initialSourceId={origenDeComparacion ?? undefined}
                onBack={volver}
              />
            )}
          </div>
        ) : null}
      </>
    );
  }

  if (screen === "about") {
    return <About onBack={volver} />;
  }

  if (screen === "settings") {
    return <Settings onBack={volver} />;
  }

  if (screen === "compare") {
    return (
      <CompareScreen
        connections={connections}
        initialSourceId={origenDeComparacion ?? undefined}
        onBack={volver}
      />
    );
  }

  return (
    <>
      {screen === "welcome" ? (
        <Welcome
          onNew={() => void openNew()}
          onOpenFile={() => void abrirArchivoSQLite()}
          onAbout={() => abrirPantallaDeLaApp("about")}
          onSettings={() => abrirPantallaDeLaApp("settings")}
          connectionsPath={connectionsPath}
        />
      ) : (
        <ConnectionManager
          connections={connections}
          error={error}
          onNew={() => void openNew()}
          onOpenFile={() => void abrirArchivoSQLite()}
          onEdit={(view) => setEditor({ view, isNew: false })}
          onConnect={(view) => void connect(view)}
          onToggleReadOnly={(view, readOnly) =>
            void run(() =>
              Connections.Save(
                { ...view.connection, safety: { ...view.connection.safety, readOnly } },
                PasswordAction.PasswordKeep,
                "",
              ),
            )
          }
          onDuplicate={(id) => void run(() => Connections.Duplicate(id))}
          onMoveToFolder={(id, folder) => void run(() => Connections.MoveToFolder(id, folder))}
          onExport={(ids, sugerido) => void exportarConexiones(ids, sugerido)}
          onImport={() => void importarConexiones()}
          onDelete={(id) => void run(() => Connections.Delete(id))}
          onAbout={() => abrirPantallaDeLaApp("about")}
          onSettings={() => abrirPantallaDeLaApp("settings")}
          onCompare={abrirComparacion}
        />
      )}

      {importacion ? (
        <ImportConnectionsDialog
          preview={importacion.preview}
          importing={importacion.importing}
          error={importacion.error}
          onCancel={() => setImportacion(null)}
          onImport={(include) => void confirmarImportacion(include)}
        />
      ) : null}

      {aviso ? <ToastStack toasts={[aviso]} onDismiss={() => setAviso(null)} /> : null}

      {editor ? (
        <ConnectionEditor
          initial={editor.view}
          isNew={editor.isNew}
          folders={carpetasDe(connections)}
          onCancel={() => setEditor(null)}
          onSaved={(saved, shouldConnect) => {
            setEditor(null);
            void (async () => {
              await reload();
              setScreen("manager");
              if (shouldConnect) await connect(saved);
            })();
          }}
        />
      ) : null}

      {dialogosDeConexion}
    </>
  );
}
