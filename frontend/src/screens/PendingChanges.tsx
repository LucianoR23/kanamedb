import { useCallback, useEffect, useState } from "react";
import type {
  ChangesetView,
  ChangeView,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import {
  Button,
  Checkbox,
  ConfirmDialog,
  CopyButton,
  Glyph,
  PillTabs,
  Spinner,
} from "../components/ui";
import { SqlPreview } from "./SqlPreview";
import { cx } from "../lib/cx";
import { nombreDeMotor } from "../lib/motor";
import styles from "./PendingChanges.module.css";

/** Los filtros de la tira de arriba. */
type Filtro = "all" | "schema" | "risk";

/**
 * S14 Pending changes.
 *
 * Cada edición ya es una sentencia, escrita por Go. Esta pantalla no arma SQL:
 * la muestra, deja elegir qué entra en este apply y pasa el resto a la vista
 * previa. Ver kaname-plan.md § 6 sobre por qué el changeset es un conjunto de
 * operaciones y no un diff.
 */
/** textoDeTransaccion dice lo que el MOTOR va a hacer, no lo que la casilla
 *  sugiere.
 *
 *  Antes decía «todo o nada» siempre. Contra MySQL y MariaDB eso es falso: un
 *  DDL en el medio de una transacción hace commit implícito de todo lo anterior
 *  y el ROLLBACK final no revierte nada, así que la casilla prometía algo que
 *  la base no cumple. El backend ya calcula en cuántos tramos se va a partir;
 *  acá solo se cuenta. */
function textoDeTransaccion(vista: ChangesetView, transaccion: boolean): string {
  if (!transaccion) {
    return "cada sentencia se confirma sola; una falla deja lo anterior aplicado";
  }
  if (vista.transactionalDdl) {
    return "todo o nada";
  }
  if (vista.tramos <= 1) {
    return `${nombreDeMotor(vista.engine)} no revierte cambios de esquema`;
  }
  return `${nombreDeMotor(vista.engine)} no revierte cambios de esquema: van en ${vista.tramos} tramos`;
}


export function PendingChanges({
  active,
  onApplied,
  onCount,
  onOpenTable,
}: {
  /** Si es la pestaña que se está viendo. Las pestañas quedan montadas y
   *  escondidas para no perder lo que tienen adentro, así que volver a esta no
   *  la remonta: sin esto mostraba la lista de cuando se abrió, que muchas
   *  veces estaba vacía. */
  active: boolean;
  /** Se llama cuando el apply terminó con algo aplicado. `schemaChanged` dice
   *  si el árbol hay que releerlo o si alcanza con recargar las pestañas. */
  onApplied: (schemaChanged: boolean) => void;
  /** Cuántos cambios quedan. Se avisa después de CADA lectura y no solo al
   *  aplicar: descartar todo también cambia el número, y un contador que se
   *  queda con el valor viejo es peor que no tenerlo. */
  onCount: (n: number) => void;
  onOpenTable: (schema: string, table: string) => void;
}) {
  const [vista, setVista] = useState<ChangesetView | null>(null);
  const [cargando, setCargando] = useState(true);
  const [error, setError] = useState("");
  const [filtro, setFiltro] = useState<Filtro>("all");
  const [transaccion, setTransaccion] = useState(true);
  const [previewAbierta, setPreviewAbierta] = useState(false);
  const [descartando, setDescartando] = useState(false);

  const leer = useCallback(async () => {
    setCargando(true);
    try {
      const v = await SessionSvc.Changeset();
      setVista(v);
      onCount(v.summary.total);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCargando(false);
    }
  }, [onCount]);

  useEffect(() => {
    if (!active) return;
    void leer();
  }, [active, leer]);

  if (cargando && !vista) {
    return (
      <p className={styles.cargando}>
        <Spinner size="sm" />
        Leyendo los cambios pendientes…
      </p>
    );
  }
  if (error) {
    return <p className={styles.error}>{error}</p>;
  }
  if (!vista || vista.summary.total === 0) {
    return (
      <div className={styles.vacio}>
        <p className={styles.vacioTitulo}>No hay cambios pendientes</p>
        <p className={styles.vacioHint}>
          Editá la estructura de una tabla o el diagrama y los cambios se juntan acá. Nada toca la
          base hasta que lo apliques.
        </p>
      </div>
    );
  }

  const todos = vista.changes ?? [];
  const visibles = todos.filter((v) => {
    if (filtro === "schema") return v.change.type !== undefined;
    if (filtro === "risk") return v.statement.destructive || v.statement.lock === "all";
    return true;
  });

  // Agrupadas por tabla, como el diseño: los cambios se piensan por objeto, no
  // por orden de tipeo.
  const grupos = new Map<string, ChangeView[]>();
  for (const v of visibles) {
    const clave = `${v.change.schema}.${v.change.table}`;
    grupos.set(clave, [...(grupos.get(clave) ?? []), v]);
  }

  async function incluirTodo(incluir: boolean) {
    for (const v of todos) {
      if (v.change.excluded === incluir) {
        await SessionSvc.IncludeChange(v.change.id, incluir);
      }
    }
    void leer();
  }

  const r = vista.summary;
  const bloqueado = vista.readOnly || r.included === 0;

  return (
    <div className={styles.screen}>
      <div className={styles.head}>
        <div>
          <h1 className={styles.titulo}>Cambios pendientes</h1>
          <p className={styles.bajada}>
            Cada edición está acá como una sentencia. Destildá lo que quieras dejar fuera de este
            apply: se queda pendiente para después.
          </p>
        </div>
        <div className={styles.numeros}>
          <Numero valor={r.schema} etiqueta="esquema" tono="accent" />
          <Numero valor={r.data} etiqueta="datos" tono="warning" />
          <Numero valor={r.destructive} etiqueta="destructivos" tono="danger" />
        </div>
      </div>

      <div className={styles.barra}>
        <button type="button" className={styles.enlace} onClick={() => void incluirTodo(true)}>
          Incluir todo
        </button>
        <button type="button" className={styles.enlace} onClick={() => void incluirTodo(false)}>
          Incluir nada
        </button>
        <span className={styles.divider} />
        <PillTabs
          items={[
            { id: "all", label: "Todos", count: todos.length },
            { id: "schema", label: "Esquema", count: r.schema },
            {
              id: "risk",
              label: "Riesgosos",
              count: todos.filter((v) => v.statement.destructive || v.statement.lock === "all")
                .length,
            },
          ]}
          activeId={filtro}
          onSelect={(id) => setFiltro(id as Filtro)}
          ariaLabel="Filtrar los cambios"
        />
        <span className={styles.grow} />
        <Checkbox checked={transaccion} onChange={setTransaccion}>
          Una sola transacción
        </Checkbox>
        <span className={styles.hint}>{textoDeTransaccion(vista, transaccion)}</span>
      </div>

      <div className={styles.cuerpo}>
        <div className={styles.lista}>
          {[...grupos.entries()].map(([tabla, items]) => (
            <section key={tabla}>
              <div className={styles.grupo}>
                <Glyph kind="table" />
                <button
                  type="button"
                  className={styles.grupoNombre}
                  onClick={() => {
                    const [esq, ...resto] = tabla.split(".");
                    onOpenTable(esq ?? "", resto.join("."));
                  }}
                  title="Abrir la tabla"
                >
                  {tabla}
                </button>
                <span className={styles.grow} />
                <span className={styles.grupoMeta}>
                  {items.length === 1 ? "1 cambio" : `${items.length} cambios`}
                </span>
              </div>
              {items.map((v) => (
                <Fila key={v.change.id} v={v} onCambio={() => void leer()} />
              ))}
            </section>
          ))}
        </div>

        <aside className={styles.panel}>
          <section className={styles.seccion}>
            <div className={styles.seccionTitulo}>Va a correr</div>
            <dl className={styles.datos}>
              <dt>Sentencias</dt>
              <dd>{r.included}</dd>
              <dt>Tablas</dt>
              <dd>{(vista.tables ?? []).length}</dd>
              <dt>Fuera de este apply</dt>
              <dd className={r.total - r.included > 0 ? styles.aviso : undefined}>
                {r.total - r.included === 0 ? "nada" : `${r.total - r.included}`}
              </dd>
            </dl>
          </section>

          {(vista.warnings ?? []).length > 0 ? (
            <section className={styles.seccion}>
              <div className={styles.seccionTitulo}>Antes de aplicar</div>
              <ul className={styles.avisos}>
                {(vista.warnings ?? []).map((w) => (
                  <li key={w}>{w}</li>
                ))}
              </ul>
            </section>
          ) : null}

          {vista.readOnly ? (
            <p className={styles.readonly}>
              Esta conexión es de solo lectura. Los cambios se pueden preparar y copiar, pero no
              aplicar desde acá.
            </p>
          ) : null}

          <section className={styles.seccion}>
            <div className={styles.seccionTitulo}>Orden de ejecución</div>
            <ol className={styles.orden}>
              {(vista.order ?? []).map((v) => (
                <li key={v.change.id} className={v.statement.destructive ? styles.peligro : undefined}>
                  {resumen(v)}
                </li>
              ))}
            </ol>
            <p className={styles.ordenNota}>
              No es el orden en que se editó: una tabla se crea antes que la clave que la
              referencia, y una restricción se borra antes que su columna.
            </p>
          </section>
        </aside>
      </div>

      <div className={styles.pie}>
        <Button
          variant="primary"
          disabled={bloqueado}
          title={vista.readOnly ? "La conexión es de solo lectura" : undefined}
          onClick={() => setPreviewAbierta(true)}
        >
          Ver la SQL y aplicar {r.included} {r.included === 1 ? "sentencia" : "sentencias"}
        </Button>
        <CopyButton text={vista.script} label="Copiar la SQL" />
        <span className={styles.grow} />
        <Button variant="dangerOutline" size="sm" onClick={() => setDescartando(true)}>
          Descartar todo
        </Button>
      </div>

      <ConfirmDialog
        open={descartando}
        severidad="aviso"
        title={`¿Descartar ${r.total} ${r.total === 1 ? "cambio" : "cambios"}?`}
        etiqueta="Descartar todo"
        onClose={() => setDescartando(false)}
        onConfirm={() => {
          setDescartando(false);
          void SessionSvc.DiscardChanges().then(leer);
        }}
      >
        Se vacía la lista. <strong>No se pierde ningún dato</strong> —nada se aplicó todavía—,
        pero las ediciones hay que volver a hacerlas.
      </ConfirmDialog>

      {previewAbierta ? (
        <SqlPreview
          vista={vista}
          singleTransaction={transaccion}
          onClose={() => setPreviewAbierta(false)}
          onApplied={(schemaChanged) => {
            setPreviewAbierta(false);
            void leer();
            onApplied(schemaChanged);
          }}
          onFalloParcial={(schemaChanged) => {
            // Falló, pero algo quedó aplicado. La lista y el árbol tienen que
            // reflejarlo igual: es justo el momento en que la pantalla y la
            // base más pueden discrepar.
            void leer();
            onApplied(schemaChanged);
          }}
        />
      ) : null}
    </div>
  );
}

function Fila({ v, onCambio }: { v: ChangeView; onCambio: () => void }) {
  const incluido = !v.change.excluded;
  return (
    <div className={cx(styles.fila, !incluido && styles.filaFuera)}>
      <div className={styles.filaCheck}>
        <Checkbox
          checked={incluido}
          onChange={(siguiente) => {
            void SessionSvc.IncludeChange(v.change.id, siguiente).then(onCambio);
          }}
        >
          <span className={styles.oculto}>Incluir</span>
        </Checkbox>
      </div>
      <div className={styles.filaOp}>
        <span className={cx(styles.op, styles[`op_${opDe(v)}`])}>{opDe(v)}</span>
      </div>
      <div className={styles.filaSql}>
        <pre className={styles.sql}>{v.statement.sql}</pre>
        {v.error ? <p className={styles.filaError}>{v.error}</p> : null}
        {v.statement.note ? (
          <p
            className={cx(
              styles.nota,
              v.statement.destructive ? styles.notaPeligro : styles.notaAviso,
            )}
          >
            {v.statement.note}
          </p>
        ) : null}
      </div>
      <div className={styles.filaCosto}>
        <span className={cx(styles.costo, v.statement.destructive && styles.peligro)}>
          {costo(v)}
        </span>
        <span className={styles.fuente}>{fuente(v.change.source)}</span>
      </div>
    </div>
  );
}

function Numero({
  valor,
  etiqueta,
  tono,
}: {
  valor: number;
  etiqueta: string;
  tono: "accent" | "warning" | "danger";
}) {
  return (
    <div className={styles.numero}>
      <div className={cx(styles.numeroValor, styles[`tono_${tono}`])}>{valor}</div>
      <div className={styles.numeroEtiqueta}>{etiqueta}</div>
    </div>
  );
}

/** CREATE / ALTER / DROP, deducido del tipo de operación. */
function opDe(v: ChangeView): "CREATE" | "ALTER" | "DROP" {
  const t = v.change.type;
  if (t === "createTable") return "CREATE";
  if (t === "dropTable" || t === "dropColumn" || t === "dropConstraint" || t === "dropIndex") {
    return "DROP";
  }
  return "ALTER";
}

/** "solo metadatos" · "lee 12.481 filas" · "reescribe 12.481 filas" */
function costo(v: ChangeView): string {
  const filas =
    v.rowEstimate >= 0 ? ` ${v.rowEstimate.toLocaleString("es", { useGrouping: true })} filas` : "";
  switch (v.statement.impact) {
    case "metadata":
      return "solo metadatos";
    case "scan":
      return filas ? `lee${filas}` : "lee la tabla entera";
    case "rewrite":
      return filas ? `reescribe${filas}` : "reescribe la tabla";
    default:
      return "";
  }
}

function fuente(s: string): string {
  switch (s) {
    case "erd":
      return "diagrama";
    case "structure":
      return "estructura";
    case "grid":
      return "grilla";
    default:
      return s;
  }
}

/** Una línea para el orden de ejecución: qué operación sobre qué objeto. */
function resumen(v: ChangeView): string {
  const c = v.change;
  const objeto = c.column?.name ?? c.name ?? c.newName ?? "";
  return `${etiquetaDeTipo(c.type)} · ${c.table}${objeto ? "." + objeto : ""}`;
}

const TIPOS: Record<string, string> = {
  createTable: "crear tabla",
  dropTable: "borrar tabla",
  renameTable: "renombrar tabla",
  setTableComment: "comentar tabla",
  addColumn: "agregar columna",
  dropColumn: "borrar columna",
  renameColumn: "renombrar columna",
  setColumnType: "cambiar tipo",
  setNotNull: "exigir no nulo",
  dropNotNull: "permitir nulos",
  setDefault: "poner default",
  dropDefault: "sacar default",
  setColumnComment: "comentar columna",
  addPrimaryKey: "agregar clave primaria",
  addForeignKey: "agregar clave foránea",
  addCheck: "agregar restricción",
  addUnique: "agregar único",
  dropConstraint: "borrar restricción",
  addIndex: "agregar índice",
  dropIndex: "borrar índice",
};

function etiquetaDeTipo(t: string): string {
  return TIPOS[t] ?? t;
}
