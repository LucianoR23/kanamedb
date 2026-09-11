import { useState } from "react";
import * as Connections from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/connections";
import type { TestResult } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { TLS, Warning } from "../../bindings/github.com/LucianoR23/kanamedb/internal/connection";
import { SSLMode } from "../../bindings/github.com/LucianoR23/kanamedb/internal/engine";
import { Badge, Button, Input } from "../components/ui";
import { elegirCertificado, elegirDestinoCertificado } from "../lib/certificados";
import type { TipoDeCertificado } from "../lib/certificados";
import { cx } from "../lib/cx";
import { textoDe } from "../lib/dialogos";
import { Field } from "./ConnectionField";
import editor from "./ConnectionEditor.module.css";
import styles from "./TlsTab.module.css";

/** Los modos, en el orden de libpq, con lo que hay que saber de cada uno. */
const MODOS: { value: SSLMode; hint: string }[] = [
  {
    value: SSLMode.SSLDisable,
    hint: "Sin cifrado: todo viaja en claro, credenciales incluidas. Solo tiene sentido contra localhost.",
  },
  {
    value: SSLMode.SSLPrefer,
    hint: "Cifra si el servidor ofrece TLS y sigue en claro si no. No verifica el certificado: no protege contra un intermediario.",
  },
  {
    value: SSLMode.SSLRequire,
    hint: "Exige cifrado. Sin raíz cargada no verifica el certificado; con una, verifica la cadena igual que verify-ca.",
  },
  {
    value: SSLMode.SSLVerifyCA,
    hint: "Verifica que el certificado lo firmó la raíz —la cargada, o las del sistema— sin comprobar el nombre del host.",
  },
  {
    value: SSLMode.SSLVerifyFull,
    hint: "Verifica la cadena y que el nombre del host coincida con el certificado. Recomendado para cualquier cosa que no sea localhost.",
  },
];

/** `allow` existe en libpq y en el modelo, pero no se ofrece: es `prefer` al
 *  revés —empieza en claro y cifra solo si el servidor lo exige— y nadie lo
 *  elige a propósito. Si viene del archivo, se muestra para que se vea. */
const ALLOW = {
  value: SSLMode.SSLAllow,
  hint: "Empieza en claro y pasa a TLS solo si el servidor lo exige. No verifica nada.",
};

interface Problemas {
  get: (campo: string) => string | undefined;
  has: (campo: string) => boolean;
}

interface Props {
  /** SQLite: no hay canal que cifrar, y la pestaña lo dice. */
  esArchivo: boolean;
  host: string;
  sslMode: SSLMode;
  tls: TLS;
  problems: Problemas;
  /** Los avisos de la conexión que hablan de esta pestaña. */
  avisos: Warning[];
  /** El resultado de «Probar conexión», que es de donde sale el certificado. */
  test: TestResult | null;
  onMode: (m: SSLMode) => void;
  onTLS: (t: TLS) => void;
}

/**
 * S03, pestaña TLS: el modo, los certificados por ruta, y lo que el servidor
 * presentó la última vez que se probó.
 *
 * Los tres archivos se guardan por ruta, como la clave del túnel: viven en
 * esta máquina y la libreta solo los nombra. La clave del cliente va sin
 * cifrar, que es como la lee pgx.
 */
export function TlsTab({
  esArchivo,
  host,
  sslMode,
  tls,
  problems,
  avisos,
  test,
  onMode,
  onTLS,
}: Props) {
  // Un fallo del selector del sistema, al lado del campo. Cancelar NO cae acá.
  const [errorSelector, setErrorSelector] = useState<{ campo: keyof TLS; texto: string } | null>(null);
  const [guardado, setGuardado] = useState<string | null>(null);
  const [errorGuardar, setErrorGuardar] = useState<string | null>(null);

  if (esArchivo) {
    return (
      <div className={styles.pane}>
        <p className={editor.tunnelOff}>
          SQLite es un archivo local: no hay conexión de red que cifrar. El permiso lo da el
          sistema de archivos.
        </p>
      </div>
    );
  }

  const modo = sslMode || SSLMode.SSLPrefer;
  const modos = modo === SSLMode.SSLAllow ? [MODOS[0]!, ALLOW, ...MODOS.slice(1)] : MODOS;
  const actual = modos.find((m) => m.value === modo) ?? MODOS[1]!;
  const verifica =
    modo === SSLMode.SSLVerifyCA ||
    modo === SSLMode.SSLVerifyFull ||
    (modo === SSLMode.SSLRequire && tls.rootCertPath !== "");

  function set(campo: keyof TLS, valor: string) {
    setGuardado(null);
    onTLS({ ...tls, [campo]: valor });
  }

  function elegirModo(m: SSLMode) {
    setGuardado(null);
    onMode(m);
  }

  async function elegir(campo: keyof TLS, tipo: TipoDeCertificado) {
    setErrorSelector(null);
    let ruta = "";
    try {
      ruta = await elegirCertificado(tipo);
    } catch (err) {
      setErrorSelector({ campo, texto: textoDe(err) });
      return;
    }
    if (ruta) set(campo, ruta);
  }

  /** Guarda el certificado que presentó el servidor y lo carga como raíz.
   *
   *  Es el camino para un servidor propio, que casi siempre es autofirmado:
   *  se prueba con require, se compara la huella con la que pasó quien lo
   *  administra, y se guarda. El modo pasa a verify-ca si no verificaba,
   *  porque guardarlo no tiene otro propósito. */
  async function guardarComoRaiz(pem: string) {
    setErrorGuardar(null);
    setGuardado(null);
    let ruta = "";
    try {
      ruta = await elegirDestinoCertificado(`${host || "servidor"}.crt`);
    } catch (err) {
      setErrorGuardar(textoDe(err));
      return;
    }
    if (!ruta) return;
    try {
      await Connections.SaveCertificate(pem, ruta);
    } catch (err) {
      setErrorGuardar(textoDe(err));
      return;
    }
    onTLS({ ...tls, rootCertPath: ruta });
    if (!verifica) onMode(SSLMode.SSLVerifyCA);
    setGuardado(ruta);
  }

  const canal = test?.ok ? (test.server?.tls ?? null) : null;

  return (
    <div className={styles.pane}>
      <div className={editor.fields}>
        <Field label="Modo" error={problems.get("sslMode")} align="start">
          <div className={editor.envColumn}>
            <div className={editor.chips}>
              {modos.map((m) => (
                <button
                  key={m.value}
                  type="button"
                  className={cx(editor.chip, editor.chipMono, modo === m.value && editor.chipOn)}
                  onClick={() => elegirModo(m.value)}
                >
                  {m.value}
                </button>
              ))}
            </div>
            <p className={editor.envHint}>{actual.hint}</p>
          </div>
        </Field>

        <Archivo
          label="Raíz"
          campo="rootCertPath"
          valor={tls.rootCertPath}
          placeholder="las raíces del sistema"
          error={errorSelector?.campo === "rootCertPath" ? errorSelector.texto : problems.get("tls.rootCertPath")}
          hint={
            <>
              El certificado con el que se verifica al servidor: el de la CA interna, el
              paquete que entrega el proveedor, o el del propio servidor si es autofirmado.
              <br />
              <br />
              Vacío usa las raíces del sistema, que alcanzan para un certificado comprado y
              no para uno interno.
              <br />
              <br />
              Es una ruta, no el archivo: <code>~/.postgresql/root.crt</code> vale, y el{" "}
              <code>~</code> se resuelve al conectar en cada máquina.
            </>
          }
          onChange={(v) => set("rootCertPath", v)}
          onElegir={() => void elegir("rootCertPath", "raiz")}
        />
        <Archivo
          label="Cert. cliente"
          campo="clientCertPath"
          valor={tls.clientCertPath}
          placeholder="opcional"
          error={errorSelector?.campo === "clientCertPath" ? errorSelector.texto : problems.get("tls.clientCertPath")}
          hint={
            <>
              Solo si el servidor exige que el cliente se identifique con un certificado.
              Va junto con su clave; sin ella no demuestra nada.
            </>
          }
          onChange={(v) => set("clientCertPath", v)}
          onElegir={() => void elegir("clientCertPath", "cliente")}
        />
        <Archivo
          label="Clave cliente"
          campo="clientKeyPath"
          valor={tls.clientKeyPath}
          placeholder="opcional"
          error={errorSelector?.campo === "clientKeyPath" ? errorSelector.texto : problems.get("tls.clientKeyPath")}
          hint={
            <>
              La clave privada del certificado de cliente, <strong>sin cifrar</strong>: es
              como la leen los drivers. Se guarda la ruta, no la clave; el archivo se queda
              donde está.
            </>
          }
          onChange={(v) => set("clientKeyPath", v)}
          onElegir={() => void elegir("clientKeyPath", "clave")}
        />
      </div>

      {avisos.length > 0 ? (
        <ul className={styles.avisos}>
          {avisos.map((w) => (
            <li key={w.field}>{w.message}</li>
          ))}
        </ul>
      ) : null}

      <section className={styles.certificado} aria-label="Certificado del servidor">
        <div className={editor.cardLabel}>Certificado del servidor</div>
        {canal ? (
          <>
            <dl className={styles.datos}>
              <dt>Sujeto</dt>
              <dd>{canal.subject || "—"}</dd>
              <dt>Emisor</dt>
              <dd>
                {canal.issuer || "—"}
                {canal.selfSigned ? (
                  <span className={styles.badge}>
                    <Badge tone="warning">autofirmado</Badge>
                  </span>
                ) : null}
              </dd>
              {canal.validFor && canal.validFor.length > 0 ? (
                <>
                  <dt>Vale para</dt>
                  <dd>{canal.validFor.join(", ")}</dd>
                </>
              ) : null}
              <dt>Vigente</dt>
              <dd>
                {canal.validFrom} → {canal.validUntil}
                {canal.expired ? (
                  <span className={styles.badge}>
                    <Badge tone="danger">fuera de fecha</Badge>
                  </span>
                ) : null}
              </dd>
              <dt>SHA-256</dt>
              <dd className={styles.huella}>{canal.sha256}</dd>
              <dt>Canal</dt>
              <dd>
                {canal.version} · {canal.cipher}
              </dd>
            </dl>
            <p className={styles.nota}>
              {verifica
                ? "La conexión verifica este certificado."
                : "La conexión NO verifica este certificado: cualquiera en el medio podría presentar otro. Compará la huella con la que te pasó quien administra el servidor."}
            </p>
            {!verifica ? (
              <div className={styles.acciones}>
                <Button size="sm" onClick={() => void guardarComoRaiz(canal.pem)}>
                  Guardar como raíz de confianza…
                </Button>
              </div>
            ) : null}
          </>
        ) : guardado ? (
          /* Guardar la raíz cambia la conexión, y eso limpia la prueba —lo
             que se probó ya no es lo que se va a conectar—. La confirmación
             va acá, en el lugar de la tarjeta que acaba de vaciarse, y pide
             la prueba de nuevo: es la que va a decir «verifica». */
          <p className={styles.nota}>
            Guardado en <code>{guardado}</code> y cargado como raíz: desde ahora la conexión
            verifica contra él. Probá de nuevo para verlo.
          </p>
        ) : test?.ok ? (
          <p className={styles.nota}>
            La prueba conectó <strong>sin cifrar</strong>
            {modo === SSLMode.SSLDisable
              ? ", como pide el modo disable."
              : ": el servidor no ofreció TLS y el modo lo permite."}
          </p>
        ) : test ? (
          <p className={styles.nota}>
            La última prueba no conectó. El certificado se muestra cuando la conexión se
            establece; si lo que falló es la verificación, el detalle de abajo dice por qué.
          </p>
        ) : (
          <p className={styles.nota}>
            Probá la conexión para ver el certificado que presenta el servidor: quién dice ser,
            quién lo firmó, hasta cuándo vale y su huella.
          </p>
        )}
        {errorGuardar ? (
          <p className={cx(styles.nota, styles.notaError)} role="alert">
            {errorGuardar}
          </p>
        ) : null}
      </section>
    </div>
  );
}

function Archivo({
  label,
  campo,
  valor,
  placeholder,
  error,
  hint,
  onChange,
  onElegir,
}: {
  label: string;
  campo: keyof TLS;
  valor: string;
  placeholder: string;
  error: string | undefined;
  hint: React.ReactNode;
  onChange: (v: string) => void;
  onElegir: () => void;
}) {
  return (
    <Field label={label} error={error} hint={hint}>
      <div className={editor.fileRow}>
        <Input
          value={valor}
          placeholder={placeholder}
          invalid={error !== undefined}
          aria-label={label}
          data-campo={campo}
          onChange={(e) => onChange(e.currentTarget.value)}
        />
        <Button variant="ghost" onClick={onElegir}>
          Elegir…
        </Button>
      </div>
    </Field>
  );
}
