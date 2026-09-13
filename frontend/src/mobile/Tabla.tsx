import { useEffect, useRef, useState } from "react";
import * as Queries from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import type { SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { Table } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import type { Column } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import { Button } from "../components/ui";
import type { ToastItem } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import { cx } from "../lib/cx";
import { BarraDeSesion } from "./Barra";
import { Tarjeta } from "./Tarjeta";
import type { Valor } from "./Tarjeta";
import { Fila } from "./Fila";
import { FiltroYOrden } from "./FiltroYOrden";
import type { Vista } from "./FiltroYOrden";
import { HojaDeValor } from "./HojaDeValor";
import type { CampoElegido } from "./HojaDeValor";
import { siLaSesionSeCerro } from "./sesionCerrada";
import styles from "./mobile.module.css";

/** Filas por página. Pocas: cada una es una tarjeta, no una línea. */
const PAGINA = 40;

/**
 * M07: las filas de una tabla como tarjetas, paginadas con «cargar más». Sin
 * orden elegido lo decide Go (la clave primaria); con uno elegido, la clave va
 * detrás como desempate, para que dos filas iguales en esa columna no cambien
 * de página entre una carga y la siguiente. Sin clave lo dice, porque «cargar
 * más» puede repetir o saltear filas.
 *
 * Tocar una tarjeta abre la fila: ver entera, editar, borrar. Mantener
 * apretado un campo muestra el valor entero con «Copiar». «Nueva fila» flota
 * abajo si se puede escribir. Con la sesión en solo lectura, solo se mira.
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
  const tipos = new Map((tabla.columns ?? []).map((c) => [c.name, c.dataType]));

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
          setError(res.failure?.message ?? "No se pudieron leer las filas.");
          setDetalle([res.failure?.detail ?? "", res.failure?.sqlState ? `SQLSTATE ${res.failure.sqlState}` : ""].filter(Boolean).join("\n"));
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
    if (c) setCampo({ columna: c.name, valor: f[i] ?? null, tipo: tipos.get(c.name) ?? c.dataType });
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

  if (eligiendo) {
    return <FiltroYOrden tabla={tabla.name} columnas={columnas} vista={vista} onAplicar={aplicarVista} onCerrar={() => setEligiendo(false)} />;
  }

  const puedeEditar = !sesion.readOnly && clave.size > 0;
  const conVista = vista.orden !== null || vista.filtros.length > 0;
  // Con un orden elegido, Go devuelve las columnas que le pedimos, así que
  // «ordenada» no dice nada de la clave: el desempate es fiable solo si hay.
  const paginadoFiable = vista.orden ? clave.size > 0 : ordenada;
  const primeraCarga = cargando && filas.length === 0;

  return (
    <>
      <BarraDeSesion
        sesion={sesion}
        titulo={tabla.name}
        mono
        atras={onVolver}
        derecha={
          <>
            <button
              type="button"
              className={cx(styles.accion, conVista && styles.accionActiva)}
              onClick={() => setEligiendo(true)}
              aria-label="Filtrar y ordenar"
              title="Filtrar y ordenar"
              disabled={columnas.length === 0}
            >
              ⇅
            </button>
            <button type="button" className={styles.plano} onClick={() => setVersion((v) => v + 1)} aria-label="Recargar" title="Recargar" disabled={cargando}>
              ↻
            </button>
          </>
        }
      />

      {conVista ? (
        <button type="button" className={styles.filtrosPuestos} onClick={() => setEligiendo(true)} aria-label="Cambiar el filtro o el orden">
          {vista.orden ? (
            <span className={styles.chipFiltro}>
              {vista.orden.descendente ? "↓" : "↑"} {vista.orden.columna}
            </span>
          ) : null}
          {descripcion.map((d, i) => (
            <span key={i} className={styles.chipFiltro}>
              {d}
            </span>
          ))}
        </button>
      ) : null}

      <main className={cx(styles.cuerpo, styles.cuerpoGap12, puedeEditar && styles.cuerpoConFlotante, error && filas.length === 0 && styles.cuerpoCentrado)}>
        {error ? (
          <>
            <div className={styles.errorTarjeta}>
              <div className={styles.errorTitulo}>No se pudieron leer las filas</div>
              <div className={styles.errorTexto}>{error}</div>
              {detalle ? <pre className={styles.detalleMotor}>{detalle}</pre> : null}
            </div>
            <div className={styles.par}>
              {conVista ? (
                <Button onClick={() => aplicarVista({ orden: null, filtros: [] }, [])}>Quitar filtro y orden</Button>
              ) : (
                <Button onClick={onVolver}>Volver</Button>
              )}
              <Button variant="primary" onClick={() => setVersion((v) => v + 1)}>
                Reintentar
              </Button>
            </div>
          </>
        ) : null}

        {!error && !paginadoFiable && filas.length > 0 ? (
          <div className={styles.aviso}>
            <span>Esta tabla no tiene clave primaria. Cargar más puede repetir o saltear filas, y no se puede editar.</span>
          </div>
        ) : null}
        {!error && sesion.readOnly ? <div className={styles.notaPie}>La conexión es de solo lectura: {sesion.readOnlyReason || "esta conexión no escribe"}.</div> : null}

        {primeraCarga ? (
          <>
            <div className={styles.cargando}>
              <span className={cx(styles.aro, styles.aroChico)} />
              <span>leyendo {PAGINA} filas…</span>
            </div>
            <div className={cx(styles.esqueleto, styles.esqueletoFila)} />
            <div className={cx(styles.esqueleto, styles.esqueletoFila)} />
            <div className={cx(styles.esqueleto, styles.esqueletoFila)} />
          </>
        ) : null}

        {filas.map((f, i) => (
          <Tarjeta key={i} columnas={columnas} fila={f} clave={clave} onClick={() => setAbierta(i)} onMantener={(c) => mantener(f, c)} />
        ))}

        {!error && !cargando && filas.length === 0 ? (
          <div className={styles.vacio}>
            <span className={styles.vacioTitulo}>{vista.filtros.length > 0 ? "Ninguna fila cumple el filtro" : "La tabla está vacía"}</span>
          </div>
        ) : null}

        {hayMas && !cargando ? (
          <Button className={styles.cargarMas} onClick={() => void cargar(filas.length)}>
            Cargar más
          </Button>
        ) : null}
        {cargando && filas.length > 0 ? (
          <div className={cx(styles.cargando, styles.centradoFila)}>
            <span className={cx(styles.aro, styles.aroChico)} />
            <span>leyendo…</span>
          </div>
        ) : null}
        {!error && filas.length > 0 ? (
          <span className={styles.contador}>
            {filas.length}
            {vista.filtros.length === 0 && tabla.rowEstimate >= 0 ? ` de ~${formatear(tabla.rowEstimate)}` : ""}{" "}
            {filas.length === 1 ? "fila" : "filas"}
            {vista.filtros.length > 0 && !hayMas ? " con este filtro" : ""}
          </span>
        ) : null}
      </main>

      {puedeEditar && !error ? (
        <div className={styles.flotante}>
          <Button variant="primary" onClick={() => setAbierta(-1)}>
            Nueva fila
          </Button>
        </div>
      ) : null}

      <HojaDeValor campo={campo} onCerrar={() => setCampo(null)} onAviso={onAviso} />
    </>
  );
}

function formatear(n: number): string {
  return new Intl.NumberFormat("es-AR").format(n);
}
