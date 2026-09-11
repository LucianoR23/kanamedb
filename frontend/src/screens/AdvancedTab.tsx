import type { Advanced, Warning } from "../../bindings/github.com/LucianoR23/kanamedb/internal/connection";
import { Kind as Engine } from "../../bindings/github.com/LucianoR23/kanamedb/internal/engine";
import { Input, Textarea } from "../components/ui";
import { cx } from "../lib/cx";
import { Field } from "./ConnectionField";
import editor from "./ConnectionEditor.module.css";
import styles from "./AdvancedTab.module.css";

interface Problemas {
  get: (campo: string) => string | undefined;
  has: (campo: string) => boolean;
}

interface Props {
  engine: Engine;
  advanced: Advanced;
  readOnly: boolean;
  problems: Problemas;
  /** Los avisos de la conexión que hablan de esta pestaña. */
  avisos: Warning[];
  onChange: (a: Advanced) => void;
}

/** Ejemplos de SQL de sesión por motor: lo que la gente escribe ahí. */
const EJEMPLO: Record<string, string> = {
  [Engine.Postgres]: "SET lock_timeout = '3s';\nSET TIME ZONE 'America/Argentina/Buenos_Aires';",
  [Engine.MySQL]: "SET NAMES utf8mb4;\nSET SESSION sql_mode = 'STRICT_ALL_TABLES';",
  [Engine.MariaDB]: "SET NAMES utf8mb4;\nSET SESSION sql_mode = 'STRICT_ALL_TABLES';",
  [Engine.SQLite]: "PRAGMA cache_size = -20000;\nPRAGMA temp_store = MEMORY;",
};

/**
 * S03, pestaña Advanced: lo que casi nadie toca y alguien necesita.
 *
 * El artboard tenía además «Default schema». No está: en Postgres es el
 * primer nombre del search_path, en MySQL es la base de la pestaña General y
 * en SQLite es `main`. Un campo aparte sería una segunda forma de decir lo
 * mismo, y dos formas de decir lo mismo se separan.
 */
export function AdvancedTab({ engine, advanced, readOnly, problems, avisos, onChange }: Props) {
  const set = <K extends keyof Advanced>(key: K, value: Advanced[K]) =>
    onChange({ ...advanced, [key]: value });

  const esArchivo = engine === Engine.SQLite;
  const esPostgres = engine === Engine.Postgres;
  const poolPorDefecto = readOnly ? 2 : 4;

  return (
    <div className={styles.pane}>
      <div className={editor.fields}>
        {esPostgres ? (
          <Field
            label="Search path"
            error={problems.get("advanced.searchPath")}
            hint={
              <>
                Como en un <code>SET search_path</code>: <code>shop, public</code>. Viaja en
                el arranque de cada conexión del pool, así que vale para todas; un{" "}
                <code>SET</code> en el editor vale para una sola.
                <br />
                <br />
                Vacío usa el del servidor, que casi siempre es <code>"$user", public</code>.
              </>
            }
          >
            <Input
              value={advanced.searchPath}
              placeholder="el del servidor"
              invalid={problems.has("advanced.searchPath")}
              className={styles.mono}
              onChange={(e) => set("searchPath", e.currentTarget.value)}
            />
          </Field>
        ) : null}

        {!esArchivo ? (
          <Field
            label="Aplicación"
            error={problems.get("advanced.applicationName")}
            hint={
              <>
                Cómo se ve Kaname desde el servidor: <code>application_name</code> en{" "}
                <code>pg_stat_activity</code>, <code>program_name</code> en los atributos de
                sesión de MySQL. Sirve para que quien administra distinga dos personas con el
                mismo usuario. Vacío es <code>kaname</code>.
              </>
            }
          >
            <Input
              value={advanced.applicationName}
              placeholder="kaname"
              invalid={problems.has("advanced.applicationName")}
              className={styles.mono}
              onChange={(e) => set("applicationName", e.currentTarget.value)}
            />
          </Field>
        ) : null}

        <Field
          label="Conexiones"
          error={problems.get("advanced.poolSize")}
          hint={
            <>
              Cuántas abre el pool. Vacío es {poolPorDefecto}
              {readOnly ? " —dos en solo lectura—" : ""}. Dos como mínimo: cancelar una
              consulta necesita otra conexión. Más de cuatro solo si varias pestañas consultan
              a la vez; quien administra el servidor las ve todas.
            </>
          }
        >
          <Input
            value={advanced.poolSize === 0 ? "" : String(advanced.poolSize)}
            placeholder={String(poolPorDefecto)}
            inputMode="numeric"
            invalid={problems.has("advanced.poolSize")}
            className={cx(styles.mono, styles.corto)}
            onChange={(e) => set("poolSize", Number(e.currentTarget.value.replace(/\D/g, "")) || 0)}
          />
        </Field>

        <Field
          label="SQL de sesión"
          error={problems.get("advanced.sessionSql")}
          align="start"
          hint={
            <>
              Corre en <strong>cada</strong> conexión que se abre, antes que nada, y sin vista
              previa ni confirmación. Es para configurar la sesión: <code>SET</code>,{" "}
              <code>PRAGMA</code>. Las protecciones de Safety se aplican después, así que no se
              pueden apagar desde acá.
              <br />
              <br />
              Si una sentencia falla, la conexión no se abre y el error dice en qué línea.
            </>
          }
        >
          <div className={styles.sesion}>
            <Textarea
              value={advanced.sessionSql}
              placeholder={EJEMPLO[engine] ?? ""}
              rows={5}
              spellCheck={false}
              invalid={problems.has("advanced.sessionSql")}
              className={styles.sql}
              aria-label="SQL de sesión"
              onChange={(e) => set("sessionSql", e.currentTarget.value)}
            />
            <p className={editor.hint}>Una sentencia por línea, con punto y coma.</p>
          </div>
        </Field>
      </div>

      {avisos.length > 0 ? (
        <ul className={styles.avisos}>
          {avisos.map((w) => (
            <li key={w.field}>{w.message}</li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}
