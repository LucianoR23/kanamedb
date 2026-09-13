import { useState } from "react";
import * as Connections from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/connections";
import type {
  ConnectionView,
  ImportPreview,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Button, ConfirmDialog, EnvBadge } from "../components/ui";
import type { ToastItem } from "../components/ui";
import { ImportConnectionsDialog } from "../screens/ImportConnectionsDialog";
import { elegirLibreta } from "../lib/libreta";
import { textoDe } from "../lib/dialogos";
import { Credenciales } from "./Credenciales";
import { cx } from "../lib/cx";
import styles from "./mobile.module.css";

/**
 * S01/S02 en el teléfono: la lista, conectar, importar desde el archivo que
 * se exportó en la PC, y cargar las credenciales. Sin editor completo: los
 * campos del túnel y TLS se importan, no se escriben con el pulgar.
 */
export function Conexiones({
  conexiones,
  error,
  onConectar,
  onRecargar,
  onAviso,
  onError,
}: {
  conexiones: ConnectionView[];
  error: string | null;
  onConectar: (v: ConnectionView) => void;
  onRecargar: () => void;
  onAviso: (t: ToastItem) => void;
  onError: (mensaje: string | null) => void;
}) {
  const [credenciales, setCredenciales] = useState<ConnectionView | null>(null);
  const [aBorrar, setABorrar] = useState<ConnectionView | null>(null);
  const [borrando, setBorrando] = useState(false);
  const [importacion, setImportacion] = useState<{
    preview: ImportPreview;
    importing: boolean;
    error: string | null;
  } | null>(null);

  /** Elige un archivo compartido y muestra qué trae. Todavía no agrega nada. */
  async function importar() {
    onError(null);
    try {
      const ruta = await elegirLibreta();
      if (!ruta) return;
      const preview = await Connections.PreviewImport(ruta);
      setImportacion({ preview, importing: false, error: null });
    } catch (err) {
      onError(textoDe(err));
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
    setImportacion(null);
    onRecargar();
    onAviso({
      id: `import-${Date.now()}`,
      tone: "success",
      title: nuevas.length === 1 ? "Se agregó 1 conexión" : `Se agregaron ${nuevas.length} conexiones`,
      detail: "Sin contraseña: tocá «Credenciales» para cargarla.",
    });
  }

  /** Borra la conexión y, con ella, sus secretos del vault: lo hace Go. */
  async function borrar() {
    if (!aBorrar) return;
    setBorrando(true);
    onError(null);
    try {
      await Connections.Delete(aBorrar.connection.id);
      setABorrar(null);
      onRecargar();
      onAviso({ id: `del-${Date.now()}`, tone: "success", title: `Se borró «${aBorrar.connection.name}»` });
    } catch (err) {
      setABorrar(null);
      onError(textoDe(err));
    } finally {
      setBorrando(false);
    }
  }

  return (
    <>
      <header className={styles.barra}>
        <div className={styles.titulo}>
          <strong>Kaname</strong>
          <small>{conexiones.length === 1 ? "1 conexión" : `${conexiones.length} conexiones`}</small>
        </div>
        <button type="button" className={styles.icono} onClick={() => void importar()} aria-label="Importar conexiones" title="Importar">
          ⤓
        </button>
      </header>

      <main className={styles.cuerpo}>
        {error ? <div className={styles.error}>{error}</div> : null}

        {conexiones.length === 0 ? (
          <div className={styles.vacio}>
            <p>No hay conexiones en este teléfono.</p>
            <p className={styles.nota}>
              Exportalas desde Kaname en la PC (sin contraseñas) y traé el archivo. Las
              contraseñas se cargan acá, una vez, y quedan cifradas con tu huella.
            </p>
            <Button variant="primary" className={styles.grande} onClick={() => void importar()}>
              Importar conexiones…
            </Button>
          </div>
        ) : (
          <div className={styles.lista}>
            {conexiones.map((v) => (
              <Conexion
                key={v.connection.id}
                v={v}
                onConectar={() => onConectar(v)}
                onCredenciales={() => setCredenciales(v)}
                onBorrar={() => setABorrar(v)}
              />
            ))}
          </div>
        )}
      </main>

      {credenciales ? (
        <Credenciales
          view={credenciales}
          onClose={() => {
            setCredenciales(null);
            onRecargar();
          }}
          onSaved={(mensaje) => {
            setCredenciales(null);
            onRecargar();
            onAviso({ id: `cred-${Date.now()}`, tone: "success", title: mensaje });
          }}
        />
      ) : null}

      <ConfirmDialog
        open={aBorrar !== null}
        title="Borrar la conexión"
        severidad="aviso"
        etiqueta={borrando ? "Borrando…" : "Borrar"}
        onClose={() => {
          if (!borrando) setABorrar(null);
        }}
        onConfirm={() => void borrar()}
      >
        Se borra <strong>{aBorrar?.connection.name}</strong> de este teléfono, con su contraseña y
        lo que tuviera del túnel. La libreta de la PC no se toca: si la volvés a importar, hay que
        cargar las credenciales otra vez.
      </ConfirmDialog>

      {importacion ? (
        <ImportConnectionsDialog
          preview={importacion.preview}
          importing={importacion.importing}
          error={importacion.error}
          onCancel={() => setImportacion(null)}
          onImport={(include) => void confirmarImportacion(include)}
        />
      ) : null}
    </>
  );
}

function Conexion({
  v,
  onConectar,
  onCredenciales,
  onBorrar,
}: {
  v: ConnectionView;
  onConectar: () => void;
  onCredenciales: () => void;
  onBorrar: () => void;
}) {
  const c = v.connection;
  const prod = c.environment === "production";
  const ssh = c.ssh.enabled;
  const porClave = ssh && c.ssh.auth === "key";
  // Lo que falta para poder conectar. Se dice antes de intentar, no después
  // de un error de autenticación que no explica nada. La frase de paso no
  // cuenta: una clave sin cifrar no tiene, y no hay forma de saberlo desde
  // acá. El agente SSH no tiene secreto que guardar.
  const faltan: string[] = [];
  if (!v.hasPassword && c.engine !== "sqlite") faltan.push("contraseña");
  if (ssh && c.ssh.auth === "password" && !v.hasSSHSecret) faltan.push("contraseña del bastión");
  if (porClave && !v.hasSSHKey) faltan.push("clave privada");
  const problemas = (v.problems ?? []).map((p) => p.message);

  return (
    <div className={cx(styles.tarjeta, prod && styles.tarjetaProd)}>
      <div className={styles.fila1}>
        <strong>{c.name}</strong>
        <EnvBadge env={c.environment as "local" | "dev" | "staging" | "production"} />
      </div>
      <div className={styles.mono}>{v.uri}</div>
      <div className={styles.resumen}>
        {ssh ? <span className={styles.chip}>SSH {c.ssh.user}@{c.ssh.host}</span> : null}
        {c.safety.readOnly ? <span className={styles.chip}>solo lectura</span> : null}
        {faltan.length === 0 ? (
          <span className={cx(styles.chip, styles.chipOk)}>credenciales cargadas</span>
        ) : (
          <span className={cx(styles.chip, styles.chipMal)}>falta: {faltan.join(", ")}</span>
        )}
      </div>
      {problemas.length > 0 ? <div className={styles.error}>{problemas.join(" · ")}</div> : null}
      <div className={styles.acciones}>
        <Button variant="ghost" onClick={onBorrar} aria-label={`Borrar ${c.name}`} style={{ flex: "0 0 auto" }}>
          Borrar
        </Button>
        <Button onClick={onCredenciales}>Credenciales</Button>
        <Button variant="primary" onClick={onConectar} disabled={problemas.length > 0}>
          Conectar
        </Button>
      </div>
    </div>
  );
}
