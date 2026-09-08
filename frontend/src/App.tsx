import { useCallback, useEffect, useState } from "react";
import { Service as AppInfo } from "../bindings/github.com/LucianoR23/kanamedb/internal/appinfo";
import * as Connections from "../bindings/github.com/LucianoR23/kanamedb/internal/service/connections";
import * as SessionSvc from "../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import { PasswordAction } from "../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { ConnectionView } from "../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { About } from "./screens/About";
import { ConnectionEditor } from "./screens/ConnectionEditor";
import { ConnectionManager } from "./screens/ConnectionManager";
import { Shell } from "./screens/Shell";
import { Welcome } from "./screens/Welcome";
import { ConnectionError } from "./screens/ConnectionError";
import { HostKeyDialog } from "./screens/HostKeyDialog";
import * as HostsSvc from "../bindings/github.com/LucianoR23/kanamedb/internal/service/hosts";
import { Verdict } from "../bindings/github.com/LucianoR23/kanamedb/internal/tunnel";
import type { Inspection } from "../bindings/github.com/LucianoR23/kanamedb/internal/tunnel";
import type { ConnectionFailure } from "./screens/ConnectionError";

type Screen = "loading" | "welcome" | "manager" | "shell" | "about";

interface EditorState {
  view: ConnectionView;
  isNew: boolean;
}

export default function App() {
  const [screen, setScreen] = useState<Screen>("loading");
  const [connections, setConnections] = useState<ConnectionView[]>([]);
  const [editor, setEditor] = useState<EditorState | null>(null);
  const [failure, setFailure] = useState<ConnectionFailure | null>(null);
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

  // La verificación de la clave del bastión, esperando decisión.
  //
  // Se guarda la conexión junto con la inspección porque el diálogo puede
  // terminar en "conectar", y para eso hace falta saber a qué conexión volver.
  const [hostKey, setHostKey] = useState<{
    view: ConnectionView;
    inspection: Inspection;
  } | null>(null);
  const [knownHostsPath, setKnownHostsPath] = useState("");

  /**
   * Conecta, verificando antes la clave del bastión si la conexión usa túnel.
   *
   * El orden importa y no es cosmético: se inspecciona ANTES de conectar, y la
   * inspección corta el handshake antes de la autenticación. Cuando aparece el
   * diálogo, el bastión todavía no recibió ninguna credencial.
   */
  async function connect(view: ConnectionView) {
    setError(null);
    setFailure(null);

    if (view.connection.ssh.enabled) {
      const insp = await HostsSvc.Inspect(view.connection);
      if (!insp.ok || !insp.inspection) {
        setFailure({
          connection: view,
          kind: "network",
          message: "No se pudo contactar al bastión SSH.",
          hint: "Revisá el host, el puerto y que el servidor esté escuchando.",
          detail: insp.error ?? "",
          sqlState: "",
          elapsedMs: 0,
        });
        return;
      }
      if (insp.inspection.verdict !== Verdict.VerdictTrusted) {
        // Se corta acá: la decisión es de la persona, y hasta que la tome no
        // se manda nada.
        setKnownHostsPath(await HostsSvc.KnownHostsPath());
        setHostKey({ view, inspection: insp.inspection });
        return;
      }
    }
    await conectarDeVerdad(view);
  }

  async function conectarDeVerdad(view: ConnectionView, acceptOnce = "") {
    // El tiempo se mide acá y no en Go: un rechazo inmediato y un timeout de
    // diez segundos se ven distinto, y eso ya dice algo antes de leer nada.
    const inicio = performance.now();
    try {
      // El fallo no es un error de Go: la promesa se resuelve igual. Por eso
      // Connect devuelve un resultado con `ok` en vez de un par, que se podía
      // ignorar a medias.
      const res = await SessionSvc.ConnectAccepting(view.connection.id, acceptOnce);
      if (!res.ok) {
        const f = res.failure;
        setFailure({
          connection: view,
          kind: f?.kind ?? "other",
          message: f?.message ?? "No se pudo conectar.",
          hint: f?.hint ?? "",
          detail: f?.detail ?? "",
          sqlState: f?.sqlState ?? "",
          elapsedMs: Math.round(performance.now() - inicio),
        });
        return;
      }
      setScreen("shell");
    } catch (err) {
      // El servicio devuelve el fallo ya interpretado; si el puente falla, se
      // muestra lo que haya en vez de nada.
      const f = err as Partial<ConnectionFailure> | undefined;
      setFailure({
        connection: view,
        kind: f?.kind ?? "other",
        message: f?.message ?? (err instanceof Error ? err.message : String(err)),
        hint: f?.hint ?? "",
        detail: f?.detail ?? "",
        sqlState: f?.sqlState ?? "",
        elapsedMs: Math.round(performance.now() - inicio),
      });
    }
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

  if (screen === "about") {
    return (
      <About onBack={() => setScreen(connections.length === 0 ? "welcome" : "manager")} />
    );
  }

  if (screen === "shell") {
    return (
      <Shell
        onOpenAbout={() => setScreen("about")}
        onDisconnect={() => setScreen(connections.length === 0 ? "welcome" : "manager")}
      />
    );
  }

  return (
    <>
      {screen === "welcome" ? (
        <Welcome
          onNew={() => void openNew()}
          onAbout={() => setScreen("about")}
          connectionsPath={connectionsPath}
        />
      ) : (
        <ConnectionManager
          connections={connections}
          error={error}
          onNew={() => void openNew()}
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
          onDelete={(id) => void run(() => Connections.Delete(id))}
          onAbout={() => setScreen("about")}
        />
      )}

      {editor ? (
        <ConnectionEditor
          initial={editor.view}
          isNew={editor.isNew}
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

      {hostKey ? (
        <HostKeyDialog
          inspection={hostKey.inspection}
          knownHostsPath={knownHostsPath}
          onCancel={() => setHostKey(null)}
          onConnectOnce={() => {
            // Sin guardar: la aceptación vale para este intento y nada más.
            // El backend la recibe por AcceptOnce y no toca known_hosts.
            const { view, inspection } = hostKey;
            setHostKey(null);
            void conectarDeVerdad(view, inspection.presented.fingerprint);
          }}
          onTrust={() => {
            const { view, inspection } = hostKey;
            setHostKey(null);
            void HostsSvc.Trust(inspection.address, inspection.authorizedKey)
              .then(() => conectarDeVerdad(view))
              .catch((err) => setError(err instanceof Error ? err.message : String(err)));
          }}
        />
      ) : null}

      {failure ? (
        <ConnectionError
          failure={failure}
          onClose={() => setFailure(null)}
          onRetry={() => {
            const view = failure.connection;
            setFailure(null);
            void connect(view);
          }}
          onEdit={() => {
            const view = failure.connection;
            setFailure(null);
            setEditor({ view, isNew: false });
          }}
        />
      ) : null}
    </>
  );
}
