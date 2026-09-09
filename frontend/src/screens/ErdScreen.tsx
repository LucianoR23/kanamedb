import { useCallback, useEffect, useRef, useState } from "react";
import {
  Background,
  BackgroundVariant,
  ReactFlow,
  ReactFlowProvider,
  useReactFlow,
} from "@xyflow/react";
import type { Connection, NodeChange } from "@xyflow/react";
import "@xyflow/react/dist/base.css";

import type { Snapshot } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import type { Positions } from "../../bindings/github.com/LucianoR23/kanamedb/internal/layout";
import type { ChangesetView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Type as OpType } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import type { Change } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import * as SessionSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/session";
import { Button, ConfirmDialog, Glyph, PillTabs, SearchInput, Toggle } from "../components/ui";
import { TableNode } from "../components/erd/TableNode";
import { RelationEdge } from "../components/erd/RelationEdge";
import { ErdEditPanel } from "./ErdEditPanel";
import { NewTableDialog } from "./NewTableDialog";
import { ColumnEditor } from "./ColumnEditor";
import { acomodar, construirGrafo, idDeTabla } from "../lib/erd";
import type { Grafo, Herramienta, NodoErd } from "../lib/erd";
import { pendientesDe } from "../lib/erdStaged";
import { useStage } from "../lib/useStage";
import { cx } from "../lib/cx";
import styles from "./ErdScreen.module.css";

/* Los tipos se declaran fuera del componente: xyflow compara por identidad y un
   objeto nuevo en cada render le hace recrear todos los nodos. Es la advertencia
   que la propia librería imprime en consola, y acá además tiraría el arrastre a
   la mitad. */
/** Las posiciones sin el `null` del binding: un mapa de Go llega como null
 *  cuando está vacío, y adentro del componente siempre hay un objeto. */
type Posiciones = NonNullable<Positions>;

const TIPOS_DE_NODO = { tabla: TableNode };
const TIPOS_DE_ARISTA = { relacion: RelationEdge };

/**
 * S12 ERD canvas, solo lectura.
 *
 * Dibuja el esquema que ya está en memoria: las claves foráneas viajan en el
 * snapshot justamente para que abrir el diagrama no cueste un viaje a la base
 * por tabla.
 */
export function ErdScreen(props: {
  snapshot: Snapshot | null;
  schema: string;
  /** Tabla a la que ir apenas se abre, si se llegó desde «Ver en el diagrama».
   *  Cambia de valor en cada pedido —aunque sea la misma tabla— para que pedirlo
   *  dos veces vuelva a centrarla. */
  foco: { tabla: string; pedido: number } | null;
  onOpenTable: (schema: string, table: string) => void;
  /** Sin escritura el modo edición ni se ofrece. */
  readOnly: boolean;
  /** Se llama cuando el changeset cambió, para que el Shell actualice su cuenta. */
  onStaged: () => void;
  /** Abre la pantalla de cambios pendientes. */
  onRevisar: () => void;
}) {
  return (
    <ReactFlowProvider>
      <Canvas {...props} />
    </ReactFlowProvider>
  );
}

/** Las herramientas del modo edición y qué hace cada una. */
const HERRAMIENTAS: { id: Herramienta; label: string; hint: string }[] = [
  { id: "select", label: "Elegir", hint: "clic para seleccionar, arrastrar para mover" },
  { id: "column", label: "Agregar columna", hint: "clic en una tabla para agregarle una columna" },
  { id: "table", label: "Tabla nueva", hint: "clic en el lienzo vacío para crear una tabla" },
  { id: "relate", label: "Dibujar relación", hint: "arrastrá de una columna a la que referencia" },
  { id: "drop", label: "Borrar", hint: "clic en una tabla para preparar su borrado" },
];

function Canvas({
  snapshot,
  schema,
  foco,
  onOpenTable,
  readOnly,
  onStaged,
  onRevisar,
}: {
  snapshot: Snapshot | null;
  schema: string;
  foco: { tabla: string; pedido: number } | null;
  onOpenTable: (schema: string, table: string) => void;
  readOnly: boolean;
  onStaged: () => void;
  onRevisar: () => void;
}) {
  const flow = useReactFlow();
  const [todasLasColumnas, setTodasLasColumnas] = useState(true);
  const [tipos, setTipos] = useState(true);
  const [seleccionada, setSeleccionada] = useState<string | null>(null);
  const [ocultas, setOcultas] = useState<ReadonlySet<string>>(new Set());
  const [busqueda, setBusqueda] = useState("");
  const [zoom, setZoom] = useState(1);
  // El panel se pliega: son 292 píxeles que en un esquema grande se extrañan más
  // que el inspector.
  const [panelAbierto, setPanelAbierto] = useState(true);

  // El modo edición es explícito y no un estado en el que se cae: en un diagrama
  // que se está mirando, un clic de más no puede preparar un DROP.
  const [editando, setEditando] = useState(false);
  const [herramienta, setHerramienta] = useState<Herramienta>("select");
  const [changeset, setChangeset] = useState<ChangesetView | null>(null);
  const [tablaNueva, setTablaNueva] = useState<{ x: number; y: number } | null>(null);
  const [columnaEn, setColumnaEn] = useState<string | null>(null);
  const [errorEdicion, setErrorEdicion] = useState("");
  const [descartando, setDescartando] = useState(false);

  const leerChangeset = useCallback(async () => {
    try {
      setChangeset(await SessionSvc.Changeset());
    } catch {
      // Sin changeset el diagrama se dibuja sin marcas. Perder el resaltado
      // molesta; no poder abrir el ERD, más.
    }
  }, []);

  const staging = useStage(() => {
    void leerChangeset();
    onStaged();
  });

  useEffect(() => {
    void leerChangeset();
  }, [leerChangeset]);

  // Manda un cambio al changeset y refresca todo lo que lo muestra.
  const preparar = useCallback(
    async (c: Change) => {
      setErrorEdicion("");
      await staging.stage(c);
    },
    [staging],
  );

  const pendientes = pendientesDe(changeset?.changes ?? [], schema);

  /** Qué significa tocar una tabla, según la herramienta elegida. */
  function alTocarTabla(id: string, nombre: string) {
    if (!editando || herramienta === "select" || herramienta === "relate") {
      setSeleccionada(id);
      return;
    }
    if (herramienta === "column") {
      setColumnaEn(nombre);
      return;
    }
    if (herramienta === "drop") {
      void preparar({
        id: "",
        type: OpType.DropTable,
        schema,
        table: nombre,
        source: "erd",
      } as Change);
    }
  }

  /** Un clic en el lienzo vacío: crea una tabla donde se tocó, o deselecciona. */
  function alTocarLienzo(e: { clientX: number; clientY: number }) {
    if (editando && herramienta === "table") {
      // La posición del clic se convierte a coordenadas del lienzo para que la
      // tarjeta aparezca donde se tocó y no donde dagre decida.
      setTablaNueva(flow.screenToFlowPosition({ x: e.clientX, y: e.clientY }));
      return;
    }
    setSeleccionada(null);
  }

  /**
   * Se soltó un arrastre entre dos columnas: eso es una clave foránea.
   *
   * Los conectores llevan el nombre de la columna en su id, así que de acá sale
   * todo lo que la operación necesita sin adivinar nada.
   */
  function alConectar(c: Connection) {
    const columna = (c.sourceHandle ?? "").replace(/^col:/, "");
    const refColumna = (c.targetHandle ?? "").replace(/^col:/, "");
    if (!columna || !refColumna) {
      setErrorEdicion("Arrastrá desde una columna hasta la columna que referencia.");
      return;
    }
    const origen = nombreDeTabla(c.source);
    const destino = nombreDeTabla(c.target);
    if (!origen || !destino) return;

    void preparar({
      id: "",
      type: OpType.AddForeignKey,
      schema,
      table: origen,
      source: "erd",
      names: [columna],
      refSchema: schema,
      refTable: destino,
      refNames: [refColumna],
      onDelete: "no action",
      onUpdate: "no action",
    } as Change);
  }

  /** "esquema.tabla" → "tabla". */
  function nombreDeTabla(id: string): string {
    const punto = id.indexOf(".");
    return punto < 0 ? id : id.slice(punto + 1);
  }

  // Las posiciones vivas. Arrancan vacías, se llenan con lo que haya guardado y
  // las pisa el arrastre.
  //
  // Se leen del disco y no se reciben por props para que el diagrama sobreviva a
  // cerrar la pestaña, y hasta a cerrar la aplicación: acomodar cuarenta tablas
  // es trabajo que no se puede pedir dos veces.
  const [pos, setPos] = useState<Posiciones>({});
  const [leido, setLeido] = useState(false);
  const encuadrado = useRef(false);

  useEffect(() => {
    let vigente = true;
    void SessionSvc.ErdLayout(schema)
      .then((guardadas) => {
        if (!vigente) return;
        setPos(guardadas ?? {});
      })
      .catch(() => {
        // Un diagrama que no se pudo leer no es motivo para no dibujar: se
        // acomoda solo y el primer arrastre lo vuelve a guardar.
      })
      .finally(() => {
        if (vigente) setLeido(true);
      });
    return () => {
      vigente = false;
    };
  }, [schema]);

  // Guardar es "olvidalo y seguí": si el archivo no se puede escribir, el
  // diagrama en pantalla es correcto igual y avisarlo con un cartel sería peor
  // que el problema.
  const guardar = (p: Posiciones) => {
    void SessionSvc.SaveErdLayout(schema, p).catch(() => {});
  };

  const grafo = construirGrafo(snapshot, schema, {
    todasLasColumnas,
    tipos,
    ocultas,
    seleccionada,
    pendientes,
    editando,
    herramienta,
  });

  // Los nodos con su posición. Las tablas que nunca se movieron ni se
  // acomodaron quedan apiladas en el origen, así que si falta alguna se corre el
  // auto-layout: es mejor que un montón de tarjetas una encima de otra.
  const faltanPosiciones = grafo.nodos.some((n) => !pos[n.id]);
  const nodos: NodoErd[] = (
    faltanPosiciones ? acomodar(grafo.nodos, grafo.aristas) : grafo.nodos
  ).map((n) => ({ ...n, position: pos[n.id] ?? n.position }));

  // El primer encuadre, una sola vez y recién con las posiciones guardadas ya
  // leídas: encuadrar antes deja el diagrama fuera de cuadro apenas llegan.
  // Reencuadrar en cada cambio movería el diagrama debajo del mouse cada vez que
  // se toca un interruptor.
  useEffect(() => {
    if (!leido || encuadrado.current || nodos.length === 0) return;
    encuadrado.current = true;
    void flow.fitView({ padding: 0.14, maxZoom: 1 });
  }, [flow, leido, nodos.length]);

  function alMover(cambios: NodeChange<NodoErd>[]) {
    setPos((prev) => {
      let tocado = false;
      const siguiente = { ...prev };
      for (const c of cambios) {
        if (c.type === "position" && c.position) {
          siguiente[c.id] = { x: c.position.x, y: c.position.y };
          tocado = true;
        }
      }
      return tocado ? siguiente : prev;
    });
  }

  // Guardar al soltar y no en cada píxel del arrastre: son decenas de escrituras
  // a disco por segundo para un dato que solo importa cuando el arrastre terminó.
  const alSoltar = () => guardar(pos);

  function autoLayout() {
    const acomodados = acomodar(grafo.nodos, grafo.aristas);
    const nuevas: Posiciones = {};
    for (const n of acomodados) nuevas[n.id] = n.position;
    setPos(nuevas);
    guardar(nuevas);
    void flow.fitView({ padding: 0.14, maxZoom: 1, duration: 220 });
  }

  function irA(id: string) {
    setSeleccionada(id);
    const n = nodos.find((x) => x.id === id);
    if (n) void flow.setCenter(n.position.x + 126, n.position.y + 60, { zoom, duration: 220 });
  }

  // Ir a la tabla que pidió la pantalla de estructura, una vez por pedido y
  // recién cuando el diagrama ya sabe dónde está cada una: centrar antes de leer
  // las posiciones guardadas apuntaría al origen.
  const ultimoFoco = useRef(0);
  useEffect(() => {
    if (!foco || !leido || foco.pedido === ultimoFoco.current) return;
    const id = idDeTabla(schema, foco.tabla);
    if (!nodos.some((n) => n.id === id)) return;
    ultimoFoco.current = foco.pedido;
    irA(id);
    // irA depende del render actual; incluirlo en las dependencias lo haría
    // correr en cada movimiento del lienzo.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [foco, leido, nodos, schema]);

  const sel = seleccionada ? nodos.find((n) => n.id === seleccionada) : undefined;
  const tablas = (snapshot?.schemas ?? []).find((s) => s.name === schema)?.tables ?? [];
  const filtradas = tablas.filter((t) =>
    t.name.toLowerCase().includes(busqueda.trim().toLowerCase()),
  );

  return (
    <div className={styles.screen}>
      <div className={styles.barra}>
        <label className={styles.interruptor}>
          <Toggle checked={todasLasColumnas} onChange={setTodasLasColumnas} label="Todas las columnas" />
          Todas las columnas
        </label>
        <label className={styles.interruptor}>
          <Toggle checked={tipos} onChange={setTipos} label="Tipos" />
          Tipos
        </label>
        <span className={styles.grow} />
        <Button
          size="sm"
          variant={editando ? "primary" : "ghost"}
          disabled={readOnly}
          title={readOnly ? "La conexión es de solo lectura" : undefined}
          onClick={() => {
            setEditando((e) => !e);
            setHerramienta("select");
          }}
        >
          {editando ? "Salir de edición" : "Editar"}
        </Button>
        <Button size="sm" variant="ghost" onClick={autoLayout}>
          Auto-acomodar
        </Button>
        <span className={styles.divider} />
        <div className={styles.zoom}>
          <button
            type="button"
            className={styles.zoomBtn}
            aria-label="Alejar"
            onClick={() => void flow.zoomOut({ duration: 120 })}
          >
            −
          </button>
          <span className={styles.zoomLabel}>{Math.round(zoom * 100)}%</span>
          <button
            type="button"
            className={styles.zoomBtn}
            aria-label="Acercar"
            onClick={() => void flow.zoomIn({ duration: 120 })}
          >
            +
          </button>
        </div>
      </div>

      {editando ? (
        <div className={styles.herramientas}>
          <PillTabs
            items={HERRAMIENTAS.map((h) => ({ id: h.id, label: h.label }))}
            activeId={herramienta}
            onSelect={(id) => setHerramienta(id as Herramienta)}
            ariaLabel="Herramienta de edición"
          />
          <span className={styles.divider} />
          <span className={styles.pista}>
            {HERRAMIENTAS.find((h) => h.id === herramienta)?.hint}
          </span>
          <span className={styles.grow} />
          {errorEdicion || staging.error ? (
            <span className={styles.errorEdicion}>{errorEdicion || staging.error}</span>
          ) : null}
        </div>
      ) : null}

      <div className={cx(styles.cuerpo, !panelAbierto && styles.cuerpoSolo)}>
        <div className={styles.lienzo}>
          <Marcadores />
          <ReactFlow
            nodes={nodos}
            edges={grafo.aristas}
            nodeTypes={TIPOS_DE_NODO}
            edgeTypes={TIPOS_DE_ARISTA}
            onNodesChange={alMover}
            onNodeDragStop={alSoltar}
            onNodeClick={(_, n) => alTocarTabla(n.id, n.data.name)}
            onNodeDoubleClick={(_, n) => onOpenTable(schema, n.data.name)}
            onPaneClick={(e) => alTocarLienzo(e)}
            onConnect={alConectar}
            onMove={(_, v) => setZoom(v.zoom)}
            minZoom={0.25}
            maxZoom={2}
            nodesConnectable={editando && herramienta === "relate"}
            elementsSelectable={false}
            proOptions={{ hideAttribution: true }}
          >
            <Background variant={BackgroundVariant.Dots} gap={22} size={1} color="var(--border)" />
          </ReactFlow>

          {nodos.length === 0 ? (
            <p className={styles.vacio}>
              El esquema <code>{schema}</code> no tiene tablas para dibujar.
            </p>
          ) : null}

          {panelAbierto ? null : (
            <button
              type="button"
              className={styles.desplegar}
              title="Mostrar el panel"
              aria-label="Mostrar el panel"
              onClick={() => setPanelAbierto(true)}
            >
              &lt;
            </button>
          )}

          <div className={styles.leyenda}>
            <div className={styles.leyendaTitulo}>Referencias</div>
            <div className={styles.leyendaFila}>
              <span className={cx(styles.muestra, styles.muestraNormal)} />
              uno a muchos
            </div>
            <div className={styles.leyendaFila}>
              <span className={cx(styles.muestra, styles.muestraPunteada)} />
              lado opcional
            </div>
            <div className={styles.leyendaFila}>
              <span className={cx(styles.muestra, styles.muestraCascada)} />
              borra en cascada
            </div>
          </div>
        </div>

        <aside className={styles.panel} hidden={!panelAbierto}>
          {/* En modo edición el panel cambia de trabajo: deja de describir lo
              que hay y pasa a mostrar lo que se va a hacer. Tener las dos cosas
              a la vez dejaría al inspector compitiendo con el changeset por el
              mismo espacio, y lo que importa mientras se edita es lo segundo. */}
          {editando ? (
            <ErdEditPanel
              vista={changeset}
              onQuitar={(id) => {
                void SessionSvc.Unstage(id)
                  .then(leerChangeset)
                  .then(onStaged);
              }}
              onDescartar={() => setDescartando(true)}
              onRevisar={onRevisar}
            />
          ) : (
            <>
          <div className={styles.panelCabecera}>
            <span className={styles.panelTitulo}>{sel ? "Tabla" : "Diagrama"}</span>
            <span className={styles.grow} />
            <span className={styles.panelMeta}>
              {sel ? relacionesDe(grafo, sel.id) : `${nodos.length} tablas`}
            </span>
            <button
              type="button"
              className={styles.plegar}
              aria-label="Plegar el panel"
              title="Plegar el panel"
              onClick={() => setPanelAbierto(false)}
            >
              &gt;
            </button>
          </div>

          <div className={styles.panelCuerpo}>
            {sel ? (
              <Inspector
                nodo={sel}
                grafo={grafo}
                onIr={irA}
                onAbrir={() => onOpenTable(schema, sel.data.name)}
                onOcultar={() => {
                  setOcultas((prev) => new Set([...prev, sel.id]));
                  setSeleccionada(null);
                }}
              />
            ) : (
              <>
                <section className={styles.seccion}>
                  <div className={styles.seccionTitulo}>Esquema</div>
                  <dl className={styles.datos}>
                    <dt>tablas</dt>
                    <dd>
                      {nodos.length} de {tablas.length}
                    </dd>
                    <dt>relaciones</dt>
                    <dd>{grafo.aristas.length}</dd>
                    {grafo.fueraDelEsquema > 0 ? (
                      <>
                        <dt>fuera del esquema</dt>
                        <dd title="Apuntan a tablas de otro esquema, que no se dibujan acá.">
                          {grafo.fueraDelEsquema}
                        </dd>
                      </>
                    ) : null}
                  </dl>
                </section>

                <section className={styles.seccion}>
                  <div className={styles.seccionTitulo}>Tablas en el diagrama</div>
                  <SearchInput
                    value={busqueda}
                    placeholder="Buscar una tabla…"
                    onChange={(e) => setBusqueda(e.currentTarget.value)}
                  />
                  <div className={styles.lista}>
                    {filtradas.map((t) => {
                      const id = idDeTabla(schema, t.name);
                      const escondida = ocultas.has(id);
                      return (
                        <button
                          key={t.name}
                          type="button"
                          className={cx(styles.item, escondida && styles.itemOculto)}
                          onClick={() =>
                            escondida
                              ? setOcultas((prev) => {
                                  const s = new Set(prev);
                                  s.delete(id);
                                  return s;
                                })
                              : irA(id)
                          }
                          title={escondida ? "Escondida del diagrama. Clic para traerla." : t.name}
                        >
                          <Glyph kind="table" />
                          <span className={styles.itemNombre}>{t.name}</span>
                          <span className={styles.itemMeta}>
                            {escondida
                              ? "escondida"
                              : t.rowEstimate >= 0
                                ? `≈ ${t.rowEstimate.toLocaleString("es")}`
                                : ""}
                          </span>
                        </button>
                      );
                    })}
                    {filtradas.length === 0 ? (
                      <p className={styles.sinResultados}>Ninguna tabla coincide.</p>
                    ) : null}
                  </div>
                </section>

                <p className={styles.nota}>
                  Las posiciones se guardan por conexión y esquema, al lado de la libreta de
                  conexiones. Nunca se escribe nada en la base.
                </p>
              </>
            )}
          </div>
            </>
          )}
        </aside>
      </div>

      {staging.dialogo}

      <ConfirmDialog
        open={descartando}
        severidad="aviso"
        title={`¿Descartar ${changeset?.summary.total ?? 0} cambios?`}
        etiqueta="Descartar todo"
        onClose={() => setDescartando(false)}
        onConfirm={() => {
          setDescartando(false);
          void SessionSvc.DiscardChanges().then(leerChangeset).then(onStaged);
        }}
      >
        Se vacía la lista de cambios preparados. <strong>No se pierde ningún dato</strong> —nada
        se aplicó todavía—, pero las ediciones hay que volver a hacerlas.
      </ConfirmDialog>

      {tablaNueva ? (
        <NewTableDialog
          onCerrar={() => setTablaNueva(null)}
          onCrear={(t) => {
            const donde = tablaNueva;
            setTablaNueva(null);
            void preparar({
              id: "",
              type: OpType.CreateTable,
              schema,
              table: t.name,
              source: "erd",
              columns: [{ name: t.pkName, dataType: t.pkType, nullable: false }],
              names: [t.pkName],
            } as Change).then(() => {
              // La tarjeta aparece donde se hizo clic. Si dejara que la acomode
              // el layout, la tabla recién creada saltaría a cualquier lado y
              // habría que buscarla.
              if (donde) {
                setPos((prev) => ({ ...prev, [idDeTabla(schema, t.name)]: donde }));
              }
            });
          }}
        />
      ) : null}

      {columnaEn ? (
        <ColumnEditor
          modo="agregar"
          tabla={columnaEn}
          onCerrar={() => setColumnaEn(null)}
          onGuardar={(v) => {
            const tabla = columnaEn;
            setColumnaEn(null);
            if (!tabla) return;
            void preparar({
              id: "",
              type: OpType.AddColumn,
              schema,
              table: tabla,
              source: "erd",
              column: {
                name: v.name,
                dataType: v.dataType,
                nullable: v.nullable,
                ...(v.default ? { default: v.default } : {}),
                ...(v.comment ? { comment: v.comment } : {}),
              },
            } as Change);
          }}
        />
      ) : null}
    </div>
  );
}

/**
 * Los marcadores de pata de gallo.
 *
 * Van en un SVG propio y no dentro del que dibuja xyflow porque ese se rearma
 * con cada cambio del grafo. Un `<marker>` se referencia por id desde cualquier
 * parte del documento, así que basta con que exista una vez.
 *
 * Los colores son tokens: un marcador resuelve `var(--…)` contra sus PROPIOS
 * ancestros, no contra la línea que lo usa, y este SVG cuelga del árbol de la
 * aplicación. Por eso el tema claro los va a alcanzar igual que a todo lo demás.
 */
function Marcadores() {
  // Pata de gallo de verdad: tres dedos que se abren TOCANDO la caja de la
  // tabla, y el vértice sobre la línea. La forma anterior era la inversa —una
  // flecha apuntando hacia adentro de la tarjeta con las plumas hacia afuera— y
  // por eso se veía como una punta fuera de lugar.
  //
  // El anclaje va en el VÉRTICE (refX 1) y no en los dedos, porque la línea se
  // retira RETIRO_PATA píxeles del borde: así el marcador hace el último tramo
  // en vez de superponerse a una línea que llega igual hasta la caja.
  //
  // `orient="auto-start-reverse"` deja el +x del dibujo apuntando HACIA la
  // tarjeta, así que la x que crece se acerca a la caja: el vértice en 1 y los
  // dedos en 13, doce píxeles más adentro, que es justo el retiro.
  const pata = "M1 7 L13 1 M1 7 L13 7 M1 7 L13 13";
  return (
    <svg className={styles.marcadores} aria-hidden="true">
      <defs>
        {[
          ["kn-muchos", "var(--border-strong)", 1.3],
          ["kn-muchos-activo", "var(--accent)", 1.6],
          ["kn-muchos-cascada", "var(--warning)", 1.6],
          ["kn-muchos-nueva", "var(--success)", 1.8],
          ["kn-muchos-borrando", "var(--danger)", 1.6],
        ].map(([id, color, ancho]) => (
          <marker
            key={id as string}
            id={id as string}
            /* Sin userSpaceOnUse el marcador escala con el grosor de la línea, y
               las de cascada son de 2px: la pata quedaba un 40% más grande solo
               en esas. */
            markerUnits="userSpaceOnUse"
            markerWidth="14"
            markerHeight="14"
            refX="1"
            refY="7"
            /* La pata de gallo va del lado de «muchos», que es la tabla que
               DECLARA la clave, y la línea nace ahí. Sin el reverse el dibujo
               cae para el lado contrario. */
            orient="auto-start-reverse"
          >
            <path
              d={pata}
              stroke={color as string}
              strokeWidth={ancho as number}
              strokeLinecap="round"
              fill="none"
            />
          </marker>
        ))}
      </defs>
    </svg>
  );
}

function Inspector({
  nodo,
  grafo,
  onIr,
  onAbrir,
  onOcultar,
}: {
  nodo: NodoErd;
  grafo: Grafo;
  onIr: (id: string) => void;
  onAbrir: () => void;
  onOcultar: () => void;
}) {
  const relaciones = grafo.aristas.filter((a) => a.source === nodo.id || a.target === nodo.id);

  return (
    <>
      <section className={styles.seccion}>
        <div className={styles.selNombre}>
          <Glyph kind="table" />
          <span>{nodo.data.name}</span>
        </div>
        <dl className={styles.datos}>
          <dt>filas</dt>
          <dd
            title={
              nodo.data.rowEstimate >= 0
                ? "Estimación del planificador, no un conteo."
                : "Nunca se le corrió ANALYZE. No dice nada sobre cuántas filas tiene."
            }
          >
            {nodo.data.rowEstimate >= 0
              ? `≈ ${nodo.data.rowEstimate.toLocaleString("es")}`
              : "sin analizar"}
          </dd>
          <dt>columnas</dt>
          <dd>{nodo.data.columnas.length + nodo.data.ocultas}</dd>
        </dl>
      </section>

      <section className={styles.seccion}>
        <div className={styles.seccionTitulo}>Relaciones</div>
        {relaciones.length === 0 ? (
          <p className={styles.sinResultados}>Esta tabla no está relacionada con ninguna otra.</p>
        ) : (
          <div className={styles.relaciones}>
            {relaciones.map((a) => {
              const fk = a.data!.fk;
              const saliente = a.source === nodo.id;
              const otra = saliente ? a.target : a.source;
              return (
                <button
                  key={a.id}
                  type="button"
                  className={cx(
                    styles.relacion,
                    fk.onDelete === "cascade" && styles.relacionCascada,
                  )}
                  onClick={() => onIr(otra)}
                >
                  <span className={styles.relacionCard}>{saliente ? "∞ → 1" : "1 → ∞"}</span>
                  <span className={styles.relacionOtra}>{otra.split(".")[1]}</span>
                  <span className={styles.relacionDetalle}>
                    {(fk.columns ?? []).join(", ")} → {fk.refTable}.{(fk.refColumns ?? []).join(", ")}
                    {" · al borrar: "}
                    {fk.onDelete}
                  </span>
                </button>
              );
            })}
          </div>
        )}
      </section>

      <section className={styles.seccion}>
        <div className={styles.seccionTitulo}>Acciones</div>
        <div className={styles.acciones}>
          <button type="button" className={styles.accion} onClick={onAbrir}>
            Abrir la tabla
          </button>
          <button type="button" className={styles.accion} onClick={onOcultar}>
            Esconder del diagrama
          </button>
        </div>
      </section>
    </>
  );
}

function relacionesDe(grafo: Grafo, id: string): string {
  const n = grafo.aristas.filter((a) => a.source === id || a.target === id).length;
  return n === 1 ? "1 relación" : `${n} relaciones`;
}
