import type { Safety } from "../../bindings/github.com/LucianoR23/kanamedb/internal/connection";
import { Field, Input, RadioGroup, Toggle } from "../components/ui";
import styles from "./SafetyTab.module.css";

/**
 * S03, pestaña Safety: las protecciones de una conexión.
 *
 * Quedó huérfana desde la Iteración 1 —la 1 la difirió a la 5, la 5 no la hizo,
 * y la lista de la 9 nombraba solo TLS y Advanced, así que el cartel «Llega en
 * la Iteración 5» iba a quedar ahí para siempre—. Hasta ahora estos valores solo
 * se editaban a mano en `connections.toml`.
 *
 * # El cero no es «sin límite»
 *
 * Los tres números tienen la misma convención en el archivo y NO es la que
 * parece: cero significa «usá el default», y para sacar el límite hay que pedir
 * −1. La razón está en el modelo: un archivo al que le falta la clave tiene que
 * quedar PROTEGIDO y no desprotegido, y el valor ausente de un entero es cero.
 *
 * Eso convierte el campo en una trampa: un número 0 en pantalla se lee como
 * «ninguno» y significa «treinta segundos». Por eso acá no se edita el número
 * crudo — se elige entre tres cosas que dicen lo que son, y el número solo
 * aparece cuando se eligió poner uno.
 */
export function SafetyTab({
  safety,
  onChange,
}: {
  safety: Safety;
  onChange: (s: Safety) => void;
}) {
  const set = <K extends keyof Safety>(key: K, value: Safety[K]) =>
    onChange({ ...safety, [key]: value });

  return (
    <div className={styles.pane}>
      <section className={styles.grupo}>
        <h3 className={styles.titulo}>Qué se puede hacer</h3>

        <div className={styles.fila}>
          <label className={styles.rotulo}>
            <Toggle
              checked={safety.readOnly}
              onChange={(v) => set("readOnly", v)}
              label="Solo lectura"
            />
            <span>Solo lectura</span>
          </label>
          <p className={styles.ayuda}>Bloquea toda escritura desde Kaname, tenga los permisos que tenga el usuario en el motor. Es una decisión de esta conexión, no del servidor.</p>
        </div>

        <div className={styles.fila}>
          <label className={styles.rotulo}>
            <Toggle
              checked={safety.blockDropTruncate}
              onChange={(v) => set("blockDropTruncate", v)}
              label="No ejecutar DROP ni TRUNCATE"
            />
            <span>No ejecutar DROP ni TRUNCATE</span>
          </label>
          <p className={styles.ayuda}>Las sentencias se siguen generando y se ven en la vista previa; lo que no se hace es correrlas. Sirve para revisar un cambio destructivo sin poder aplicarlo por accidente.</p>
        </div>
      </section>

      <section className={styles.grupo}>
        <h3 className={styles.titulo}>Qué se saltea</h3>
        {/* Estas dos APAGAN protecciones, así que se agrupan aparte y se
            describen por lo que se pierde. Puestas entre las otras con el mismo
            aspecto, un tilde de más se lee como «más seguro». */}

        <div className={styles.fila}>
          <label className={styles.rotulo}>
            <Toggle
              checked={safety.allowApplyWithoutPreview}
              onChange={(v) => set("allowApplyWithoutPreview", v)}
              label="Aplicar sin abrir la vista previa"
            />
            <span>Aplicar sin abrir la vista previa</span>
          </label>
          <p className={styles.ayuda}>La vista previa es la única garantía de que lo que corre es lo que se quiso: toda la SQL de este proyecto pasa por ahí antes de ejecutarse. Apagarla es aceptar que nadie la lea.</p>
        </div>

        <div className={styles.fila}>
          <label className={styles.rotulo}>
            <Toggle
              checked={safety.allowWriteWithoutConfirmation}
              onChange={(v) => set("allowWriteWithoutConfirmation", v)}
              label="Escribir sin confirmar el nombre de la base"
            />
            <span>Escribir sin confirmar el nombre de la base</span>
          </label>
          <p className={styles.ayuda}>En una conexión marcada como producción esto se IGNORA: ahí el nombre se escribe siempre. Vale para las demás.</p>
        </div>
      </section>

      <section className={styles.grupo}>
        <h3 className={styles.titulo}>Límites</h3>

        <Limite
          etiqueta="Tiempo por sentencia"
          unidad="segundos"
          porDefecto={30}
          valor={safety.statementTimeoutSeconds}
          onChange={(v) => set("statementTimeoutSeconds", v)}
          ayuda="Lo corta el SERVIDOR, no Kaname. Cancelar desde el cliente depende de que el cliente siga vivo; esto no."
        />

        <Limite
          etiqueta="Filas por consulta"
          unidad="filas"
          porDefecto={1000}
          valor={safety.rowLimit}
          onChange={(v) => set("rowLimit", v)}
          ayuda="Cuántas trae antes de «cargar más». Sin límite, un SELECT sobre una tabla de dos millones las trae todas a la memoria de la aplicación."
        />

        <Limite
          etiqueta="Desconectar si no se usa"
          unidad="minutos"
          porDefecto={15}
          valor={safety.idleDisconnectMinutes}
          onChange={(v) => set("idleDisconnectMinutes", v)}
          ayuda="Una sesión abierta contra producción toda la noche es una que alguien puede usar sin darse cuenta."
        />
      </section>
    </div>
  );
}

/** Los tres estados que puede tener un límite. Ver `Limite`. */
type Modo = "default" | "fijo" | "sin";

/**
 * Un límite con las TRES opciones que existen, en vez del número crudo.
 *
 * Es lo que vuelve legible la convención del archivo: cero es el default y −1
 * es sin límite. Escribir «0» en una casilla de segundos y que eso signifique
 * treinta es la clase de detalle que nadie adivina y que se descubre cuando una
 * consulta se corta sola.
 */
function Limite({
  etiqueta,
  unidad,
  porDefecto,
  valor,
  onChange,
  ayuda,
}: {
  etiqueta: string;
  unidad: string;
  porDefecto: number;
  valor: number;
  onChange: (v: number) => void;
  ayuda: string;
}) {
  const modo: Modo = valor === 0 ? "default" : valor < 0 ? "sin" : "fijo";

  return (
    <Field label={etiqueta}>
      <div className={styles.limite}>
        <p className={styles.ayuda}>{ayuda}</p>
        <RadioGroup
          label={etiqueta}
          value={modo}
          onChange={(id) => onChange(id === "default" ? 0 : id === "sin" ? -1 : porDefecto)}
          options={[
            { value: "default", label: `Por defecto (${porDefecto} ${unidad})` },
            { value: "fijo", label: "Un valor" },
            { value: "sin", label: "Sin límite" },
          ]}
        />
        {modo === "fijo" ? (
          <Input
            type="number"
            min={1}
            value={String(valor)}
            aria-label={`${etiqueta}, en ${unidad}`}
            onChange={(e) => {
              const n = Number.parseInt(e.currentTarget.value, 10);
              // Un campo vacío o basura NO se guarda como cero: cero significa
              // «por defecto» y el usuario estaría eligiendo otra cosa sin
              // saberlo. Se deja lo que había hasta que escriba un número.
              if (Number.isFinite(n) && n > 0) onChange(n);
            }}
          />
        ) : null}
      </div>
    </Field>
  );
}
