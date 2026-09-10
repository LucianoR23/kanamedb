import { useEffect, useRef, useState } from "react";
import type { Inspection, Options } from "../../bindings/github.com/LucianoR23/kanamedb/internal/csvimport";
import type { Failure } from "../../bindings/github.com/LucianoR23/kanamedb/internal/engine";
import type { DetailColumn } from "../../bindings/github.com/LucianoR23/kanamedb/internal/schema";
import type { ImportResult, ImportTarget } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { OnConflict } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import * as ImportsSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/imports";
import * as QueriesSvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/queries";
import { Button, Checkbox, Combobox, Dialog, Input, Spinner } from "../components/ui";
import type { ComboOption } from "../components/ui";
import { cx } from "../lib/cx";
import { textoDe } from "../lib/dialogos";
import { tamano } from "../lib/exportar";
import { DELIMITADORES_CSV, elegirCSV, emparejar, miles, opcionesDeLectura } from "../lib/importar";
import { plural } from "../lib/motor";
import styles from "./ImportWizard.module.css";

/** Cuántas filas del archivo se muestran en el mapeo. Pocas: es para reconocer
 *  la columna, no para leer los datos. */
const FILAS_DE_MUESTRA = 5;

type Paso = "origen" | "mapeo" | "validacion" | "importar";

const PASOS: readonly { id: Paso; n: number; label: string }[] = [
  { id: "origen", n: 1, label: "Origen" },
  { id: "mapeo", n: 2, label: "Mapeo" },
  { id: "validacion", n: 3, label: "Validación" },
  { id: "importar", n: 4, label: "Importar" },
];

/** Lo que está pasando con una corrida —el ensayo o la importación—. Son dos
 *  estados separados a propósito: el resultado del ensayo tiene que seguir a la
 *  vista mientras la importación corre, porque es lo que la justifica. */
type Corrida =
  | { fase: "quieto" }
  | { fase: "corriendo" }
  | { fase: "listo"; res: ImportResult }
  | { fase: "cancelado" }
  | { fase: "error"; mensaje: string };

/**
 * S18 CSV import wizard.
 *
 * Cuatro pasos, y son cuatro porque cada uno responde una pregunta que el
 * siguiente da por contestada: qué archivo es y cómo se lee, a dónde va cada
 * columna, si el servidor lo acepta, y recién ahí escribirlo.
 *
 * El tercero es el que justifica que esto sea una pantalla y no un botón. No
 * comprueba los tipos del lado de Kaname —«esto parece una fecha» es inventar
 * las reglas del motor y equivocarse en los casos raros—: hace la importación
 * de verdad adentro de una transacción y la revierte, así que la respuesta la
 * da el servidor, que es quien sabe. Ver kaname-plan.md § 6.
 */
export function ImportWizard({
  open,
  schema,
  table,
  columnas,
  onClose,
  onImportado,
}: {
  open: boolean;
  schema: string;
  table: string;
  /** Las columnas de la tabla destino, ya leídas por la pestaña. */
  columnas: readonly DetailColumn[];
  onClose: () => void;
  /** Se llama cuando entraron filas, para que la grilla las relea. */
  onImportado: () => void;
}) {
  const [paso, setPaso] = useState<Paso>("origen");
  // Lo que se escribe se guarda tal cual —recortar en cada tecla no dejaría
  // escribir `C:\Program Files\…`, porque el espacio del medio se come
  // apenas se tipea— y `ruta` es lo que de verdad se usa, recortado UNA vez.
  //
  // Antes se recortaba solo al mirar el archivo: el paso 1 encontraba las
  // columnas y la importación fallaba después con «no se pudo abrir», por un
  // espacio al final que nadie ve —el que viene pegado cuando se copia una ruta
  // de una terminal o de un chat—.
  const [rutaEscrita, setRutaEscrita] = useState("");
  const ruta = rutaEscrita.trim();
  const [opciones, setOpciones] = useState<Options>(opcionesDeLectura);
  const [inspeccion, setInspeccion] = useState<Inspection | null>(null);
  const [mirando, setMirando] = useState(false);
  const [errorArchivo, setErrorArchivo] = useState("");
  const [mapeo, setMapeo] = useState<string[]>([]);
  const [conflicto, setConflicto] = useState<OnConflict>(OnConflict.ConflictFail);
  const [confirmacion, setConfirmacion] = useState("");
  const [target, setTarget] = useState<ImportTarget | null>(null);
  const [ensayo, setEnsayo] = useState<Corrida>({ fase: "quieto" });
  const [importacion, setImportacion] = useState<Corrida>({ fase: "quieto" });

  // El identificador es de esta apertura: cancelar tiene que cortar ESTA
  // importación y no otra que esté corriendo en otra pestaña.
  const [runID] = useState(() => `import:${schema}.${table}:${Date.now()}`);

  // Qué candados tiene esta conexión. Lo dice Go, que es quien después los
  // exige: una comprobación que viva solo acá sería un cartel, no una
  // protección.
  useEffect(() => {
    if (!open) return;
    let vivo = true;
    void ImportsSvc.Target()
      .then((t) => {
        if (vivo) setTarget(t);
      })
      .catch((err: unknown) => {
        if (vivo) setErrorArchivo(textoDe(err));
      });
    return () => {
      vivo = false;
    };
  }, [open]);

  const columnasDeLaTabla = columnas.filter((c) => !c.generated);
  const nombresDeLaTabla = columnasDeLaTabla.map((c) => c.name);
  const columnasDelArchivo = inspeccion?.columns ?? [];

  // Mirar el archivo con las opciones puestas. Cada cambio de delimitador o de
  // encabezado lo vuelve a leer, porque cambia qué columnas tiene.
  const mirar = async (r: string, o: Options) => {
    if (!r) return;
    setMirando(true);
    setErrorArchivo("");
    try {
      const i = await ImportsSvc.Inspect(r, o);
      setInspeccion(i);
      setMapeo(emparejar(i?.columns ?? [], nombresDeLaTabla));
      // Un archivo distinto invalida lo que se había probado del anterior.
      setEnsayo({ fase: "quieto" });
      setImportacion({ fase: "quieto" });
    } catch (err) {
      setInspeccion(null);
      setErrorArchivo(textoDe(err));
    } finally {
      setMirando(false);
    }
  };

  const elegirArchivo = async () => {
    try {
      const r = await elegirCSV();
      if (!r) return;
      setRutaEscrita(r);
      await mirar(r, opciones);
    } catch (err) {
      setErrorArchivo(textoDe(err));
    }
  };

  const cambiarOpciones = (parche: Partial<Options>) => {
    const o = { ...opciones, ...parche };
    setOpciones(o);
    void mirar(ruta, o);
  };

  const destinos = mapeo.filter((m) => m !== "");
  const sinMapear = nombresDeLaTabla.filter((c) => !destinos.includes(c));
  // Las que la base va a exigir y nadie llenó. Una columna con default o
  // identidad se llena sola, así que no se avisa de ella: avisar de todo es no
  // avisar de nada.
  const faltantes = columnasDeLaTabla.filter(
    (c) => !c.nullable && !c.default && !c.identity && !destinos.includes(c.name),
  );
  const filas = inspeccion?.rows ?? 0;
  const desparejas = inspeccion?.ragged ?? [];

  const necesitaPalabra = target?.needsConfirmation ?? false;
  const palabra = target?.confirmWord ?? "";
  const confirmado = !necesitaPalabra || confirmacion.trim() === palabra;
  const soloLectura = target?.readOnly ?? false;

  const plan = () => ({
    runId: runID,
    path: ruta,
    schema,
    table,
    options: opciones,
    mapping: mapeo,
    onConflict: conflicto,
    confirm: confirmacion.trim(),
  });

  // Si el corte lo pidió quien mira, el fallo no es un fallo: se cuenta como
  // cancelación y no con el error crudo del contexto muerto.
  const pidioCancelar = useRef(false);

  const correr = async (
    ejecutar: () => Promise<ImportResult>,
    poner: (c: Corrida) => void,
  ): Promise<ImportResult | null> => {
    poner({ fase: "corriendo" });
    pidioCancelar.current = false;
    try {
      const res = await ejecutar();
      // Cancelar llega como un fallo del motor, no como un rechazo de la
      // promesa: la importación devuelve su ImportResult igual.
      if (pidioCancelar.current && !res.ok) {
        poner({ fase: "cancelado" });
        return null;
      }
      poner({ fase: "listo", res });
      return res;
    } catch (err) {
      poner(
        pidioCancelar.current
          ? { fase: "cancelado" }
          : { fase: "error", mensaje: textoDe(err) },
      );
      return null;
    }
  };

  const ensayar = () => correr(() => ImportsSvc.DryRun(plan()), setEnsayo);
  const importar = async () => {
    const res = await correr(() => ImportsSvc.Run(plan()), setImportacion);
    // Releer solo si de verdad entraron filas. Una importación que falló no
    // dejó nada —va en una transacción— y recargar la grilla por eso sería
    // hacerle perder la página a quien está mirando, por nada.
    if (res?.ok) onImportado();
  };

  // El ensayo arranca solo al llegar al paso, que es lo que hace que sea un
  // paso y no un botón más. Contra producción no: ahí primero hay que escribir
  // el nombre de la base, y arrancarlo solo daría un error que no es un error.
  useEffect(() => {
    if (paso !== "validacion") return;
    if (necesitaPalabra || soloLectura) return;
    if (ensayo.fase !== "quieto") return;
    void ensayar();
    // `ensayar` se rearma en cada render; lo que decide es la fase.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paso, necesitaPalabra, soloLectura, ensayo.fase]);

  const corriendo = ensayo.fase === "corriendo" || importacion.fase === "corriendo";
  const cancelar = async () => {
    if (!corriendo) {
      onClose();
      return;
    }
    pidioCancelar.current = true;
    await QueriesSvc.Cancel(runID);
  };

  // Qué falta para poder pasar de este paso. La cadena vacía significa que se
  // puede: el motivo se muestra en el botón, así que no hay que adivinar por
  // qué está apagado.
  const trabado = (): string => {
    switch (paso) {
      case "origen":
        if (!ruta) return "Elegí un archivo primero";
        if (!inspeccion) return "El archivo todavía no se pudo leer";
        if (columnasDelArchivo.length === 0) return "El archivo no tiene columnas";
        return "";
      case "mapeo":
        if (destinos.length === 0) return "Elegí al menos una columna de destino";
        return "";
      default:
        return "";
    }
  };
  const motivo = trabado();
  const indice = PASOS.findIndex((p) => p.id === paso);
  const importado = importacion.fase === "listo" && importacion.res.ok;

  return (
    <Dialog
      open={open}
      title={`Importar CSV en ${table}`}
      size="xl"
      production={necesitaPalabra}
      {...(corriendo ? {} : { onClose })}
      footer={
        <>
          <Pie
            paso={paso}
            ensayo={ensayo}
            importacion={importacion}
            soloLectura={soloLectura}
            razon={target?.reason ?? ""}
          />
          <span className={styles.grow} />
          <Button onClick={() => void cancelar()}>
            {corriendo ? "Cancelar" : importado ? "Cerrar" : "Cancelar"}
          </Button>
          {indice > 0 && !importado ? (
            <Button disabled={corriendo} onClick={() => setPaso(PASOS[indice - 1]!.id)}>
              Atrás
            </Button>
          ) : null}
          {paso === "importar" ? (
            <Button
              variant={necesitaPalabra ? "danger" : "primary"}
              disabled={corriendo || soloLectura || !confirmado || importado}
              title={soloLectura ? target?.reason : undefined}
              onClick={() => void importar()}
            >
              {importacion.fase === "corriendo"
                ? "Importando…"
                : importado
                  ? "Importado"
                  : necesitaPalabra
                    ? `Importar en ${palabra}`
                    : `Importar ${miles(filas)} ${plural(filas, "fila", "filas")}`}
            </Button>
          ) : (
            <Button
              variant="primary"
              disabled={motivo !== "" || corriendo}
              title={motivo || undefined}
              onClick={() => setPaso(PASOS[indice + 1]!.id)}
            >
              Siguiente
            </Button>
          )}
        </>
      }
    >
      <div className={styles.marco}>
        <aside className={styles.rail}>
          {PASOS.map((p, i) => (
            <div
              key={p.id}
              className={cx(
                styles.railPaso,
                p.id === paso && styles.railActivo,
                i < indice && styles.railHecho,
              )}
            >
              <span className={styles.railNum}>{i < indice ? "✓" : p.n}</span>
              <span>{p.label}</span>
            </div>
          ))}
          <span className={styles.grow} />
          <p className={styles.railNota}>
            Todo entra en UNA transacción: si una fila falla, no queda ninguna.
          </p>
        </aside>

        <section className={styles.cuerpo}>
          {paso === "origen" ? (
            <Origen
              ruta={rutaEscrita}
              opciones={opciones}
              inspeccion={inspeccion}
              mirando={mirando}
              error={errorArchivo}
              onElegir={() => void elegirArchivo()}
              onRuta={setRutaEscrita}
              onMirar={(v) => void mirar(v.trim(), opciones)}
              onCambiar={cambiarOpciones}
            />
          ) : paso === "mapeo" ? (
            <Mapeo
              columnasDelArchivo={columnasDelArchivo}
              muestra={inspeccion?.sample ?? []}
              columnasDeLaTabla={columnasDeLaTabla}
              mapeo={mapeo}
              sinMapear={sinMapear}
              faltantes={faltantes}
              onCambiar={(i, v) =>
                setMapeo((m) => {
                  const out = [...m];
                  // Una columna de la tabla no puede recibir dos del archivo:
                  // Go lo rechaza, así que elegirla acá suelta la anterior en
                  // vez de dejar armar un plan que va a fallar.
                  if (v !== "") {
                    for (let k = 0; k < out.length; k++) if (out[k] === v) out[k] = "";
                  }
                  out[i] = v;
                  return out;
                })
              }
            />
          ) : paso === "validacion" ? (
            <Validacion
              corrida={ensayo}
              filas={filas}
              desparejas={desparejas.length}
              conflicto={conflicto}
              soloLectura={soloLectura}
              razon={target?.reason ?? ""}
              necesitaPalabra={necesitaPalabra}
              palabra={palabra}
              confirmacion={confirmacion}
              confirmado={confirmado}
              onConfirmacion={setConfirmacion}
              onConflicto={(c) => {
                setConflicto(c);
                // El ensayo probó la otra política: dejarlo en pantalla sería
                // decir que se probó algo que no se probó.
                setEnsayo({ fase: "quieto" });
              }}
              onEnsayar={() => void ensayar()}
            />
          ) : (
            <Importar
              corrida={importacion}
              ensayo={ensayo}
              filas={filas}
              destino={`${schema ? `${schema}.` : ""}${table}`}
              columnas={destinos.length}
              conflicto={conflicto}
              archivo={inspeccion?.file.name ?? ""}
              necesitaPalabra={necesitaPalabra}
              palabra={palabra}
              confirmacion={confirmacion}
              confirmado={confirmado}
              onConfirmacion={setConfirmacion}
            />
          )}
        </section>
      </div>
    </Dialog>
  );
}

/* ---- 1. origen ----------------------------------------------------------- */

function Origen({
  ruta,
  opciones,
  inspeccion,
  mirando,
  error,
  onElegir,
  onRuta,
  onMirar,
  onCambiar,
}: {
  ruta: string;
  opciones: Options;
  inspeccion: Inspection | null;
  mirando: boolean;
  error: string;
  onElegir: () => void;
  onRuta: (v: string) => void;
  onMirar: (v: string) => void;
  onCambiar: (parche: Partial<Options>) => void;
}) {
  return (
    <>
      <h3 className={styles.titulo}>De qué archivo</h3>
      {/* La ruta se puede ESCRIBIR, igual que en el formulario de SQLite de
          S03: el selector del sistema es la comodidad, no el único camino.
          Pegar una ruta que ya se tiene es más rápido que buscarla, y si el
          selector falla el asistente sigue sirviendo. */}
      <div className={styles.rutaFila}>
        <Input
          value={ruta}
          placeholder="C:\ruta\al\archivo.csv"
          aria-label="Ruta del archivo"
          spellCheck={false}
          onChange={(e) => onRuta(e.currentTarget.value)}
          onBlur={(e) => onMirar(e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") onMirar(e.currentTarget.value);
          }}
        />
        <Button size="sm" onClick={onElegir}>
          {ruta ? "Cambiar…" : "Elegir archivo…"}
        </Button>
      </div>
      {error ? (
        <p className={styles.error} role="alert">
          {error}
        </p>
      ) : null}

      <div className={styles.etiqueta}>Delimitador</div>
      <div className={styles.pills} role="radiogroup" aria-label="Delimitador">
        {DELIMITADORES_CSV.map((d) => (
          <button
            type="button"
            role="radio"
            aria-checked={opciones.delimiter === d.valor}
            key={d.valor}
            className={cx(styles.pill, opciones.delimiter === d.valor && styles.pillActiva)}
            disabled={!ruta}
            onClick={() => onCambiar({ delimiter: d.valor })}
          >
            {d.label}
          </button>
        ))}
      </div>

      <div className={styles.opciones}>
        <Checkbox
          checked={opciones.hasHeader}
          disabled={!ruta}
          onChange={(v) => onCambiar({ hasHeader: v })}
        >
          La primera línea son los nombres de las columnas
        </Checkbox>
        <Checkbox
          checked={opciones.emptyAsNull}
          disabled={!ruta}
          onChange={(v) => onCambiar({ emptyAsNull: v })}
        >
          El campo vacío es NULL
          <span className={styles.nota}>y no la cadena vacía</span>
        </Checkbox>
        <Checkbox
          checked={opciones.trim}
          disabled={!ruta}
          onChange={(v) => onCambiar({ trim: v })}
        >
          Recortar los espacios de los bordes
        </Checkbox>
      </div>

      {mirando ? (
        <div className={styles.cargando}>
          <Spinner />
        </div>
      ) : inspeccion ? (
        <>
          <div className={styles.hechos}>
            <Hecho valor={inspeccion.file.name} label="archivo" />
            <Hecho valor={tamano(inspeccion.file.bytes)} label="tamaño" />
            <Hecho valor={miles(inspeccion.rows)} label={plural(inspeccion.rows, "fila", "filas")} />
            <Hecho
              valor={String(inspeccion.columns?.length ?? 0)}
              label={plural(inspeccion.columns?.length ?? 0, "columna", "columnas")}
            />
          </div>

          {inspeccion.notUtf8 ? (
            <Aviso tono="malo">
              El archivo tiene bytes que no son UTF-8. Importarlo así escribe los acentos mal
              en la base y no hay forma de darse cuenta después. Convertirlo necesita saber de
              qué codificación viene —latin-1, windows-1252—, y adivinar sería peor: guardalo
              de nuevo como UTF-8 desde donde salió.
            </Aviso>
          ) : null}
          {(inspeccion.ragged?.length ?? 0) > 0 ? (
            <Aviso tono="ojo">
              {miles(inspeccion.ragged!.length)}{" "}
              {plural(inspeccion.ragged!.length, "línea tiene", "líneas tienen")} otra cantidad
              de campos que la primera —la {inspeccion.ragged![0]!.line}, con{" "}
              {inspeccion.ragged![0]!.fields}—. Si son muchas, mirá el delimitador antes que
              las líneas.
            </Aviso>
          ) : null}
          {(inspeccion.columns?.length ?? 0) === 1 && inspeccion.rows > 0 ? (
            <Aviso tono="ojo">
              Todo el archivo entra en una sola columna. Casi siempre es el delimitador: mirá
              abajo con qué están separados los campos de verdad.
            </Aviso>
          ) : null}

          <div className={styles.etiqueta}>Las primeras líneas, tal como están</div>
          <pre className={styles.crudo}>{inspeccion.raw || "(vacío)"}</pre>
        </>
      ) : null}
    </>
  );
}

function Hecho({ valor, label }: { valor: string; label: string }) {
  return (
    <span className={styles.hecho}>
      <span className={styles.hechoValor}>{valor}</span>
      <span className={styles.hechoLabel}>{label}</span>
    </span>
  );
}

function Aviso({ tono, children }: { tono: "ojo" | "malo"; children: React.ReactNode }) {
  return (
    <p className={cx(styles.aviso, tono === "malo" ? styles.avisoMalo : styles.avisoOjo)}>
      <span className={styles.avisoBang} aria-hidden="true">
        !
      </span>
      <span>{children}</span>
    </p>
  );
}

/* ---- 2. mapeo ------------------------------------------------------------ */

function Mapeo({
  columnasDelArchivo,
  muestra,
  columnasDeLaTabla,
  mapeo,
  sinMapear,
  faltantes,
  onCambiar,
}: {
  columnasDelArchivo: readonly string[];
  muestra: readonly (string[] | null)[];
  columnasDeLaTabla: readonly DetailColumn[];
  mapeo: readonly string[];
  sinMapear: readonly string[];
  faltantes: readonly DetailColumn[];
  onCambiar: (i: number, v: string) => void;
}) {
  const opciones: ComboOption[] = [
    { value: "", label: "— no importar —" },
    ...columnasDeLaTabla.map((c) => ({
      value: c.name,
      tag: c.dataType,
      title: `${c.dataType}${c.nullable ? "" : " · obligatoria"}${c.primaryKey ? " · clave primaria" : ""}`,
    })),
  ];

  return (
    <>
      <h3 className={styles.titulo}>A dónde va cada columna</h3>
      <p className={styles.subtitulo}>
        Emparejadas por nombre. Lo que no coincidió queda sin elegir a propósito: asignarlo
        por posición importaría un archivo con las columnas en otro orden cruzado y en
        silencio.
      </p>

      <div className={styles.mapa}>
        {columnasDelArchivo.map((c, i) => (
          <div key={i} className={styles.mapaFila}>
            <div className={styles.mapaOrigen}>
              <span className={styles.mapaNombre}>{c}</span>
              <span className={styles.mapaMuestra}>
                {muestra
                  .slice(0, FILAS_DE_MUESTRA)
                  .map((f) => f?.[i] ?? "")
                  .filter((v) => v !== "")
                  .join(" · ") || "sin datos"}
              </span>
            </div>
            <span className={styles.mapaFlecha} aria-hidden="true">
              →
            </span>
            <div className={styles.mapaDestino}>
              <Combobox
                value={mapeo[i] ?? ""}
                options={opciones}
                estricto
                ariaLabel={`Destino de ${c}`}
                placeholder="— no importar —"
                vacio="Ninguna columna coincide"
                onChange={(v) => onCambiar(i, v)}
              />
            </div>
          </div>
        ))}
      </div>

      {faltantes.length > 0 ? (
        <Aviso tono="ojo">
          {faltantes.map((c) => c.name).join(", ")}{" "}
          {plural(faltantes.length, "es obligatoria y no recibe", "son obligatorias y no reciben")}{" "}
          ningún valor. El servidor va a rechazar la importación; el ensayo del paso siguiente
          lo confirma.
        </Aviso>
      ) : null}
      {sinMapear.length > 0 && faltantes.length === 0 ? (
        <p className={styles.pieNota}>
          Sin llenar: {sinMapear.join(", ")}. Toman su valor por defecto.
        </p>
      ) : null}
    </>
  );
}

/* ---- 3. validación ------------------------------------------------------- */

function Validacion({
  corrida,
  filas,
  desparejas,
  conflicto,
  soloLectura,
  razon,
  necesitaPalabra,
  palabra,
  confirmacion,
  confirmado,
  onConfirmacion,
  onConflicto,
  onEnsayar,
}: {
  corrida: Corrida;
  filas: number;
  desparejas: number;
  conflicto: OnConflict;
  soloLectura: boolean;
  razon: string;
  necesitaPalabra: boolean;
  palabra: string;
  confirmacion: string;
  confirmado: boolean;
  onConfirmacion: (v: string) => void;
  onConflicto: (c: OnConflict) => void;
  onEnsayar: () => void;
}) {
  return (
    <>
      <h3 className={styles.titulo}>Si el servidor lo acepta</h3>
      <p className={styles.subtitulo}>
        El ensayo hace la importación de verdad adentro de una transacción y la revierte. No
        se comprueban los tipos acá: decir «esto parece una fecha» sería inventar las reglas
        del motor y errarle justo en los casos raros. La respuesta la da el servidor.
      </p>

      <div className={styles.etiqueta}>Si una fila choca con una que ya está</div>
      <div className={styles.pills} role="radiogroup" aria-label="Qué hacer con las que chocan">
        <button
          type="button"
          role="radio"
          aria-checked={conflicto === OnConflict.ConflictFail}
          className={cx(styles.pill, conflicto === OnConflict.ConflictFail && styles.pillActiva)}
          onClick={() => onConflicto(OnConflict.ConflictFail)}
        >
          Cortar la importación
        </button>
        <button
          type="button"
          role="radio"
          aria-checked={conflicto === OnConflict.ConflictSkip}
          className={cx(styles.pill, conflicto === OnConflict.ConflictSkip && styles.pillActiva)}
          onClick={() => onConflicto(OnConflict.ConflictSkip)}
        >
          Saltearla y seguir
        </button>
      </div>
      <p className={styles.pieNota}>
        {conflicto === OnConflict.ConflictFail
          ? "El default, porque es el único que no pierde información en silencio: si hay choques, te enterás."
          : "Se deja la fila que ya estaba. Las salteadas no se cuentan como importadas."}
      </p>

      {soloLectura ? (
        <Aviso tono="malo">{razon} No se puede importar en esta conexión.</Aviso>
      ) : necesitaPalabra ? (
        <Confirmar
          palabra={palabra}
          valor={confirmacion}
          ok={confirmado}
          onCambiar={onConfirmacion}
          nota="El ensayo también lo pide: inserta las filas de verdad antes de revertirlas, así que toma los mismos candados de la tabla."
        />
      ) : null}

      <div className={styles.resultado}>
        {corrida.fase === "corriendo" ? (
          <div className={styles.cargando}>
            <Spinner />
            <span className={styles.nota}>
              Insertando {miles(filas)} {plural(filas, "fila", "filas")} para revertirlas…
            </span>
          </div>
        ) : corrida.fase === "listo" ? (
          <Veredicto res={corrida.res} ensayo />
        ) : corrida.fase === "error" ? (
          <p className={styles.error} role="alert">
            {corrida.mensaje}
          </p>
        ) : corrida.fase === "cancelado" ? (
          <div className={styles.enEspera}>
            <Button variant="primary" disabled={soloLectura || !confirmado} onClick={onEnsayar}>
              Ensayar de nuevo
            </Button>
            <span className={styles.nota}>
              Ensayo cancelado. No quedó nada, y no porque se revirtiera: el ensayo nunca
              confirma la transacción.
            </span>
          </div>
        ) : (
          <div className={styles.enEspera}>
            <Button
              variant="primary"
              disabled={soloLectura || !confirmado}
              onClick={onEnsayar}
            >
              Ensayar
            </Button>
            <span className={styles.nota}>
              {desparejas > 0
                ? `Ojo: ${miles(desparejas)} ${plural(desparejas, "línea despareja", "líneas desparejas")} en el archivo.`
                : "Nada de esto queda escrito."}
            </span>
          </div>
        )}
      </div>
    </>
  );
}

/* ---- 4. importar --------------------------------------------------------- */

function Importar({
  corrida,
  ensayo,
  filas,
  destino,
  columnas,
  conflicto,
  archivo,
  necesitaPalabra,
  palabra,
  confirmacion,
  confirmado,
  onConfirmacion,
}: {
  corrida: Corrida;
  ensayo: Corrida;
  filas: number;
  destino: string;
  columnas: number;
  conflicto: OnConflict;
  archivo: string;
  necesitaPalabra: boolean;
  palabra: string;
  confirmacion: string;
  confirmado: boolean;
  onConfirmacion: (v: string) => void;
}) {
  const ensayoOK = ensayo.fase === "listo" && ensayo.res.ok;
  return (
    <>
      <h3 className={styles.titulo}>Qué se va a escribir</h3>
      <dl className={styles.resumen}>
        <dt>Archivo</dt>
        <dd>{archivo}</dd>
        <dt>Destino</dt>
        <dd>{destino}</dd>
        <dt>Filas</dt>
        <dd>
          {miles(filas)} {plural(filas, "fila", "filas")} en {columnas}{" "}
          {plural(columnas, "columna", "columnas")}
        </dd>
        <dt>Si chocan</dt>
        <dd>
          {conflicto === OnConflict.ConflictFail
            ? "se corta la importación entera"
            : "se saltean y sigue"}
        </dd>
        <dt>Ensayo</dt>
        <dd className={ensayoOK ? styles.ok : styles.dim}>
          {ensayo.fase !== "listo"
            ? "sin correr"
            : ensayoOK
              ? `pasó · ${miles(Number(ensayo.res.inserted))} ${plural(Number(ensayo.res.inserted), "fila entraría", "filas entrarían")}`
              : "falló"}
        </dd>
      </dl>

      {ensayo.fase === "listo" && !ensayo.res.ok ? (
        <Aviso tono="malo">
          El ensayo falló y la importación va a fallar igual: es la misma operación sin el
          rollback. Volvé al paso anterior a ver el motivo.
        </Aviso>
      ) : null}
      {ensayo.fase !== "listo" ? (
        <Aviso tono="ojo">
          Esto no se ensayó. Si algo no entra, la importación se corta y no queda ninguna
          fila —va en una sola transacción—, pero el motivo lo vas a ver recién acá.
        </Aviso>
      ) : null}

      {necesitaPalabra ? (
        <Confirmar
          palabra={palabra}
          valor={confirmacion}
          ok={confirmado}
          onCambiar={onConfirmacion}
          nota="Esto escribe en producción."
        />
      ) : null}

      <div className={styles.resultado}>
        {corrida.fase === "corriendo" ? (
          <div className={styles.cargando}>
            <Spinner />
            <span className={styles.nota}>Escribiendo en una sola transacción…</span>
          </div>
        ) : corrida.fase === "listo" ? (
          <Veredicto res={corrida.res} ensayo={false} />
        ) : corrida.fase === "error" ? (
          <p className={styles.error} role="alert">
            {corrida.mensaje}
          </p>
        ) : corrida.fase === "cancelado" ? (
          <p className={styles.nota}>
            Importación cancelada. No quedó ninguna fila: iba todo en una sola transacción y
            se cortó antes de confirmarla.
          </p>
        ) : null}
      </div>
    </>
  );
}

function Confirmar({
  palabra,
  valor,
  ok,
  nota,
  onCambiar,
}: {
  palabra: string;
  valor: string;
  ok: boolean;
  nota: string;
  onCambiar: (v: string) => void;
}) {
  return (
    <section className={styles.confirmar}>
      <div className={styles.confirmarTitulo}>
        <span className={styles.avisoBang} aria-hidden="true">
          !
        </span>
        Esto es producción
      </div>
      <p className={styles.nota}>
        {nota} Escribí el nombre de la base para habilitar el botón.
      </p>
      <Input
        value={valor}
        placeholder={palabra}
        aria-label={`Escribí ${palabra} para confirmar`}
        onChange={(e) => onCambiar(e.currentTarget.value)}
        className={ok ? styles.okInput : styles.malInput}
      />
    </section>
  );
}

/** Cómo terminó una corrida. Dice lo que PASÓ, no lo que se esperaba: el
 *  ensayo afirma haber revertido solo si Go lo afirma. */
function Veredicto({ res, ensayo }: { res: ImportResult; ensayo: boolean }) {
  if (!res.ok) {
    return <Fallo failure={res.failure ?? null} linea={res.line ?? 0} />;
  }
  const n = Number(res.inserted);
  return (
    <div className={styles.veredicto}>
      <p className={styles.ok}>
        {ensayo
          ? `Entran ${miles(n)} ${plural(n, "fila", "filas")} de las ${miles(res.read)} del archivo.`
          : `Importadas ${miles(n)} ${plural(n, "fila", "filas")} de las ${miles(res.read)} del archivo.`}
      </p>
      {res.read > n ? (
        <p className={styles.nota}>
          {miles(res.read - n)} {plural(res.read - n, "fila chocó", "filas chocaron")} con una
          que ya estaba y se {plural(res.read - n, "salteó", "saltearon")}.
        </p>
      ) : null}
      <p className={styles.nota}>
        {ensayo
          ? res.rolledBack
            ? `Se revirtió: no quedó nada en la tabla. (${res.elapsedMs} ms)`
            : `El ensayo terminó pero no se pudo confirmar que revirtiera. (${res.elapsedMs} ms)`
          : `En ${res.elapsedMs} ms.`}
      </p>
    </div>
  );
}

function Fallo({ failure, linea }: { failure: Failure | null; linea: number }) {
  return (
    <div className={styles.fallo} role="alert">
      <p className={styles.falloMensaje}>
        {failure?.message ?? "La importación falló sin detalle."}
      </p>
      {linea > 0 ? <p className={styles.nota}>Línea {miles(linea)} del archivo.</p> : null}
      {failure?.hint ? <p className={styles.nota}>{failure.hint}</p> : null}
      {failure?.sqlState ? <p className={styles.dim}>{failure.sqlState}</p> : null}
    </div>
  );
}

function Pie({
  paso,
  ensayo,
  importacion,
  soloLectura,
  razon,
}: {
  paso: Paso;
  ensayo: Corrida;
  importacion: Corrida;
  soloLectura: boolean;
  razon: string;
}) {
  if (soloLectura) {
    return (
      <span className={styles.pieError} role="alert">
        {razon}
      </span>
    );
  }
  if (importacion.fase === "listo" && importacion.res.ok) {
    return <span className={styles.ok}>Listo. La tabla ya tiene las filas nuevas.</span>;
  }
  if (paso === "validacion" && ensayo.fase === "listo" && ensayo.res.ok) {
    return <span className={styles.dim}>El ensayo pasó. Nada quedó escrito todavía.</span>;
  }
  return (
    <span className={styles.dim}>
      Los valores viajan como parámetros: nunca se pegan dentro de la sentencia.
    </span>
  );
}
