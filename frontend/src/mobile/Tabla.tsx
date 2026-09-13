import { useEffect, useRef, useState } from "react";
import * as Queries from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import type { SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Table } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import type { Column } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import { Button, Spinner } from "../components/ui";
import type { ToastItem } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { BarraDeSesion } from "./Sesion";
import { Tarjeta } from "./Tarjeta";
import type { Valor } from "./Tarjeta";
import { Fila } from "./Fila";
import { FiltroYOrden } from "./FiltroYOrden";
import type { Vista } from "./FiltroYOrden";
import { HojaDeValor } from "./HojaDeValor";
import type { CampoElegido } from "./HojaDeValor";
import { siLaSesionSeCerro } from "./sesionCerrada";
import { cx } from "../lib/cx";
import styles from "./mobile.module.css";

/** Filas por página. Pocas: cada una es una tarjeta, no una línea. */
const PAGINA = 40;

/**
 * Las filas de una tabla como tarjetas, paginadas con «cargar más». Sin orden
 * elegido lo decide Go (la clave primaria); con uno elegido, la clave va detrás
 * como desempate, para que dos filas iguales en esa columna no cambien de
 * página entre una carga y la siguiente. Sin clave lo dice, porque «cargar
 * más» puede repetir o saltear filas.
 *
 * Tocar una tarjeta abre la fila: ver entera, editar, borrar. Mantener
 * apretado un campo muestra el valor entero con «Copiar». «Nueva fila» abre
 * una vacía. Con la sesión en solo lectura, solo se mira.
 */
export function Tabla({
  sesion,
  esquema,
  tabla,
  onVolver,
  onSesionCerrada,
  onAviso,
}: {
  sesion: SessionView;
  esquema: string;
  tabla: Table;
  onVolver: () => void;
  onSesionCerrada: (motivo: string) => void;
  onAviso: (t: ToastItem) => void;
}) {
  const [columnas, setColumnas] = useState<Column[]>([]);
  const [filas, setFilas] = useState<Valor[][]>([]);
  const [hayMas, setHayMas] = useState(false);
  const [ordenada, setOrdenada] = useState(true);
  const [cargando, setCargando] = useState(false);
  const [error, setError] = useState("");
  // Lo que dijo el motor, debajo del mensaje: con un filtro rechazado es lo
  // que explica cuál.
  const [detalle, setDetalle] = useState("");
  // La fila abierta, por índice en lo cargado; -1 es «nueva».
  const [abierta, setAbierta] = useState<number | null>(null);
  // Cambia para volver a leer desde cero después de aplicar.
  const [version, setVersion] = useState(0);
  // Orden y filtros puestos, y cada filtro en palabras para el resumen.
  const [vista, setVista] = useState<Vista>({ orden: null, filtros: [] });
  const [descripcion, setDescripcion] = useState<string[]>([]);
  const [eligiendo, setEligiendo] = useState(false);
  // El campo que alguien mantuvo apretado, para verlo entero y copiarlo.
  const [campo, setCampo] = useState<CampoElegido | null>(null);
  // La última carga pedida. Una respuesta de una carga anterior —«cargar
  // más» que llega después de un «recargar»— se descarta: si se aplicara,
  // pegaría una página vieja sobre una lista nueva.
  const ultima = useRef(0);

  const clave = new Set((tabla.columns ?? []).filter((c) => c.primaryKey).map((c) => c.name));

  async function cargar(offset: number) {
    const pedido = ++ultima.current;
    setCargando(true);
    setError("");
    setDetalle("");
    try {
      const res = await Queries.TableData({
        runId: crypto.randomUUID(),
        schema: esquema,
        table: tabla.name,
        orderBy: vista.orden ? [vista.orden.columna, ...[...clave].filter((c) => c !== vista.orden?.columna)] : null,
        descending: vista.orden?.descendente ?? false,
        where: vista.filtros.length > 0 ? vista.filtros : null,
        limit: PAGINA,
        offset,
      });
      if (pedido !== ultima.current) return;
      if (!res.ok || !res.result) {
        // Una lectura desde el principio que falla —un filtro que el motor
        // rechaza— no deja las filas de antes debajo de los chips nuevos: se
        // verían filas que no cumplen lo que dice arriba.
        if (offset === 0) {
          setFilas([]);
          setHayMas(false);
        }
        // Sin sesión Go no lanza: devuelve un fallo. Se pregunta igual.
        if (!(await siLaSesionSeCerro(onSesionCerrada))) {
          setError(res.failure?.message ?? "No se pudo leer la tabla.");
          setDetalle(res.failure?.detail ?? "");
        }
        return;
      }
      const nuevas = (res.result.rows ?? []).map((r) => (r ?? []).map((v) => v ?? null));
      setColumnas(res.result.columns ?? []);
      setFilas((prev) => (offset === 0 ? nuevas : [...prev, ...nuevas]));
      setHayMas(nuevas.length === PAGINA);
      setOrdenada((res.orderedBy ?? []).length > 0);
    } catch (err) {
      if (pedido !== ultima.current) return;
      if (offset === 0) {
        setFilas([]);
        setHayMas(false);
      }
      if (!(await siLaSesionSeCerro(onSesionCerrada))) setError(textoDe(err));
    } finally {
      if (pedido === ultima.current) setCargando(false);
    }
  }

  useEffect(() => {
    void cargar(0);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [esquema, tabla.name, version, vista]);

  function aplicarVista(v: Vista, d: string[]) {
    setEligiendo(false);
    setVista(v);
    setDescripcion(d);
  }

  function mantener(f: Valor[], i: number) {
    const c = columnas[i];
    if (c) setCampo({ columna: c.name, valor: f[i] ?? null });
  }

  if (abierta !== null) {
    const fila = abierta >= 0 ? filas[abierta] : undefined;
    return (
      <Fila
        sesion={sesion}
        esquema={esquema}
        tabla={tabla}
        columnas={columnas}
        clave={clave}
        fila={fila ?? null}
        onVolver={() => setAbierta(null)}
        onAplicado={(mensaje) => {
          setAbierta(null);
          setVersion((v) => v + 1);
          onAviso({ id: `apply-${Date.now()}`, tone: "success", title: mensaje });
        }}
        onSesionCerrada={onSesionCerrada}
        onAviso={onAviso}
      />
    );
  }

  const puedeEditar = !sesion.readOnly && clave.size > 0;
  const conVista = vista.orden !== null || vista.filtros.length > 0;
  // Con un orden elegido, Go devuelve las columnas que le pedimos, así que
  // «ordenada» no dice nada de la clave: el desempate es fiable solo si hay.
  const paginadoFiable = vista.orden ? clave.size > 0 : ordenada;

  return (
    <>
      <BarraDeSesion
        sesion={sesion}
        titulo={tabla.name}
        atras={onVolver}
        derecha={
          <>
            <button
              type="button"
              className={cx(styles.icono, conVista && styles.iconoActivo)}
              onClick={() => setEligiendo(true)}
              aria-label="Filtrar y ordenar"
              title="Filtrar y ordenar"
              disabled={columnas.length === 0}
            >
              ⇅
            </button>
            <button type="button" className={styles.icono} onClick={() => setVersion((v) => v + 1)} aria-label="Recargar" title="Recargar">
              ↻
            </button>
          </>
        }
      />
      <main className={styles.cuerpo}>
        {error ? (
          <div className={styles.error}>
            {error}
            {detalle ? <div className={styles.mono}>{detalle}</div> : null}
          </div>
        ) : null}
        {conVista ? (
          <button type="button" className={styles.vistaPuesta} onClick={() => setEligiendo(true)} aria-label="Cambiar el filtro o el orden">
            {vista.orden ? (
              <span className={styles.chip}>
                {vista.orden.descendente ? "↓" : "↑"} {vista.orden.columna}
              </span>
            ) : null}
            {descripcion.map((d, i) => (
              <span key={i} className={styles.chip}>
                {d}
              </span>
            ))}
          </button>
        ) : null}
        {!paginadoFiable && filas.length > 0 ? (
          <div className={styles.aviso}>
            Sin clave primaria: «cargar más» puede repetir o saltear filas, y no se puede editar.
          </div>
        ) : null}
        {sesion.readOnly ? <div className={styles.nota}>Solo lectura: {sesion.readOnlyReason || "esta conexión no escribe"}.</div> : null}

        <div className={styles.lista}>
          {filas.map((f, i) => (
            <Tarjeta key={i} columnas={columnas} fila={f} clave={clave} onClick={() => setAbierta(i)} onMantener={(c) => mantener(f, c)} />
          ))}
        </div>

        {cargando ? (
          <div className={styles.vacio}>
            <Spinner />
          </div>
        ) : filas.length === 0 && !error ? (
          <div className={styles.vacio}>{vista.filtros.length > 0 ? "Ninguna fila cumple el filtro." : "La tabla está vacía."}</div>
        ) : null}

        {hayMas && !cargando ? (
          <Button className={styles.grande} onClick={() => void cargar(filas.length)}>
            Cargar más
          </Button>
        ) : null}
      </main>
      {puedeEditar ? (
        <div className={styles.pie}>
          <Button variant="primary" onClick={() => setAbierta(-1)}>
            Nueva fila
          </Button>
        </div>
      ) : null}

      {eligiendo ? <FiltroYOrden columnas={columnas} vista={vista} onAplicar={aplicarVista} onCerrar={() => setEligiendo(false)} /> : null}
      <HojaDeValor campo={campo} onCerrar={() => setCampo(null)} onAviso={onAviso} />
    </>
  );
}
