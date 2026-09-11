import { useEffect, useState } from "react";
import type { HistoryEntry, SavedQuery } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import * as HistorySvc from "../../bindings/github.com/LucianoR23/kanamedb/internal/service/history";
import { Button, ConfirmDialog } from "../components/ui";
import { textoDe } from "../lib/dialogos";
import styles from "./HistoryPanel.module.css";

/**
 * S21: el historial y las consultas guardadas.
 *
 * Son las dos pestañas que el sidebar declaraba desde la Iteración 1 y que
 * hasta ahora decían «llega en la Iteración 9». Eso, a diferencia del chip
 * «Ctrl K», era honesto: prometía una fecha, no una función.
 *
 * Las dos listas se ven parecidas y son cosas distintas, y el panel lo dice:
 * el historial es de esta máquina y se llena solo; las guardadas se sincronizan
 * con la libreta de conexiones y se llenan a mano.
 */
export function HistoryPanel({
  modo,
  recarga,
  conectado,
  onAbrir,
}: {
  modo: "history" | "queries";
  /** Cambia cuando algo pudo haber tocado la lista: una consulta que corrió,
   *  una que se guardó, una conexión nueva. */
  recarga: number;
  conectado: boolean;
  /** Abre el texto en una pestaña de consulta nueva. */
  onAbrir: (sql: string) => void;
}) {
  const [entradas, setEntradas] = useState<HistoryEntry[]>([]);
  const [guardadas, setGuardadas] = useState<SavedQuery[]>([]);
  const [error, setError] = useState("");
  const [cargando, setCargando] = useState(true);
  const [confirmando, setConfirmando] = useState(false);

  useEffect(() => {
    let vigente = true;
    setCargando(true);
    setError("");
    const pedido =
      modo === "history"
        ? HistorySvc.List(200).then((v) => vigente && setEntradas(v ?? []))
        : HistorySvc.Saved().then((v) => vigente && setGuardadas(v ?? []));
    pedido
      .catch((err: unknown) => vigente && setError(textoDe(err)))
      .finally(() => vigente && setCargando(false));
    return () => {
      vigente = false;
    };
  }, [modo, recarga, conectado]);

  async function borrarTodo() {
    try {
      await HistorySvc.Clear();
      setEntradas([]);
    } catch (err: unknown) {
      setError(textoDe(err));
    }
  }

  async function borrarGuardada(id: string) {
    try {
      await HistorySvc.DeleteSaved(id);
      setGuardadas((prev) => prev.filter((q) => q.id !== id));
    } catch (err: unknown) {
      setError(textoDe(err));
    }
  }

  if (cargando) return <p className={styles.nota}>Leyendo…</p>;
  if (error) return <p className={styles.error}>{error}</p>;

  if (modo === "queries") {
    if (guardadas.length === 0) {
      return (
        <p className={styles.nota}>
          Todavía no guardaste ninguna consulta. En el editor SQL, «Guardar…» le pone un nombre a
          la que tengas escrita — y esas viajan con la libreta de conexiones, así que las vas a
          encontrar en la otra máquina.
        </p>
      );
    }
    return (
      <div className={styles.lista}>
        {guardadas.map((q) => (
          <div key={q.id} className={styles.fila}>
            <button
              type="button"
              className={styles.abrir}
              onClick={() => onAbrir(q.sql)}
              title={q.sql}
            >
              <span className={styles.nombre}>{q.name}</span>
              <span className={styles.sql}>{unaLinea(q.sql)}</span>
            </button>
            <Button
              variant="ghost"
              size="sm"
              aria-label={`Borrar ${q.name}`}
              onClick={() => void borrarGuardada(q.id)}
            >
              ✕
            </Button>
          </div>
        ))}
      </div>
    );
  }

  if (!conectado) {
    return <p className={styles.nota}>El historial es de cada conexión. Abrí una para verlo.</p>;
  }
  if (entradas.length === 0) {
    return (
      <p className={styles.nota}>
        Todavía no corriste ninguna consulta contra esta conexión. Las que lleven una contraseña
        escrita no se guardan acá: los secretos van al keychain del sistema.
      </p>
    );
  }

  return (
    <div className={styles.lista}>
      <div className={styles.cabecera}>
        <span className={styles.nota}>{entradas.length} en esta conexión</span>
        <span className={styles.spacer} />
        {/* Pregunta antes, como cualquier otra cosa que borra en esta app.
            Acá el historial es lo único que no se puede volver a generar: la
            consulta que uno quiere recuperar es justamente la que no se acuerda
            de haber escrito. */}
        <Button variant="ghost" size="sm" onClick={() => setConfirmando(true)}>
          Borrar
        </Button>
      </div>
      <ConfirmDialog
        open={confirmando}
        severidad="aviso"
        title="Borrar el historial de esta conexión"
        etiqueta="Borrar"
        onClose={() => setConfirmando(false)}
        onConfirm={() => {
          setConfirmando(false);
          void borrarTodo();
        }}
      >
        {entradas.length === 1
          ? "Se va la única consulta que corriste contra esta conexión, y no hay de dónde recuperarla."
          : `Se van las ${entradas.length} consultas que corriste contra esta conexión, y no hay de dónde recuperarlas.`}{" "}
        Las que guardaste con nombre no se tocan: están en el otro archivo, el que viaja
        con la libreta de conexiones.
      </ConfirmDialog>
      {entradas.map((e) => (
        <button
          key={e.id}
          type="button"
          className={e.failed ? styles.filaFallada : styles.filaEntrada}
          onClick={() => onAbrir(e.sql)}
          // La hora exacta va acá, con la consulta. El renglón dice «hace 3
          // días» porque es lo que uno busca, pero después de un día eso deja de
          // alcanzar y el dato tiene que estar en algún lado.
          title={`${horaExacta(e.ranAt)}\n\n${e.sql}`}
        >
          <span className={styles.sql}>{unaLinea(e.sql)}</span>
          <span className={styles.meta}>
            {cuando(e.ranAt)}
            {e.runs > 1 ? ` · ${e.runs}×` : ""}
            {/* El fallo se dice, no se esconde: la consulta que uno busca en el
                historial es a menudo justamente la que falló, para arreglarla. */}
            {e.failed ? " · falló" : ` · ${e.rows} ${e.rows === 1 ? "fila" : "filas"}`}
            {` · ${e.elapsedMs} ms`}
          </span>
        </button>
      ))}
    </div>
  );
}

/** La primera línea con contenido, para el renglón de la lista. */
function unaLinea(sql: string): string {
  const linea = sql
    .split("\n")
    .map((l) => l.trim())
    .find((l) => l !== "" && !l.startsWith("--"));
  return linea ?? sql.trim().slice(0, 120);
}

/** La fecha y la hora completas, para el `title` del renglón. */
function horaExacta(iso: string): string {
  const t = new Date(iso);
  if (Number.isNaN(t.getTime())) return "";
  return t.toLocaleString();
}

/**
 * Hace cuánto, en palabras.
 *
 * Relativo y no la hora exacta: lo que uno busca en un historial es «lo de
 * recién» o «lo de esta mañana», y una lista de timestamps completos obliga a
 * restar mentalmente en cada renglón. La hora exacta está en el `title`.
 */
function cuando(iso: string): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "";
  const seg = Math.max(0, Math.round((Date.now() - t) / 1000));
  if (seg < 60) return "recién";
  const min = Math.round(seg / 60);
  if (min < 60) return `hace ${min} min`;
  const hs = Math.round(min / 60);
  if (hs < 24) return `hace ${hs} h`;
  const d = Math.round(hs / 24);
  return d === 1 ? "ayer" : `hace ${d} días`;
}
