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
import { cx } from "../lib/cx";
import { Barra } from "./Barra";
import { Credenciales } from "./Credenciales";
import styles from "./mobile.module.css";

/**
 * M01: la lista de conexiones, la pantalla de entrada. Conectar, importar
 * desde el archivo que se exportó en la PC, cargar las credenciales, borrar.
 * Sin editor completo: los campos del túnel y TLS se importan, no se escriben
 * con el pulgar.
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

  if (credenciales) {
    return (
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
    );
  }

  const vacia = conexiones.length === 0;

  return (
    <div className={styles.pantalla}>
      <Barra
        titulo="Kaname"
        subtitulo={vacia ? "sin conexiones" : conexiones.length === 1 ? "1 conexión" : `${conexiones.length} conexiones`}
        derecha={
          <button
            type="button"
            className={cx(styles.accion, vacia && styles.accionActiva)}
            onClick={() => void importar()}
            aria-label="Importar conexiones"
            title="Importar"
          >
            ⤓
          </button>
        }
      />

      {vacia ? (
        <main className={cx(styles.cuerpo, styles.cuerpoCentrado)}>
          {error ? <ErrorDeCarga texto={error} /> : null}
          <div className={styles.vacio}>
            <span className={styles.vacioIcono} aria-hidden="true">
              ⤓
            </span>
            <span className={styles.vacioTitulo}>Todavía no hay conexiones</span>
            <span className={styles.vacioTexto}>
              Exportá desde la PC un archivo <code>.toml</code> con tus conexiones y abrilo acá. Las contraseñas
              no viajan en el archivo: se cargan una sola vez en este teléfono.
            </span>
            <Button variant="primary" onClick={() => void importar()}>
              Importar archivo
            </Button>
          </div>
        </main>
      ) : (
        <main className={styles.cuerpo}>
          {error ? <ErrorDeCarga texto={error} /> : null}
          {conexiones.map((v) => (
            <Conexion
              key={v.connection.id}
              v={v}
              onConectar={() => onConectar(v)}
              onCredenciales={() => setCredenciales(v)}
              onBorrar={() => setABorrar(v)}
            />
          ))}
          <div className={styles.notaPie}>La contraseña y la clave privada viven solamente en este teléfono.</div>
        </main>
      )}

      <ConfirmDialog
        open={aBorrar !== null}
        title={`Borrar «${aBorrar?.connection.name ?? ""}»`}
        severidad="aviso"
        etiqueta={borrando ? "Borrando…" : "Borrar"}
        onClose={() => {
          if (!borrando) setABorrar(null);
        }}
        onConfirm={() => void borrar()}
      >
        Se van la conexión y sus secretos de este teléfono: contraseña de la base, contraseña del bastión y clave
        privada. La base de datos no se toca, y la libreta de la PC tampoco.
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
    </div>
  );
}

/** M01 C: algo falló al importar o al leer la libreta. */
function ErrorDeCarga({ texto }: { texto: string }) {
  return (
    <div className={styles.errorTarjeta}>
      <div className={styles.errorTitulo}>No se pudo</div>
      <div className={styles.errorTexto}>{texto}</div>
    </div>
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
  const listo = faltan.length === 0 && problemas.length === 0;

  return (
    <div className={cx(styles.tarjeta, prod && styles.tarjetaProd)}>
      <div className={styles.fila1}>
        <span className={styles.nombre}>{c.name}</span>
        <EnvBadge env={c.environment as "local" | "dev" | "staging" | "production"} />
      </div>
      <div className={styles.uri}>{v.uri}</div>
      {ssh || c.safety.readOnly ? (
        <div className={styles.chips}>
          {ssh ? (
            <span className={styles.chip}>
              SSH {c.ssh.user}@{c.ssh.host}
            </span>
          ) : null}
          {c.safety.readOnly ? <span className={cx(styles.chip, styles.chipSuave)}>solo lectura</span> : null}
        </div>
      ) : null}
      <div className={cx(styles.estado, faltan.length > 0 && styles.estadoMal)}>
        <span className={styles.punto} aria-hidden="true" />
        <span>{faltan.length === 0 ? "credenciales cargadas" : `falta: ${faltan.join(", ")}`}</span>
      </div>
      {problemas.length > 0 ? <div className={cx(styles.aviso, styles.avisoMal)}>{problemas.join(" · ")}</div> : null}
      <div className={styles.acciones}>
        <Button className={cx(styles.fijo, styles.borrar)} onClick={onBorrar} aria-label={`Borrar ${c.name}`}>
          Borrar
        </Button>
        {/* Cuando falta algo, Credenciales es la acción y Conectar pasa a
            segundo plano, pero sigue: una base con acceso sin contraseña
            existe, y el que sabe que la suya lo es no tiene por qué cargar
            una. Lo que sí deshabilita son los problemas de la conexión. */}
        <Button variant={listo ? "secondary" : "primary"} onClick={onCredenciales}>
          Credenciales
        </Button>
        <Button variant={listo ? "primary" : "secondary"} onClick={onConectar} disabled={problemas.length > 0}>
          Conectar
        </Button>
      </div>
    </div>
  );
}
