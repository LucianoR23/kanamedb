import { useEffect, useState } from "react";
import { Operator } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import type { Column, Condition, OperatorInfo } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import * as Queries from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import { Button, Input } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { plural } from "../lib/motor";
import { cx } from "../lib/cx";
import { Selector } from "./Selector";
import styles from "./mobile.module.css";

/** Por qué columna y hacia dónde. `null` es el orden de Go: la clave primaria. */
export interface Orden {
  columna: string;
  descendente: boolean;
}

/** Lo que la tabla tiene puesto: orden y condiciones, las dos cosas juntas. */
export interface Vista {
  orden: Orden | null;
  filtros: Condition[];
}

/**
 * M08: filtrar y ordenar una tabla desde el teléfono, a pantalla completa.
 *
 * Es el constructor de filtros de escritorio (`TableFilters`) puesto en
 * vertical y con el orden arriba, porque en el teléfono no hay cabecera de
 * columna que tocar. Las mismas reglas: la columna y el operador salen de
 * listas cerradas —hojas con `Selector`—, el valor viaja como parámetro y
 * nada de lo escrito entra en el texto de la consulta. Las condiciones se
 * combinan con Y.
 */
export function FiltroYOrden({
  tabla,
  columnas,
  vista,
  onAplicar,
  onCerrar,
}: {
  tabla: string;
  columnas: readonly Column[];
  vista: Vista;
  /** Lo elegido y, para el resumen de la tabla, cada condición en palabras. */
  onAplicar: (v: Vista, descripcion: string[]) => void;
  onCerrar: () => void;
}) {
  // Se parte de lo puesto: cambiar un valor es lo más común, y arrancar de
  // cero obligaría a escribir todo de nuevo.
  const [orden, setOrden] = useState<Orden | null>(vista.orden);
  const [borrador, setBorrador] = useState<Condition[]>(vista.filtros.map(clonar));
  const [operadores, setOperadores] = useState<OperatorInfo[]>([]);
  const [errorOperadores, setErrorOperadores] = useState("");

  useEffect(() => {
    let vivo = true;
    void Queries.Operators()
      .then((ops) => {
        if (vivo) setOperadores(ops ?? []);
      })
      .catch((err: unknown) => {
        if (vivo) setErrorOperadores(textoDe(err));
      });
    return () => {
      vivo = false;
    };
  }, []);

  // Sin la lista de operadores —falló el puente— se respeta lo que la
  // condición ya trae, para no dibujar un campo de más que no se puede llenar.
  const cuantosValores = (op: Operator, c?: Condition): number =>
    operadores.find((o) => o.key === op)?.values ?? (c ? (c.values ?? []).length : 1);

  function cambiar(i: number, f: (c: Condition) => Condition) {
    setBorrador((cs) => cs.map((c, j) => (j === i ? f(c) : c)));
  }

  function cambiarOperador(i: number, op: Operator) {
    cambiar(i, (c) => {
      // Al cambiar de operador se recortan o se completan los valores: pasar
      // de «entre» a «es igual a» dejaría uno de más, y el revés uno de menos.
      const n = cuantosValores(op);
      const previos = c.values ?? [];
      const values = n < 0 ? (previos.length > 0 ? previos : [""]) : Array.from({ length: n }, (_, k) => previos[k] ?? "");
      return { ...c, operator: op, values };
    });
  }

  const listo = borrador.every((c) => c.column !== "" && (c.values ?? []).every((v) => v !== null && v !== ""));

  const opcionesDeColumna = columnas.map((c) => ({ value: c.name, tag: c.dataType }));
  // La primera opción es «ninguna»: la lista es cerrada y sin ella no habría
  // forma de volver al orden por clave primaria una vez elegida una columna.
  const opcionesDeOrden = [{ value: "", label: "clave primaria (por defecto)" }, ...opcionesDeColumna];
  const opcionesDeOperador = operadores.map((o) => ({
    value: o.key,
    label: o.label,
    tag: o.values === 0 ? "sin valor" : o.values === 2 ? "2 valores" : o.values < 0 ? "lista" : "1 valor",
  }));

  return (
    <div className={styles.pantalla}>
      <header className={cx(styles.barra, styles.barraConAtras)}>
        <button type="button" className={styles.plano} onClick={onCerrar} aria-label="Cerrar">
          ✕
        </button>
        <div className={styles.tituloCaja}>
          <span className={styles.titulo}>Filtrar y ordenar</span>
        </div>
        <span className={cx(styles.subtitulo, styles.tituloDerecha)}>{tabla}</span>
      </header>

      <main className={cx(styles.cuerpo, styles.cuerpoGap20)}>
        <section className={cx(styles.columna, styles.gap10)}>
          <span className={styles.rotulo}>Ordenar por</span>
          <Selector
            valor={orden?.columna ?? ""}
            opciones={opcionesDeOrden}
            titulo="Columna"
            ariaLabel="Columna para ordenar"
            onChange={(v) => setOrden(v ? { columna: v, descendente: orden?.descendente ?? false } : null)}
          />
          {orden ? (
            <div className={styles.direccion}>
              <Button className={cx(!orden.descendente && styles.elegido)} aria-pressed={!orden.descendente} onClick={() => setOrden({ ...orden, descendente: false })}>
                ↑ Ascendente
              </Button>
              <Button className={cx(orden.descendente && styles.elegido)} aria-pressed={orden.descendente} onClick={() => setOrden({ ...orden, descendente: true })}>
                ↓ Descendente
              </Button>
            </div>
          ) : (
            <span className={styles.ayuda}>Sin elegir, el orden es la clave primaria.</span>
          )}
        </section>

        <div className={styles.divisor} />

        <section className={cx(styles.columna, styles.gap12)}>
          <div className={styles.rotuloFila}>
            <span className={styles.rotulo}>Filtrar</span>
            <span>{borrador.length === 0 ? "sin condiciones" : `${borrador.length} ${plural(borrador.length, "condición", "condiciones")}, con Y`}</span>
          </div>

          {borrador.map((c, i) => {
            const n = cuantosValores(c.operator, c);
            return (
              <div key={i} className={styles.condicion}>
                <div className={styles.condicionCabeza}>
                  <span className={cx(i === 0 && styles.primera)}>{i === 0 ? "Donde" : "Y"}</span>
                  <button type="button" className={styles.quitar} aria-label="Quitar esta condición" onClick={() => setBorrador((cs) => cs.filter((_, j) => j !== i))}>
                    ✕
                  </button>
                </div>
                <Selector
                  valor={c.column}
                  opciones={opcionesDeColumna}
                  titulo="Columna"
                  ariaLabel="Columna"
                  placeholder="columna"
                  onChange={(v) => cambiar(i, (x) => ({ ...x, column: v }))}
                />
                <Selector
                  valor={c.operator}
                  opciones={opcionesDeOperador}
                  titulo="Operador"
                  ariaLabel="Operador"
                  ui
                  onChange={(v) => cambiarOperador(i, v as Operator)}
                />
                {n === 0 ? null : n === -1 ? (
                  <Input
                    className={styles.entradaValor}
                    value={(c.values ?? []).join(", ")}
                    placeholder="uno, otro, otro más"
                    aria-label="Valores separados por coma"
                    autoCapitalize="off"
                    autoCorrect="off"
                    onChange={(e) => cambiar(i, (x) => ({ ...x, values: partirLista(e.target.value) }))}
                  />
                ) : (
                  <div className={styles.valores}>
                    {Array.from({ length: n }, (_, k) => (
                      <Input
                        key={k}
                        className={styles.entradaValor}
                        value={(c.values ?? [])[k] ?? ""}
                        placeholder={n === 2 ? (k === 0 ? "desde" : "hasta") : "valor"}
                        aria-label={n === 2 ? (k === 0 ? "Desde" : "Hasta") : "Valor"}
                        autoCapitalize="off"
                        autoCorrect="off"
                        onChange={(e) => cambiar(i, (x) => ({ ...x, values: (x.values ?? []).map((y, m) => (m === k ? e.target.value : y)) }))}
                      />
                    ))}
                  </div>
                )}
              </div>
            );
          })}

          {errorOperadores ? (
            <div className={cx(styles.aviso, styles.avisoMal)} role="alert">
              <span>No se pudo leer la lista de operadores, así que el desplegable está vacío. {errorOperadores}</span>
            </div>
          ) : null}

          <Button className={styles.agregar} onClick={() => setBorrador((cs) => [...cs, nueva(columnas)])}>
            + Agregar condición
          </Button>
        </section>
      </main>

      <div className={styles.pie}>
        <Button
          variant="ghost"
          className={styles.fijo}
          onClick={() => onAplicar({ orden: null, filtros: [] }, [])}
          disabled={vista.orden === null && vista.filtros.length === 0}
        >
          Quitar todo
        </Button>
        <Button onClick={onCerrar}>Cancelar</Button>
        <Button
          variant="primary"
          disabled={!listo}
          onClick={() =>
            onAplicar(
              { orden, filtros: borrador.map(clonar) },
              borrador.map((c) => describir(c, operadores)),
            )
          }
        >
          Aplicar
        </Button>
      </div>
    </div>
  );
}

function nueva(columnas: readonly Column[]): Condition {
  return { column: columnas[0]?.name ?? "", operator: Operator.OpEq, values: [""] };
}

function clonar(c: Condition): Condition {
  return { ...c, values: [...(c.values ?? [])] };
}

/** Parte «a, b, c» en tres valores. Una coma sola no agrega un valor vacío. */
function partirLista(v: string): string[] {
  const partes = v.split(",").map((s) => s.trim());
  return partes.length > 1 ? partes.filter((s, i) => s !== "" || i === partes.length - 1) : partes;
}

/** «nombre contiene "ana"», para los chips de arriba de la lista. */
function describir(c: Condition, operadores: readonly OperatorInfo[]): string {
  const op = operadores.find((o) => o.key === c.operator);
  const etiqueta = op?.label ?? c.operator;
  const vals = (c.values ?? []).filter((v): v is string => v !== null);
  if (op?.values === 0 || vals.length === 0) return `${c.column} ${etiqueta}`;
  return `${c.column} ${etiqueta} ${vals.map((v) => `"${v}"`).join(" y ")}`;
}
