import { useEffect, useState } from "react";
import type { Cell, Change } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import type { DetailColumn } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import type {
  ChangeView,
  RowReview,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import { Button, CopyButton, Dialog, PillTabs, Spinner } from "../components/ui";
import { cx } from "../lib/cx";
import { plural } from "../lib/motor";
import styles from "./DataReview.module.css";

type Vista = "unificada" | "lado";

/** Los tres tipos de cambio de datos, con su etiqueta. */
const OP: Record<string, { etiqueta: "INSERT" | "UPDATE" | "DELETE"; clase: string }> = {
  insertRow: { etiqueta: "INSERT", clase: styles.opInsert ?? "" },
  updateRow: { etiqueta: "UPDATE", clase: styles.opUpdate ?? "" },
  deleteRow: { etiqueta: "DELETE", clase: styles.opDelete ?? "" },
};

export function esCambioDeDatos(c: Change): boolean {
  return c.type in OP;
}

/**
 * S08 Data review: una fila por vez, con lo que había en la base y lo que va a
 * quedar, la sentencia, y las comprobaciones que Go hace mirando la base
 * —si la fila sigue estando, si el padre existe, cuántas hijas arrastra un
 * borrado—.
 *
 * Trabaja sobre el changeset, no sobre la grilla: para cuando se abre, las
 * ediciones ya son cambios. Las comprobaciones se piden de a una, para la fila
 * que se está mirando: son consultas a la base y cien filas abiertas de golpe
 * serían quinientas consultas antes de leer nada.
 */
export function DataReview({
  cambios,
  initialId,
  onClose,
  onDiscarded,
  onReviewSql,
}: {
  /** Los cambios pendientes de DATOS, en orden de edición. */
  cambios: ChangeView[];
  /** Con cuál abrir. */
  initialId?: string;
  onClose: () => void;
  /** Se descartó una fila: el que llama relee el changeset. */
  onDiscarded: () => void;
  /** «Ver la SQL y aplicar»: cierra esto y abre la vista previa. */
  onReviewSql: () => void;
}) {
  const [i, setI] = useState(() => {
    const at = cambios.findIndex((v) => v.change.id === initialId);
    return at >= 0 ? at : 0;
  });
  const [vista, setVista] = useState<Vista>("unificada");
  // Revisiones ya pedidas, por id: volver a una fila no vuelve a consultar.
  const [revisiones, setRevisiones] = useState<Map<string, RowReview | Error>>(new Map());

  // Si el que llama releyó el changeset y la fila que se miraba ya no está,
  // se corre a la anterior; si no queda ninguna, se cierra.
  useEffect(() => {
    if (cambios.length === 0) onClose();
    else if (i >= cambios.length) setI(cambios.length - 1);
  }, [cambios.length, i, onClose]);

  const actual = cambios[i];

  useEffect(() => {
    if (!actual) return;
    const id = actual.change.id;
    if (revisiones.has(id)) return;
    let vigente = true;
    SessionSvc.ReviewRow(id)
      .then((r) => {
        if (vigente) setRevisiones((m) => new Map(m).set(id, r));
      })
      .catch((err: unknown) => {
        if (vigente) {
          setRevisiones((m) =>
            new Map(m).set(id, err instanceof Error ? err : new Error(String(err))),
          );
        }
      });
    return () => {
      vigente = false;
    };
  }, [actual, revisiones]);

  if (!actual) return null;

  const revision = revisiones.get(actual.change.id);
  const cargando = revision === undefined;
  const c = actual.change;
  const op = OP[c.type] ?? OP.updateRow!;
  const filas = filasDe(c, revision instanceof Error ? undefined : revision?.columns ?? undefined);
  const cambiadas = filas.filter((f) => f.cambia).length;

  const cuenta = { insertRow: 0, updateRow: 0, deleteRow: 0 };
  for (const v of cambios) {
    if (v.change.type in cuenta) cuenta[v.change.type as keyof typeof cuenta]++;
  }

  function anterior() {
    setI((n) => (n - 1 + cambios.length) % cambios.length);
  }
  function siguiente() {
    setI((n) => (n + 1) % cambios.length);
  }

  async function descartar() {
    await SessionSvc.Unstage(c.id);
    onDiscarded();
  }

  return (
    <Dialog open size="xl" title="Revisar los cambios de filas" onClose={onClose}>
      <div
        className={styles.marco}
        onKeyDown={(e) => {
          if (e.key === "ArrowUp") {
            e.preventDefault();
            anterior();
          } else if (e.key === "ArrowDown") {
            e.preventDefault();
            siguiente();
          }
        }}
      >
        <aside className={styles.lista}>
          <div className={styles.listaCabecera}>
            <span className={styles.seccionTitulo}>Filas</span>
            <span className={styles.grow} />
            <span className={styles.dim}>
              {new Set(cambios.map((v) => `${v.change.schema}.${v.change.table}`)).size}{" "}
              {plural(new Set(cambios.map((v) => v.change.table)).size, "tabla", "tablas")}
            </span>
          </div>
          <div className={styles.listaCuerpo} role="listbox" aria-label="Filas con cambios">
            {cambios.map((v, k) => {
              const o = OP[v.change.type] ?? OP.updateRow!;
              return (
                <button
                  type="button"
                  key={v.change.id}
                  role="option"
                  aria-selected={k === i}
                  className={cx(styles.item, k === i && styles.itemActivo, k === i && o.clase)}
                  onClick={() => setI(k)}
                >
                  <div className={styles.itemLinea}>
                    <span className={cx(styles.op, o.clase)}>{o.etiqueta}</span>
                    <span className={styles.itemTabla}>{v.change.table}</span>
                    <span className={styles.grow} />
                    <span className={styles.dim}>{claveCorta(v.change)}</span>
                  </div>
                  <div className={styles.itemResumen}>{resumenDe(v.change)}</div>
                </button>
              );
            })}
          </div>
          <div className={styles.listaPie}>
            <span className={styles.masVerde}>+{cuenta.insertRow}</span>
            <span className={styles.masAmbar}>~{cuenta.updateRow}</span>
            <span className={styles.masRojo}>−{cuenta.deleteRow}</span>
            <span className={styles.grow} />
            <span className={styles.dim}>↑↓ para moverse</span>
          </div>
        </aside>

        <section className={styles.detalle}>
          <div className={styles.detalleCabecera}>
            <span className={cx(styles.opGrande, op.clase)}>{op.etiqueta}</span>
            <span className={styles.detalleTabla}>
              {c.schema ? `${c.schema}.` : ""}
              {c.table}
            </span>
            <span className={styles.dim}>{claveCorta(c)}</span>
            <span className={styles.grow} />
            <span className={styles.dim}>
              {c.type === "updateRow"
                ? `${cambiadas} de ${filas.length} ${plural(filas.length, "columna cambia", "columnas cambian")}`
                : `${filas.length} ${plural(filas.length, "columna", "columnas")}`}
            </span>
            <PillTabs
              items={[
                { id: "unificada", label: "Unificada" },
                { id: "lado", label: "Lado a lado" },
              ]}
              activeId={vista}
              onSelect={(id) => setVista(id as Vista)}
              ariaLabel="Cómo mostrar la fila"
            />
          </div>

          <div className={styles.detalleCuerpo}>
            {vista === "unificada" ? (
              <Unificada filas={filas} tipo={c.type} />
            ) : (
              <LadoALado filas={filas} tipo={c.type} />
            )}

            <div className={styles.abajo}>
              <div className={styles.caja}>
                <div className={styles.cajaCabecera}>
                  <span className={styles.seccionTitulo}>Sentencia de esta fila</span>
                  <span className={styles.grow} />
                  <CopyButton text={actual.statement.sql} label="Copiar" />
                </div>
                <pre className={styles.sql}>{actual.statement.sql}</pre>
              </div>
              <div className={cx(styles.caja, styles.comprobaciones)}>
                <div className={styles.cajaCabecera}>
                  <span className={styles.seccionTitulo}>Comprobaciones</span>
                </div>
                <div className={styles.cajaCuerpo}>
                  {cargando ? (
                    <p className={styles.dim}>
                      <Spinner size="sm" /> Mirando la base…
                    </p>
                  ) : revision instanceof Error ? (
                    <p className={styles.malo}>{revision.message}</p>
                  ) : (
                    <ul className={styles.checks}>
                      {(revision.checks ?? []).map((ck, k) => (
                        <li key={k} className={styles.check}>
                          <span className={cx(styles.punto, styles[`punto_${ck.level}`])}>
                            {ck.level === "ok" ? "✓" : ck.level === "warn" ? "!" : "✕"}
                          </span>
                          <div className={styles.checkTexto}>
                            <div className={styles.checkLabel}>{ck.label}</div>
                            {ck.detail ? <div className={styles.checkDetalle}>{ck.detail}</div> : null}
                          </div>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              </div>
            </div>
          </div>

          <div className={styles.detallePie}>
            <button type="button" className={styles.enlace} onClick={anterior}>
              ← Anterior
            </button>
            <button type="button" className={styles.enlace} onClick={siguiente}>
              Siguiente →
            </button>
            <span className={styles.dim}>
              fila {i + 1} de {cambios.length}
            </span>
            <span className={styles.grow} />
            <Button size="sm" variant="dangerOutline" onClick={() => void descartar()}>
              Descartar esta fila
            </Button>
            <Button size="sm" variant="primary" onClick={onReviewSql}>
              Ver la SQL y aplicar
            </Button>
          </div>
        </section>
      </div>
    </Dialog>
  );
}

/** Una columna de la fila, con lo que había y lo que va a quedar. */
interface FilaCol {
  nombre: string;
  tipo: string;
  tag: string;
  antes: string | null | undefined;
  despues: string | null | undefined;
  /** undefined = «por defecto», que no es NULL. */
  cambia: boolean;
  porDefecto: boolean;
}

/**
 * Arma la fila entera a partir del cambio y, si ya se leyó, de las columnas de
 * la tabla: así se ven también las columnas que no se tocaron, y en un alta
 * las que toman su valor por defecto.
 */
function filasDe(c: Change, columnas: DetailColumn[] | undefined): FilaCol[] {
  const previos = new Map<string, string | null>();
  for (const p of c.previous ?? []) previos.set(p.column, p.value);
  const nuevos = new Map<string, string | null>();
  for (const v of c.values ?? []) nuevos.set(v.column, v.value);

  // El orden de las columnas: el de la tabla si se leyó; si no, el de lo que
  // trae el cambio.
  const nombres: string[] = columnas
    ? columnas.map((col) => col.name)
    : [...new Set([...previos.keys(), ...nuevos.keys()])];
  const tipos = new Map<string, DetailColumn>();
  for (const col of columnas ?? []) tipos.set(col.name, col);

  return nombres.map((nombre) => {
    const col = tipos.get(nombre);
    const tag = col?.primaryKey ? "PK" : col?.foreignKey ? "FK" : "";
    const antes = previos.has(nombre) ? previos.get(nombre) : undefined;
    const escrita = nuevos.has(nombre);
    let despues: string | null | undefined;
    let porDefecto = false;
    if (c.type === "insertRow") {
      despues = escrita ? nuevos.get(nombre) : undefined;
      porDefecto = !escrita;
    } else if (c.type === "deleteRow") {
      despues = undefined;
    } else {
      despues = escrita ? nuevos.get(nombre) : antes;
    }
    return {
      nombre,
      tipo: col?.dataType ?? "",
      tag,
      antes,
      despues,
      cambia: c.type === "updateRow" ? escrita : c.type === "insertRow" ? escrita : true,
      porDefecto,
    };
  });
}

function Valor({ v, porDefecto }: { v: string | null | undefined; porDefecto?: boolean }) {
  if (porDefecto) return <span className={styles.valorApagado}>[default]</span>;
  if (v === undefined) return <span className={styles.valorApagado}>—</span>;
  if (v === null) return <span className={styles.valorNulo}>[null]</span>;
  if (v === "") return <span className={styles.valorApagado}>cadena vacía</span>;
  return <span className={styles.valor}>{v}</span>;
}

function Unificada({ filas, tipo }: { filas: FilaCol[]; tipo: string }) {
  return (
    <div className={styles.tabla}>
      <div className={cx(styles.tr, styles.th)}>
        <div>Columna</div>
        <div>Tipo</div>
        <div>Valor</div>
      </div>
      {filas.map((f) => (
        <div
          key={f.nombre}
          className={cx(
            styles.tr,
            f.cambia && tipo === "updateRow" && styles.trCambia,
            tipo === "insertRow" && f.cambia && styles.trNueva,
            tipo === "deleteRow" && styles.trBorrada,
          )}
        >
          <div className={styles.tdNombre}>
            {f.tag ? <span className={cx(styles.tag, styles[`tag_${f.tag}`])}>{f.tag}</span> : null}
            <span className={cx(styles.nombre, f.cambia && styles.nombreCambia)}>{f.nombre}</span>
          </div>
          <div className={styles.tdTipo}>{f.tipo}</div>
          <div className={styles.tdValor}>
            {tipo === "updateRow" && f.cambia ? (
              <div className={styles.viejo}>
                <span className={styles.marcaMenos}>−</span>
                <span className={styles.tachado}>
                  <Valor v={f.antes} />
                </span>
              </div>
            ) : null}
            <div className={styles.nuevo}>
              <span
                className={cx(
                  styles.marca,
                  tipo === "deleteRow" ? styles.marcaMenos : f.cambia ? styles.marcaMas : styles.marcaNada,
                )}
              >
                {tipo === "deleteRow" ? "−" : f.cambia ? "+" : ""}
              </span>
              <span className={cx(tipo === "deleteRow" && styles.tachado)}>
                <Valor v={tipo === "deleteRow" ? f.antes : f.despues} porDefecto={f.porDefecto} />
              </span>
              {f.porDefecto ? <span className={styles.nota}>lo pone la base</span> : null}
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

function LadoALado({ filas, tipo }: { filas: FilaCol[]; tipo: string }) {
  return (
    <div className={styles.lados}>
      <div className={styles.tabla}>
        <div className={cx(styles.trLado, styles.th)}>
          <div>En la base</div>
          <div className={styles.thNota}>como se leyó</div>
        </div>
        {filas.map((f) => (
          <div key={f.nombre} className={cx(styles.trLado, f.cambia && tipo === "updateRow" && styles.trBorrada)}>
            <div className={styles.tdNombreLado}>{f.nombre}</div>
            <div className={styles.tdValor}>
              {tipo === "insertRow" ? <span className={styles.valorApagado}>—</span> : <Valor v={f.antes} />}
            </div>
          </div>
        ))}
      </div>
      <div className={styles.tabla}>
        <div className={cx(styles.trLado, styles.th)}>
          <div className={styles.thPendiente}>Pendiente</div>
          <div className={styles.thNota}>sin aplicar</div>
        </div>
        {filas.map((f) => (
          <div
            key={f.nombre}
            className={cx(
              styles.trLado,
              tipo === "deleteRow" ? styles.trBorrada : f.cambia && styles.trNueva,
            )}
          >
            <div className={cx(styles.tdNombreLado, f.cambia && styles.nombreCambia)}>{f.nombre}</div>
            <div className={styles.tdValor}>
              {tipo === "deleteRow" ? (
                <span className={styles.tachado}>
                  <Valor v={f.antes} />
                </span>
              ) : (
                <Valor v={f.despues} porDefecto={f.porDefecto} />
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

/** «id = 1043», o «fila nueva». */
function claveCorta(c: Change): string {
  if (c.type === "insertRow") return "fila nueva";
  return (c.key ?? []).map((k: Cell) => `${k.column} = ${k.value ?? "NULL"}`).join(", ");
}

/** Lo que dice el ítem de la lista debajo de la tabla. */
function resumenDe(c: Change): string {
  const cols = (c.values ?? []).map((v) => v.column);
  switch (c.type) {
    case "updateRow":
      return cols.join(", ");
    case "insertRow":
      return `${cols.length} ${plural(cols.length, "columna cargada", "columnas cargadas")}`;
    default:
      return "borrar la fila";
  }
}
