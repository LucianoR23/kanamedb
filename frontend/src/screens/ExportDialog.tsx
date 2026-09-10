import { useEffect, useRef, useState } from "react";
import { Format } from "../../bindings/github.com/LucianoR23/kanamedb/internal/export";
import type { Options } from "../../bindings/github.com/LucianoR23/kanamedb/internal/export";
import type { Column } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import type {
  FormatInfo,
  ResultExport,
  TableExport,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import * as ExportsSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/exports";
import * as QueriesSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import { Button, Checkbox, Dialog, Spinner } from "../components/ui";
import { cx } from "../lib/cx";
import { textoDe } from "../lib/dialogos";
import {
  DELIMITADORES,
  FORMATOS,
  elegirDestino,
  nombreDeArchivo,
  opcionesPorDefecto,
  tamano,
} from "../lib/exportar";
import type { Opcion } from "../lib/exportar";
import { plural } from "../lib/motor";
import styles from "./ExportDialog.module.css";

/** Cuántas filas muestra la vista previa. Pocas: es para ver el formato, no
 *  los datos, y cada cambio de opción la vuelve a pedir. */
const FILAS_DE_VISTA = 4;

/** La vista previa tiene su propio estado: un fallo al formatearla no es lo
 *  mismo que uno al guardar, y dejarla en «pidiendo» mostraba el spinner para
 *  siempre al lado de un pie que decía que había fallado. */
type Vista = { fase: "pidiendo" } | { fase: "lista"; texto: string } | { fase: "falló" };

type Estado =
  | { fase: "quieto" }
  | { fase: "guardando" }
  | { fase: "guardado"; ruta: string; bytes: number; filas: number }
  | { fase: "copiado" }
  | { fase: "cancelado" }
  | { fase: "error"; mensaje: string };

/**
 * De dónde salen las filas que se exportan. Son dos caminos distintos y no un
 * detalle:
 *
 * - «resultado» es lo que ya está en la grilla del editor. Viaja de vuelta por
 *   el puente para formatearse en Go; está acotado por el límite de filas de
 *   la conexión, así que cabe.
 * - «tabla» NO viaja: una tabla puede tener dos millones de filas. Go la lee
 *   del motor a medida que escribe el archivo, y acá solo se dice cuál es.
 */
export type OrigenExport =
  | {
      tipo: "resultado";
      columns: readonly Column[];
      /** Como vienen del puente: una fila nula es una fila vacía. */
      rows: readonly ((string | null)[] | null)[];
      /** La grilla no tiene todas las filas de la consulta. */
      truncated?: boolean;
    }
  | {
      tipo: "tabla";
      schema: string;
      table: string;
      /** Por dónde ordenar. Vacío deja que el motor elija, que es más rápido. */
      orderBy?: string[];
    };

/**
 * S19 Export.
 *
 * El formato se elige a la izquierda y se ve a la derecha antes de guardar;
 * las opciones que no aplican al formato elegido se ven apagadas y no
 * desaparecen, para que cambiar de formato no mueva el diálogo. El texto lo
 * arma Go, así que lo que se ve en la vista previa sale del mismo escritor que
 * lo que se va a guardar.
 *
 * El identificador de ejecución es de este diálogo: una exportación larga se
 * registra donde se registran las consultas, y así «Cancelar» puede cortarla.
 */
export function ExportDialog({
  open,
  origen,
  nombre,
  runID,
  onClose,
}: {
  open: boolean;
  origen: OrigenExport;
  /** Nombre base sugerido para el archivo. */
  nombre: string;
  /** Para poder cancelar esta exportación y no otra. */
  runID: string;
  onClose: () => void;
}) {
  const [formato, setFormato] = useState<Format>(Format.CSV);
  const [opciones, setOpciones] = useState<Options>(opcionesPorDefecto);
  const [formatos, setFormatos] = useState<FormatInfo[]>([]);
  const [ruta, setRuta] = useState("");
  const [vista, setVista] = useState<Vista>({ fase: "pidiendo" });
  const [estado, setEstado] = useState<Estado>({ fase: "quieto" });
  // Si el corte lo pidió quien mira, el fallo no es un fallo: se cuenta como
  // cancelación y no con el error crudo del contexto muerto.
  const pidioCancelar = useRef(false);

  const info = FORMATOS.find((f) => f.key === formato) ?? FORMATOS[0]!;
  // La extensión la dice Go, y tarda un viaje por el puente. Vacía significa
  // «todavía no llegó», no «este formato no tiene»: sin esperarla, apretar
  // «Elegir…» apenas se abre el diálogo proponía `consulta` sin extensión y
  // escribía el archivo sin `.csv`.
  const extension = formatos.find((f) => f.key === formato)?.extension ?? "";
  const listo = extension !== "";
  // Mientras se escribe el archivo no se puede cambiar lo que se está
  // escribiendo: el pedido ya salió con el formato y las opciones de antes.
  const guardando = estado.fase === "guardando";
  const aplica = (o: Opcion) => info.opciones.includes(o);

  const pedidoDeResultado = (limite: number, opts = opciones): ResultExport => {
    if (origen.tipo !== "resultado") throw new Error("no es un resultado");
    const filas = limite > 0 ? origen.rows.slice(0, limite) : origen.rows;
    return {
      format: formato,
      options: opts,
      columns: [...origen.columns],
      rows: filas.map((r) => r ?? []),
    };
  };
  // La vista previa y el guardado NO comparten el identificador. El registro de
  // cancelaciones es un mapa por runID: si los dos usaran el mismo, tocar una
  // casilla durante un guardado largo registraría la vista previa encima, y al
  // terminar ella borraría la entrada — dejando el guardado corriendo sin nadie
  // que pueda cortarlo.
  const pedidoDeTabla = (opts = opciones, id = runID): TableExport => {
    if (origen.tipo !== "tabla") throw new Error("no es una tabla");
    return {
      runId: id,
      schema: origen.schema,
      table: origen.table,
      format: formato,
      options: opts,
      orderBy: origen.orderBy ?? [],
      descending: false,
    };
  };

  // Las extensiones las dice Go. Una sola vez por apertura.
  useEffect(() => {
    if (!open) return;
    let vivo = true;
    void ExportsSvc.Formats().then((fs) => {
      if (vivo) setFormatos(fs ?? []);
    });
    return () => {
      vivo = false;
    };
  }, [open]);

  // La vista previa se pide de nuevo con cada cambio de formato u opción, con
  // las primeras filas nada más.
  useEffect(() => {
    if (!open) return;
    let vivo = true;
    setVista({ fase: "pidiendo" });
    const pedir =
      origen.tipo === "resultado"
        ? ExportsSvc.Preview(pedidoDeResultado(FILAS_DE_VISTA), FILAS_DE_VISTA)
        : ExportsSvc.PreviewTable(pedidoDeTabla(opciones, `${runID}:vista`), FILAS_DE_VISTA);
    void pedir
      .then((texto) => {
        if (vivo) setVista({ fase: "lista", texto });
      })
      .catch((err: unknown) => {
        if (!vivo) return;
        setVista({ fase: "falló" });
        setEstado({ fase: "error", mensaje: textoDe(err) });
      });
    return () => {
      vivo = false;
    };
    // origen es estable mientras el diálogo está abierto: quien lo abre lo
    // arma con lo que hay en pantalla en ese momento.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, formato, opciones]);

  // Cambiar el formato invalida la ruta elegida: la extensión ya no coincide.
  const elegirFormato = (f: Format) => {
    if (f === formato) return;
    setFormato(f);
    setRuta("");
    setEstado({ fase: "quieto" });
  };
  const cambiar = (parche: Partial<Options>) => {
    setOpciones((o) => ({ ...o, ...parche }));
    setEstado({ fase: "quieto" });
  };

  const elegir = async (): Promise<string> => {
    try {
      const r = await elegirDestino(nombreDeArchivo(nombre), extension, opciones.gzip);
      if (r) setRuta(r);
      return r;
    } catch (err) {
      setEstado({ fase: "error", mensaje: textoDe(err) });
      return "";
    }
  };

  const exportar = async () => {
    const destino = ruta || (await elegir());
    if (!destino) return;
    setEstado({ fase: "guardando" });
    pidioCancelar.current = false;
    try {
      const info =
        origen.tipo === "resultado"
          ? await ExportsSvc.Save(pedidoDeResultado(0), destino)
          : await ExportsSvc.SaveTable(pedidoDeTabla(), destino);
      setEstado({ fase: "guardado", ruta: info.path, bytes: info.bytes, filas: info.rows });
    } catch (err) {
      setEstado(
        pidioCancelar.current
          ? { fase: "cancelado" }
          : { fase: "error", mensaje: textoDe(err) },
      );
    }
  };

  // Copiar es solo para el resultado: una tabla entera no va al portapapeles
  // —puede ser de gigabytes— y ofrecerlo sería prometer algo que no se puede
  // cumplir. El botón no está cuando el origen es una tabla.
  const copiar = async () => {
    if (origen.tipo !== "resultado") return;
    try {
      const texto = await ExportsSvc.Render(pedidoDeResultado(0, { ...opciones, gzip: false }));
      await navigator.clipboard.writeText(texto);
      setEstado({ fase: "copiado" });
    } catch (err) {
      setEstado({ fase: "error", mensaje: textoDe(err) });
    }
  };

  // Cuántas filas se van a exportar.
  //
  // Con un resultado se sabe: son las que están en la grilla. Con una TABLA no,
  // y el diálogo no lo inventa. El conteo que muestra la pantalla de la tabla
  // puede estar viejo —se lee al abrirla y no se vuelve a pedir solo—, y se vio
  // en una prueba: la pantalla decía «0 filas» de una tabla que ya tenía 1500,
  // así que el botón habría prometido «Exportar 0 filas» y habría escrito 1500.
  // Prometer un número que la exportación no controla es peor que no darlo: el
  // número exacto lo dice Go al terminar, contando lo que realmente escribió.
  const filas = origen.tipo === "resultado" ? origen.rows.length : null;
  // Cancelar mientras se guarda tiene que CORTAR la exportación, no solo cerrar
  // el diálogo: del otro lado hay un recorrido escribiendo un archivo, y cerrar
  // la ventana no lo detiene. Lo que quede a medio escribir se descarta solo:
  // el archivo definitivo aparece recién al renombrar el temporal.
  const cancelar = async () => {
    if (estado.fase === "guardando") {
      pidioCancelar.current = true;
      await QueriesSvc.Cancel(runID);
      return;
    }
    onClose();
  };

  const cuenta =
    filas === null
      ? "la tabla entera"
      : `${filas.toLocaleString("es", { useGrouping: true })} ${plural(filas, "fila", "filas")}`;
  const alcance = origen.tipo === "resultado" ? "Este resultado" : `${origen.table}, entera`;
  const nota =
    origen.tipo === "tabla"
      ? "Se lee del servidor y se escribe al archivo a medida que llega: no pasa por la memoria, así que el tamaño de la tabla no es un problema."
      : origen.truncated
        ? `Se exporta lo que está cargado en la grilla: ${cuenta}. La consulta devolvía más, cortadas por el límite de la conexión.`
        : `Se exporta lo que está cargado en la grilla: ${cuenta}, ya en memoria.`;

  return (
    <Dialog
      open={open}
      title="Exportar"
      size="xl"
      onClose={onClose}
      footer={
        <>
          <Pie estado={estado} />
          <span className={styles.grow} />
          <Button onClick={() => void cancelar()}>
            {estado.fase === "guardando" ? "Cancelar la exportación" : "Cancelar"}
          </Button>
          {origen.tipo === "resultado" ? (
            <Button onClick={() => void copiar()} disabled={guardando}>
              Copiar al portapapeles
            </Button>
          ) : null}
          <Button
            variant="primary"
            onClick={() => void exportar()}
            disabled={!listo || guardando}
          >
            {estado.fase === "guardando" ? "Guardando…" : `Exportar ${cuenta}`}
          </Button>
        </>
      }
    >
      <div className={styles.marco}>
        <aside className={styles.lateral}>
          <div className={styles.seccionTitulo}>Formato</div>
          <div role="listbox" aria-label="Formato">
            {FORMATOS.map((f) => (
              <button
                type="button"
                role="option"
                aria-selected={f.key === formato}
                key={f.key}
                className={cx(styles.formato, f.key === formato && styles.formatoActivo)}
                disabled={guardando}
                onClick={() => elegirFormato(f.key)}
              >
                <span className={styles.formatoTag}>{f.tag}</span>
                <span className={styles.formatoTexto}>
                  <span className={styles.formatoLabel}>{f.label}</span>
                  <span className={styles.formatoNota}>
                    {formatos.find((x) => x.key === f.key)?.extension ?? ""} · {f.nota}
                  </span>
                </span>
              </button>
            ))}
          </div>

          <div className={cx(styles.seccionTitulo, styles.seccionSegunda)}>Alcance</div>
          <div className={styles.alcance}>
            <span className={styles.alcanceRadio} aria-hidden="true">
              <span />
            </span>
            <span className={styles.alcanceLabel}>{alcance}</span>
            <span className={styles.grow} />
            {filas === null ? null : <span className={styles.dim}>{cuenta}</span>}
          </div>

          <span className={styles.grow} />
          <p className={styles.lateralNota}>
            {nota}
          </p>
        </aside>

        <section className={styles.principal}>
          <div className={styles.ajustes}>
            <label className={styles.etiqueta}>Guardar en</label>
            <div className={styles.rutaFila}>
              <span className={cx(styles.ruta, !ruta && styles.rutaVacia)} title={ruta}>
                {ruta || (listo ? "se pregunta al exportar" : "…")}
              </span>
              <Button size="sm" disabled={!listo || guardando} onClick={() => void elegir()}>
                Elegir…
              </Button>
            </div>

            <label className={cx(styles.etiqueta, !aplica("delimiter") && styles.apagado)}>
              Delimitador
            </label>
            <div className={styles.pills} role="radiogroup" aria-label="Delimitador">
              {DELIMITADORES.map((d) => (
                <button
                  type="button"
                  role="radio"
                  aria-checked={opciones.delimiter === d.valor}
                  key={d.valor}
                  className={cx(styles.pill, opciones.delimiter === d.valor && styles.pillActiva)}
                  disabled={guardando || !aplica("delimiter")}
                  onClick={() => cambiar({ delimiter: d.valor })}
                >
                  {d.label}
                </button>
              ))}
            </div>

            <label className={cx(styles.etiqueta, styles.etiquetaArriba)}>Opciones</label>
            <div className={styles.opciones}>
              <OpcionFila aplica={aplica("header")}>
                <Checkbox
                  checked={!opciones.noHeader}
                  disabled={guardando || !aplica("header")}
                  onChange={(v) => cambiar({ noHeader: !v })}
                >
                  Incluir la fila de encabezado
                </Checkbox>
              </OpcionFila>
              <OpcionFila aplica={aplica("nullEmpty")}>
                <Checkbox
                  checked={opciones.nullAsEmpty}
                  disabled={guardando || !aplica("nullEmpty")}
                  onChange={(v) => cambiar({ nullAsEmpty: v })}
                >
                  Escribir NULL como campo vacío
                  <span className={styles.opcionNota}>
                    {formato === Format.Markdown ? "en vez de NULL" : "en vez de \\N"}
                  </span>
                </Checkbox>
              </OpcionFila>
              <OpcionFila aplica={aplica("quoteAll")}>
                <Checkbox
                  checked={opciones.quoteAll}
                  disabled={guardando || !aplica("quoteAll")}
                  onChange={(v) => cambiar({ quoteAll: v })}
                >
                  Citar todos los campos
                  <span className={styles.opcionNota}>más seguro para planillas</span>
                </Checkbox>
              </OpcionFila>
              <OpcionFila aplica={aplica("bom")}>
                <Checkbox
                  checked={opciones.bom}
                  disabled={guardando || !aplica("bom")}
                  onChange={(v) => cambiar({ bom: v })}
                >
                  Marca de orden de bytes
                  <span className={styles.opcionNota}>para que Excel lea los acentos</span>
                </Checkbox>
              </OpcionFila>
              <OpcionFila aplica={aplica("gzip")}>
                <Checkbox
                  checked={opciones.gzip}
                  disabled={guardando || !aplica("gzip")}
                  onChange={(v) => {
                    cambiar({ gzip: v });
                    // La ruta elegida tenía otra extensión.
                    setRuta("");
                  }}
                >
                  Comprimir con gzip
                  <span className={styles.opcionNota}>{extension}.gz</span>
                </Checkbox>
              </OpcionFila>
            </div>
          </div>

          <div className={styles.vistaCabecera}>
            <span className={styles.seccionTitulo}>Vista previa</span>
            <span className={styles.grow} />
            <span className={styles.dim}>
              {filas === 0
                ? "sin filas"
                : filas === null
                  ? `primeras ${FILAS_DE_VISTA} filas`
                  : filas <= FILAS_DE_VISTA
                    ? cuenta
                    : `primeras ${FILAS_DE_VISTA} de ${cuenta}`}
            </span>
          </div>
          <div className={styles.vistaCuerpo}>
            {vista.fase === "pidiendo" ? (
              <div className={styles.cargando}>
                <Spinner />
              </div>
            ) : vista.fase === "falló" ? (
              <p className={styles.vistaFallo}>
                No se pudo armar la vista previa. El detalle está abajo.
              </p>
            ) : (
              <pre className={styles.pre}>
                {vista.texto}
                {filas === null || filas > FILAS_DE_VISTA ? (
                  <span className={styles.masFilas}>
                    {filas === null
                      ? "… y el resto de la tabla"
                      : `… ${(filas - FILAS_DE_VISTA).toLocaleString("es", { useGrouping: true })} ${plural(filas - FILAS_DE_VISTA, "fila más", "filas más")}`}
                  </span>
                ) : null}
              </pre>
            )}
          </div>
        </section>
      </div>
    </Dialog>
  );
}

function OpcionFila({ aplica, children }: { aplica: boolean; children: React.ReactNode }) {
  return (
    <div className={cx(styles.opcion, !aplica && styles.apagado)}>
      {children}
      {aplica ? null : <span className={styles.opcionNota}>no aplica a este formato</span>}
    </div>
  );
}

function Pie({ estado }: { estado: Estado }) {
  switch (estado.fase) {
    case "guardado":
      return (
        <span className={styles.pieOk} title={estado.ruta}>
          Guardado · {estado.filas.toLocaleString("es", { useGrouping: true })}{" "}
          {plural(estado.filas, "fila", "filas")} · {tamano(estado.bytes)} · {estado.ruta}
        </span>
      );
    case "copiado":
      return <span className={styles.pieOk}>Copiado al portapapeles</span>;
    case "cancelado":
      return (
        <span className={styles.dim}>
          Exportación cancelada. No quedó ningún archivo: el definitivo aparece recién al final.
        </span>
      );
    case "error":
      return (
        <span className={styles.pieError} role="alert">
          {estado.mensaje}
        </span>
      );
    case "guardando":
      return <span className={styles.dim}>Escribiendo…</span>;
    default:
      return <span className={styles.dim}>Se escribe entero en un temporal y recién después con el nombre elegido.</span>;
  }
}
