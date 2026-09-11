import { useRef, useState } from "react";
import type {
  CSSProperties,
  MouseEvent as ReactMouseEvent,
  PointerEvent as ReactPointerEvent,
  ReactNode,
} from "react";
import { Type as OpType } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import type { Change } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import type {
  CheckConstraint,
  DetailColumn,
  ForeignKey,
  Index,
  TableDetail,
  Trigger,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import { Button, ContextMenu, Dialog, Glyph, Spinner, Textarea } from "../components/ui";
import { columnasElegibles, tablasElegibles, sinPendientes } from "../lib/pendientesDeTabla";
import type { PendientesDeTabla } from "../lib/pendientesDeTabla";
import type { MenuAnchor, MenuEntry } from "../components/ui";
import { ColumnEditor } from "./ColumnEditor";
import { ConstraintEditor } from "./ConstraintEditor";
import type { ObjetoNuevo } from "./ConstraintEditor";
import type { ColumnaNueva } from "./ColumnEditor";
import { cx } from "../lib/cx";
import { nombreDeMotor, soportaComentarios } from "../lib/motor";
import styles from "./TableStructure.module.css";

/** Las vistas de estructura que existen, en el orden en que se muestran. */
export type StructureView = "structure" | "indexes" | "keys" | "constraints" | "triggers";

/**
 * S11 Table structure.
 *
 * Cinco vistas sobre el mismo `TableDetail`: columnas, índices, claves,
 * restricciones y triggers. Las columnas se pueden editar: cada acción manda una
 * operación al changeset y NO toca la base — la SQL la escribe Go y se aplica
 * desde la pantalla de cambios pendientes, nunca desde acá.
 *
 * No hay virtualización a propósito. Una tabla con más de cien columnas existe
 * pero es rarísima, y cien filas de grilla las dibuja el navegador sin
 * despeinarse. La grilla de datos sí virtualiza porque ahí las filas son miles.
 */
export function TableStructure({
  view,
  detail,
  loading,
  error,
  readOnly,
  engine,
  tablas,
  pendientes = sinPendientes(),
  onStage,
}: {
  view: StructureView;
  detail: TableDetail | null;
  loading: boolean;
  error: string;
  /** Sin escritura no se ofrece ninguna edición: el menú explica por qué. */
  readOnly: boolean;
  /** El motor de la conexión. Decide qué opciones tienen sentido: SQLite no
   *  guarda comentarios en ningún lado. */
  engine: string;
  /** Las tablas del esquema, para elegir a cuál apunta una clave foránea. */
  tablas: string[];
  /** Lo que el changeset le agrega o le saca a esta tabla y todavía no existe. */
  pendientes?: PendientesDeTabla;
  /** Manda un cambio al changeset. La SQL la escribe Go. */
  onStage: (c: Change) => void;
}) {
  if (error) {
    return (
      <div className={styles.errorBox}>
        <p className={styles.errorMsg}>{error}</p>
      </div>
    );
  }
  if (!detail) {
    return (
      <p className={styles.cargando}>
        {loading ? (
          <>
            <Spinner size="sm" />
            Leyendo el catálogo…
          </>
        ) : (
          "Sin datos."
        )}
      </p>
    );
  }

  switch (view) {
    case "structure":
      return (
        <Columnas
          detail={detail}
          columnas={detail.columns ?? []}
          claves={detail.foreignKeys ?? []}
          readOnly={readOnly}
          engine={engine}
          onStage={onStage}
        />
      );
    case "indexes":
      return (
        <Editables
          tipo="index"
          rotulo="un índice"
          detail={detail}
          tablas={tablas}
          pendientes={pendientes}
          readOnly={readOnly}
          onStage={onStage}
        >
          {(borrar) => (
            <Indices
              indices={detail.indexes ?? []}
              {...(readOnly ? {} : { onBorrar: (n: string) => borrar(OpType.DropIndex, n) })}
            />
          )}
        </Editables>
      );
    case "keys":
      return (
        <Editables
          tipo="foreignKey"
          rotulo="una clave foránea"
          detail={detail}
          tablas={tablas}
          pendientes={pendientes}
          readOnly={readOnly}
          onStage={onStage}
        >
          {(borrar) => (
            <Claves
              salientes={detail.foreignKeys ?? []}
              entrantes={detail.referencedBy ?? []}
              tabla={detail.name}
              {...(readOnly ? {} : { onBorrar: (n: string) => borrar(OpType.DropConstraint, n) })}
            />
          )}
        </Editables>
      );
    case "constraints":
      return (
        <Editables
          tipo="check"
          rotulo="una restricción"
          detail={detail}
          tablas={tablas}
          pendientes={pendientes}
          readOnly={readOnly}
          onStage={onStage}
        >
          {(borrar) => (
            <Restricciones
              checks={detail.checks ?? []}
              {...(readOnly ? {} : { onBorrar: (n: string) => borrar(OpType.DropConstraint, n) })}
            />
          )}
        </Editables>
      );
    case "triggers":
      return <Triggers triggers={detail.triggers ?? []} />;
  }
}

/* ---------------------------------------------------------------- columnas */

function Columnas({
  detail,
  columnas,
  claves,
  readOnly,
  engine,
  onStage,
}: {
  detail: TableDetail;
  columnas: DetailColumn[];
  claves: ForeignKey[];
  readOnly: boolean;
  engine: string;
  onStage: (c: Change) => void;
}) {
  const [menu, setMenu] = useState<{ ancla: MenuAnchor; col: DetailColumn } | null>(null);
  const [editor, setEditor] = useState<{
    modo: "agregar" | "renombrar" | "comentar";
    col?: DetailColumn;
  } | null>(null);
  const comentarios = soportaComentarios(engine);
  // El comentario de la TABLA. Ya se podía comentar una columna, y
  // `setTableComment` existía en el changeset y se aplicaba bien: lo único que
  // faltaba era la forma de crearlo. Sin él, la mitad de la operación existía
  // en el modelo y en la pantalla de pendientes pero nada la producía nunca.
  const [comentandoTabla, setComentandoTabla] = useState(false);
  const [textoComentario, setTextoComentario] = useState("");

  // Todo cambio lleva de dónde salió: la pantalla de pendientes lo muestra para
  // poder volver al lugar donde se hizo la edición.
  const base = (extra: Partial<Change>): Change =>
    ({
      id: "",
      type: OpType.AddColumn,
      schema: detail.schema,
      table: detail.name,
      source: "structure",
      ...extra,
    }) as Change;

  const entradas = (c: DetailColumn): MenuEntry[] => [
    { kind: "label", id: "l", label: c.name },
    {
      id: "renombrar",
      label: "Renombrar…",
      disabled: readOnly,
      disabledReason: "solo lectura",
      onSelect: () => setEditor({ modo: "renombrar", col: c }),
    },
    {
      id: "comentar",
      label: c.comment ? "Cambiar el comentario…" : "Comentar…",
      disabled: readOnly || !comentarios,
      disabledReason: readOnly
        ? "solo lectura"
        : `${nombreDeMotor(engine)} no guarda comentarios de columna`,
      onSelect: () => setEditor({ modo: "comentar", col: c }),
    },
    {
      id: "nulos",
      label: c.nullable ? "Exigir que no sea nula" : "Permitir nulos",
      disabled: readOnly,
      disabledReason: "solo lectura",
      onSelect: () =>
        onStage(
          base({
            type: c.nullable ? OpType.SetNotNull : OpType.DropNotNull,
            column: { name: c.name, dataType: c.dataType, nullable: c.nullable },
          }),
        ),
    },
    {
      // Una columna generada guarda su expresión en el mismo lugar del catálogo
      // que un valor por defecto, así que `default` viene lleno y la opción se
      // ofrecía. PostgreSQL la rechaza —«is a generated column»— y el error
      // llegaba recién al aplicar. Lo que se le cambia a una generada es la
      // expresión, que es otra operación y todavía no existe.
      id: "default",
      label: "Sacar el valor por defecto",
      // `generated` es opcional en los bindings: viene `undefined` cuando la
      // columna no es generada, así que la comprobación va por verdadero o
      // falso. Compararlo contra "" deshabilitaría la opción en TODAS.
      disabled: readOnly || c.default === "" || Boolean(c.generated),
      disabledReason: readOnly
        ? "solo lectura"
        : c.generated
          ? "es una columna generada: eso es su expresión, no un default"
          : "no tiene",
      onSelect: () =>
        onStage(
          base({
            type: OpType.DropDefault,
            column: { name: c.name, dataType: c.dataType, nullable: c.nullable },
          }),
        ),
    },
    { kind: "separator", id: "s" },
    {
      id: "borrar",
      label: "Borrar la columna…",
      destructive: true,
      disabled: readOnly,
      disabledReason: "solo lectura",
      onSelect: () =>
        onStage(
          base({
            type: OpType.DropColumn,
            column: { name: c.name, dataType: c.dataType, nullable: c.nullable },
          }),
        ),
    },
  ];

  // A qué apunta cada columna que es clave foránea. Se arma una vez y no por
  // fila: una tabla con veinte columnas y cinco claves compuestas haría cien
  // recorridos para pintar veinte celdas.
  const destino = new Map<string, string>();
  for (const fk of claves) {
    (fk.columns ?? []).forEach((col, i) => {
      const otra = (fk.refColumns ?? [])[i];
      destino.set(col, otra ? `${fk.refTable}.${otra}` : fk.refTable);
    });
  }

  const tabla = (
    <Tabla
      vacia="Esta tabla no tiene columnas."
      filas={columnas.length}
      columnas={["34px", "minmax(160px, 240px)", "minmax(130px, 190px)", "162px", "minmax(130px, 210px)", "minmax(120px, 170px)", "minmax(160px, 1fr)"]}
      encabezados={["", "Nombre", "Tipo", "Nulos", "Default", "Clave", "Comentario"]}
    >
      {columnas.map((c) => (
        <div
          key={c.name}
          className={styles.fila}
          onContextMenu={(e) => {
            e.preventDefault();
            setMenu({ ancla: { x: e.clientX, y: e.clientY }, col: c });
          }}
        >
          <div className={cx(styles.celda, styles.gutter)}>{c.position}</div>
          <div className={cx(styles.celda, styles.nombre)}>
            <span className={styles.mono} title={c.name}>
              {c.name}
            </span>
            {c.identity ? (
              <span className={styles.marca} title={`GENERATED ${c.identity.toUpperCase()} AS IDENTITY`}>
                identity
              </span>
            ) : null}
            {c.generated ? (
              <span
                className={cx(styles.marca, c.generated === "virtual" && styles.marcaAviso)}
                title={
                  c.generated === "virtual"
                    ? "Columna generada VIRTUAL: se calcula al leer, no ocupa lugar. Es de PostgreSQL 18."
                    : "Columna generada STORED: se calcula al escribir y se guarda."
                }
              >
                {c.generated === "virtual" ? "virtual" : "generada"}
              </span>
            ) : null}
          </div>
          <div className={cx(styles.celda, styles.mono)} title={c.dataType}>
            {c.dataType}
          </div>
          <div className={cx(styles.celda, styles.mono, c.nullable && styles.apagado)}>
            {c.nullable ? "null" : "not null"}
            {c.notNullNotValid ? (
              <span
                className={cx(styles.marca, styles.marcaAviso)}
                title={
                  "El NOT NULL se agregó con NOT VALID: las filas que ya estaban nunca se " +
                  "comprobaron y pueden tener nulos. Solo se exige a las nuevas."
                }
              >
                sin validar
              </span>
            ) : null}
          </div>
          <div className={cx(styles.celda, styles.mono, styles.apagado)} title={c.default}>
            {c.default || "—"}
          </div>
          <div className={styles.celda}>
            {c.primaryKey ? <Glyph kind="primaryKey" /> : null}
            {!c.primaryKey && c.foreignKey ? <Glyph kind="foreignKey" /> : null}
            <span className={cx(styles.mono, styles.apagado)}>{destino.get(c.name) ?? ""}</span>
          </div>
          <div className={cx(styles.celda, styles.apagado)} title={c.comment}>
            {c.comment}
          </div>
        </div>
      ))}
    </Tabla>
  );

  return (
    <>
      {tabla}

      <div className={styles.pieAcciones}>
        <button
          type="button"
          className={styles.accion}
          disabled={readOnly}
          title={readOnly ? "La conexión es de solo lectura" : undefined}
          onClick={() => setEditor({ modo: "agregar" })}
        >
          Agregar una columna
        </button>
        <button
          type="button"
          className={styles.accion}
          disabled={readOnly || !comentarios}
          title={
            !comentarios
              ? `${nombreDeMotor(engine)} no guarda comentarios`
              : readOnly
                ? "La conexión es de solo lectura"
                : undefined
          }
          onClick={() => {
            setTextoComentario(detail.comment ?? "");
            setComentandoTabla(true);
          }}
        >
          {detail.comment ? "Cambiar el comentario de la tabla…" : "Comentar la tabla…"}
        </button>
        <span className={styles.pieNota}>
          Clic derecho sobre una columna para el resto. Nada toca la base hasta que se aplique.
        </span>
      </div>

      <ContextMenu
        anchor={menu?.ancla ?? null}
        entries={menu ? entradas(menu.col) : []}
        onClose={() => setMenu(null)}
      />

      <Dialog
        open={comentandoTabla}
        title={`Comentario de ${detail.name}`}
        onClose={() => setComentandoTabla(false)}
        footer={
          <>
            <Button variant="ghost" onClick={() => setComentandoTabla(false)}>
              Cancelar
            </Button>
            <Button
              variant="primary"
              onClick={() => {
                setComentandoTabla(false);
                onStage(base({ type: OpType.SetTableComment, comment: textoComentario.trim() }));
              }}
            >
              Preparar el cambio
            </Button>
          </>
        }
      >
        {/* Vacío BORRA el comentario, y se dice: es lo que significa `COMMENT ON
            … IS NULL`, y alguien que limpia el campo para «dejarlo como estaba»
            estaría haciendo lo contrario de lo que cree. */}
        <p className={styles.pieNota}>
          Dejarlo vacío borra el comentario que tenga. Nada toca la base hasta que se aplique.
        </p>
        <Textarea
          value={textoComentario}
          onChange={(e) => setTextoComentario(e.target.value)}
          rows={4}
          aria-label="Comentario de la tabla"
          autoFocus
        />
      </Dialog>

      {editor ? (
        <ColumnEditor
          modo={editor.modo}
          {...(editor.col ? { columna: editor.col } : {})}
          tabla={detail.name}
          soportaComentarios={comentarios}
          onCerrar={() => setEditor(null)}
          onGuardar={(v: ColumnaNueva) => {
            setEditor(null);
            if (editor.modo === "comentar" && editor.col) {
              onStage(
                base({
                  type: OpType.SetColumnComment,
                  // La definición entera y no solo el nombre: MySQL no tiene
                  // COMMENT ON, así que reescribe la columna con MODIFY —y lo
                  // que no se le repita, lo pierde—.
                  column: {
                    name: editor.col.name,
                    dataType: editor.col.dataType,
                    nullable: editor.col.nullable,
                    ...(editor.col.default ? { default: editor.col.default } : {}),
                  },
                  comment: v.comment,
                }),
              );
              return;
            }
            if (editor.modo === "renombrar" && editor.col) {
              onStage(
                base({
                  type: OpType.RenameColumn,
                  column: {
                    name: editor.col.name,
                    dataType: editor.col.dataType,
                    nullable: editor.col.nullable,
                  },
                  newName: v.name,
                }),
              );
              return;
            }
            onStage(
              base({
                type: OpType.AddColumn,
                column: {
                  name: v.name,
                  dataType: v.dataType,
                  nullable: v.nullable,
                  ...(v.default ? { default: v.default } : {}),
                },
              }),
            );
            // El comentario va como su PROPIO cambio y no adentro del
            // AddColumn. Iba adentro, y ningún motor lo renderizaba: se
            // escribía en el diálogo y se perdía en silencio. Postgres necesita
            // un COMMENT ON aparte de todos modos, y un cambio produce una
            // sentencia; así además se ve en la lista de pendientes.
            if (v.comment) {
              onStage(
                base({
                  type: OpType.SetColumnComment,
                  column: { name: v.name, dataType: v.dataType, nullable: v.nullable },
                  comment: v.comment,
                }),
              );
            }
          }}
        />
      ) : null}
    </>
  );
}

/* ------------------------------------------------- índices, claves, checks */

/**
 * El marco editable de las tres pestañas de objetos.
 *
 * Las tres comparten exactamente lo mismo —un botón para agregar, un diálogo, y
 * un menú para borrar sobre cada fila—, así que vive una vez acá y cada pestaña
 * solo aporta su tabla.
 *
 * `children` es una función y no un nodo porque la tabla necesita recibir el
 * «borrar» que este componente prepara: pasarlo al revés obligaría a cada
 * pestaña a saber cómo se arma un cambio.
 */
function Editables({
  tipo,
  rotulo,
  detail,
  tablas,
  pendientes,
  readOnly,
  onStage,
  children,
}: {
  tipo: ObjetoNuevo;
  rotulo: string;
  detail: TableDetail;
  tablas: string[];
  pendientes: PendientesDeTabla;
  readOnly: boolean;
  onStage: (c: Change) => void;
  children: (borrar: (tipo: OpType, nombre: string) => void) => ReactNode;
}) {
  const [abierto, setAbierto] = useState(false);

  const borrar = (t: OpType, nombre: string) =>
    onStage({
      id: "",
      type: t,
      schema: detail.schema,
      table: detail.name,
      source: "structure",
      name: nombre,
    } as Change);

  return (
    <>
      {children(borrar)}

      <div className={styles.pieAcciones}>
        <button
          type="button"
          className={styles.accion}
          disabled={readOnly}
          title={readOnly ? "La conexión es de solo lectura" : undefined}
          onClick={() => setAbierto(true)}
        >
          Agregar {rotulo}
        </button>
        <span className={styles.pieNota}>
          Clic derecho sobre una fila para borrarla. Nada toca la base hasta que se aplique.
        </span>
      </div>

      {abierto ? (
        <ConstraintEditor
          tipo={tipo}
          schema={detail.schema}
          tabla={detail.name}
          columnas={columnasElegibles(detail.columns ?? [], pendientes)}
          tablas={tablasElegibles(tablas, pendientes)}
          onCerrar={() => setAbierto(false)}
          onGuardar={(c) => {
            setAbierto(false);
            onStage(c);
          }}
        />
      ) : null}
    </>
  );
}

/** Menú de una fila borrable, compartido por las tres pestañas. */
function useMenuBorrar(onBorrar?: (nombre: string) => void) {
  const [menu, setMenu] = useState<{ ancla: MenuAnchor; nombre: string } | null>(null);
  const abrir = (nombre: string) => (e: ReactMouseEvent) => {
    if (!onBorrar) return;
    e.preventDefault();
    setMenu({ ancla: { x: e.clientX, y: e.clientY }, nombre });
  };
  const nodo = (
    <ContextMenu
      anchor={menu?.ancla ?? null}
      entries={
        menu
          ? [
              { kind: "label", id: "l", label: menu.nombre },
              {
                id: "borrar",
                label: "Borrar…",
                destructive: true,
                onSelect: () => onBorrar?.(menu.nombre),
              },
            ]
          : []
      }
      onClose={() => setMenu(null)}
    />
  );
  return { abrir, nodo };
}

/* ----------------------------------------------------------------- índices */

function Indices({ indices, onBorrar }: { indices: Index[]; onBorrar?: (n: string) => void }) {
  const menu = useMenuBorrar(onBorrar);
  return (
    <>
      <Tabla
        vacia="Esta tabla no tiene índices."
        filas={indices.length}
        columnas={["34px", "minmax(190px, 300px)", "96px", "80px", "minmax(220px, 1fr)", "104px", "104px"]}
        encabezados={["", "Índice", "Método", "Único", "Columnas", "Tamaño", "Usos"]}
      >
        {indices.map((ix) => (
          <div
            key={ix.name}
            className={cx(styles.fila, !ix.valid && styles.filaApagada)}
            onContextMenu={menu.abrir(ix.name)}
          >
            <div className={cx(styles.celda, styles.gutter)}>
              <Glyph kind={ix.primary ? "primaryKey" : "index"} />
            </div>
            <div className={cx(styles.celda, styles.nombre)}>
              <span className={cx(styles.recorte, styles.mono)} title={ix.definition}>
                {ix.name}
              </span>
              {ix.valid ? null : (
                <span
                  className={cx(styles.marca, styles.marcaAviso)}
                  title={
                    "Quedó a medio construir, casi siempre por un CREATE INDEX CONCURRENTLY " +
                    "que falló. Ocupa lugar y el planificador no lo usa nunca."
                  }
                >
                  inválido
                </span>
              )}
            </div>
            <div className={cx(styles.celda, styles.mono)}>{ix.method}</div>
            <div className={cx(styles.celda, styles.mono, !ix.unique && styles.apagado)}>
              {ix.unique ? "sí" : "no"}
            </div>
            <div className={cx(styles.celda, styles.mono)} title={columnasDeIndice(ix)}>
              <span className={styles.recorte}>{(ix.columns ?? []).join(", ")}</span>
              {(ix.included ?? []).length > 0 ? (
                <span
                  className={cx(styles.recorte, styles.incluidas)}
                  title="Columnas de INCLUDE: viajan en el índice pero no se puede buscar por ellas."
                >
                  + {(ix.included ?? []).join(", ")}
                </span>
              ) : null}
              {ix.predicate ? (
                <span
                  className={cx(styles.recorte, styles.predicado)}
                  title="Índice parcial: solo indexa las filas que cumplen esta condición."
                >
                  where {ix.predicate}
                </span>
              ) : null}
            </div>
            <div className={cx(styles.celda, styles.mono, styles.apagado)}>{bytes(ix.sizeBytes)}</div>
            <div className={cx(styles.celda, styles.mono, styles.apagado)}>
              {ix.scans < 0 ? (
                "—"
              ) : ix.scans === 0 ? (
                <span
                  className={styles.sinUsar}
                  title={
                    "Ningún escaneo desde el último reset de estadísticas. Puede ser un índice " +
                    "que no sirve para ninguna consulta, o que las estadísticas se reiniciaron hace poco."
                  }
                >
                  0
                </span>
              ) : (
                ix.scans.toLocaleString("es")
              )}
            </div>
          </div>
        ))}
      </Tabla>
      {indices.length > 0 ? (
        <p className={styles.nota}>
          Los tamaños salen del catálogo y no se recalculan. «Usos» son los escaneos que registró{" "}
          <code className={styles.codigo}>pg_stat_user_indexes</code> desde el último reset de
          estadísticas: un cero significa «no se usó desde entonces», no «no se usa nunca».
        </p>
      ) : null}
    </>
  );
}

/** El texto completo para el `title`, cuando la celda recorta.
 *
 * En pantalla las tres partes van en spans distintos y con colores distintos:
 * pegadas en una sola cadena, "puesto where (enviado IS NULL)" se lee como si
 * `where` fuera una columna más. */
function columnasDeIndice(ix: Index): string {
  const partes = [(ix.columns ?? []).join(", ")];
  if ((ix.included ?? []).length > 0) partes.push(`+ ${(ix.included ?? []).join(", ")} (include)`);
  if (ix.predicate) partes.push(`where ${ix.predicate}`);
  return partes.join("  ");
}

/* ------------------------------------------------------------------ claves */

function Claves({
  salientes,
  entrantes,
  tabla,
  onBorrar,
}: {
  salientes: ForeignKey[];
  entrantes: ForeignKey[];
  tabla: string;
  onBorrar?: (n: string) => void;
}) {
  const menu = useMenuBorrar(onBorrar);
  // Las dos direcciones van en la misma lista porque para entender qué pasa al
  // borrar una fila hacen falta las dos: un ON DELETE CASCADE que borra en otra
  // tabla no se ve mirando solo las claves propias.
  const filas = [
    ...salientes.map((fk) => ({ fk, entrante: false })),
    ...entrantes.map((fk) => ({ fk, entrante: true })),
  ];

  return (
    <>
      <Tabla
        vacia="Esta tabla no participa de ninguna clave foránea."
        filas={filas.length}
        columnas={["34px", "minmax(190px, 300px)", "minmax(220px, 1fr)", "120px", "120px", "150px"]}
        encabezados={["", "Restricción", "Referencia", "Al borrar", "Al cambiar", "Diferible"]}
      >
        {filas.map(({ fk, entrante }) => (
          <div
            key={`${fk.schema}.${fk.table}:${fk.name}`}
            className={styles.fila}
            onContextMenu={entrante ? undefined : menu.abrir(fk.name)}
          >
            <div className={cx(styles.celda, styles.gutter)}>
              <Glyph kind="foreignKey" />
            </div>
            <div className={cx(styles.celda, styles.nombre)}>
              <span className={styles.mono} title={fk.name}>
                {fk.name}
              </span>
              {entrante ? (
                <span className={styles.marca} title={`${fk.table} apunta a ${tabla}`}>
                  entrante
                </span>
              ) : null}
              {fk.optional ? (
                <span
                  className={styles.marca}
                  title="Alguna columna admite nulos, así que la fila puede no tener padre."
                >
                  opcional
                </span>
              ) : null}
            </div>
            <div className={cx(styles.celda, styles.mono)} title={referencia(fk, tabla)}>
              {referencia(fk, tabla)}
            </div>
            <div
              className={cx(
                styles.celda,
                styles.mono,
                fk.onDelete === "cascade" && styles.aviso,
              )}
            >
              {fk.onDelete}
            </div>
            <div className={cx(styles.celda, styles.mono, styles.apagado)}>{fk.onUpdate}</div>
            <div className={cx(styles.celda, styles.mono, styles.apagado)}>
              {fk.deferrable || "no"}
            </div>
          </div>
        ))}
      </Tabla>
      {filas.some((f) => f.fk.onDelete === "cascade") ? (
        <p className={styles.nota}>
          Una clave marcada <span className={styles.aviso}>cascade</span> borra filas en la otra
          tabla sin preguntar. Es la que conviene mirar dos veces antes de un <code
            className={styles.codigo}
          >
            DELETE
          </code>
          .
        </p>
      ) : null}
    </>
  );
}

/**
 * "cliente_id → clientes.id" saliendo, "envios.pedido_id → id" entrando.
 *
 * Se califica el lado que NO es la tabla que se está mirando. Sin eso, una clave
 * entrante se lee como si la declarara esta tabla, y con dos tablas distintas
 * apuntando acá con restricciones del mismo nombre —perfectamente legal— las dos
 * filas quedan idénticas.
 */
function referencia(fk: ForeignKey, tabla: string): string {
  const juntar = (cols: string[]) => (cols.length > 1 ? `(${cols.join(", ")})` : cols.join(", "));
  const propia = fk.table === tabla;
  const izq = propia
    ? juntar(fk.columns ?? [])
    : `${fk.table}.${juntar(fk.columns ?? [])}`;
  const der = propia
    ? `${fk.refTable}.${juntar(fk.refColumns ?? [])}`
    : juntar(fk.refColumns ?? []);
  return `${izq} → ${der}`;
}

/* ----------------------------------------------------------- restricciones */

function Restricciones({
  checks,
  onBorrar,
}: {
  checks: CheckConstraint[];
  onBorrar?: (n: string) => void;
}) {
  const menu = useMenuBorrar(onBorrar);
  return (
    <Tabla
      vacia="Esta tabla no tiene restricciones CHECK."
      filas={checks.length}
      columnas={["34px", "minmax(190px, 300px)", "minmax(240px, 1fr)", "120px"]}
      encabezados={["", "Restricción", "Expresión", "Validada"]}
    >
      {checks.map((c) => (
        <div key={c.name} className={styles.fila} onContextMenu={menu.abrir(c.name)}>
          <div className={cx(styles.celda, styles.gutter, styles.mono)}>CK</div>
          <div className={cx(styles.celda, styles.mono)} title={c.name}>
            {c.name}
          </div>
          <div className={cx(styles.celda, styles.mono)} title={c.expression}>
            {c.expression}
          </div>
          <div
            className={cx(styles.celda, styles.mono, c.validated ? styles.ok : styles.aviso)}
            title={
              c.validated
                ? "Se comprobó contra todas las filas."
                : "Se agregó con NOT VALID: las filas que ya estaban nunca se comprobaron."
            }
          >
            {c.validated ? "sí" : "no"}
          </div>
        </div>
      ))}
    </Tabla>
  );
}

/* ---------------------------------------------------------------- triggers */

function Triggers({ triggers }: { triggers: Trigger[] }) {
  return (
    <Tabla
      vacia="Esta tabla no tiene triggers."
      filas={triggers.length}
      columnas={["34px", "minmax(190px, 280px)", "110px", "160px", "96px", "minmax(160px, 1fr)"]}
      encabezados={["", "Trigger", "Momento", "Eventos", "Nivel", "Función"]}
    >
      {triggers.map((tg) => (
        <div key={tg.name} className={cx(styles.fila, !tg.enabled && styles.filaApagada)}>
          <div className={cx(styles.celda, styles.gutter)}>
            <Glyph kind="trigger" />
          </div>
          <div className={cx(styles.celda, styles.nombre)}>
            <span className={styles.mono} title={tg.definition}>
              {tg.name}
            </span>
            {tg.enabled ? null : (
              <span
                className={cx(styles.marca, styles.marcaAviso)}
                title="Está deshabilitado: existe pero no se ejecuta."
              >
                deshabilitado
              </span>
            )}
          </div>
          <div className={cx(styles.celda, styles.mono)}>{tg.timing}</div>
          <div className={cx(styles.celda, styles.mono)}>{(tg.events ?? []).join(", ")}</div>
          <div className={cx(styles.celda, styles.mono, styles.apagado)}>{tg.level}</div>
          <div className={cx(styles.celda, styles.mono)} title={tg.function}>
            {tg.function}
          </div>
        </div>
      ))}
    </Tabla>
  );
}

/* ------------------------------------------------------------------ marco */

/** Lo mínimo que puede quedar una columna al arrastrarla. */
const ANCHO_MINIMO = 48;

/**
 * El marco común de las cinco vistas: encabezado pegajoso, cuerpo scrolleable y
 * columnas que se pueden ensanchar.
 *
 * Las columnas se declaran una vez y valen para el encabezado y para las filas,
 * porque son la misma grilla: si se declararan por separado se desalinearían al
 * primer cambio, que es el error clásico de las tablas hechas con divs.
 *
 * Los anchos por defecto están puestos para que el caso común entre entero, pero
 * un nombre de restricción o una expresión pueden ser de cualquier largo, así
 * que se pueden arrastrar. Se redimensiona en vivo y no al soltar: ver el
 * resultado recién cuando soltás obliga a adivinar. Doble clic vuelve al ancho
 * de fábrica.
 */
function Tabla({
  columnas,
  encabezados,
  filas,
  vacia,
  children,
}: {
  columnas: string[];
  encabezados: string[];
  filas: number;
  vacia: string;
  children: ReactNode;
}) {
  const [anchos, setAnchos] = useState<Record<number, number>>({});
  const celdas = useRef<(HTMLDivElement | null)[]>([]);

  function arrastrar(i: number, e: ReactPointerEvent<HTMLSpanElement>) {
    e.preventDefault();
    e.stopPropagation();
    const celda = celdas.current[i];
    if (!celda) return;
    // El ancho de partida se mide, no se lee de la plantilla: la mayoría son
    // `minmax()` y no hay ningún número que copiar hasta que el navegador la
    // resolvió.
    const inicial = celda.offsetWidth;
    const desde = e.clientX;

    const mover = (ev: PointerEvent) =>
      setAnchos((prev) => ({ ...prev, [i]: Math.max(ANCHO_MINIMO, inicial + ev.clientX - desde) }));
    const soltar = () => {
      window.removeEventListener("pointermove", mover);
      window.removeEventListener("pointerup", soltar);
    };
    window.addEventListener("pointermove", mover);
    window.addEventListener("pointerup", soltar);
  }

  if (filas === 0) {
    return <p className={styles.vacia}>{vacia}</p>;
  }

  const plantilla = columnas.map((def, i) => (anchos[i] ? `${anchos[i]}px` : def)).join(" ");

  return (
    <div className={styles.scroll}>
      <div className={styles.grid} style={{ "--cols": plantilla } as CSSProperties}>
        <div className={cx(styles.fila, styles.cabecera)}>
          {encabezados.map((h, i) => (
            <div
              key={i}
              ref={(n) => {
                celdas.current[i] = n;
              }}
              className={cx(styles.celda, styles.celdaCabecera)}
            >
              <span className={styles.recorte}>{h}</span>
              {/* La última no se arrastra: es la que absorbe el espacio que
                  sobra, y fijarla dejaría un hueco a la derecha. */}
              {i < columnas.length - 1 ? (
                <span
                  className={styles.tirador}
                  role="separator"
                  aria-orientation="vertical"
                  aria-label={`Ancho de ${h || "la primera columna"}`}
                  title="Arrastrá para cambiar el ancho. Doble clic para volver al original."
                  onPointerDown={(e) => arrastrar(i, e)}
                  onDoubleClick={() =>
                    setAnchos((prev) => {
                      const { [i]: _fuera, ...resto } = prev;
                      return resto;
                    })
                  }
                />
              ) : null}
            </div>
          ))}
        </div>
        {children}
      </div>
    </div>
  );
}

/** 182 MB. Se redondea porque nadie decide nada con los bytes exactos. */
export function bytes(n: number): string {
  if (n <= 0) return "0 B";
  const u = ["B", "kB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 && i > 0 ? v.toFixed(1) : Math.round(v)} ${u[i]}`;
}
