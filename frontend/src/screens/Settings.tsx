import { useEffect, useRef, useState } from "react";
import { Browser } from "@wailsio/runtime";
import { Theme } from "../../bindings/github.com/LucianoR23/kanamedb/internal/config";
import type { Config } from "../../bindings/github.com/LucianoR23/kanamedb/internal/config";
import * as SettingsSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/settings";
import type { HistoryStats, SettingsView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Result as UpdateResult } from "../../bindings/github.com/LucianoR23/kanamedb/internal/update";
import { Button, ConfirmDialog, Field, RadioGroup, Spinner, Toggle } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { aplicar, temaEfectivo } from "../lib/preferencias";
import { SafetyTab } from "./SafetyTab";
import styles from "./Settings.module.css";

/**
 * S23 Settings — las preferencias de la aplicación.
 *
 * Hasta ahora `config.toml` era una ruta que la pantalla About mostraba y que
 * nadie escribía nunca: un archivo prometido y vacío.
 *
 * # Dos decisiones que explican la pantalla
 *
 * **Acá solo hay cosas que hacen algo.** Una pantalla de ajustes llena de
 * interruptores a medio conectar es peor que no tenerla, porque enseña a
 * desconfiar de los que sí funcionan. Lo que todavía no existe no aparece.
 *
 * **Se guarda solo.** No hay botón de «Guardar» ni de «Cancelar»: cada control
 * es una decisión discreta y el archivo se escribe de forma atómica, así que no
 * hay un estado intermedio que confirmar. Un botón de guardar acá solo agrega
 * una forma de perder lo que uno ya creía cambiado.
 */
export function Settings({ onBack }: { onBack: () => void }) {
  const [vista, setVista] = useState<SettingsView | null>(null);
  const [error, setError] = useState("");
  const [historial, setHistorial] = useState<HistoryStats | null>(null);
  const [olvidando, setOlvidando] = useState(false);

  useEffect(() => {
    let vigente = true;
    SettingsSvc.Get()
      .then((v) => {
        if (!vigente) return;
        setVista(v);
        aplicar(v.config);
      })
      .catch((err: unknown) => vigente && setError(textoDe(err)));
    SettingsSvc.HistorySize()
      .then((h) => vigente && setHistorial(h))
      .catch(() => {
        // Cuánto historial hay es un dato de adorno para el botón de borrar.
        // No poder contarlo no es motivo para no mostrar los ajustes.
      });
    return () => {
      vigente = false;
    };
  }, []);

  // Las escrituras se agrupan. Casi todos los controles son discretos —un clic,
  // una decisión— pero el número de un límite se teclea dígito a dígito, y sin
  // esto cada tecla sería un archivo escrito y renombrado.
  const pendiente = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Lo que quedó por escribir, si hay algo. Ver el cierre de abajo.
  const porGuardar = useRef<Config | null>(null);

  // Al salir de la pantalla se GUARDA lo que quedaba, no se cancela.
  //
  // Cancelar es lo que hacía antes, y perdía cambios en silencio: elegir un tema
  // y apretar «Volver» en el mismo medio segundo dejaba la pantalla ya pintada
  // con el tema nuevo y el archivo con el viejo, así que al abrir la aplicación
  // de nuevo volvía el anterior sin explicación. Salió probando: quedó
  // `theme = "system"` después de haber elegido «Claro».
  useEffect(
    () => () => {
      if (pendiente.current) clearTimeout(pendiente.current);
      const ultimaPedida = porGuardar.current;
      porGuardar.current = null;
      // Sin `then`: la pantalla ya no existe y no hay dónde mostrar nada. Lo
      // que importa es que la escritura salga.
      if (ultimaPedida) void SettingsSvc.Save(ultimaPedida).catch(() => {});
    },
    [],
  );

  /**
   * La última configuración que se pidió, aunque React todavía no haya vuelto a
   * dibujar.
   *
   * Sin esto, dos cambios en el mismo lote parten los dos del `c` de la
   * renderización anterior y el segundo pisa al primero. Salió probando: apagar
   * dos interruptores en la misma tanda dejaba solo el segundo apagado. Un
   * humano rara vez llega a hacerlo, pero la forma correcta —partir de lo
   * último, no de lo dibujado— cuesta una ref.
   */
  const ultima = useRef<Config | null>(null);

  function cambiar(f: (c: Config) => Config) {
    const base = ultima.current ?? vista?.config;
    if (!base) return;
    const next = f(base);
    ultima.current = next;
    porGuardar.current = next;

    // La pantalla se actualiza YA y el disco después: un control que espera a
    // la respuesta del disco para moverse se siente roto.
    setVista((prev) => (prev ? { ...prev, config: next } : prev));
    aplicar(next);

    if (pendiente.current) clearTimeout(pendiente.current);
    pendiente.current = setTimeout(() => {
      porGuardar.current = null;
      SettingsSvc.Save(next)
        .then((v) => {
          // Se adopta lo que quedó GUARDADO, no lo que se pidió: Go normaliza,
          // y un valor fuera de rango tiene que verse corregido en el acto en
          // vez de a la próxima apertura.
          ultima.current = v.config;
          setVista(v);
          aplicar(v.config);
          setError("");
        })
        .catch((err: unknown) => setError(textoDe(err)));
    }, 350);
  }

  if (error && !vista) {
    return (
      <div className={styles.page}>
        <Cabecera onBack={onBack} />
        <p className={styles.error} role="alert">{error}</p>
      </div>
    );
  }
  if (!vista) {
    return (
      <div className={styles.page}>
        <Cabecera onBack={onBack} />
        <p className={styles.nota}><Spinner /> Leyendo las preferencias…</p>
      </div>
    );
  }

  const c = vista.config;

  return (
    <div className={styles.page}>
      <Cabecera onBack={onBack} />

      <div className={styles.inner}>
        <div className={styles.columna}>
        {vista.problem ? (
          // El caso silencioso es el peligroso: unos defaults mostrados como si
          // fueran los guardados, y el primer cambio pisando el archivo que
          // estaba roto. Se dice antes de que eso pase.
          <p className={styles.aviso} role="alert">
            No se pudo leer el archivo de preferencias, así que lo que ves abajo son los
            valores de fábrica. Cambiar cualquier cosa lo va a reemplazar.
            <span className={styles.detalle}>{vista.problem}</span>
          </p>
        ) : null}
        {error ? <p className={styles.error} role="alert">{error}</p> : null}

        <section className={styles.grupo}>
          <h2 className={styles.titulo}>Apariencia</h2>

          <Field label="Tema">
            <div className={styles.control}>
              <RadioGroup
                label="Tema de la interfaz"
                value={c.theme || Theme.ThemeDark}
                onChange={(theme) => cambiar((c) => ({ ...c, theme }))}
                options={[
                  { value: Theme.ThemeDark, label: "Oscuro" },
                  { value: Theme.ThemeLight, label: "Claro" },
                  { value: Theme.ThemeSystem, label: "El del sistema" },
                ]}
              />
              {c.theme === Theme.ThemeSystem ? (
                <p className={styles.ayuda}>
                  Ahora mismo el sistema está en {temaEfectivo()}. Si lo cambiás, Kaname
                  cambia con él sin reiniciar.
                </p>
              ) : null}
            </div>
          </Field>
        </section>

        <section className={styles.grupo}>
          <h2 className={styles.titulo}>Editor SQL</h2>

          <Field label="Tamaño">
            <div className={styles.control}>
              <RadioGroup
                label="Tamaño del texto del editor"
                value={String(c.editor.fontSize)}
                onChange={(v) =>
                  cambiar((c) => ({ ...c, editor: { ...c.editor, fontSize: Number(v) } }))
                }
                options={tamanios(c.editor.fontSize)}
              />
              <p className={styles.ayuda}>
                Cambia el cuerpo del editor y el de la regleta de números. La grilla de
                resultados no se toca: ahí el alto de fila es lo que decide cuántos datos
                entran en pantalla.
              </p>
            </div>
          </Field>

          <Interruptor
            titulo="Números de línea"
            puesto={!c.editor.hideLineNumbers}
            onChange={(v) => cambiar((c) => ({ ...c, editor: { ...c.editor, hideLineNumbers: !v } }))}
            ayuda="Los mensajes de error del motor dicen en qué línea falló la sentencia, así que apagarlos cuesta algo."
          />

          <Interruptor
            titulo="Ajuste de línea"
            puesto={!c.editor.noWrap}
            onChange={(v) => cambiar((c) => ({ ...c, editor: { ...c.editor, noWrap: !v } }))}
            ayuda="Con el ajuste apagado, una línea larga se va a la derecha en vez de partirse."
          />
        </section>

        <section className={styles.grupo}>
          <h2 className={styles.titulo}>Conexiones nuevas</h2>
          <p className={styles.ayuda}>
            Con qué protecciones NACE una conexión que creás desde acá en adelante. No
            toca ninguna de las que ya existen: cada una tiene las suyas en su pestaña
            Safety, y cambiarlas a todas de una sería cambiar algo que nadie miró.
          </p>
          {/* Es literalmente la misma pantalla que la pestaña de S03, con el
              mismo tipo de Go detrás. Dos formularios para los mismos seis
              campos se separan, y un default que se separa del valor real es un
              default que empieza a mentir. */}
          <SafetyTab
            safety={c.newConnection}
            onChange={(newConnection) => cambiar((c) => ({ ...c, newConnection }))}
          />
        </section>

        <section className={styles.grupo}>
          <h2 className={styles.titulo}>Esta máquina</h2>

          <Field label="Historial">
            <div className={styles.control}>
              <p className={styles.ayuda}>
                {historial
                  ? `${historial.entries} ${historial.entries === 1 ? "consulta" : "consultas"} de todas las conexiones, guardadas en este equipo.`
                  : "Las consultas que corriste, guardadas en este equipo."}
              </p>
              <div>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={historial?.entries === 0}
                  onClick={() => setOlvidando(true)}
                >
                  Olvidar todo el historial
                </Button>
              </div>
            </div>
          </Field>

          {/* No es una preferencia: es qué NO se puede cambiar. Va acá porque es
              la pregunta que alguien trae a una pantalla de ajustes cuando la
              app maneja credenciales de producción, y la respuesta honesta es
              «esto no tiene interruptor». */}
          <Field label="Sin interruptor">
            <ul className={styles.duras}>
              <li>Las contraseñas viven en el keychain del sistema. Nunca en un archivo.</li>
              <li>
                Una conexión marcada como producción siempre pide que escribas el nombre
                de la base antes de escribir, diga lo que diga su pestaña Safety.
              </li>
              <li>Ningún valor de fila ni ninguna cadena de conexión va a un log ni al historial.</li>
              <li>
                No hay telemetría, ni cuenta, ni chequeo automático de versiones. El único
                tráfico de red es hacia las bases que configures y el botón de abajo.
              </li>
            </ul>
          </Field>

          <Field label="Archivo">
            <p className={styles.ruta}>{vista.path}</p>
          </Field>
        </section>

        <Versiones
          vista={vista}
          onConsultado={(v) => {
            ultima.current = v.config;
            setVista(v);
          }}
        />
        </div>
      </div>

      <ConfirmDialog
        open={olvidando}
        severidad="aviso"
        title="Olvidar el historial de esta máquina"
        etiqueta="Olvidar"
        onClose={() => setOlvidando(false)}
        onConfirm={() => {
          setOlvidando(false);
          SettingsSvc.ForgetHistory()
            .then(() => setHistorial((h) => (h ? { ...h, entries: 0 } : h)))
            .catch((err: unknown) => setError(textoDe(err)));
        }}
      >
        Se borra el historial de TODAS las conexiones, no solo el de la que tengas
        abierta, y no hay de dónde recuperarlo. Las consultas que guardaste con nombre no
        se tocan: están en el otro archivo, el que viaja con la libreta de conexiones.
      </ConfirmDialog>
    </div>
  );
}

/** Los cuatro tamaños con nombre, más el que haya puesto si no es ninguno.
 *
 * Go acepta de 10 a 24 px y el archivo se edita a mano —está escrito en el
 * encabezado que se puede—, así que un `font_size = 16` es legítimo. Sin esta
 * opción de más, los cuatro radios quedaban todos en `false`: la pantalla no
 * mostraba ningún valor elegido y el primer clic cambiaba en silencio un ajuste
 * que la persona había puesto a propósito. */
function tamanios(actual: number): { value: string; label: string }[] {
  const base = [
    { value: "12", label: "Chico" },
    { value: "13", label: "Normal" },
    { value: "15", label: "Grande" },
    { value: "17", label: "Más grande" },
  ];
  if (base.some((o) => o.value === String(actual))) return base;
  return [...base, { value: String(actual), label: `${actual} px` }];
}

function Cabecera({ onBack }: { onBack: () => void }) {
  return (
    <div className={styles.topbar}>
      <span className={styles.marca}>AJUSTES</span>
      <span className={styles.spacer} />
      <Button size="sm" onClick={onBack}>
        Volver
      </Button>
    </div>
  );
}

/** Un interruptor con su título visible y su explicación. */
function Interruptor({
  titulo,
  puesto,
  onChange,
  ayuda,
}: {
  titulo: string;
  puesto: boolean;
  onChange: (v: boolean) => void;
  ayuda: string;
}) {
  return (
    <div className={styles.fila}>
      {/* El Toggle del design system solo lleva `aria-label`: no dibuja texto.
          El rótulo visible va al lado, adentro del mismo <label>, así que
          clickear la palabra también conmuta. */}
      <label className={styles.rotulo}>
        <Toggle checked={puesto} onChange={onChange} label={titulo} />
        <span>{titulo}</span>
      </label>
      <p className={styles.ayuda}>{ayuda}</p>
    </div>
  );
}

/**
 * El chequeo manual de versiones.
 *
 * Es lo único de la aplicación que sale a internet por su cuenta, así que la
 * pantalla dice exactamente qué hace ANTES de que alguien lo apriete. CLAUDE.md
 * prohíbe el phone-home; lo que está permitido es un botón, y un botón que
 * explica lo que va a hacer no es lo mismo que un chequeo silencioso.
 */
function Versiones({
  vista,
  onConsultado,
}: {
  vista: SettingsView;
  /** Las preferencias que quedaron en el archivo después de la consulta.
   *
   *  Consultar ESCRIBE —queda anotado cuándo fue— así que la copia que tiene la
   *  pantalla queda vieja al instante. Sin adoptarla, el próximo cambio de
   *  cualquier ajuste guardaba la copia vieja y borraba esa constancia. */
  onConsultado: (v: SettingsView) => void;
}) {
  const [consultando, setConsultando] = useState(false);
  const [res, setRes] = useState<UpdateResult | null>(null);
  const [problema, setProblema] = useState("");

  return (
    <section className={styles.grupo}>
      <h2 className={styles.titulo}>Versión</h2>

      <Field label="Instalada">
        <p className={styles.ruta}>
          {vista.version}
          {vista.localBuild ? " · compilada en esta máquina" : ""}
        </p>
      </Field>

      <Field label="Buscar">
        <div className={styles.control}>
          <p className={styles.ayuda}>
            Consulta la última versión publicada en GitHub. Es la única salida a internet que
            tiene Kaname, y solo pasa cuando apretás el botón: un pedido HTTPS a{" "}
            <code>api.github.com</code>, sin identificadores ni la versión que tenés puesta
            —la comparación se hace acá—. GitHub ve tu dirección IP, como cualquier sitio que
            visitás. No descarga ni instala nada.
          </p>
          <div>
            <Button
              size="sm"
              disabled={consultando}
              onClick={() => {
                setConsultando(true);
                setProblema("");
                SettingsSvc.CheckForUpdates()
                  .then((v) => {
                    setRes(v.result);
                    onConsultado(v.settings);
                  })
                  // Go nunca devuelve error acá —un problema de red es parte del
                  // resultado— pero el puente sí puede rechazar, y sin esto el
                  // botón se destraba y no pasa nada visible.
                  .catch((err: unknown) => setProblema(textoDe(err)))
                  .finally(() => setConsultando(false));
              }}
            >
              {consultando ? "Consultando…" : "Buscar actualizaciones"}
            </Button>
          </div>

          {problema ? <p className={styles.error} role="alert">{problema}</p> : null}
          {res ? <Respuesta res={res} local={vista.localBuild} /> : null}

          {!res && vista.config.updates.lastCheck ? (
            <p className={styles.ayuda}>
              Última consulta: {fecha(vista.config.updates.lastCheck)}
              {vista.config.updates.lastSeen ? ` · última publicada: ${vista.config.updates.lastSeen}` : ""}
            </p>
          ) : null}
        </div>
      </Field>
    </section>
  );
}

function Respuesta({ res, local }: { res: UpdateResult; local: boolean }) {
  if (res.problem) return <p className={styles.aviso}>{res.problem}</p>;

  if (!res.comparable) {
    // «No pude comparar» y «estás al día» NO se dicen igual. Confundirlos es
    // cómo una versión nueva pasa desapercibida para siempre.
    return (
      <p className={styles.aviso}>
        La última publicada es {res.latest}, y no se pudo comparar con {res.current}.
        {res.url ? <> Mirala en <Enlace url={res.url} />.</> : null}
      </p>
    );
  }
  if (res.newer) {
    return (
      <p className={styles.novedad}>
        Hay una versión más nueva: {res.latest}.{" "}
        {res.url ? <Enlace url={res.url} /> : null}
      </p>
    );
  }
  return (
    <p className={styles.ayuda}>
      {local
        ? `Estás en una build local. La última publicada es ${res.latest}.`
        : `Tenés la última: ${res.latest}.`}
    </p>
  );
}

/**
 * Un enlace que abre en el navegador del sistema.
 *
 * NO es un `<a href>`, por lo mismo que los de la firma: adentro de un webview
 * eso navega la ventana en el lugar —te reemplaza la aplicación por una página
 * web, sin forma de volver— o abre una ventana pelada, según cómo esté
 * configurado WebView2. `Browser.OpenURL` se lo entrega al sistema operativo,
 * que es lo único aceptable: esta aplicación no carga contenido remoto en su
 * propio proceso.
 */
function Enlace({ url }: { url: string }) {
  return (
    <button type="button" className={styles.enlace} onClick={() => void Browser.OpenURL(url)}>
      {url}
    </button>
  );
}

/** La fecha de la última consulta, en palabras del sistema. */
function fecha(iso: string): string {
  const t = new Date(iso);
  return Number.isNaN(t.getTime()) ? iso : t.toLocaleString();
}
