import { useState } from "react";
import type { ImportCandidate, ImportPreview } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Badge, Button, Checkbox, Dialog, EnvBadge, DialogClose } from "../components/ui";
import { cx } from "../lib/cx";
import { nombreDeMotor, plural } from "../lib/motor";
import styles from "./ImportConnectionsDialog.module.css";

/**
 * La vista previa de un archivo compartido, antes de agregar nada.
 *
 * Muestra lo que trae —nombre, motor, adónde apunta, entorno, carpeta— y deja
 * elegir. Lo que no se puede elegir se ve igual, con el motivo: una entrada
 * rota, para que se sepa que venía y qué le falta. Y lo que ya se tiene sale
 * destildado pero elegible: importar dos veces el mismo archivo no tiene por
 * qué duplicar la libreta sin avisar, pero tampoco impedirlo.
 *
 * La contraseña no viene en el archivo y no se pide acá: se pide al conectar.
 */
export function ImportConnectionsDialog({
  preview,
  importing,
  error,
  onCancel,
  onImport,
}: {
  preview: ImportPreview;
  importing: boolean;
  error: string | null;
  onCancel: () => void;
  onImport: (include: number[]) => void;
}) {
  const candidatas = preview.connections ?? [];
  const [elegidas, setElegidas] = useState<ReadonlySet<number>>(
    () => new Set(candidatas.filter(elegiblePorDefecto).map((c) => c.index)),
  );
  const notas = preview.notes ?? [];
  const n = elegidas.size;
  const produccion = candidatas.filter((c) => c.production && elegidas.has(c.index)).length;

  function alternar(c: ImportCandidate) {
    setElegidas((prev) => {
      const next = new Set(prev);
      if (next.has(c.index)) next.delete(c.index);
      else next.add(c.index);
      return next;
    });
  }

  return (
    <Dialog
      open
      size="lg"
      title="Importar conexiones"
      // Mientras importa no se cierra: la llamada sigue igual, y un diálogo
      // que desaparece y vuelve con un error se lee como una app rota.
      {...(importing ? {} : { onClose: onCancel })}
      footer={
        <>
          {error ? (
            <span className={styles.error} role="alert">
              {error}
            </span>
          ) : null}
          <span className={styles.grow} />
          <DialogClose onClose={onCancel} disabled={importing}>
            Cancelar
          </DialogClose>
          <Button
            variant="primary"
            disabled={n === 0 || importing}
            onClick={() => onImport([...elegidas].sort((a, b) => a - b))}
          >
            {importing ? "Importando…" : `Importar ${n} ${plural(n, "conexión", "conexiones")}`}
          </Button>
        </>
      }
    >
      <div className={styles.cuerpo}>
        <p className={styles.origen}>
          <span className={styles.ruta}>{preview.path}</span>
          {" · "}
          {candidatas.length} {plural(candidatas.length, "conexión", "conexiones")}
        </p>

        <ul className={styles.lista}>
          {candidatas.map((c) => {
            const rota = (c.problems?.length ?? 0) > 0;
            return (
              <li key={c.index} className={cx(styles.fila, rota && styles.filaRota)}>
                <Checkbox
                  checked={elegidas.has(c.index)}
                  disabled={rota || importing}
                  onChange={() => alternar(c)}
                >
                  <span className={styles.nombre}>{c.name || "(sin nombre)"}</span>
                </Checkbox>
                <div className={styles.detalle}>
                  <span className={styles.motor}>{nombreDeMotor(c.engine)}</span>
                  <span className={styles.describe}>{c.describe}</span>
                  <EnvBadge env={c.environment as "local" | "dev" | "staging" | "production"} />
                  {c.folder ? <Badge tone="neutral">carpeta {c.folder}</Badge> : null}
                  {c.ssh ? <Badge tone="info">túnel SSH</Badge> : null}
                </div>
                {c.existing ? (
                  <p className={styles.nota}>
                    Ya apunta ahí <strong>{c.existing}</strong>. Se puede importar igual: queda otra
                    entrada.
                  </p>
                ) : null}
                {c.sessionSql ? (
                  /* SQL escrita por otra persona que va a correr con las
                     credenciales de esta, en cada conexión y sin preguntar.
                     Se muestra entera: es lo que hay que leer antes de tildar. */
                  <div className={styles.sesion}>
                    <p className={styles.nota}>Al conectar corre esta SQL, en cada conexión:</p>
                    <pre className={styles.sesionSql}>{c.sessionSql}</pre>
                  </div>
                ) : null}
                {rota ? (
                  <ul className={styles.problemas}>
                    {(c.problems ?? []).map((p) => (
                      <li key={p.field}>{p.message}</li>
                    ))}
                  </ul>
                ) : null}
              </li>
            );
          })}
        </ul>

        {notas.length > 0 ? (
          <ul className={styles.avisos}>
            {notas.map((t) => (
              <li key={t}>{t}</li>
            ))}
          </ul>
        ) : null}

        <p className={styles.pie}>
          Cada conexión entra con un identificador nuevo y sin contraseña: se pide al conectar y
          queda en el keychain de esta máquina.
          {produccion > 0
            ? ` ${produccion} ${plural(produccion, "es de producción", "son de producción")}: entran con sus protecciones puestas.`
            : ""}
        </p>
      </div>
    </Dialog>
  );
}

/** Tildada de entrada salvo que esté rota o ya se tenga una igual. */
function elegiblePorDefecto(c: ImportCandidate): boolean {
  return (c.problems?.length ?? 0) === 0 && !c.existing;
}
