import { useEffect, useState } from "react";
import { Operator } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import type { Column, Condition, OperatorInfo } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import * as QueriesSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import { Button, Combobox, Input } from "../components/ui";
import { cx } from "../lib/cx";
import { textoDe } from "../lib/dialogos";
import { plural } from "../lib/motor";
import styles from "./TableFilters.module.css";

/**
 * El constructor de filtros de una tabla, en la misma pestaña donde se la mira.
 *
 * Cada condición es columna + operador + valor, y varias se combinan con Y.
 * Está acá y no en el editor SQL porque filtrar lo que se está mirando no
 * debería obligar a irse a otra pantalla y escribir un SELECT — que además es
 * la forma más fácil de perder las ediciones sin preparar.
 *
 * Nada de lo que se escribe termina en el texto de la consulta: la columna y el
 * operador salen de listas cerradas y el valor viaja como parámetro. Ver
 * `internal/dml.Where`.
 */
export function TableFilters({
  columns,
  aplicados,
  onAplicar,
}: {
  /** Las columnas de la tabla, para elegir sobre cuál filtrar. */
  columns: readonly Column[];
  /** Lo que está aplicado ahora mismo. */
  aplicados: readonly Condition[];
  onAplicar: (conds: Condition[]) => void;
}) {
  const [abierto, setAbierto] = useState(false);
  const [borrador, setBorrador] = useState<Condition[]>([]);
  const [operadores, setOperadores] = useState<OperatorInfo[]>([]);
  const [errorOperadores, setErrorOperadores] = useState("");

  useEffect(() => {
    let vivo = true;
    // Con `catch`: sin él, un fallo del puente dejaba la lista de operadores
    // VACÍA y sin ningún aviso. Y como el desplegable es estricto, la única
    // señal era un combo que no ofrece nada — un callejón sin salida que parece
    // un cuelgue.
    void QueriesSvc.Operators()
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

  // Al abrir se parte de lo aplicado: editar un filtro puesto es lo más común,
  // y arrancar de cero obligaría a escribirlo de nuevo para cambiar un valor.
  function abrir() {
    setBorrador(aplicados.length > 0 ? aplicados.map(clonar) : [nueva(columns)]);
    setAbierto(true);
  }

  const cuantosValores = (op: Operator): number =>
    operadores.find((o) => o.key === op)?.values ?? 1;

  function cambiarOperador(i: number, op: Operator) {
    setBorrador((cs) =>
      cs.map((c, j) => {
        if (j !== i) return c;
        // Al cambiar de operador se recortan o se completan los valores: pasar
        // de «entre» a «es igual a» dejaría uno de más, y el revés uno de menos.
        const n = cuantosValores(op);
        const previos = c.values ?? [];
        const values =
          n < 0
            ? previos.length > 0
              ? previos
              : [""]
            : Array.from({ length: n }, (_, k) => previos[k] ?? "");
        return { ...c, operator: op, values };
      }),
    );
  }

  function cambiarValor(i: number, k: number, v: string) {
    setBorrador((cs) =>
      cs.map((c, j) =>
        j === i ? { ...c, values: (c.values ?? []).map((x, m) => (m === k ? v : x)) } : c,
      ),
    );
  }

  const listo = borrador.every(
    (c) => c.column !== "" && (c.values ?? []).every((v) => v !== null && v !== ""),
  );

  function aplicar() {
    if (!listo) return;
    onAplicar(borrador.map(clonar));
    setAbierto(false);
  }

  function limpiar() {
    onAplicar([]);
    setBorrador([nueva(columns)]);
    setAbierto(false);
  }

  const opcionesDeColumna = columns.map((c) => ({ value: c.name, tag: c.dataType }));

  return (
    <div className={styles.caja}>
      <button
        type="button"
        className={cx(styles.disparador, aplicados.length > 0 && styles.conFiltro)}
        onClick={() => (abierto ? setAbierto(false) : abrir())}
        aria-expanded={abierto}
      >
        <span className={styles.lupa} aria-hidden="true">
          ⌕
        </span>
        <span className={styles.resumen}>
          {aplicados.length === 0
            ? "Filtrar filas…"
            : aplicados.map((c) => describir(c, operadores)).join(" y ")}
        </span>
      </button>
      {aplicados.length > 0 ? (
        <button
          type="button"
          className={styles.limpiar}
          aria-label="Quitar el filtro"
          title="Quitar el filtro"
          onClick={limpiar}
        >
          ✕
        </button>
      ) : null}

      {abierto ? (
        <div className={styles.panel} role="dialog" aria-label="Filtrar filas">
          {borrador.map((c, i) => {
            const n = cuantosValores(c.operator);
            return (
              <div key={i} className={styles.fila}>
                <span className={styles.juntor}>{i === 0 ? "donde" : "y"}</span>
                <div className={styles.columna}>
                  <Combobox
                    value={c.column}
                    options={opcionesDeColumna}
                    ariaLabel="Columna"
                    placeholder="columna"
                    onChange={(v) =>
                      setBorrador((cs) => cs.map((x, j) => (j === i ? { ...x, column: v } : x)))
                    }
                  />
                </div>
                <div className={styles.operador}>
                  <Combobox
                    value={c.operator}
                    options={operadores.map((o) => ({ value: o.key, label: o.label }))}
                    ariaLabel="Operador"
                    estricto
                    onChange={(v) => cambiarOperador(i, v as Operator)}
                  />
                </div>
                <div className={styles.valores}>
                  {n === 0 ? (
                    <span className={styles.sinValor}>sin valor</span>
                  ) : n === -1 ? (
                    <Input
                      value={(c.values ?? []).join(", ")}
                      placeholder="uno, otro, otro más"
                      aria-label="Valores separados por coma"
                      onChange={(e) =>
                        setBorrador((cs) =>
                          cs.map((x, j) =>
                            j === i ? { ...x, values: partirLista(e.target.value) } : x,
                          ),
                        )
                      }
                    />
                  ) : (
                    Array.from({ length: n }, (_, k) => (
                      <Input
                        key={k}
                        value={(c.values ?? [])[k] ?? ""}
                        placeholder={n === 2 ? (k === 0 ? "desde" : "hasta") : "valor"}
                        aria-label={n === 2 ? (k === 0 ? "Desde" : "Hasta") : "Valor"}
                        onChange={(e) => cambiarValor(i, k, e.target.value)}
                      />
                    ))
                  )}
                </div>
                <button
                  type="button"
                  className={styles.quitar}
                  aria-label="Quitar esta condición"
                  disabled={borrador.length === 1}
                  onClick={() => setBorrador((cs) => cs.filter((_, j) => j !== i))}
                >
                  ✕
                </button>
              </div>
            );
          })}

          {errorOperadores ? (
            <p className={styles.errorOperadores} role="alert">
              No se pudo leer la lista de operadores, así que el desplegable está vacío.{" "}
              {errorOperadores}
            </p>
          ) : null}

          <div className={styles.pie}>
            <button
              type="button"
              className={styles.agregar}
              onClick={() => setBorrador((cs) => [...cs, nueva(columns)])}
            >
              + Agregar condición
            </button>
            <span className={styles.grow} />
            <span className={styles.nota}>
              se combinan con Y · {borrador.length}{" "}
              {plural(borrador.length, "condición", "condiciones")}
            </span>
            <Button size="sm" onClick={() => setAbierto(false)}>
              Cancelar
            </Button>
            <Button size="sm" variant="primary" disabled={!listo} onClick={aplicar}>
              Aplicar
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function nueva(columns: readonly Column[]): Condition {
  return { column: columns[0]?.name ?? "", operator: Operator.OpEq, values: [""] };
}

function clonar(c: Condition): Condition {
  return { ...c, values: [...(c.values ?? [])] };
}

/** Parte «a, b, c» en tres valores. Una coma sola no agrega un valor vacío. */
function partirLista(v: string): string[] {
  const partes = v.split(",").map((s) => s.trim());
  return partes.length > 1 ? partes.filter((s, i) => s !== "" || i === partes.length - 1) : partes;
}

/** «nombre contiene "ana"», para el resumen de la barra. */
function describir(c: Condition, operadores: readonly OperatorInfo[]): string {
  const op = operadores.find((o) => o.key === c.operator);
  const etiqueta = op?.label ?? c.operator;
  const vals = (c.values ?? []).filter((v): v is string => v !== null);
  if (op?.values === 0 || vals.length === 0) return `${c.column} ${etiqueta}`;
  return `${c.column} ${etiqueta} ${vals.map((v) => `"${v}"`).join(" y ")}`;
}
