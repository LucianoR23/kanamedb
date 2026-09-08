import { useEffect, useState } from "react";
import * as Connections from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/connections";
import type { ConnectionView, TestResult } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Connection } from "../../bindings/github.com/LucianoR23/kanamedb/internal/connection";
import {
  Engine,
  Environment,
  SSLMode,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/connection";
import {
  Badge,
  Button,
  EnvBadge,
  Input,
  PasswordField,
  passwordAction,
} from "../components/ui";
import type { PasswordState } from "../components/ui";
import { cx } from "../lib/cx";
import styles from "./ConnectionEditor.module.css";

/** Las tabs de S03. En la Iteración 1 solo General está viva. */
const TABS = [
  { id: "general", label: "General", ready: true, since: "" },
  { id: "tunnel", label: "SSH tunnel", ready: false, since: "Iteración 3" },
  { id: "tls", label: "TLS", ready: false, since: "Iteración 9" },
  { id: "safety", label: "Safety", ready: false, since: "Iteración 5" },
  { id: "advanced", label: "Advanced", ready: false, since: "Iteración 9" },
] as const;

/** Los nombres se escriben como los escribe cada proyecto, no como salen del
 *  enum: "MySQL" y "MariaDB" llevan mayúsculas y "sqlite" no. */
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
  const [pasteNotices, setPasteNotices] = useState<string[]>([]);

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

  const problems = new Map(view.problems?.map((p) => [p.field, p.message]) ?? []);
  // `Valid()` es un método de Go y no cruza el puente: se deriva de los
  // problemas, que sí vienen.
  const valid = problems.size === 0;
  const warnings = view.warnings ?? [];
  const envInfo = ENVIRONMENTS.find((e) => e.value === conn.environment) ?? ENVIRONMENTS[0]!;

  function set<K extends keyof Connection>(key: K, value: Connection[K]) {
    setConn((c) => ({ ...c, [key]: value }));
    setTest(null);
  }

  async function runTest() {
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
    setSaving(true);
    setSaveError(null);
    try {
      const { action, password: pw } = passwordAction(password);
      onSaved(await Connections.Save(conn, action, pw), connect);
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
                        disabled={value !== Engine.Postgres}
                        title={value === Engine.Postgres ? undefined : "Llega en la Iteración 6"}
                        className={cx(styles.chip, conn.engine === value && styles.chipOn)}
                        onClick={() => set("engine", value)}
                      >
                        {label}
                      </button>
                    ))}
                  </div>
                </Field>

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
                ? `${test.server.display} · ${test.server.latencyMs} ms · ${test.server.visibleTables} tablas visibles`
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
          <Button disabled={!valid || saving} onClick={() => void save(false)}>
            Guardar
          </Button>
          <Button
            variant={conn.environment === Environment.Production ? "danger" : "primary"}
            disabled={!valid || saving}
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
  children,
}: {
  label: string;
  error?: string | undefined;
  compact?: boolean;
  align?: "center" | "start";
  children: React.ReactNode;
}) {
  return (
    <div className={cx(styles.field, compact && styles.fieldCompact)}>
      <label className={cx(styles.label, align === "start" && styles.labelTop)}>{label}</label>
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
