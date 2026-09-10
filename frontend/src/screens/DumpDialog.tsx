import { useEffect, useRef, useState } from "react";
import type {
  DumpInfo,
  DumpPreview,
  PgDumpInfo,
  PgDumpStatus,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Ref } from "../../bindings/github.com/LucianoR23/kanamedb/internal/dump";
import * as DumpsSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/dumps";
import * as QueriesSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import { Button, Checkbox, CopyButton, Dialog, Spinner } from "../components/ui";
import { cx } from "../lib/cx";
import { textoDe } from "../lib/dialogos";
import { elegirDestino, nombreDeArchivo, tamano } from "../lib/exportar";
import { miles } from "../lib/importar";
import { plural } from "../lib/motor";
import styles from "./DumpDialog.module.css";

/** Qué escribe el volcado. */
type Alcance = "todo" | "estructura" | "datos";

type Estado =
  | { fase: "quieto" }
  | { fase: "escribiendo" }
  | { fase: "listo"; ruta: string; bytes: number; filas: number; tablas: number; ms: number }
  | { fase: "cancelado" }
  | { fase: "error"; mensaje: string };

/**
 * El volcado: la estructura, los datos, o los dos.
 *
 * La palabra «backup» no aparece, y no es un descuido. Un backup es una copia
 * de la que se restaura TODO; esto no lo es, y el archivo lo dice arriba de
 * todo. Lo que sí hace —y lo que ningún export de esquema suele hacer— es
 * declarar con nombre y apellido qué deja afuera. Ese panel es la razón de ser
 * de la pantalla: enterarse al abrir el archivo sería tarde.
 *
 * `pg_dump` va en su propia pestaña porque es otra cosa: no lo genera Kaname,
 * lo genera la herramienta de PostgreSQL. Para ESO la palabra backup sí vale, y
 * por eso la pestaña dice lo que dice.
 */
export function DumpDialog({
  open,
  schema,
  runID,
  onClose,
}: {
  open: boolean;
  /** El esquema que se vuelca. Vacío es la base entera. */
  schema: string;
  runID: string;
  onClose: () => void;
}) {
  const [pestana, setPestana] = useState<"kaname" | "pgdump">("kaname");
  const [alcance, setAlcance] = useState<Alcance>("todo");
  const [dropFirst, setDropFirst] = useState(false);
  const [vista, setVista] = useState<DumpPreview | null>(null);
  const [cargando, setCargando] = useState(true);
  const [verDetalle, setVerDetalle] = useState(false);
  const [estado, setEstado] = useState<Estado>({ fase: "quieto" });
  const [pg, setPg] = useState<PgDumpStatus | null>(null);
  const [pgError, setPgError] = useState("");

  const estructura = alcance !== "datos";
  const datos = alcance !== "estructura";
  const pedido = () => ({
    runId: runID,
    schemas: schema ? [schema] : [],
    structure: estructura,
    data: datos,
    dropFirst,
  });

  // La vista previa se pide con cada cambio de alcance: la cobertura solo
  // aplica a la estructura, así que «solo datos» no tiene nada que declarar.
  useEffect(() => {
    if (!open) return;
    let vivo = true;
    setCargando(true);
    void DumpsSvc.Preview(pedido())
      .then((p) => {
        if (!vivo) return;
        setVista(p);
        // El estado NO se toca si hay una escritura en curso: ponerlo en
        // «quieto» apagaba `escribiendo`, y con eso volvían el botón de cerrar,
        // el «Guardar» y un «Cancelar» que ya no cancelaba nada.
        setEstado((e) => (e.fase === "escribiendo" ? e : { fase: "quieto" }));
      })
      .catch((err: unknown) => {
        if (vivo) setEstado({ fase: "error", mensaje: textoDe(err) });
      })
      .finally(() => {
        if (vivo) setCargando(false);
      });
    return () => {
      vivo = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, alcance, dropFirst, schema]);

  // El estado de `pg_dump` se pide al abrir esa pestaña, no antes: busca en el
  // PATH y corre `--version`, que es trabajo que la mayoría no va a necesitar.
  useEffect(() => {
    if (!open || pestana !== "pgdump") return;
    let vivo = true;
    setPgError("");
    void DumpsSvc.PgDump(pedido())
      .then((s) => {
        if (vivo) setPg(s);
      })
      .catch((err: unknown) => {
        if (vivo) setPgError(textoDe(err));
      });
    return () => {
      vivo = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, pestana, alcance, dropFirst, schema]);

  const escribiendo = estado.fase === "escribiendo";
  // Si el corte lo pidió quien mira, el fallo no es un fallo: se cuenta como
  // cancelación y no con el error crudo del contexto muerto.
  const pidioCancelar = useRef(false);

  const guardar = async (conPgDump: boolean) => {
    const nombre = nombreDeArchivo(schema || "volcado");
    let destino = "";
    try {
      destino = await elegirDestino(nombre, ".sql", false);
    } catch (err) {
      setEstado({ fase: "error", mensaje: textoDe(err) });
      return;
    }
    if (!destino) return;

    setEstado({ fase: "escribiendo" });
    pidioCancelar.current = false;
    try {
      if (conPgDump) {
        const info: PgDumpInfo = await DumpsSvc.RunPgDump(pedido(), destino);
        setEstado({
          fase: "listo",
          ruta: info.path,
          bytes: info.bytes,
          filas: 0,
          tablas: 0,
          ms: info.elapsedMs,
        });
        return;
      }
      const info: DumpInfo = await DumpsSvc.Save(pedido(), destino);
      setEstado({
        fase: "listo",
        ruta: info.path,
        bytes: info.bytes,
        filas: info.rows,
        tablas: info.tables,
        ms: info.elapsedMs,
      });
    } catch (err) {
      setEstado(
        pidioCancelar.current
          ? { fase: "cancelado" }
          : { fase: "error", mensaje: textoDe(err) },
      );
    }
  };

  const cancelar = async () => {
    if (!escribiendo) {
      onClose();
      return;
    }
    // El estado lo pone el `catch` de `guardar` cuando la promesa se rompe:
    // ponerlo acá lo pisaba el error crudo que llega un instante después.
    pidioCancelar.current = true;
    await QueriesSvc.Cancel(runID);
  };

  const tablas = vista?.tables ?? [];
  const ciclos = vista?.cycles ?? [];

  return (
    <Dialog
      open={open}
      title={`Volcar ${schema || "la base"}`}
      size="xl"
      {...(escribiendo ? {} : { onClose })}
      footer={
        <>
          <Pie estado={estado} />
          <span className={styles.grow} />
          <Button onClick={() => void cancelar()}>
            {escribiendo ? "Cancelar el volcado" : "Cerrar"}
          </Button>
          {pestana === "kaname" ? (
            <Button
              variant="primary"
              disabled={cargando || escribiendo || tablas.length === 0}
              onClick={() => void guardar(false)}
            >
              {escribiendo ? "Escribiendo…" : "Guardar el volcado…"}
            </Button>
          ) : (
            <Button
              variant="primary"
              disabled={!pg?.canRun || escribiendo}
              title={pg?.canRun ? undefined : (pg?.reason ?? "")}
              onClick={() => void guardar(true)}
            >
              {escribiendo ? "Corriendo pg_dump…" : "Correr pg_dump…"}
            </Button>
          )}
        </>
      }
    >
      <div className={styles.marco}>
        <aside className={styles.lateral}>
          <div className={styles.seccion}>Qué lleva</div>
          <div role="radiogroup" aria-label="Qué lleva el volcado">
            {/* Mientras se escribe no se cambia lo que se está escribiendo:
                el pedido ya salió con el alcance de antes. */}
            <Opcion
              elegido={alcance === "todo"}
              label="Estructura y datos"
              nota="el archivo entero"
              disabled={escribiendo}
              onElegir={() => setAlcance("todo")}
            />
            <Opcion
              elegido={alcance === "estructura"}
              label="Solo la estructura"
              nota="tablas, claves e índices"
              disabled={escribiendo}
              onElegir={() => setAlcance("estructura")}
            />
            <Opcion
              elegido={alcance === "datos"}
              label="Solo los datos"
              nota="INSERTs, en orden de dependencias"
              disabled={escribiendo}
              onElegir={() => setAlcance("datos")}
            />
          </div>

          <div className={cx(styles.seccion, styles.seccionSegunda)}>Opciones</div>
          <div className={styles.opciones}>
            <Checkbox checked={dropFirst} disabled={escribiendo} onChange={setDropFirst}>
              Borrar antes de crear
              <span className={styles.nota}>DROP TABLE al principio de cada tabla</span>
            </Checkbox>
          </div>

          <span className={styles.grow} />
          <p className={styles.lateralNota}>
            No se llama «backup» a propósito: un backup es una copia de la que se restaura
            todo, y esto deja cosas afuera. El archivo dice cuáles.
          </p>
        </aside>

        <section className={styles.principal}>
          <div className={styles.pestanas} role="tablist" aria-label="Cómo volcar">
            <button
              type="button"
              role="tab"
              aria-selected={pestana === "kaname"}
              className={cx(styles.pestana, pestana === "kaname" && styles.pestanaOn)}
              onClick={() => setPestana("kaname")}
            >
              Volcado de Kaname
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={pestana === "pgdump"}
              className={cx(styles.pestana, pestana === "pgdump" && styles.pestanaOn)}
              onClick={() => setPestana("pgdump")}
            >
              pg_dump
            </button>
          </div>

          {pestana === "kaname" ? (
            cargando ? (
              <div className={styles.cargando}>
                <Spinner />
              </div>
            ) : (
              <div className={styles.cuerpo}>
                <Cobertura
                  vista={vista}
                  estructura={estructura}
                  abierto={verDetalle}
                  onAbrir={() => setVerDetalle((v) => !v)}
                />
                {ciclos.length > 0 && datos ? (
                  <Aviso tono="ojo">
                    <strong>Hay tablas que se apuntan entre sí.</strong> No existe ningún orden
                    de inserción que funcione, así que estos grupos van a fallar al cargarse
                    salvo que difieras las restricciones:{" "}
                    {ciclos.map((g) => (g ?? []).map(nombreDeRef).join(" ↔ ")).join("; ")}.
                  </Aviso>
                ) : null}

                <div className={styles.seccion}>
                  {tablas.length} {plural(tablas.length, "tabla", "tablas")}
                  {datos ? ", en orden de dependencias" : ""}
                </div>
                <ol className={styles.tablas}>
                  {tablas.map((t) => (
                    <li key={nombreDeRef(t)}>{nombreDeRef(t)}</li>
                  ))}
                </ol>

                <div className={cx(styles.seccion, styles.seccionSegunda)}>
                  Lo que va arriba del archivo
                </div>
                <pre className={styles.pre}>{vista?.header ?? ""}</pre>
              </div>
            )
          ) : (
            <PgDump estado={pg} error={pgError} />
          )}
        </section>
      </div>
    </Dialog>
  );
}

function nombreDeRef(r: Ref): string {
  return r.schema ? `${r.schema}.${r.table}` : r.table;
}

/**
 * La cobertura declarada: qué NO lleva el archivo.
 *
 * Es la condición para que el volcado de estructura exista. El problema de un
 * export de esquema no es la dificultad sino el silencio: uno que se olvida de
 * una política de RLS se ve idéntico a uno correcto. La lista larga va detrás
 * de un botón, pero el resumen está siempre a la vista — no puede vivir en un
 * tooltip, porque es la información que decide si el archivo sirve.
 */
function Cobertura({
  vista,
  estructura,
  abierto,
  onAbrir,
}: {
  vista: DumpPreview | null;
  estructura: boolean;
  abierto: boolean;
  onAbrir: () => void;
}) {
  if (!estructura) {
    return (
      <p className={styles.nota}>
        Solo datos: no se escribe ninguna definición, así que no hay nada que dejar afuera.
      </p>
    );
  }
  const resumen = vista?.summary ?? "";
  if (!resumen) {
    return (
      <Aviso tono="bien">
        <strong>Este volcado no deja nada afuera.</strong> En este esquema no hay vistas,
        funciones ni triggers. Se comprobó contra el catálogo, no se está suponiendo.
      </Aviso>
    );
  }
  return (
    <div className={styles.cobertura}>
      <Aviso tono="ojo">
        <strong>{resumen}</strong> {plural(vista?.uncovered?.length ?? 0, "queda", "quedan")}{" "}
        afuera de este archivo. Kaname escribe tablas, columnas, claves, restricciones e
        índices; lo demás no. Restaurarlo no deja la base como estaba.
      </Aviso>
      <button type="button" className={styles.verDetalle} onClick={onAbrir}>
        {abierto ? "Ocultar la lista" : "Ver qué queda afuera"}
      </button>
      {abierto ? (
        <ul className={styles.detalle}>
          {(vista?.detail ?? []).map((d) => (
            <li key={d}>{d}</li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

/** La pestaña de `pg_dump`. El comando se muestra siempre, se pueda correr o no. */
function PgDump({ estado, error }: { estado: PgDumpStatus | null; error: string }) {
  if (error) {
    return (
      <div className={styles.cuerpo}>
        <p className={styles.error} role="alert">
          {error}
        </p>
      </div>
    );
  }
  if (!estado) {
    return (
      <div className={styles.cargando}>
        <Spinner />
      </div>
    );
  }
  return (
    <div className={styles.cuerpo}>
      <p className={styles.nota}>
        Esto no lo genera Kaname: lo genera la herramienta de PostgreSQL. Es la opción
        completa de verdad, y para lo que sale de acá la palabra «backup» sí vale.
      </p>

      <div className={styles.seccion}>El comando</div>
      <div className={styles.comandoFila}>
        <pre className={styles.comando}>{estado.command}</pre>
        <CopyButton text={estado.command} />
      </div>
      <p className={styles.nota}>
        La contraseña no está en la línea a propósito: esto se copia a un chat y al historial
        del shell. Cuando lo corre Kaname va por el entorno del proceso; cuando lo corrés vos,
        `pg_dump` la pide.
      </p>

      <div className={cx(styles.seccion, styles.seccionSegunda)}>La herramienta</div>
      <dl className={styles.hechos}>
        <dt>En el PATH</dt>
        <dd className={estado.found ? styles.ok : styles.dim}>
          {estado.found ? (estado.path ?? "sí") : "no está"}
        </dd>
        {estado.version ? (
          <>
            <dt>Versión</dt>
            <dd>{estado.version}</dd>
          </>
        ) : null}
        {estado.serverVersion ? (
          <>
            <dt>Servidor</dt>
            <dd>{estado.serverVersion}</dd>
          </>
        ) : null}
      </dl>

      {estado.canRun ? (
        <Aviso tono="bien">
          Se puede correr desde acá. La versión de la herramienta alcanza para este servidor.
        </Aviso>
      ) : (
        <Aviso tono="ojo">
          <strong>{estado.reason}</strong> {estado.hint}
        </Aviso>
      )}
    </div>
  );
}

function Opcion({
  elegido,
  label,
  nota,
  disabled = false,
  onElegir,
}: {
  elegido: boolean;
  label: string;
  nota: string;
  disabled?: boolean;
  onElegir: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={elegido}
      disabled={disabled}
      className={cx(styles.alcance, elegido && styles.alcanceOn)}
      onClick={onElegir}
    >
      <span className={cx(styles.radio, !elegido && styles.radioOff)} aria-hidden="true">
        <span />
      </span>
      <span className={styles.alcanceTexto}>
        <span>{label}</span>
        <span className={styles.nota}>{nota}</span>
      </span>
    </button>
  );
}

function Aviso({ tono, children }: { tono: "ojo" | "bien"; children: React.ReactNode }) {
  return (
    <p className={cx(styles.aviso, tono === "bien" ? styles.avisoBien : styles.avisoOjo)}>
      <span className={styles.bang} aria-hidden="true">
        {tono === "bien" ? "✓" : "!"}
      </span>
      <span>{children}</span>
    </p>
  );
}

function Pie({ estado }: { estado: Estado }) {
  switch (estado.fase) {
    case "listo":
      return (
        <span className={styles.ok} title={estado.ruta}>
          Guardado · {estado.tablas > 0 ? `${estado.tablas} ${plural(estado.tablas, "tabla", "tablas")} · ` : ""}
          {estado.filas > 0 ? `${miles(estado.filas)} ${plural(estado.filas, "fila", "filas")} · ` : ""}
          {tamano(estado.bytes)} · {estado.ruta}
        </span>
      );
    case "escribiendo":
      return <span className={styles.dim}>Escribiendo…</span>;
    case "cancelado":
      return (
        <span className={styles.dim}>
          Volcado cancelado. No quedó ningún archivo: el definitivo aparece recién al final.
        </span>
      );
    case "error":
      return (
        <span className={styles.error} role="alert">
          {estado.mensaje}
        </span>
      );
    default:
      return (
        <span className={styles.dim}>
          Se escribe entero en un temporal y recién después con el nombre elegido.
        </span>
      );
  }
}
