import { useState } from "react";
import { Type as OpType } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import type { Change } from "../../bindings/github.com/LucianoR23/kanamedb/internal/change";
import { Button, Checkbox, Combobox, Dialog, Field, Input, DialogClose } from "../components/ui";
import type { ColumnaElegible } from "../lib/pendientesDeTabla";
import { cx } from "../lib/cx";
import styles from "./ConstraintEditor.module.css";

/** Qué se está agregando. */
export type ObjetoNuevo = "index" | "foreignKey" | "check";

/**
 * Alta de índice, clave foránea y restricción CHECK.
 *
 * Los tres en un diálogo porque comparten casi todo —un nombre opcional y una
 * lista de columnas de esta tabla— y separarlos serían tres formularios con las
 * mismas dos terceras partes.
 *
 * El nombre es OPCIONAL a propósito: dejándolo vacío lo elige PostgreSQL, con su
 * convención —`tabla_columna_fkey`— que es la que espera cualquiera que después
 * lea el esquema desde otra herramienta.
 *
 * Las columnas y las tablas incluyen las que están PREPARADAS y todavía no
 * existen, marcadas. El orden natural es crear la columna y después colgarla de
 * otra tabla; si acá solo apareciera lo que ya está en el catálogo, ese orden
 * sería imposible y habría que aplicar dos veces.
 */
export function ConstraintEditor({
  tipo,
  schema,
  tabla,
  columnas,
  tablas,
  onGuardar,
  onCerrar,
}: {
  tipo: ObjetoNuevo;
  schema: string;
  tabla: string;
  columnas: ColumnaElegible[];
  /** Las tablas del esquema, para elegir a cuál apunta una clave foránea. */
  tablas: ColumnaElegible[];
  onGuardar: (c: Change) => void;
  onCerrar: () => void;
}) {
  const [nombre, setNombre] = useState("");
  const [elegidas, setElegidas] = useState<string[]>([]);
  const [unico, setUnico] = useState(false);
  const [donde, setDonde] = useState("");
  const [expresion, setExpresion] = useState("");
  const [refTabla, setRefTabla] = useState("");
  const [refColumnas, setRefColumnas] = useState("");
  const [alBorrar, setAlBorrar] = useState("no action");

  const titulo = {
    index: `Índice sobre ${tabla}`,
    foreignKey: `Clave foránea desde ${tabla}`,
    check: `Restricción sobre ${tabla}`,
  }[tipo];

  const remotas = refColumnas
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);

  const puede =
    tipo === "check"
      ? expresion.trim() !== ""
      : tipo === "index"
        ? elegidas.length > 0
        : elegidas.length > 0 && refTabla.trim() !== "" && remotas.length === elegidas.length;

  function guardar() {
    const base = { id: "", schema, table: tabla, source: "structure" };
    if (tipo === "check") {
      onGuardar({
        ...base,
        type: OpType.AddCheck,
        ...(nombre.trim() ? { name: nombre.trim() } : {}),
        expression: expresion.trim(),
      } as Change);
      return;
    }
    if (tipo === "index") {
      onGuardar({
        ...base,
        type: OpType.AddIndex,
        ...(nombre.trim() ? { name: nombre.trim() } : {}),
        names: elegidas,
        unique: unico,
        ...(donde.trim() ? { where: donde.trim() } : {}),
      } as Change);
      return;
    }
    onGuardar({
      ...base,
      type: OpType.AddForeignKey,
      ...(nombre.trim() ? { name: nombre.trim() } : {}),
      names: elegidas,
      refSchema: schema,
      refTable: refTabla.trim(),
      refNames: remotas,
      onDelete: alBorrar,
      onUpdate: "no action",
    } as Change);
  }

  return (
    <Dialog
      open
      size="md"
      title={titulo}
      onClose={onCerrar}
      footer={
        <>
          <DialogClose variant="secondary" size="sm" onClose={onCerrar}>
            Cancelar
          </DialogClose>
          <Button size="sm" disabled={!puede} onClick={guardar}>
            Preparar
          </Button>
        </>
      }
    >
      <div className={styles.cuerpo}>
        <Field label="Nombre">
          <Input
            value={nombre}
            placeholder="lo elige PostgreSQL si lo dejás vacío"
            onChange={(e) => setNombre(e.currentTarget.value)}
          />
        </Field>

        {tipo === "check" ? (
          <>
            <Field label="Expresión">
              <Input
                value={expresion}
                autoFocus
                placeholder="total >= 0"
                onChange={(e) => setExpresion(e.currentTarget.value)}
              />
            </Field>
            <p className={styles.nota}>
              Se comprueba contra todas las filas que ya están. Si alguna no la cumple, la
              sentencia falla y no se aplica nada.
            </p>
          </>
        ) : (
          <>
            <div className={styles.grupo}>
              <span className={styles.etiqueta}>Columnas</span>
              <div className={styles.columnas}>
                {columnas.map((c) => {
                  const on = elegidas.includes(c.name);
                  return (
                    <button
                      key={c.name}
                      type="button"
                      className={cx(styles.chip, on && styles.chipOn)}
                      title={c.pendiente ? "Preparada: todavía no existe en la base" : undefined}
                      onClick={() =>
                        setElegidas((prev) =>
                          on ? prev.filter((x) => x !== c.name) : [...prev, c.name],
                        )
                      }
                    >
                      {on ? `${elegidas.indexOf(c.name) + 1}. ` : ""}
                      {c.name}
                      {c.pendiente ? <span className={styles.chipPendiente}>pendiente</span> : null}
                    </button>
                  );
                })}
              </div>
              <p className={styles.nota}>
                El orden en que las elegís es el orden de la clave, y para un índice decide para
                qué consultas sirve.
                {columnas.some((c) => c.pendiente)
                  ? " Las marcadas como pendientes todavía no existen: se crean antes que esto, en el mismo apply."
                  : ""}
              </p>
            </div>

            {tipo === "index" ? (
              <>
                <Checkbox checked={unico} onChange={setUnico}>
                  Único
                </Checkbox>
                <Field label="Solo las filas que">
                  <Input
                    value={donde}
                    placeholder="opcional: enviado IS NULL"
                    onChange={(e) => setDonde(e.currentTarget.value)}
                  />
                </Field>
              </>
            ) : (
              <>
                <Field label="Apunta a la tabla">
                  <Combobox
                    value={refTabla}
                    options={tablas.map((t) => ({
                      value: t.name,
                      ...(t.pendiente ? { tag: "pendiente" } : {}),
                    }))}
                    ariaLabel="Tabla referenciada"
                    placeholder="buscá una tabla…"
                    vacio="Ninguna tabla coincide. Se puede escribir el nombre igual."
                    onChange={setRefTabla}
                  />
                </Field>
                <Field label="A las columnas">
                  <Input
                    value={refColumnas}
                    placeholder="id  ·  separadas por coma si son varias"
                    onChange={(e) => setRefColumnas(e.currentTarget.value)}
                  />
                </Field>
                {elegidas.length > 0 && remotas.length > 0 && remotas.length !== elegidas.length ? (
                  <p className={styles.error}>
                    Elegiste {elegidas.length} columnas de esta tabla y {remotas.length} de la
                    otra. Emparejan por posición, así que tienen que ser la misma cantidad.
                  </p>
                ) : null}
                <Field label="Al borrar la fila apuntada">
                  <div className={styles.acciones}>
                    {["no action", "restrict", "cascade", "set null", "set default"].map((a) => (
                      <button
                        key={a}
                        type="button"
                        className={cx(styles.chip, alBorrar === a && styles.chipOn)}
                        onClick={() => setAlBorrar(a)}
                      >
                        {a}
                      </button>
                    ))}
                  </div>
                </Field>
                {alBorrar === "cascade" ? (
                  <p className={styles.aviso}>
                    Con <code>cascade</code>, borrar una fila de {refTabla || "la otra tabla"}{" "}
                    borra las de {tabla} que la apuntan, sin preguntar.
                  </p>
                ) : null}
              </>
            )}
          </>
        )}
      </div>
    </Dialog>
  );
}
