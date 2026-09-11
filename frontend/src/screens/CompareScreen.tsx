import { useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";
import { Dialogs } from "@wailsio/runtime";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import type {
  CompareResult,
  CompareSide,
  ConnectionView,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Clase, Lado, Riesgo } from "../../bindings/github.com/LucianoR23/kanamedb/internal/drift";
import type { Diferencia } from "../../bindings/github.com/LucianoR23/kanamedb/internal/drift";
import type { Statement } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import { Button, Checkbox, Combobox, Spinner } from "../components/ui";
import type { ComboOption } from "../components/ui";
import { cancelado, textoDe } from "../lib/dialogos";
import { nombreDeMotor, plural } from "../lib/motor";
import { cx } from "../lib/cx";
import styles from "./CompareScreen.module.css";

/**
 * S20 Schema drift: comparar dos conexiones y generar la SQL que alinearía la
 * segunda con la primera.
 *
 * Tres cosas que la pantalla promete y que Go sostiene:
 *
 * - **No ejecuta nada.** Lee dos catálogos y escribe un archivo. Ni siquiera
 *   pasa por el changeset: la migración es texto que corre quien la lea.
 * - **No borra nada.** Lo que existe solo en el destino se reporta sin
 *   sentencia y con el motivo; el archivo lo lleva como comentario.
 * - **Dice qué no miró.** El catálogo trae vistas y funciones por nombre, así
 *   que dos homónimas con cuerpos distintos se ven iguales; la lista de «no
 *   comparado» está a la vista y no en un tooltip.
 *
 * La SQL de cada diferencia la escribió el motor del DESTINO al comparar. La
 * pantalla la muestra; no la arma, no la edita y no la manda de vuelta.
 */

type Filtro = "all" | Lado;

interface Incluir {
  [Lado.SoloEnOrigen]: boolean;
  [Lado.Distinto]: boolean;
  [Lado.SoloEnDestino]: boolean;
}

export function CompareScreen({
  connections,
  initialSourceId,
  onBack,
}: {
  connections: readonly ConnectionView[];
  /** La conexión desde la que se entró, si hubo: queda como origen. */
  initialSourceId?: string | undefined;
  onBack: () => void;
}) {
  const [origenId, setOrigenId] = useState(initialSourceId ?? "");
  const [destinoId, setDestinoId] = useState("");

  const [comparando, setComparando] = useState<{ cancelar: () => void } | null>(null);
  const [resultado, setResultado] = useState<CompareResult | null>(null);
  const [comparadoA, setComparadoA] = useState("");
  const [fallo, setFallo] = useState<{ message: string; hint: string; detail: string } | null>(
    null,
  );

  const [filtro, setFiltro] = useState<Filtro>("all");
  const [seleccion, setSeleccion] = useState<string | null>(null);
  // Lo que va a la migración, por lado. «Solo en el destino» arranca apagado:
  // nunca tiene sentencia —borrar no se genera— así que solo agregaría
  // comentarios, y quien los quiera los prende.
  const [incluir, setIncluir] = useState<Incluir>({
    [Lado.SoloEnOrigen]: true,
    [Lado.Distinto]: true,
    [Lado.SoloEnDestino]: false,
  });

  const [guardando, setGuardando] = useState(false);
  const [aviso, setAviso] = useState("");
  const [errorMigracion, setErrorMigracion] = useState("");
  // «Copiado» un momento después de copiar, como CopyButton. Con un estado
  // propio y no con CopyButton porque el texto no está hasta que se pide: se
  // pide y se copia en el mismo clic, así que nunca se copia una migración
  // de antes de tocar una casilla.
  const [copiado, setCopiado] = useState(false);
  const timerCopiado = useRef<number | null>(null);
  useEffect(() => {
    return () => {
      if (timerCopiado.current !== null) window.clearTimeout(timerCopiado.current);
    };
  }, []);

  // La comparación en vuelo se cancela al salir: es una llamada que abre dos
  // conexiones, y una pantalla que ya no está no tiene por qué dejarlas
  // abriéndose.
  const enVuelo = useRef<{ cancelar: () => void } | null>(null);
  useEffect(() => {
    return () => enVuelo.current?.cancelar();
  }, []);

  const opciones: ComboOption[] = connections.map((c) => ({
    value: c.connection.id,
    label: c.connection.name,
    tag: nombreDeMotor(c.connection.engine),
  }));

  async function comparar(src = origenId, dst = destinoId) {
    setFallo(null);
    setAviso("");
    setErrorMigracion("");
    const pedido = SessionSvc.Compare({ sourceId: src, targetId: dst });
    const handle = { cancelar: () => pedido.cancel() };
    enVuelo.current = handle;
    setComparando(handle);
    try {
      const res = await pedido;
      if (!res.ok) {
        const f = res.failure;
        setFallo({
          message: f?.message ?? "No se pudo comparar.",
          hint: f?.hint ?? "",
          detail: f?.detail ?? "",
        });
        return;
      }
      setResultado(res);
      setComparadoA(hora(new Date()));
      setFiltro("all");
      // La selección se conserva entre corridas si la diferencia sigue: volver
      // a comparar es lo que uno hace después de arreglar algo, y perder el
      // lugar en la lista cada vez obliga a buscarlo de nuevo.
      const difs = res.result?.differences ?? [];
      setSeleccion((prev) =>
        prev !== null && difs.some((d) => d.id === prev) ? prev : (difs[0]?.id ?? null),
      );
    } catch (err) {
      if (cancelado(err)) return;
      setFallo({ message: textoDe(err), hint: "", detail: "" });
    } finally {
      if (enVuelo.current === handle) enVuelo.current = null;
      setComparando(null);
    }
  }

  function cambiarLados() {
    enVuelo.current?.cancelar();
    setResultado(null);
    setFallo(null);
    setSeleccion(null);
  }

  const diferencias = resultado?.result?.differences ?? [];
  const sentencias = resultado?.statements ?? {};
  const cuentas = {
    [Lado.SoloEnOrigen]: diferencias.filter((d) => d.side === Lado.SoloEnOrigen).length,
    [Lado.Distinto]: diferencias.filter((d) => d.side === Lado.Distinto).length,
    [Lado.SoloEnDestino]: diferencias.filter((d) => d.side === Lado.SoloEnDestino).length,
  };
  const visibles = filtro === "all" ? diferencias : diferencias.filter((d) => d.side === filtro);
  const elegida = diferencias.find((d) => d.id === seleccion) ?? null;
  const incluidas = diferencias.filter((d) => incluir[d.side as keyof Incluir]);
  const conSentencia = incluidas.filter((d) => sentencias[d.id] !== undefined).length;

  async function generar() {
    if (!resultado) return;
    setErrorMigracion("");
    setAviso("");
    const req = { compareId: resultado.id ?? "", include: incluidas.map((d) => d.id) };
    let ruta: string;
    try {
      ruta = await elegirDestinoSQL(`migracion-${resultado.target.database || "destino"}.sql`);
    } catch (err) {
      setErrorMigracion(textoDe(err));
      return;
    }
    if (ruta === "") return;
    setGuardando(true);
    try {
      const info = await SessionSvc.SaveMigration(req, ruta);
      setAviso(
        `Guardado en ${info.path} · ${info.statements} ${plural(info.statements, "sentencia", "sentencias")}` +
          (info.commented > 0
            ? ` · ${info.commented} ${plural(info.commented, "comentada", "comentadas")}`
            : ""),
      );
    } catch (err) {
      setErrorMigracion(textoDe(err));
    } finally {
      setGuardando(false);
    }
  }

  // El texto para el portapapeles se pide a Go, que es donde está: la pantalla
  // no arma SQL ni junta sentencias. Es el mismo texto que va al archivo.
  async function copiar() {
    if (!resultado) return;
    setErrorMigracion("");
    try {
      const m = await SessionSvc.Migration({
        compareId: resultado.id ?? "",
        include: incluidas.map((d) => d.id),
      });
      await navigator.clipboard.writeText(m.sql);
      setCopiado(true);
      if (timerCopiado.current !== null) window.clearTimeout(timerCopiado.current);
      timerCopiado.current = window.setTimeout(() => setCopiado(false), 1800);
    } catch (err) {
      setErrorMigracion(textoDe(err));
    }
  }

  const listo = resultado !== null && comparando === null;

  return (
    <div className={styles.page}>
      <header className={styles.topbar}>
        <span className={styles.marca}>KANAME</span>
        <span className={styles.divider} />
        <span className={styles.titulo}>Comparar esquemas</span>
        <span className={styles.spacer} />
        {listo ? (
          <>
            <Button size="sm" onClick={cambiarLados}>
              Cambiar los lados
            </Button>
            <Button size="sm" onClick={() => void comparar()}>
              Volver a comparar
            </Button>
          </>
        ) : null}
        <Button size="sm" variant="ghost" onClick={onBack}>
          Volver
        </Button>
      </header>

      {resultado === null ? (
        <Elegir
          opciones={opciones}
          connections={connections}
          origenId={origenId}
          destinoId={destinoId}
          comparando={comparando !== null}
          fallo={fallo}
          onOrigen={setOrigenId}
          onDestino={setDestinoId}
          onComparar={() => void comparar()}
          onCancelar={() => {
            comparando?.cancelar();
            setComparando(null);
          }}
        />
      ) : (
        <>
          <div className={styles.cabecera}>
            <Tarjeta lado={resultado.source} rol="Origen" />
            <span className={styles.flecha} aria-hidden="true">
              →
            </span>
            <Tarjeta lado={resultado.target} rol="Destino" />
            <span className={styles.spacer} />
            <div className={styles.cuentas} aria-label="Resumen de la comparación">
              <Cuenta n={cuentas[Lado.SoloEnOrigen]} tono={styles.masOrigen} que="solo en el origen" />
              <Cuenta n={cuentas[Lado.Distinto]} tono={styles.distinto} que="distintas" />
              <Cuenta n={cuentas[Lado.SoloEnDestino]} tono={styles.masDestino} que="solo en el destino" />
            </div>
          </div>

          <div className={styles.filtros}>
            <div role="tablist" aria-label="Filtrar diferencias" className={styles.chips}>
              <Chip activo={filtro === "all"} onClick={() => setFiltro("all")}>
                Todas {diferencias.length}
              </Chip>
              <Chip activo={filtro === Lado.SoloEnOrigen} onClick={() => setFiltro(Lado.SoloEnOrigen)}>
                Solo en el origen {cuentas[Lado.SoloEnOrigen]}
              </Chip>
              <Chip activo={filtro === Lado.Distinto} onClick={() => setFiltro(Lado.Distinto)}>
                Distintas {cuentas[Lado.Distinto]}
              </Chip>
              <Chip activo={filtro === Lado.SoloEnDestino} onClick={() => setFiltro(Lado.SoloEnDestino)}>
                Solo en el destino {cuentas[Lado.SoloEnDestino]}
              </Chip>
            </div>
            <span className={styles.spacer} />
            <span className={styles.meta}>
              {comparando ? "comparando de nuevo…" : `comparado a las ${comparadoA}`} · solo
              catálogos, sin datos
            </span>
          </div>

          {fallo ? <Fallo fallo={fallo} /> : null}

          <div className={styles.cuerpo}>
            <div className={styles.lista} role="listbox" aria-label="Diferencias">
              {diferencias.length === 0 ? (
                <div className={styles.vacio}>
                  <p className={styles.vacioTitulo}>Los dos catálogos coinciden en lo que se comparó.</p>
                  <p className={styles.vacioNota}>
                    Lo que NO se comparó está a la derecha. Un «no hay diferencias» sobre eso no
                    se puede afirmar desde acá.
                  </p>
                </div>
              ) : visibles.length === 0 ? (
                <div className={styles.vacio}>
                  <p className={styles.vacioNota}>Nada con este filtro.</p>
                </div>
              ) : (
                visibles.map((d) => (
                  <button
                    key={d.id}
                    type="button"
                    role="option"
                    aria-selected={d.id === seleccion}
                    className={cx(styles.fila, d.id === seleccion && styles.filaElegida)}
                    onClick={() => setSeleccion(d.id)}
                    title={d.summary}
                  >
                    <span className={cx(styles.marca2, tonoDeLado(d.side))} aria-hidden="true">
                      {marcaDe(d.side)}
                    </span>
                    <span className={cx(styles.clase, claseTono(d.kind))}>{claseEtiqueta(d.kind)}</span>
                    <span className={styles.objeto}>{nombreCompleto(d)}</span>
                    <span className={styles.resumen}>{d.summary}</span>
                  </button>
                ))
              )}
            </div>

            <aside className={styles.panel}>
              {elegida ? (
                <Detalle
                  d={elegida}
                  sentencia={sentencias[elegida.id]}
                  origen={resultado.source}
                  destino={resultado.target}
                />
              ) : (
                <NoComparado items={resultado.result?.notCompared ?? []} />
              )}

              <div className={styles.migracion}>
                <div className={styles.rotulo}>Incluir en la migración</div>
                <div className={styles.incluir}>
                  <Checkbox
                    checked={incluir[Lado.SoloEnOrigen]}
                    onChange={(v) => setIncluir((p) => ({ ...p, [Lado.SoloEnOrigen]: v }))}
                  >
                    Lo que está solo en el origen
                    <span className={styles.cuentaChica}>{cuentas[Lado.SoloEnOrigen]}</span>
                  </Checkbox>
                  <Checkbox
                    checked={incluir[Lado.Distinto]}
                    onChange={(v) => setIncluir((p) => ({ ...p, [Lado.Distinto]: v }))}
                  >
                    Lo que es distinto
                    <span className={styles.cuentaChica}>{cuentas[Lado.Distinto]}</span>
                  </Checkbox>
                  <Checkbox
                    checked={incluir[Lado.SoloEnDestino]}
                    onChange={(v) => setIncluir((p) => ({ ...p, [Lado.SoloEnDestino]: v }))}
                  >
                    Lo que está solo en el destino
                    <span className={styles.cuentaChica}>
                      {cuentas[Lado.SoloEnDestino]} · sin sentencia, como comentario
                    </span>
                  </Checkbox>
                </div>

                {elegida && resultado.result?.notCompared?.length ? (
                  <details className={styles.noComparadoPlegado}>
                    <summary>Qué no se comparó</summary>
                    <NoComparado items={resultado.result.notCompared} sinTitulo />
                  </details>
                ) : null}

                <div className={styles.acciones}>
                  <Button
                    variant="primary"
                    disabled={incluidas.length === 0}
                    loading={guardando}
                    onClick={() => void generar()}
                  >
                    Generar la migración
                    {incluidas.length > 0
                      ? ` · ${conSentencia} ${plural(conSentencia, "sentencia", "sentencias")}`
                      : ""}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={incluidas.length === 0}
                    title="Copiar la migración al portapapeles"
                    onClick={() => void copiar()}
                  >
                    {copiado ? "Copiado" : "Copiar"}
                  </Button>
                </div>
                <p className={styles.pie}>
                  Produce un archivo .sql. Nada corre contra el destino desde esta pantalla.
                </p>
                {aviso ? (
                  <p className={styles.aviso} role="status">
                    {aviso}
                  </p>
                ) : null}
                {errorMigracion ? (
                  <p className={styles.errorChico} role="alert">
                    {errorMigracion}
                  </p>
                ) : null}
              </div>
            </aside>
          </div>

          <footer className={cx(styles.estado, resultado.target.production && styles.estadoProd)}>
            <span className={styles.punto} aria-hidden="true" />
            <span className={styles.estadoTexto}>
              {resultado.source.database} → {resultado.target.database}
            </span>
            <span className={styles.spacer} />
            <span className={styles.meta}>
              {diferencias.length} {plural(diferencias.length, "diferencia", "diferencias")} ·
              comparación de solo lectura
            </span>
          </footer>
        </>
      )}
    </div>
  );
}

/* ------------------------------------------------------------- elegir lados */

function Elegir({
  opciones,
  connections,
  origenId,
  destinoId,
  comparando,
  fallo,
  onOrigen,
  onDestino,
  onComparar,
  onCancelar,
}: {
  opciones: readonly ComboOption[];
  connections: readonly ConnectionView[];
  origenId: string;
  destinoId: string;
  comparando: boolean;
  fallo: { message: string; hint: string; detail: string } | null;
  onOrigen: (id: string) => void;
  onDestino: (id: string) => void;
  onComparar: () => void;
  onCancelar: () => void;
}) {
  const origen = connections.find((c) => c.connection.id === origenId) ?? null;
  const destino = connections.find((c) => c.connection.id === destinoId) ?? null;
  const mismos = origenId !== "" && origenId === destinoId;
  const listo = origen !== null && destino !== null && !mismos;
  const motoresDistintos =
    origen !== null && destino !== null && origen.connection.engine !== destino.connection.engine;

  return (
    <div className={styles.elegir}>
      <div className={styles.elegirCaja}>
        <h1 className={styles.elegirTitulo}>Qué comparar contra qué</h1>
        <p className={styles.elegirNota}>
          El <strong>origen</strong> es lo que se quiere. Las diferencias dicen qué le falta o le
          sobra al <strong>destino</strong> para parecerse, y la migración se escribe para el
          destino. Se leen los dos catálogos y no se ejecuta nada.
        </p>

        <div className={styles.elegirLados}>
          <div className={styles.elegirLado}>
            <span className={styles.rotulo}>Origen</span>
            <Combobox
              value={origenId}
              options={opciones}
              placeholder="Elegí una conexión"
              ariaLabel="Conexión de origen"
              estricto
              disabled={comparando}
              onChange={onOrigen}
            />
            {origen ? <Descripcion view={origen} /> : null}
          </div>
          <span className={styles.flechaGrande} aria-hidden="true">
            →
          </span>
          <div className={styles.elegirLado}>
            <span className={styles.rotulo}>Destino</span>
            <Combobox
              value={destinoId}
              options={opciones}
              placeholder="Elegí una conexión"
              ariaLabel="Conexión de destino"
              estricto
              disabled={comparando}
              onChange={onDestino}
            />
            {destino ? <Descripcion view={destino} /> : null}
          </div>
        </div>

        {mismos ? (
          <p className={styles.errorChico} role="alert">
            Son la misma conexión. Comparar una base consigo misma no dice nada.
          </p>
        ) : null}
        {motoresDistintos ? (
          <p className={styles.elegirAviso}>
            Son motores distintos: se compara qué tablas y columnas hay de cada lado, pero no los
            tipos —cada motor escribe el mismo tipo a su manera— y las sentencias que dependen de
            un tipo quedan sin generar.
          </p>
        ) : null}
        {destino?.connection.environment === "production" ? (
          <p className={styles.elegirAviso}>
            El destino es producción. Esta pantalla solo lee su catálogo: no escribe, no ejecuta,
            y la migración que genera es un archivo que vas a revisar antes de correr.
          </p>
        ) : null}

        {fallo ? <Fallo fallo={fallo} /> : null}

        <div className={styles.elegirAcciones}>
          {comparando ? (
            <>
              <Spinner size="sm" />
              <span className={styles.meta}>Leyendo los dos catálogos…</span>
              <Button size="sm" onClick={onCancelar}>
                Cancelar
              </Button>
            </>
          ) : (
            <Button variant="primary" disabled={!listo} onClick={onComparar}>
              Comparar
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}

function Descripcion({ view }: { view: ConnectionView }) {
  const c = view.connection;
  const problemas = view.problems?.length ?? 0;
  return (
    <div className={styles.descripcion}>
      <span className={cx(styles.puntoEnv, tonoDeEnv(c.environment))} aria-hidden="true" />
      <span className={styles.descripcionTexto}>{c.engine === "sqlite" ? c.database : view.uri}</span>
      {problemas > 0 ? <span className={styles.errorChico}>mal configurada</span> : null}
    </div>
  );
}

/* ----------------------------------------------------------------- piezas */

function Tarjeta({ lado, rol }: { lado: CompareSide; rol: string }) {
  return (
    <div className={cx(styles.tarjeta, tonoDeEnvBorde(lado.environment))}>
      <span className={cx(styles.envChip, tonoDeEnvChip(lado.environment))}>
        {etiquetaEnv(lado.environment)}
      </span>
      <div className={styles.tarjetaTexto}>
        <div className={styles.tarjetaNombre}>
          <span className={styles.tarjetaRol}>{rol}</span> {lado.name}
        </div>
        <div className={styles.tarjetaMeta}>
          {lado.describe} · {nombreDeMotor(lado.engine)} · {lado.tables}{" "}
          {plural(lado.tables, "tabla", "tablas")}
        </div>
      </div>
    </div>
  );
}

function Cuenta({ n, tono, que }: { n: number; tono: string | undefined; que: string }) {
  return (
    <div className={styles.cuenta}>
      <div className={cx(styles.cuentaNumero, tono)}>{n}</div>
      <div className={styles.cuentaQue}>{que}</div>
    </div>
  );
}

function Chip({
  activo,
  onClick,
  children,
}: {
  activo: boolean;
  onClick: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={activo}
      className={cx(styles.chip, activo && styles.chipActivo)}
      onClick={onClick}
    >
      {children}
    </button>
  );
}

function Fallo({ fallo }: { fallo: { message: string; hint: string; detail: string } }) {
  return (
    <div className={styles.fallo} role="alert">
      <div className={styles.falloTitulo}>{fallo.message}</div>
      {fallo.hint ? <div className={styles.falloHint}>{fallo.hint}</div> : null}
      {fallo.detail ? <pre className={styles.falloDetalle}>{fallo.detail}</pre> : null}
    </div>
  );
}

function Detalle({
  d,
  sentencia,
  origen,
  destino,
}: {
  d: Diferencia;
  sentencia: Statement | undefined;
  origen: CompareSide;
  destino: CompareSide;
}) {
  const nota = notaDe(d.risk);
  return (
    <div className={styles.detalle}>
      <div className={styles.detalleCabecera}>
        <span className={styles.detalleObjeto}>{nombreCompleto(d)}</span>
        <span className={styles.spacer} />
        <span className={cx(styles.clase, claseTono(d.kind))}>{claseEtiqueta(d.kind)}</span>
      </div>

      <div className={styles.lados}>
        <LadoCaja titulo={etiquetaEnv(origen.environment)} rol="Origen" texto={d.source} env={origen.environment} />
        <LadoCaja titulo={etiquetaEnv(destino.environment)} rol="Destino" texto={d.target} env={destino.environment} />
      </div>

      <div>
        <div className={styles.rotulo}>
          {sentencia ? "Sentencia para alinear el destino" : "Sin sentencia"}
        </div>
        {sentencia ? (
          <pre className={styles.sql}>{sentencia.sql}</pre>
        ) : (
          <div className={styles.sinSentencia}>{d.noStatement || "No hay sentencia para esta diferencia."}</div>
        )}
      </div>

      {d.note || sentencia?.note ? (
        <div className={cx(styles.nota, nota.clase)}>
          <span className={cx(styles.notaPunto, nota.punto)} aria-hidden="true">
            {nota.glifo}
          </span>
          <div className={styles.notaTexto}>
            {d.note ? <p>{d.note}</p> : null}
            {sentencia?.note ? (
              <p>
                <strong>Antes de correrla:</strong> {sentencia.note}
              </p>
            ) : null}
          </div>
        </div>
      ) : null}
    </div>
  );
}

function LadoCaja({
  titulo,
  rol,
  texto,
  env,
}: {
  titulo: string;
  rol: string;
  texto: string;
  env: string;
}) {
  return (
    <div className={styles.lado}>
      <div className={styles.ladoCabecera}>
        <span className={cx(styles.ladoRol, tonoDeEnvTexto(env))}>{rol}</span>
        <span className={styles.ladoEnv}>{titulo}</span>
      </div>
      <div className={cx(styles.ladoTexto, texto === "" && styles.ladoVacio)}>
        {texto === "" ? "— no está —" : texto}
      </div>
    </div>
  );
}

function NoComparado({ items, sinTitulo = false }: { items: readonly string[]; sinTitulo?: boolean }) {
  return (
    <div className={styles.noComparado}>
      {sinTitulo ? null : <div className={styles.rotulo}>Qué no se comparó</div>}
      <ul className={styles.noComparadoLista}>
        {items.map((t) => (
          <li key={t}>{t}</li>
        ))}
      </ul>
    </div>
  );
}

/* ----------------------------------------------------------------- ayudas */

function marcaDe(l: Lado): string {
  if (l === Lado.SoloEnOrigen) return "+";
  if (l === Lado.SoloEnDestino) return "−";
  return "~";
}

function tonoDeLado(l: Lado): string | undefined {
  if (l === Lado.SoloEnOrigen) return styles.masOrigen;
  if (l === Lado.SoloEnDestino) return styles.masDestino;
  return styles.distinto;
}

function claseEtiqueta(c: Clase): string {
  switch (c) {
    case Clase.ClaseTabla:
      return "TABLA";
    case Clase.ClaseColumna:
      return "COLUMNA";
    case Clase.ClaseForanea:
      return "FK";
    case Clase.ClaseEsquema:
      return "ESQUEMA";
    default:
      return "OBJETO";
  }
}

function claseTono(c: Clase): string | undefined {
  switch (c) {
    case Clase.ClaseTabla:
      return styles.claseTabla;
    case Clase.ClaseEsquema:
      return styles.claseEsquema;
    case Clase.ClaseObjeto:
      return styles.claseObjeto;
    default:
      return undefined;
  }
}

function nombreCompleto(d: Diferencia): string {
  if (d.kind === Clase.ClaseEsquema || d.schema === "") return d.object;
  return `${d.schema}.${d.object}`;
}

function notaDe(r: Riesgo): { clase: string | undefined; punto: string | undefined; glifo: string } {
  if (r === Riesgo.RiesgoAlto) return { clase: styles.notaAlta, punto: styles.notaPuntoAlto, glifo: "✕" };
  if (r === Riesgo.RiesgoMedio) return { clase: styles.notaMedia, punto: styles.notaPuntoMedio, glifo: "!" };
  return { clase: undefined, punto: styles.notaPuntoBajo, glifo: "✓" };
}

function etiquetaEnv(env: string): string {
  switch (env) {
    case "production":
      return "Producción";
    case "staging":
      return "Staging";
    case "dev":
      return "Dev";
    default:
      return "Local";
  }
}

function tonoDeEnv(env: string): string | undefined {
  switch (env) {
    case "production":
      return styles.envProd;
    case "staging":
      return styles.envStage;
    case "dev":
      return styles.envDev;
    default:
      return styles.envLocal;
  }
}
function tonoDeEnvBorde(env: string): string | undefined {
  switch (env) {
    case "production":
      return styles.bordeProd;
    case "staging":
      return styles.bordeStage;
    case "dev":
      return styles.bordeDev;
    default:
      return styles.bordeLocal;
  }
}
function tonoDeEnvChip(env: string): string | undefined {
  switch (env) {
    case "production":
      return styles.chipProd;
    case "staging":
      return styles.chipStage;
    case "dev":
      return styles.chipDev;
    default:
      return styles.chipLocal;
  }
}
function tonoDeEnvTexto(env: string): string | undefined {
  switch (env) {
    case "production":
      return styles.textoProd;
    case "staging":
      return styles.textoStage;
    case "dev":
      return styles.textoDev;
    default:
      return styles.textoLocal;
  }
}

function hora(t: Date): string {
  return t.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}

/**
 * El selector de «guardar como» del sistema, para el .sql.
 *
 * Devuelve la cadena vacía si la persona cancela. Es un diálogo nativo: no se
 * puede probar por CDP, así que su prueba es manual.
 */
async function elegirDestinoSQL(nombre: string): Promise<string> {
  try {
    const ruta = await Dialogs.SaveFile({
      Title: "Guardar la migración",
      Filename: nombre,
      CanCreateDirectories: true,
      AllowsOtherFiletypes: true,
      Filters: [
        { DisplayName: "Archivos SQL", Pattern: "*.sql" },
        { DisplayName: "Todos los archivos", Pattern: "*" },
      ],
    });
    return ruta ?? "";
  } catch (err) {
    if (cancelado(err)) return "";
    throw new Error(`No se pudo abrir el selector de archivos del sistema. (${textoDe(err)})`);
  }
}
