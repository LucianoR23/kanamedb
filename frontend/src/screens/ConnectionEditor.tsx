import { useEffect, useState } from "react";
import * as Connections from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/connections";
import type { ConnectionView, TestResult } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Connection } from "../../bindings/github.com/LucianoR23/kanamedb/internal/connection";
import {
  Environment,
  SSLMode,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/connection";
// El enum de motores vive en el paquete `engine` desde la Iteración 6, y
// `connection.Engine` quedó como un alias de tipo. Un alias de Go se genera
// como `export type`, así que sirve para tipar y no para escribir
// `Engine.Postgres`: los VALORES hay que traerlos de donde está el enum.
import { Kind as Engine } from "../../bindings/github.com/LucianoR23/kanamedb/internal/engine";
import { AuthMethod } from "../../bindings/github.com/LucianoR23/kanamedb/internal/tunnel";
import { elegirArchivoSQLite } from "../lib/archivoSQLite";
import {
  Badge,
  Button,
  EnvBadge,
  InfoHint,
  Input,
  PasswordField,
  Toggle,
  passwordAction,
} from "../components/ui";
import type { PasswordState } from "../components/ui";
import { cx } from "../lib/cx";
import styles from "./ConnectionEditor.module.css";

/** Las tabs de S03. En la Iteración 1 solo General está viva. */
const TABS = [
  { id: "general", label: "General", ready: true, since: "" },
  { id: "tunnel", label: "Túnel SSH", ready: true, since: "" },
  { id: "tls", label: "TLS", ready: false, since: "Iteración 9" },
  { id: "safety", label: "Safety", ready: false, since: "Iteración 9" },
  { id: "advanced", label: "Advanced", ready: false, since: "Iteración 9" },
] as const;

/** Los nombres se escriben como los escribe cada proyecto, no como salen del
 *  enum: "MySQL" y "MariaDB" llevan mayúsculas y "sqlite" no. */
/** El puerto habitual de cada motor. SQLite es un archivo y no escucha en
 *  ningún lado, así que su puerto es cero — y el formulario ni lo muestra. */
const PUERTOS: Record<string, number> = {
  [Engine.Postgres]: 5432,
  [Engine.MySQL]: 3306,
  [Engine.MariaDB]: 3306,
  [Engine.SQLite]: 0,
};

const ENGINES: { value: Engine; label: string }[] = [
  { value: Engine.Postgres, label: "PostgreSQL" },
  { value: Engine.MySQL, label: "MySQL" },
  { value: Engine.MariaDB, label: "MariaDB" },
  { value: Engine.SQLite, label: "SQLite" },
];

const ENVIRONMENTS: { value: Environment; label: string; hint: string }[] = [
  {
    value: Environment.Local,
    label: "Local",
    hint: "Sin confirmación al escribir. La grilla se puede editar apenas se abre una tabla.",
  },
  {
    value: Environment.Dev,
    label: "Dev",
    hint: "Preview de SQL antes de aplicar. Sin puertas extra.",
  },
  {
    value: Environment.Staging,
    label: "Staging",
    hint: "Las sentencias destructivas se listan con la cantidad de filas afectadas antes de aplicar.",
  },
  {
    value: Environment.Production,
    label: "Production",
    hint: "Etiqueta sólida en todas partes, barra de estado teñida, y hay que tipear el nombre de la base para aplicar. Esto no se puede apagar.",
  },
];

/** Los métodos de autenticación del bastión, con lo que hay que saber de cada uno. */
const SSH_AUTH = [
  {
    value: AuthMethod.AuthAgent,
    label: "ssh-agent",
    hint: "La clave privada nunca sale del agente: Kaname le manda lo que hay que firmar y recibe la firma. Es el único método donde la aplicación no ve la clave.",
  },
  {
    value: AuthMethod.AuthKeyFile,
    label: "Clave privada",
    hint: "Se guarda la ruta, no la clave. El archivo vive en esta máquina y el de conexiones puede sincronizarse sin llevarse nada.",
  },
  {
    value: AuthMethod.AuthPassword,
    label: "Contraseña",
    hint: "Se guarda en el keychain del sistema. Existe porque hay bastiones que solo aceptan esto, no porque sea la mejor opción.",
  },
] as const;

interface Props {
  /** La conexión a editar. Un borrador nuevo viene de Draft(). */
  initial: ConnectionView;
  /** Si ya existía, el campo de contraseña arranca en "guardada". */
  isNew: boolean;
  onCancel: () => void;
  onSaved: (view: ConnectionView, connect: boolean) => void;
}

export function ConnectionEditor({ initial, isNew, onCancel, onSaved }: Props) {
  const [conn, setConn] = useState<Connection>(initial.connection);
  const [view, setView] = useState<ConnectionView>(initial);
  const [tab, setTab] = useState<string>("general");
  const [password, setPassword] = useState<PasswordState>(
    initial.hasPassword ? { kind: "stored" } : { kind: "empty" },
  );
  const [test, setTest] = useState<TestResult | null>(null);
  const [testing, setTesting] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  // Un fallo del selector de archivos del sistema. Cancelar NO cae acá.
  const [errorArchivo, setErrorArchivo] = useState<string | null>(null);
  const [pasteNotices, setPasteNotices] = useState<string[]>([]);

  // Qué campos tocó la persona, y si ya intentó guardar o probar.
  //
  // Sin esto, abrir una conexión nueva pinta de rojo todo lo obligatorio antes
  // de que nadie haya escrito una letra. Un formulario que reta por no haber
  // completado un campo al que todavía no se llegó enseña a ignorar el rojo, y
  // entonces el rojo deja de servir cuando de verdad hace falta.
  const [tocados, setTocados] = useState<ReadonlySet<string>>(new Set());
  const [intentado, setIntentado] = useState(false);
  // El bastión tiene su propio secreto —contraseña o frase de paso— y su propio
  // estado: una conexión puede tener guardada la de la base y no la del salto.
  const [sshPassword, setSshPassword] = useState<PasswordState>(
    initial.hasSSHSecret ? { kind: "stored" } : { kind: "empty" },
  );

  // La validación vive en Go: el formulario no reimplementa las reglas, las
  // consulta. Así no puede divergir de lo que el store va a aceptar.
  useEffect(() => {
    let cancelled = false;
    Connections.Check(conn)
      .then((v) => {
        if (!cancelled) setView(v);
      })
      .catch(() => {
        /* Check no falla salvo que el puente esté roto; el guardado avisará. */
      });
    return () => {
      cancelled = true;
    };
  }, [conn]);

  const todosLosProblemas = new Map(view.problems?.map((p) => [p.field, p.message]) ?? []);

  // Los problemas que se MUESTRAN: los de campos ya tocados, y todos una vez
  // que se intentó guardar o probar. En ese momento sí corresponde señalar lo
  // que falta, porque la persona dijo "listo".
  const problems = {
    get: (campo: string) =>
      intentado || tocados.has(campo) ? todosLosProblemas.get(campo) : undefined,
    has: (campo: string) => (intentado || tocados.has(campo)) && todosLosProblemas.has(campo),
  };

  // A qué pestaña pertenece cada campo, para poder señalarla y para saltar a
  // ella cuando se intenta guardar con algo incompleto en otra.
  function tabDelCampo(campo: string): string {
    if (campo.startsWith("ssh.")) return "tunnel";
    if (campo.startsWith("safety.")) return "safety";
    return "general";
  }

  const tabsConProblemas = new Set(
    [...todosLosProblemas.keys()].map(tabDelCampo),
  );

  /** La primera pestaña con algo incompleto, en el orden en que se muestran. */
  function primerTabConProblema(): string | null {
    for (const t of TABS) {
      if (tabsConProblemas.has(t.id)) return t.id;
    }
    return null;
  }

  /** Marca un campo como tocado para que sus errores empiecen a mostrarse. */
  function tocar(campo: string) {
    setTocados((prev) => {
      if (prev.has(campo)) return prev;
      const next = new Set(prev);
      next.add(campo);
      return next;
    });
  }
  // `Valid()` es un método de Go y no cruza el puente: se deriva de los
  // problemas, que sí vienen.
  const warnings = view.warnings ?? [];
  const envInfo = ENVIRONMENTS.find((e) => e.value === conn.environment) ?? ENVIRONMENTS[0]!;

  // El túnel vive en un struct anidado, así que tiene su propio setter. Sin
  // esto habría que reconstruir el objeto entero en cada tecla.
  function setSSH<K extends keyof Connection["ssh"]>(key: K, value: Connection["ssh"][K]) {
    setConn((c) => ({ ...c, ssh: { ...c.ssh, [key]: value } }));
    tocar(`ssh.${String(key)}`);
    setTest(null);
  }

  function set<K extends keyof Connection>(key: K, value: Connection[K]) {
    setConn((c) => ({ ...c, [key]: value }));
    tocar(String(key));
    setTest(null);
  }

  /** cambiarMotor arrastra el puerto por defecto.
   *
   *  Sin esto, pasar de PostgreSQL a MySQL deja el 5432 escrito y la conexión
   *  falla con un error de red que no dice que el puerto es el de otro motor.
   *  Solo se pisa si el que había era el default del motor anterior: un puerto
   *  que el usuario escribió a mano se respeta. */
  function cambiarMotor(motor: Engine) {
    setConn((c) => {
      const eraDefault = c.port === 0 || c.port === (PUERTOS[c.engine] ?? 0);
      return { ...c, engine: motor, port: eraDefault ? (PUERTOS[motor] ?? 0) : c.port };
    });
    tocar("engine");
    setTest(null);
  }

  /** elegirArchivo abre el selector del sistema para una base de SQLite.
   *
   *  Cancelar no deja rastro: `elegirArchivoSQLite` devuelve vacío. Lo que se
   *  atrapa acá es un fallo de verdad del selector, y se muestra al lado del
   *  campo —que es donde está el botón— y no en un cartel suelto. */
  async function elegirArchivo() {
    setErrorArchivo(null);
    let ruta = "";
    try {
      ruta = await elegirArchivoSQLite();
    } catch (err) {
      setErrorArchivo(err instanceof Error ? err.message : String(err));
      return;
    }
    if (ruta) {
      set("database", ruta);
    }
  }

  /** esArchivo decide la forma del formulario entero. */
  const esArchivo = conn.engine === Engine.SQLite;

  async function runTest() {
    setIntentado(true);
    // Si falta algo, se salta a la pestaña donde está en vez de mostrar un
    // error que menciona campos que no se ven. Con dos «host» en dos pestañas
    // distintas, un mensaje sin destino manda a buscar al lugar equivocado.
    const falta = primerTabConProblema();
    if (falta) {
      setTab(falta);
      return;
    }
    setTesting(true);
    setTest(null);
    try {
      const { action, password: pw } = passwordAction(password);
      setTest(await Connections.Test(conn, action, pw));
    } catch (err) {
      setTest({
        ok: false,
        failure: {
          kind: "other",
          message: err instanceof Error ? err.message : String(err),
          hint: "",
        },
      } as TestResult);
    } finally {
      setTesting(false);
    }
  }

  async function save(connect: boolean) {
    setIntentado(true);
    const falta = primerTabConProblema();
    if (falta) {
      setTab(falta);
      return;
    }
    setSaving(true);
    setSaveError(null);
    try {
      const { action, password: pw } = passwordAction(password);
      // El secreto del bastión se guarda en la misma llamada: si fueran dos,
      // la segunda podría fallar y dejar la conexión guardada apuntando a una
      // credencial de SSH que no existe.
      const ssh = passwordAction(sshPassword);
      onSaved(
        await Connections.SaveWithSSH(conn, action, pw, ssh.action, ssh.password),
        connect,
      );
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }

  async function pasteURI() {
    setPasteNotices([]);
    let raw = "";
    try {
      raw = await navigator.clipboard.readText();
    } catch {
      setPasteNotices(["No se pudo leer el portapapeles."]);
      return;
    }
    try {
      const parsed = await Connections.ParseURI(raw);
      // Solo se pisan los campos que la cadena trajo: el nombre y el entorno
      // se conservan, porque adivinar "producción" mal es peligroso en las dos
      // direcciones.
      setConn((c) => ({
        ...c,
        engine: parsed.connection.engine || c.engine,
        host: parsed.connection.host || c.host,
        port: parsed.connection.port || c.port,
        database: parsed.connection.database || c.database,
        user: parsed.connection.user || c.user,
        sslMode: parsed.connection.sslMode || c.sslMode,
      }));
      if (parsed.password) {
        setPassword({ kind: "typed", value: parsed.password });
      }
      setPasteNotices(parsed.notices ?? []);
      setTest(null);
    } catch (err) {
      setPasteNotices([err instanceof Error ? err.message : String(err)]);
    }
  }

  const envClass =
    conn.environment === Environment.Production
      ? styles.envProduction
      : conn.environment === Environment.Staging
        ? styles.envStaging
        : conn.environment === Environment.Dev
          ? styles.envDev
          : styles.envLocal;

  return (
    <div className={styles.scrim} role="dialog" aria-modal="true" aria-label="Editor de conexión">
      <div className={cx(styles.dialog, envClass)}>
        <div className={styles.envStripe} />

        <header className={styles.header}>
          <span className={styles.title}>
            {isNew ? "Nueva conexión" : "Editar conexión"}
          </span>
          <EnvBadge env={conn.environment as "local" | "dev" | "staging" | "production"} />
          <span className={styles.spacer} />
          <span className={styles.storedAt} title={initial.keychainRef}>
            se guarda en connections.toml
          </span>
          <Button variant="ghost" size="sm" aria-label="Cerrar" onClick={onCancel}>
            ✕
          </Button>
        </header>

        <div className={styles.tabs} role="tablist">
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={t.id === tab}
              disabled={!t.ready}
              title={t.ready ? undefined : `Llega en la ${t.since}`}
              className={cx(styles.tab, t.id === tab && styles.tabActive)}
              onClick={() => setTab(t.id)}
            >
              {t.label}
              {/* El punto marca la pestaña donde falta algo. Sin él, un error
                  que menciona un campo de otra pestaña obliga a abrirlas todas
                  a ver dónde está. Solo aparece después de intentar guardar o
                  probar: antes sería el mismo rojo prematuro, movido de lugar. */}
              {intentado && tabsConProblemas.has(t.id) ? (
                <span className={styles.tabAviso} aria-label="tiene campos sin completar" />
              ) : null}
            </button>
          ))}
        </div>

        <div className={styles.body}>
          {tab === "general" ? (
            <div className={styles.general}>
              <div className={styles.fields}>
                <Field label="Nombre" error={problems.get("name")}>
                  <Input
                    value={conn.name}
                    autoFocus
                    invalid={problems.has("name")}
                    onChange={(e) => set("name", e.currentTarget.value)}
                  />
                </Field>

                <Field label="Motor" error={problems.get("engine")}>
                  <div className={styles.chips}>
                    {ENGINES.map(({ value, label }) => (
                      <button
                        key={value}
                        type="button"
                        className={cx(styles.chip, conn.engine === value && styles.chipOn)}
                        onClick={() => cambiarMotor(value)}
                      >
                        {label}
                      </button>
                    ))}
                  </div>
                </Field>

                {esArchivo ? (
                  /* SQLite no es un servidor: es un archivo, y el permiso lo da
                     el sistema de archivos. Host, puerto, usuario, contraseña y
                     SSL no existen — y dejarlos en pantalla deshabilitados sería
                     peor que sacarlos: haría pensar que falta configurarlos. */
                  <Field label="Archivo" error={errorArchivo ?? problems.get("database")}>
                    <div className={styles.fileRow}>
                      <Input
                        value={conn.database}
                        invalid={problems.has("database")}
                        placeholder="C:\Users\yo\datos\app.db"
                        onChange={(e) => set("database", e.currentTarget.value)}
                      />
                      <Button variant="ghost" onClick={elegirArchivo}>
                        Elegir…
                      </Button>
                    </div>
                    <p className={styles.hint}>
                      Si el archivo no existe, SQLite lo crea vacío al conectar.
                    </p>
                  </Field>
                ) : (
                  <>
                <div className={styles.hostRow}>
                  <Field label="Host" error={problems.get("host")}>
                    <Input
                      value={conn.host}
                      invalid={problems.has("host")}
                      onChange={(e) => set("host", e.currentTarget.value)}
                    />
                  </Field>
                  <Field label="Puerto" error={problems.get("port")} compact>
                    <Input
                      value={conn.port === 0 ? "" : String(conn.port)}
                      inputMode="numeric"
                      invalid={problems.has("port")}
                      onChange={(e) => set("port", Number(e.currentTarget.value) || 0)}
                    />
                  </Field>
                </div>

                <Field label="Base" error={problems.get("database")}>
                  <Input
                    value={conn.database}
                    invalid={problems.has("database")}
                    onChange={(e) => set("database", e.currentTarget.value)}
                  />
                </Field>

                <Field label="Usuario" error={problems.get("user")}>
                  <Input
                    value={conn.user}
                    invalid={problems.has("user")}
                    onChange={(e) => set("user", e.currentTarget.value)}
                  />
                </Field>

                <Field label="Contraseña">
                  <PasswordField
                    state={password}
                    onChange={(next) => {
                      setPassword(next);
                      setTest(null);
                    }}
                    onReveal={() => Connections.RevealPassword(conn.id)}
                  />
                </Field>

                <Field label="SSL" error={problems.get("sslMode")}>
                  <div className={styles.chips}>
                    {[
                      SSLMode.SSLDisable,
                      SSLMode.SSLPrefer,
                      SSLMode.SSLRequire,
                      SSLMode.SSLVerifyCA,
                      SSLMode.SSLVerifyFull,
                    ].map((m) => (
                      <button
                        key={m}
                        type="button"
                        className={cx(
                          styles.chip,
                          styles.chipMono,
                          conn.sslMode === m && styles.chipOn,
                        )}
                        onClick={() => set("sslMode", m)}
                      >
                        {m}
                      </button>
                    ))}
                  </div>
                </Field>
                  </>
                )}

                <div className={styles.divider} />

                <Field label="Entorno" align="start">
                  <div className={styles.envColumn}>
                    <div className={styles.chips}>
                      {ENVIRONMENTS.map((e) => (
                        <button
                          key={e.value}
                          type="button"
                          className={cx(
                            styles.chip,
                            styles.envChip,
                            styles[`env_${e.value}`],
                            conn.environment === e.value && styles.envChipOn,
                          )}
                          onClick={() => set("environment", e.value)}
                        >
                          <span className={styles.envDot} />
                          {e.label}
                        </button>
                      ))}
                    </div>
                    <p className={styles.envHint}>{envInfo.hint}</p>
                  </div>
                </Field>
              </div>

              <aside className={styles.side}>
                <div className={styles.card}>
                  <div className={styles.cardLabel}>Connection URI</div>
                  <div className={styles.uri}>{view.uri}</div>
                  <div className={styles.cardActions}>
                    <button
                      type="button"
                      className={styles.link}
                      onClick={() => void navigator.clipboard.writeText(view.uri)}
                    >
                      Copiar
                    </button>
                    <button type="button" className={styles.link} onClick={() => void pasteURI()}>
                      Pegar y completar
                    </button>
                  </div>
                  {pasteNotices.length > 0 ? (
                    <ul className={styles.notices}>
                      {pasteNotices.map((n) => (
                        <li key={n}>{n}</li>
                      ))}
                    </ul>
                  ) : null}
                  <p className={styles.cardFoot}>Sin contraseña. Se puede compartir.</p>
                </div>

                {warnings.length > 0 ? (
                  <div className={cx(styles.card, styles.cardWarn)}>
                    <div className={styles.cardLabel}>Avisos</div>
                    <ul className={styles.warnList}>
                      {warnings.map((w) => (
                        <li key={w.field}>{w.message}</li>
                      ))}
                    </ul>
                  </div>
                ) : null}
              </aside>
            </div>
          ) : tab === "tunnel" ? (
            <div className={styles.tunnel}>
              <div className={styles.tunnelSwitch}>
                <Toggle
                  checked={conn.ssh.enabled}
                  onChange={(v) => setSSH("enabled", v)}
                  label="Conectar a través de un túnel SSH"
                />
                <span className={styles.tunnelSwitchLabel}>
                  Conectar a través de un túnel SSH
                </span>
              </div>

              {conn.ssh.enabled ? (
                <>
                  <div className={styles.fields}>
                    <div className={styles.hostRow}>
                      <Field
                        label="Host SSH"
                        error={problems.get("ssh.host")}
                        hint={
                          <>
                            El servidor por el que se salta, no la base. La base se configura en la
                            pestaña General con el nombre que tenga <em>desde el bastión</em>: puede
                            ser un nombre que desde esta máquina no resuelve, y está bien.
                          </>
                        }
                      >
                        <Input
                          value={conn.ssh.host}
                          invalid={problems.has("ssh.host")}
                          onChange={(e) => setSSH("host", e.currentTarget.value)}
                        />
                      </Field>
                      <Field label="Puerto" error={problems.get("ssh.port")} compact>
                        <Input
                          value={String(conn.ssh.port || "")}
                          inputMode="numeric"
                          invalid={problems.has("ssh.port")}
                          onChange={(e) =>
                            setSSH("port", Number(e.currentTarget.value.replace(/\D/g, "")) || 0)
                          }
                        />
                      </Field>
                    </div>

                    <Field label="Usuario SSH" error={problems.get("ssh.user")}>
                      <Input
                        value={conn.ssh.user}
                        invalid={problems.has("ssh.user")}
                        onChange={(e) => setSSH("user", e.currentTarget.value)}
                      />
                    </Field>

                    <Field label="Autenticación" error={problems.get("ssh.auth")} align="start">
                      <div className={styles.envColumn}>
                        <div className={styles.chips}>
                          {SSH_AUTH.map((a) => (
                            <button
                              key={a.value}
                              type="button"
                              className={cx(styles.chip, conn.ssh.auth === a.value && styles.chipOn)}
                              onClick={() => setSSH("auth", a.value)}
                            >
                              {a.label}
                            </button>
                          ))}
                        </div>
                        <p className={styles.envHint}>
                          {SSH_AUTH.find((a) => a.value === conn.ssh.auth)?.hint ?? ""}
                        </p>
                      </div>
                    </Field>

                    {conn.ssh.auth === AuthMethod.AuthKeyFile ? (
                      <Field
                        label="Clave privada"
                        error={problems.get("ssh.keyPath")}
                        hint={
                          <>
                            La ruta al archivo de la clave, no su contenido. Sirven las dos formas:
                            <br />
                            <code>C:\Users\vos\.ssh\id_ed25519</code>
                            <br />
                            <code>~/.ssh/id_ed25519</code>
                            <br />
                            <br />
                            El <code>~</code> se resuelve al conectar, no al guardar, así que el
                            archivo de conexiones se puede sincronizar entre máquinas y en cada una
                            apunta a su propio directorio.
                            <br />
                            <br />
                            Va <strong>sin comillas</strong>. Si copiaste con «Copiar como ruta»
                            del Explorador, Kaname se las saca solo; los espacios en el nombre no
                            necesitan comillas acá porque esto no pasa por una terminal.
                            <br />
                            <br />
                            Es la clave <strong>privada</strong> —sin <code>.pub</code>—. Si está
                            cifrada, la frase de paso va en el campo de abajo.
                          </>
                        }
                      >
                        <Input
                          value={conn.ssh.keyPath ?? ""}
                          placeholder="~/.ssh/id_ed25519"
                          invalid={problems.has("ssh.keyPath")}
                          onChange={(e) => setSSH("keyPath", e.currentTarget.value)}
                        />
                      </Field>
                    ) : null}

                    {conn.ssh.auth === AuthMethod.AuthKeyFile ||
                    conn.ssh.auth === AuthMethod.AuthPassword ? (
                      <Field
                        label={
                          conn.ssh.auth === AuthMethod.AuthPassword ? "Contraseña SSH" : "Frase de paso"
                        }
                      >
                        <PasswordField
                          state={sshPassword}
                          onChange={setSshPassword}
                          onReveal={() => Connections.RevealSSHSecret(conn.id)}
                        />
                      </Field>
                    ) : null}
                  </div>

                  <div className={styles.tunnelNote}>
                    <p className={styles.tunnelNoteTitle}>
                      El túnel no abre ningún puerto en esta máquina.
                    </p>
                    <p>
                      La conexión a la base viaja por dentro del canal SSH y existe solo dentro
                      del proceso. Por eso no hay un «puerto local» que configurar: un puerto en
                      loopback sería alcanzable desde cualquier pestaña del navegador.
                    </p>
                    <p>
                      La primera vez que se conecte a{" "}
                      <code>{conn.ssh.host || "el bastión"}</code>, Kaname va a mostrar la huella
                      de su clave para que la verifiques. Si esa clave cambia alguna vez, se
                      detiene y avisa <strong>antes de enviar ninguna credencial</strong>.
                    </p>
                  </div>
                </>
              ) : (
                <p className={styles.tunnelOff}>
                  La conexión va directa a la base. Activá el túnel si la base solo es alcanzable
                  a través de un bastión.
                </p>
              )}
            </div>
          ) : (
            <div className={styles.placeholder}>
              <p className={styles.placeholderTitle}>
                {TABS.find((t) => t.id === tab)?.label}
              </p>
              <p className={styles.placeholderText}>
                Llega en la {TABS.find((t) => t.id === tab)?.since}.
              </p>
            </div>
          )}
        </div>

        {test ? (
          <div className={cx(styles.testBar, test.ok ? styles.testOk : styles.testFail)}>
            <span className={styles.testDot} />
            <span className={styles.testTitle}>
              {test.ok ? "Conecta" : (test.failure?.message ?? "No conecta")}
            </span>
            <span className={styles.testDetail}>
              {test.ok && test.server
                ? `${test.server.display} · ${test.server.latencyMs} ms · ${test.server.visibleTables} ${
                    test.server.visibleTables === 1 ? "tabla visible" : "tablas visibles"
                  }`
                : (test.failure?.hint ?? "")}
            </span>
            <span className={styles.spacer} />
            {test.failure?.sqlState ? (
              <Badge tone="danger">{test.failure.sqlState}</Badge>
            ) : null}
            <Button variant="ghost" size="sm" aria-label="Cerrar" onClick={() => setTest(null)}>
              ✕
            </Button>
          </div>
        ) : null}

        {saveError ? (
          <div className={cx(styles.testBar, styles.testFail)} role="alert">
            <span className={styles.testTitle}>No se pudo guardar. {saveError}</span>
          </div>
        ) : null}

        <footer className={styles.footer}>
          <Button loading={testing} onClick={() => void runTest()}>
            Probar conexión
          </Button>
          <span className={styles.footNote}>ninguna credencial sale de esta máquina</span>
          <span className={styles.spacer} />
          <Button variant="ghost" onClick={onCancel}>
            Cancelar
          </Button>
          {/* Guardar NO se deshabilita cuando falta algo.
            *
            * Un botón apagado no dice qué falta ni dónde, y con cinco pestañas
            * el campo que falta puede estar en una que no se abrió nunca.
            * Apretarlo señala los campos y lleva a la pestaña donde están, que
            * es lo que alguien necesita para poder terminar. */}
          <Button disabled={saving} onClick={() => void save(false)}>
            Guardar
          </Button>
          <Button
            variant={conn.environment === Environment.Production ? "danger" : "primary"}
            disabled={saving}
            onClick={() => void save(true)}
          >
            Guardar y conectar
          </Button>
        </footer>
      </div>
    </div>
  );
}

function Field({
  label,
  error,
  compact = false,
  align = "center",
  hint,
  children,
}: {
  label: string;
  error?: string | undefined;
  compact?: boolean;
  align?: "center" | "start";
  /** Ayuda que se abre al pasar el mouse o al enfocar. Para lo que no cabe en
   *  la etiqueta pero hace falta ANTES de escribir. */
  hint?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className={cx(styles.field, compact && styles.fieldCompact)}>
      <label className={cx(styles.label, align === "start" && styles.labelTop)}>
        {label}
        {hint ? <InfoHint label={`Ayuda sobre ${label}`}>{hint}</InfoHint> : null}
      </label>
      <div className={styles.fieldBody}>
        {children}
        {error ? (
          <div className={styles.fieldError} role="alert">
            {error}
          </div>
        ) : null}
      </div>
    </div>
  );
}
