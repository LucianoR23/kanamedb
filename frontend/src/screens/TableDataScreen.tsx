import { useCallback, useEffect, useRef, useState } from "react";
import * as QueriesSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import type { Result } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import type { Failure } from "../../bindings/github.com/LucianoR23/kanamedb/internal/engine";
import type {
  Snapshot,
  TableDetail,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import { Button, ConfirmDialog, ContextMenu, Glyph, PillTabs } from "../components/ui";
import type { MenuAnchor, MenuEntry } from "../components/ui";
import { pendientesDeTabla, sinPendientes } from "../lib/pendientesDeTabla";
import { DataGrid } from "../components/DataGrid";
import type { CellRef, GridEdit, SortState } from "../components/DataGrid";
import {
  aGridEdits,
  conBorrada,
  conCelda,
  conCeldaNueva,
  conFilaNueva,
  cuantos,
  hayEdiciones,
  sinCeldaNueva,
  sinEdicion,
  sinFila,
  sinFilaNueva,
} from "../lib/edicion";
import type { Edicion, Valor } from "../lib/edicion";
import { cx } from "../lib/cx";
import { plural } from "../lib/motor";
import { CellViewer } from "./CellViewer";
import { DataReview } from "./DataReview";
import { esCambioDeDatos } from "../lib/cambios";
import type { ChangeView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { TableStructure, bytes } from "./TableStructure";
import { useStage } from "../lib/useStage";
import type { StructureView } from "./TableStructure";
import styles from "./TableDataScreen.module.css";

/** Cuántas filas trae cada página. */
const PAGINA = 500;

/**
 * S10 Table data y S11 Table structure.
 *
 * Son la misma pestaña con sub-pestañas, como en el diseño: quien mira una
 * tabla alterna entre sus datos y su estructura todo el tiempo, y separarlas en
 * dos pestañas del árbol duplicaría la fila abierta.
 *
 * Iteración 2: leer, ordenar, cargar más y contar. Iteración 4: las cinco
 * vistas de estructura, solo lectura. Iteración 7: editar celdas, agregar y
 * borrar filas.
 *
 * Las ediciones viven acá hasta que se preparan. Nada de lo que se escribe en
 * una celda toca la base ni el changeset: recién «Preparar» las manda a Go,
 * que las convierte en cambios y las suma a la lista de pendientes, y desde
 * ahí se revisan y se aplican como cualquier otro cambio.
 */
export function TableDataScreen({
  tabId,
  schema,
  table,
  readOnly,
  engine,
  snapshot,
  recarga,
  onShowInErd,
  onStaged,
  onRevisar,
}: {
  tabId: string;
  schema: string;
  table: string;
  readOnly: boolean;
  /** El motor de la conexión, para las opciones que dependen de él. */
  engine: string;
  snapshot: Snapshot | null;
  /** Sube cada vez que hay que releer: «Refrescar», o un apply que tocó la base. */
  recarga: number;
  onShowInErd: (schema: string, table: string) => void;
  /** Se llama cuando una edición entró al changeset. */
  onStaged: () => void;
  /** Abre la pestaña de cambios pendientes, para «ver la SQL y aplicar». */
  onRevisar: () => void;
}) {
  const [result, setResult] = useState<Result | null>(null);
  const [filas, setFilas] = useState<(string | null)[][]>([]);
  const [total, setTotal] = useState<number | null>(null);
  const [orden, setOrden] = useState<SortState | null>(null);
  const [orderedBy, setOrderedBy] = useState<string[]>([]);
  const [cargando, setCargando] = useState(false);
  const [hayMas, setHayMas] = useState(false);
  const [fallo, setFallo] = useState<Failure | null>(null);
  const [seleccion, setSeleccion] = useState<CellRef | null>(null);
  const [visor, setVisor] = useState<CellRef | null>(null);
  const [sub, setSub] = useState<"data" | StructureView>("data");

  // Lo editado y todavía no preparado. Ver lib/edicion.
  const [edicion, setEdicion] = useState<Edicion>(sinEdicion);
  const [editando, setEditando] = useState<CellRef | null>(null);
  // Cuántas veces se abrió un editor: es la key de la instancia. Ver GridEdit.
  const [editorKey, setEditorKey] = useState(0);
  const [menuCelda, setMenuCelda] = useState<{ ref: CellRef; anchor: MenuAnchor } | null>(null);
  // Lo que se quería hacer y descartaría las ediciones: ordenar por otra
  // columna. Se pregunta antes.
  const [descartarPara, setDescartarPara] = useState<{ accion: () => void; motivo: "ordenar" | "releer" } | null>(
    null,
  );
  // La base cambió —un apply desde otra pestaña, «Refrescar»— mientras acá
  // había ediciones sin preparar. No se releen las filas por encima: se avisa,
  // y releer es una decisión de quien está editando.
  const [releerPendiente, setReleerPendiente] = useState(false);
  const [preparado, setPreparado] = useState(0);
  // La revisión de filas (S08) sobre lo que ya entró al changeset.
  const [revision, setRevision] = useState<ChangeView[] | null>(null);
  // El elemento de la grilla, para enfocarlo después de «Agregar fila».
  const gridRef = useRef<HTMLDivElement | null>(null);
  // Sube de a uno cada vez que hay que llevar la vista al final. Es un pedido y
  // no un booleano porque dos «Agregar fila» seguidos tienen que hacer dos
  // desplazamientos, y un booleano en true no vuelve a disparar el efecto.
  const [irAlFinal, setIrAlFinal] = useState(0);

  // La estructura se lee al abrir la tabla, junto con la primera página de
  // datos.
  //
  // Se probó perezosa —solo al entrar a una sub-pestaña de estructura— y era
  // peor: los contadores de las pestañas y el tamaño del encabezado aparecían
  // de golpe recién después del primer clic, y un número que aparece tarde
  // confunde más que un número que falta. Son siete consultas al catálogo en un
  // solo viaje; cuestan menos que la explicación.
  const [detalle, setDetalle] = useState<TableDetail | null>(null);
  const [detalleCargando, setDetalleCargando] = useState(false);
  const [detalleError, setDetalleError] = useState("");

  // Qué le agrega o le saca el changeset a esta tabla. Sin esto, una columna
  // recién preparada no aparece en el editor de claves ni de índices: el
  // catálogo no la tiene y no la va a tener hasta que se aplique.
  const [pendientes, setPendientes] = useState(sinPendientes);

  // Si la lectura falla se deja lo que había y no se rompe la pantalla: los
  // pendientes son una ayuda para elegir, no el contenido. Quedarse sin poder
  // mirar la estructura porque no se pudo leer el changeset sería peor.
  const leerPendientes = useCallback(async () => {
    try {
      const v = await SessionSvc.Changeset();
      setPendientes(pendientesDeTabla(v.changes ?? [], schema, table));
    } catch {
      /* se conserva lo anterior */
    }
  }, [schema, table]);

  // Preparar un cambio pasa siempre por acá: la confirmación de producción la
  // exige Go y este enganche la contesta.
  const staging = useStage(() => {
    onStaged();
    void leerPendientes();
  });

  const runID = useRef(`${tabId}:data`).current;

  const cargar = useCallback(
    async (offset: number, sort: SortState | null) => {
      setCargando(true);
      setFallo(null);
      const res = await QueriesSvc.TableData({
        runId: runID,
        schema,
        table,
        orderBy: sort ? [sort.column] : [],
        descending: sort?.descending ?? false,
        limit: PAGINA,
        offset,
      });
      setCargando(false);

      if (!res.ok || !res.result) {
        setFallo(res.failure ?? null);
        return;
      }
      const nuevas = (res.result.rows ?? []).map((f) => f ?? []);
      setResult(res.result);
      setOrderedBy(res.orderedBy ?? []);
      // Se pide una página completa: si vino menos, no hay más del otro lado.
      setHayMas(nuevas.length === PAGINA);
      setFilas((previas) => (offset === 0 ? nuevas : [...previas, ...nuevas]));
      // Una lectura desde el principio reemplaza las filas, así que las
      // ediciones —que apuntan a filas por posición— dejan de tener sentido.
      // Ordenar pregunta antes; «Refrescar» y un apply las descartan sin
      // preguntar, porque la base ya cambió y lo editado era sobre la vieja.
      if (offset === 0) {
        setEdicion(sinEdicion());
        setEditando(null);
        setReleerPendiente(false);
      }
    },
    [runID, schema, table],
  );

  useEffect(() => {
    void cargar(0, null);
    // El conteo exacto va aparte y sin esperarlo: en una tabla grande recorre
    // todo, y la grilla tiene que poder mostrar las primeras filas ya.
    void QueriesSvc.TableCount(`${runID}:count`, schema, table).then((r) => {
      if (r.ok) setTotal(r.count);
    });
  }, [cargar, runID, schema, table]);

  // Cuál es la lectura vigente. Dos llamadas superpuestas —doble clic en
  // «Actualizar», o el montaje más un refresco inmediato— se pisan: la primera
  // en resolver apagaba el spinner con la otra todavía en vuelo, y si la vieja
  // llegaba última dejaba datos anteriores con una hora de lectura posterior.
  const pedidoDetalle = useRef(0);

  const leerDetalle = useCallback(async () => {
    const mio = ++pedidoDetalle.current;
    setDetalleCargando(true);
    setDetalleError("");
    try {
      const d = await SessionSvc.TableDetail(schema, table);
      if (mio !== pedidoDetalle.current) return;
      setDetalle(d);
    } catch (err) {
      if (mio !== pedidoDetalle.current) return;
      setDetalleError(err instanceof Error ? err.message : String(err));
    } finally {
      if (mio === pedidoDetalle.current) setDetalleCargando(false);
    }
  }, [schema, table]);

  useEffect(() => {
    void leerDetalle();
    void leerPendientes();
  }, [leerDetalle, leerPendientes]);

  // Releer cuando algo de afuera dice que la base cambió: «Refrescar» en la
  // barra de título, o un apply que dejó sentencias aplicadas.
  //
  // Antes solo se releía el árbol del esquema, así que la columna recién creada
  // no aparecía en la pestaña abierta y había que cerrarla y volver a abrirla.
  // El botón parecía no hacer nada, que es peor que no tenerlo.
  // El contador es lo que distingue «hay que releer» de «se volvió a renderizar
  // por cualquier otra cosa»: el efecto se dispara también cuando cambia el
  // orden, y ahí sale enseguida sin pedir nada.
  const ultimaRecarga = useRef(recarga);

  useEffect(() => {
    if (ultimaRecarga.current === recarga) return;
    ultimaRecarga.current = recarga;
    void leerDetalle();
    void leerPendientes();
    // Con ediciones sin preparar no se pisan las filas: un apply hecho desde
    // OTRA pestaña recarga todas las abiertas, y perder tres celdas escritas a
    // mano por eso sería peor que mostrar la página vieja un rato más. Se
    // avisa en la tira y se relee cuando quien edita lo decida.
    if (hayEdiciones(edicion)) {
      setReleerPendiente(true);
      return;
    }
    void cargar(0, orden);
  }, [recarga, orden, cargar, leerDetalle, leerPendientes, edicion]);

  function ordenarPor(columna: string) {
    const siguiente: SortState =
      orden?.column === columna
        ? { column: columna, descending: !orden.descending }
        : { column: columna, descending: false };
    const ordenar = () => {
      setOrden(siguiente);
      setSeleccion(null);
      void cargar(0, siguiente);
    };
    // Ordenar vuelve a leer desde el principio y las ediciones se pierden.
    // Mejor preguntar que perder tres celdas escritas a mano por un clic en
    // un encabezado.
    if (hayEdiciones(edicion)) setDescartarPara({ accion: ordenar, motivo: "ordenar" });
    else ordenar();
  }

  // El resultado que ve la grilla: las columnas de la primera página más todas
  // las filas acumuladas por "cargar más".
  const acumulado: Result | null = result ? { ...result, rows: filas } : null;
  const columnas = (result?.columns ?? []).map((c) => c.name);

  // Las claves salen del esquema ya introspectado, no de otra consulta: acá se
  // sabe qué tabla se está mirando, así que la información ya está en memoria.
  const claves: Record<string, "pk" | "fk"> = {};
  const clavePrimaria: string[] = [];
  // Las columnas se cuentan en el mismo recorrido: el encabezado las muestra
  // desde que se abre la pestaña, sin esperar a que se lea la estructura.
  // undefined mientras el snapshot no llegó o la tabla no está en él: la pastilla
  // esconde el contador cuando no hay número, y «Estructura 0» sería una cuenta
  // que ninguna tabla puede tener.
  let columnasDeLaTabla: number | undefined;
  // Las tablas del esquema, para que el editor de claves foráneas ofrezca a
  // cuáles se puede apuntar en vez de pedir que se escriba el nombre de memoria.
  const tablasDelEsquema =
    (snapshot?.schemas ?? []).find((s) => s.name === schema)?.tables?.map((t) => t.name) ?? [];
  for (const esq of snapshot?.schemas ?? []) {
    if (esq.name !== schema) continue;
    for (const t of esq.tables ?? []) {
      if (t.name !== table) continue;
      columnasDeLaTabla = (t.columns ?? []).length;
      for (const c of t.columns ?? []) {
        if (c.primaryKey) {
          claves[c.name] = "pk";
          clavePrimaria.push(c.name);
        } else if (c.foreignKey) claves[c.name] = "fk";
      }
    }
  }

  // Sin clave primaria no hay forma de identificar la fila que se edita, y
  // Go también lo rechaza. Acá se dice antes de que alguien escriba algo.
  const porQueNoEdita = readOnly
    ? "La conexión es de solo lectura"
    : clavePrimaria.length === 0
      ? "La tabla no tiene clave primaria: sin ella no se puede identificar la fila que se edita"
      : "";
  const puedeEditar = porQueNoEdita === "";

  const enDatos = sub === "data";
  const filaSeleccionada = seleccion?.row ?? -1;
  const seleccionNueva = filaSeleccionada >= filas.length;
  const seleccionBorrada = edicion.borradas.has(filaSeleccionada);

  /* ---- edición ---------------------------------------------------------- */

  function valorLeido(ref: CellRef): Valor {
    return filas[ref.row]?.[ref.col] ?? null;
  }

  // Cualquier edición nueva apaga el aviso de «N cambios preparados»: ya no
  // describe lo que hay en la tira.
  function ponerValor(ref: CellRef, valor: Valor) {
    setPreparado(0);
    if (ref.row >= filas.length) {
      setEdicion((e) => conCeldaNueva(e, ref.row - filas.length, ref.col, valor));
    } else {
      setEdicion((e) => conCelda(e, ref.row, ref.col, valor, valorLeido(ref)));
    }
  }

  function empezarEdicion(ref: CellRef) {
    if (!puedeEditar || edicion.borradas.has(ref.row)) return;
    setSeleccion(ref);
    setEditando(ref);
    setEditorKey((n) => n + 1);
  }

  function alternarBorrado(fila: number) {
    setPreparado(0);
    if (fila < 0 || !puedeEditar) return;
    if (fila >= filas.length) {
      setEdicion((e) => sinFilaNueva(e, fila - filas.length));
      setSeleccion(null);
      return;
    }
    setEdicion((e) => conBorrada(e, fila, !e.borradas.has(fila)));
  }

  function agregarFila() {
    setPreparado(0);
    if (!puedeEditar) return;
    setEdicion((e) => conFilaNueva(e));
    // La fila nueva queda seleccionada en su primera columna y la grilla con
    // el foco, lista para el Enter que abre el editor. Sin el foco, el Enter
    // volvería a apretar el botón y agregaría otra fila.
    setSeleccion({ row: filas.length + edicion.nuevas.length, col: 0 });
    gridRef.current?.focus({ preventScroll: true });
    setIrAlFinal((n) => n + 1);
  }

  // La fila nueva va al final de lo cargado, que con una página de 500 filas
  // está fuera de la vista: se creaba una fila que no se veía, y el Enter
  // abría el editor en algo que no estaba en pantalla. Se va al fondo y no a
  // un índice porque las filas nuevas son siempre las últimas. El efecto corre
  // después del commit, cuando el alto virtual ya cuenta la fila agregada.
  useEffect(() => {
    if (irAlFinal === 0) return;
    const el = gridRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, [irAlFinal]);

  function preparar() {
    if (!hayEdiciones(edicion)) return;
    const n = cuantos(edicion);
    setEditando(null);
    // Si Go acepta la tanda, deja de ser edición local: son cambios
    // pendientes, y la grilla vuelve a limpio. Si la rechaza, todo queda como
    // estaba y el error se muestra arriba.
    void staging.stageGrid(
      aGridEdits(edicion, filas, columnas, clavePrimaria, schema, table),
      () => {
        setEdicion(sinEdicion());
        setPreparado(n);
      },
    );
  }


  // Abre la revisión de filas sobre los cambios de datos que ya entraron al
  // changeset —los de esta tabla primero, pero todos—.
  async function abrirRevision() {
    try {
      const v = await SessionSvc.Changeset();
      const datos = (v.changes ?? []).filter((x) => esCambioDeDatos(x.change));
      // Sin cambios de datos no hay nada que revisar: si el diálogo estaba
      // abierto —se acaba de descartar la última fila— se cierra.
      setRevision(datos.length === 0 ? null : datos);
    } catch {
      /* si no se puede leer el changeset, el botón no hace nada visible;
         la pestaña de pendientes va a mostrar el error de verdad */
    }
  }

  const edit: GridEdit | undefined = puedeEditar
    ? {
        estado: edicion,
        editando,
        editorKey,
        onEditar: empezarEdicion,
        onConfirmar: (ref, texto) => {
          setEditando(null);
          ponerValor(ref, texto);
        },
        onCancelar: () => setEditando(null),
        onCellMenu: (ref, e) => setMenuCelda({ ref, anchor: { x: e.clientX, y: e.clientY } }),
        gridRef,
      }
    : undefined;

  function entradasDelMenu(ref: CellRef): MenuEntry[] {
    const nueva = ref.row >= filas.length;
    const borrada = edicion.borradas.has(ref.row);
    const editada = !nueva && (edicion.celdas.get(ref.row)?.has(ref.col) ?? false);
    const cargada = nueva && (edicion.nuevas[ref.row - filas.length]?.has(ref.col) ?? false);
    const out: MenuEntry[] = [
      // El visor sigue existiendo en modo edición: el doble clic ahora edita,
      // así que leer un JSON entero o un texto largo se hace desde acá. Una
      // fila nueva no tiene nada que ver todavía.
      {
        id: "ver",
        label: "Ver la celda",
        disabled: nueva,
        onSelect: () => setVisor(ref),
      },
      {
        id: "editar",
        label: "Editar",
        hint: "Enter",
        disabled: borrada,
        ...(borrada ? { disabledReason: "la fila se va a borrar" } : {}),
        onSelect: () => empezarEdicion(ref),
      },
      {
        id: "null",
        label: "Poner NULL",
        disabled: borrada,
        onSelect: () => ponerValor(ref, null),
      },
    ];
    if (editada) {
      out.push({
        id: "volver",
        label: "Volver al valor leído",
        onSelect: () => ponerValor(ref, valorLeido(ref)),
      });
    }
    if (cargada) {
      out.push({
        id: "porDefecto",
        label: "Dejar el valor por defecto",
        onSelect: () => setEdicion((e) => sinCeldaNueva(e, ref.row - filas.length, ref.col)),
      });
    }
    out.push({ kind: "separator", id: "sep" });
    if (nueva) {
      out.push({
        id: "quitar",
        label: "Quitar la fila nueva",
        onSelect: () => alternarBorrado(ref.row),
      });
    } else if (borrada) {
      out.push({
        id: "noBorrar",
        label: "No borrar la fila",
        onSelect: () => alternarBorrado(ref.row),
      });
    } else {
      out.push({
        id: "borrar",
        label: "Borrar la fila",
        destructive: true,
        onSelect: () => alternarBorrado(ref.row),
      });
    }
    return out;
  }

  function teclado(e: React.KeyboardEvent) {
    if (!enDatos || !puedeEditar || editando) return;
    if (!seleccion) return;
    // Solo las teclas que caen en la grilla misma. Un Enter escrito en un
    // diálogo o en el editor de una celda también burbujea hasta acá, y no
    // tiene que abrir nada.
    if (!(e.target instanceof HTMLElement) || e.target.getAttribute("role") !== "grid") return;
    if (e.key === "Enter" || e.key === "F2") {
      e.preventDefault();
      empezarEdicion(seleccion);
    } else if (e.key === "Escape") {
      setSeleccion(null);
    }
  }

  const nEdiciones = cuantos(edicion);

  return (
    <div className={styles.screen} onKeyDown={teclado}>
      <div className={styles.head}>
        <Glyph kind="table" />
        <span className={styles.titulo}>
          {schema}.{table}
        </span>
        <span className={styles.hechos}>{hechos(columnasDeLaTabla, total, detalle)}</span>
        <span className={styles.grow} />
        <Button size="sm" variant="ghost" onClick={() => onShowInErd(schema, table)}>
          Ver en el diagrama
        </Button>
        {!enDatos ? (
          <>
            {detalle ? (
              <span className={styles.leido}>leído a las {hora(detalle.capturedAt)}</span>
            ) : null}
            <Button
              size="sm"
              variant="ghost"
              loading={detalleCargando}
              onClick={() => void leerDetalle()}
            >
              Actualizar
            </Button>
          </>
        ) : null}
      </div>

      <div className={styles.subtabs}>
        <PillTabs
          items={[
            { id: "data", label: "Datos" },
            { id: "structure", label: "Estructura", count: columnasDeLaTabla },
            { id: "indexes", label: "Índices", count: detalle?.indexes?.length },
            {
              id: "keys",
              label: "Claves foráneas",
              count:
                detalle === null
                  ? undefined
                  : (detalle.foreignKeys?.length ?? 0) + (detalle.referencedBy?.length ?? 0),
            },
            { id: "constraints", label: "Restricciones", count: detalle?.checks?.length },
            { id: "triggers", label: "Triggers", count: detalle?.triggers?.length },
          ]}
          activeId={sub}
          onSelect={(id) => setSub(id as "data" | StructureView)}
          ariaLabel="Vistas de la tabla"
        />
        {enDatos ? (
          <>
            <span className={styles.divider} />
            <Button
              size="sm"
              disabled={!puedeEditar || !acumulado}
              title={porQueNoEdita || undefined}
              onClick={agregarFila}
            >
              Agregar fila
            </Button>
            <Button
              size="sm"
              disabled={!puedeEditar || filaSeleccionada < 0}
              title={
                porQueNoEdita ||
                (filaSeleccionada < 0 ? "Elegí una fila primero" : undefined)
              }
              onClick={() => alternarBorrado(filaSeleccionada)}
            >
              {seleccionNueva ? "Quitar fila" : seleccionBorrada ? "No borrar" : "Borrar fila"}
            </Button>
            {nEdiciones > 0 ? (
              <Button size="sm" variant="primary" onClick={preparar}>
                Preparar {nEdiciones} {plural(nEdiciones, "cambio", "cambios")}
              </Button>
            ) : null}
          </>
        ) : null}
        <span className={styles.grow} />
        {enDatos ? <span className={styles.count}>{conteo(filas.length, total, orden)}</span> : null}
      </div>

      {staging.dialogo}
      {staging.error ? <p className={styles.errorStage}>{staging.error}</p> : null}

      {!enDatos ? (
        <TableStructure
          view={sub}
          detail={detalle}
          loading={detalleCargando}
          error={detalleError}
          readOnly={readOnly}
          engine={engine}
          tablas={tablasDelEsquema}
          pendientes={pendientes}
          onStage={(c) => void staging.stage(c)}
        />
      ) : (
        <>
          {orderedBy.length === 0 && filas.length > 0 ? (
            <div className={styles.avisoOrden}>
              Esta tabla no tiene clave primaria, así que el orden de las filas no está
              garantizado: «cargar más» puede repetir o saltear alguna. Ordená por una columna
              para fijarlo.
              {!readOnly ? " Y no se puede editar desde acá: no hay cómo identificar una fila." : ""}
            </div>
          ) : null}

          {fallo ? (
            <div className={styles.error}>
              <p className={styles.errorMsg}>{fallo.message}</p>
              {fallo.detail ? <p className={styles.errorDetalle}>{fallo.detail}</p> : null}
              {fallo.hint ? <p className={styles.errorHint}>{fallo.hint}</p> : null}
            </div>
          ) : acumulado ? (
            <>
              <DataGrid
                result={acumulado}
                selection={seleccion}
                onSelect={setSeleccion}
                onOpenCell={setVisor}
                sort={orden}
                onSort={ordenarPor}
                keys={claves}
                {...(edit ? { edit } : {})}
              />
              <Preparadas
                edicion={edicion}
                filas={filas}
                clave={clavePrimaria}
                columnas={columnas}
                puedeEditar={puedeEditar}
                preparadas={preparado}
                onQuitar={(fila) => setEdicion((e) => sinFila(e, fila))}
                onQuitarNueva={(i) => setEdicion((e) => sinFilaNueva(e, i))}
                onRevisar={() => void abrirRevision()}
                releerPendiente={releerPendiente}
                onReleer={() =>
                  setDescartarPara({
                    accion: () => void cargar(0, orden),
                    motivo: "releer",
                  })
                }
              />
              <div className={styles.foot}>
                {hayMas ? (
                  <Button
                    size="sm"
                    loading={cargando}
                    onClick={() => void cargar(filas.length, orden)}
                  >
                    Cargar {PAGINA} más
                  </Button>
                ) : (
                  <span className={styles.footNota}>
                    {filas.length === 0 ? "La tabla está vacía." : "Se cargaron todas las filas."}
                  </span>
                )}
                <span className={styles.grow} />
                {readOnly ? <span className={styles.roNote}>conexión de solo lectura</span> : null}
              </div>
            </>
          ) : (
            <p className={styles.vacio}>{cargando ? "Leyendo filas…" : "Sin datos."}</p>
          )}
        </>
      )}

      {menuCelda ? (
        <ContextMenu
          anchor={menuCelda.anchor}
          entries={entradasDelMenu(menuCelda.ref)}
          onClose={() => setMenuCelda(null)}
        />
      ) : null}

      <ConfirmDialog
        open={descartarPara !== null}
        severidad="aviso"
        title={`¿Descartar ${nEdiciones} ${plural(nEdiciones, "edición", "ediciones")}?`}
        etiqueta={descartarPara?.motivo === "releer" ? "Descartar y releer" : "Descartar y ordenar"}
        onClose={() => setDescartarPara(null)}
        onConfirm={() => {
          const seguir = descartarPara;
          setDescartarPara(null);
          seguir?.accion();
        }}
      >
        {descartarPara?.motivo === "releer" ? "Releer" : "Ordenar"} vuelve a leer la tabla desde
        el principio, y lo que escribiste en las celdas todavía no está preparado. Se pierde. Si
        querés conservarlo, cancelá y tocá «Preparar» primero.
      </ConfirmDialog>

      {revision ? (
        <DataReview
          cambios={revision}
          onClose={() => setRevision(null)}
          onDiscarded={() => {
            onStaged();
            void abrirRevision();
          }}
          onReviewSql={() => {
            setRevision(null);
            onRevisar();
          }}
        />
      ) : null}

      {visor && acumulado ? (
        <CellViewer
          open
          columns={acumulado.columns ?? []}
          row={filas[visor.row] ?? []}
          index={visor.col}
          rowNumber={visor.row + 1}
          source={`${schema}.${table}`}
          onIndexChange={(i) => setVisor({ row: visor.row, col: i })}
          onClose={() => setVisor(null)}
        />
      ) : null}
    </div>
  );
}

/**
 * La tira de abajo con lo editado, como en el diseño: una pastilla por fila
 * tocada —UPDATE con cuántas columnas, DELETE con su clave, INSERT— y una cruz
 * para deshacerla. Es el resumen que evita tener que recorrer mil filas
 * buscando cuál quedó ámbar.
 */
function Preparadas({
  edicion,
  filas,
  clave,
  columnas,
  puedeEditar,
  preparadas,
  onQuitar,
  onQuitarNueva,
  onRevisar,
  releerPendiente,
  onReleer,
}: {
  edicion: Edicion;
  filas: readonly (readonly Valor[])[];
  clave: readonly string[];
  columnas: readonly string[];
  puedeEditar: boolean;
  /** Cuántos cambios entraron al changeset en la última tanda, para decirlo. */
  preparadas: number;
  onQuitar: (fila: number) => void;
  onQuitarNueva: (indice: number) => void;
  /** Abre la revisión de filas de lo que ya se preparó. */
  onRevisar: () => void;
  /** La base cambió mientras había ediciones: se ofrece releer, con aviso. */
  releerPendiente: boolean;
  onReleer: () => void;
}) {
  if (!puedeEditar) return null;

  const claveDe = (fila: number): string => {
    const partes: string[] = [];
    for (const k of clave) {
      const i = columnas.indexOf(k);
      const v = filas[fila]?.[i];
      partes.push(`${k} ${v === null || v === undefined ? "NULL" : v}`);
    }
    return partes.join(" · ");
  };

  const chips: React.ReactNode[] = [];
  for (const [fila, celdas] of edicion.celdas) {
    chips.push(
      <span key={`u${fila}`} className={cx(styles.chip, styles.chipUpdate)}>
        <span className={styles.chipOp}>UPDATE</span>
        <span className={styles.chipDetalle}>
          {claveDe(fila)} · {celdas.size} {plural(celdas.size, "columna", "columnas")}
        </span>
        <button
          type="button"
          className={styles.chipQuitar}
          title="Deshacer la edición de esta fila"
          onClick={() => onQuitar(fila)}
        >
          ✕
        </button>
      </span>,
    );
  }
  for (const fila of edicion.borradas) {
    chips.push(
      <span key={`d${fila}`} className={cx(styles.chip, styles.chipDelete)}>
        <span className={styles.chipOp}>DELETE</span>
        <span className={styles.chipDetalle}>{claveDe(fila)}</span>
        <button
          type="button"
          className={styles.chipQuitar}
          title="No borrar esta fila"
          onClick={() => onQuitar(fila)}
        >
          ✕
        </button>
      </span>,
    );
  }
  edicion.nuevas.forEach((n, i) => {
    chips.push(
      <span key={`i${i}`} className={cx(styles.chip, styles.chipInsert)}>
        <span className={styles.chipOp}>INSERT</span>
        <span className={styles.chipDetalle}>
          fila nueva · {n.size} {plural(n.size, "columna cargada", "columnas cargadas")}
        </span>
        <button
          type="button"
          className={styles.chipQuitar}
          title="Quitar la fila nueva"
          onClick={() => onQuitarNueva(i)}
        >
          ✕
        </button>
      </span>,
    );
  });

  return (
    <div className={styles.preparadas}>
      <span className={styles.preparadasTitulo}>Ediciones</span>
      {releerPendiente ? (
        <span className={styles.preparadasAviso}>
          La base cambió desde que se leyó esta tabla ·{" "}
          <button type="button" className={styles.enlace} onClick={onReleer}>
            Releer
          </button>{" "}
          (descarta las ediciones)
        </span>
      ) : null}
      {chips.length > 0 ? (
        chips
      ) : (
        <span className={styles.preparadasNota}>
          {preparadas > 0 ? (
            <>
              {preparadas} {plural(preparadas, "cambio preparado", "cambios preparados")} ·{" "}
              <button type="button" className={styles.enlace} onClick={onRevisar}>
                Revisar las filas
              </button>{" "}
              · se aplican desde «Cambios pendientes»
            </>
          ) : (
            "sin ediciones — doble clic en una celda, o «Agregar fila»"
          )}
        </span>
      )}
      <span className={styles.grow} />
      <span className={styles.preparadasAtajos}>
        Enter edita · clic derecho para ver la celda, NULL y borrar
      </span>
    </div>
  );
}

/**
 * "12.481 filas · 182 MB · 7 columnas".
 *
 * El tamaño aparece solo cuando la estructura ya se leyó: es lo único que no
 * está en el snapshot, y pedirlo al abrir la tabla sería una consulta al
 * catálogo por cada pestaña que nadie miró.
 */
function hechos(
  columnas: number | undefined,
  total: number | null,
  detalle: TableDetail | null,
): string {
  const n = (x: number) => x.toLocaleString("es", { useGrouping: true });
  const partes: string[] = [];

  if (total !== null) {
    partes.push(`${n(total)} filas`);
  } else if (detalle && detalle.rowEstimate >= 0) {
    // Es la estimación del planificador, no un conteo. El "≈" es lo que separa
    // "son 12.481" de "el planificador cree que son como 12.481".
    partes.push(`≈ ${n(detalle.rowEstimate)} filas`);
  }

  if (detalle) partes.push(bytes(detalle.totalBytes));
  if (columnas !== undefined && columnas > 0) partes.push(`${n(columnas)} columnas`);
  return partes.join(" · ");
}

/** 09:41. */
function hora(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime())
    ? ""
    : d.toLocaleTimeString("es", { hour: "2-digit", minute: "2-digit" });
}

/**
 * "1.000 de 12.481 filas · id ▲".
 *
 * El total va aparte de las cargadas a propósito: sin el "de N", quien mira mil
 * filas no tiene forma de saber si son todas.
 */
function conteo(cargadas: number, total: number | null, orden: SortState | null): string {
  const n = (x: number) => x.toLocaleString("es", { useGrouping: true });
  const base =
    total === null
      ? `${n(cargadas)} filas cargadas`
      : cargadas >= total
        ? `${n(total)} filas`
        : `${n(cargadas)} de ${n(total)} filas`;
  if (!orden) return base;
  return `${base} · ${orden.column} ${orden.descending ? "▼" : "▲"}`;
}
