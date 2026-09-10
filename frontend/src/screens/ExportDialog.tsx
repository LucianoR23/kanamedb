import { useEffect, useState } from "react";
import { Format } from "../../bindings/github.com/LucianoR23/kanamedb/internal/export";
import type { Options } from "../../bindings/github.com/LucianoR23/kanamedb/internal/export";
import type { Column } from "../../bindings/github.com/LucianoR23/kanamedb/internal/query";
import type { FormatInfo, ResultExport } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import * as ExportsSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/exports";
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
  | { fase: "error"; mensaje: string };

/**
 * S19 Export, para lo que ya está en memoria: el resultado del editor.
 *
 * El formato se elige a la izquierda y se ve a la derecha antes de guardar;
 * las opciones que no aplican al formato elegido se ven apagadas y no
 * desaparecen, para que cambiar de formato no mueva el diálogo. El texto lo
 * arma Go con los mismos escritores que S19 va a usar para una tabla entera:
 * lo que se ve acá es lo que va a salir allá.
 *
 * Las filas viajan de vuelta al puente para formatearse: es lo que hay en la
 * grilla, ya cortado por el límite de filas de la conexión, así que el tamaño
 * está acotado. Para la vista previa van solo las primeras.
 */
export function ExportDialog({
  open,
  columns,
  rows,
  nombre,
  alcance,
  truncated = false,
  onClose,
}: {
  open: boolean;
  columns: readonly Column[];
  /** Como vienen del puente: una fila nula es una fila vacía. */
  rows: readonly ((string | null)[] | null)[];
  /** Nombre base sugerido para el archivo. */
  nombre: string;
  /** De dónde salen las filas, para el encabezado: «este resultado». */
  alcance: string;
  /** La grilla no tiene todas las filas de la consulta. */
  truncated?: boolean;
  onClose: () => void;
}) {
  const [formato, setFormato] = useState<Format>(Format.CSV);
  const [opciones, setOpciones] = useState<Options>(opcionesPorDefecto);
  const [formatos, setFormatos] = useState<FormatInfo[]>([]);
  const [ruta, setRuta] = useState("");
  const [vista, setVista] = useState<Vista>({ fase: "pidiendo" });
  const [estado, setEstado] = useState<Estado>({ fase: "quieto" });

  const info = FORMATOS.find((f) => f.key === formato) ?? FORMATOS[0]!;
  // La extensión la dice Go, y tarda un viaje por el puente. Vacía significa
  // «todavía no llegó», no «este formato no tiene»: sin esperarla, apretar
  // «Elegir…» apenas se abre el diálogo proponía `consulta` sin extensión y
  // escribía el archivo sin `.csv`.
  const extension = formatos.find((f) => f.key === formato)?.extension ?? "";
  const listo = extension !== "";
  const aplica = (o: Opcion) => info.opciones.includes(o);
  const pedido = (): ResultExport => ({
    format: formato,
    options: opciones,
    columns: [...columns],
    rows: rows.map((r) => r ?? []),
  });

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
    void ExportsSvc.Preview(
      {
        format: formato,
        options: opciones,
        columns: [...columns],
        rows: rows.slice(0, FILAS_DE_VISTA).map((r) => r ?? []),
      },
      FILAS_DE_VISTA,
    )
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
  }, [open, formato, opciones, columns, rows]);

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
    try {
      const info = await ExportsSvc.Save(pedido(), destino);
      setEstado({ fase: "guardado", ruta: info.path, bytes: info.bytes, filas: info.rows });
    } catch (err) {
      setEstado({ fase: "error", mensaje: textoDe(err) });
    }
  };

  const copiar = async () => {
    try {
      const texto = await ExportsSvc.Render({ ...pedido(), options: { ...opciones, gzip: false } });
      await navigator.clipboard.writeText(texto);
      setEstado({ fase: "copiado" });
    } catch (err) {
      setEstado({ fase: "error", mensaje: textoDe(err) });
    }
  };

  const cuenta = `${rows.length.toLocaleString("es", { useGrouping: true })} ${plural(rows.length, "fila", "filas")}`;

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
          <Button onClick={onClose}>Cancelar</Button>
          <Button onClick={() => void copiar()} disabled={estado.fase === "guardando"}>
            Copiar al portapapeles
          </Button>
          <Button
            variant="primary"
            onClick={() => void exportar()}
            disabled={!listo || estado.fase === "guardando"}
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
            <span className={styles.dim}>{cuenta}</span>
          </div>

          <span className={styles.grow} />
          <p className={styles.lateralNota}>
            {truncated
              ? `Se exporta lo que está cargado en la grilla: ${cuenta}. La consulta devolvía más, cortadas por el límite de la conexión.`
              : `Se exporta lo que está cargado en la grilla: ${cuenta}, ya en memoria.`}
          </p>
        </aside>

        <section className={styles.principal}>
          <div className={styles.ajustes}>
            <label className={styles.etiqueta}>Guardar en</label>
            <div className={styles.rutaFila}>
              <span className={cx(styles.ruta, !ruta && styles.rutaVacia)} title={ruta}>
                {ruta || (listo ? "se pregunta al exportar" : "…")}
              </span>
              <Button size="sm" disabled={!listo} onClick={() => void elegir()}>
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
                  disabled={!aplica("delimiter")}
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
                  disabled={!aplica("header")}
                  onChange={(v) => cambiar({ noHeader: !v })}
                >
                  Incluir la fila de encabezado
                </Checkbox>
              </OpcionFila>
              <OpcionFila aplica={aplica("nullEmpty")}>
                <Checkbox
                  checked={opciones.nullAsEmpty}
                  disabled={!aplica("nullEmpty")}
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
                  disabled={!aplica("quoteAll")}
                  onChange={(v) => cambiar({ quoteAll: v })}
                >
                  Citar todos los campos
                  <span className={styles.opcionNota}>más seguro para planillas</span>
                </Checkbox>
              </OpcionFila>
              <OpcionFila aplica={aplica("bom")}>
                <Checkbox
                  checked={opciones.bom}
                  disabled={!aplica("bom")}
                  onChange={(v) => cambiar({ bom: v })}
                >
                  Marca de orden de bytes
                  <span className={styles.opcionNota}>para que Excel lea los acentos</span>
                </Checkbox>
              </OpcionFila>
              <OpcionFila aplica={aplica("gzip")}>
                <Checkbox
                  checked={opciones.gzip}
                  disabled={!aplica("gzip")}
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
              {rows.length === 0
                ? "sin filas"
                : rows.length <= FILAS_DE_VISTA
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
                {rows.length > FILAS_DE_VISTA ? (
                  <span className={styles.masFilas}>
                    {"… "}
                    {(rows.length - FILAS_DE_VISTA).toLocaleString("es", { useGrouping: true })}{" "}
                    {plural(rows.length - FILAS_DE_VISTA, "fila más", "filas más")}
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
