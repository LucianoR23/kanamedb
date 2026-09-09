import type {
  ChangesetView,
  ChangeView,
} from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import { Button } from "../components/ui";
import { cx } from "../lib/cx";
import styles from "./ErdEditPanel.module.css";

/**
 * El panel derecho en modo edición: qué está armado y en qué orden va a correr.
 *
 * Muestra el changeset ENTERO y no solo lo del esquema dibujado. Un cambio hecho
 * desde la pantalla de estructura de otra tabla se aplica en el mismo apply, y
 * esconderlo acá haría creer que se van a ejecutar menos cosas de las que se van
 * a ejecutar.
 */
export function ErdEditPanel({
  vista,
  onQuitar,
  onDescartar,
  onRevisar,
}: {
  vista: ChangesetView | null;
  onQuitar: (id: string) => void;
  onDescartar: () => void;
  onRevisar: () => void;
}) {
  const cambios = vista?.changes ?? [];

  return (
    <>
      <div className={styles.cabecera}>
        <span className={styles.titulo}>Cambios preparados</span>
        <span className={styles.grow} />
        <span className={cx(styles.contador, cambios.length > 0 && styles.contadorActivo)}>
          {cambios.length}
        </span>
      </div>

      <div className={styles.cuerpo}>
        {cambios.length === 0 ? (
          <p className={styles.vacio}>
            Todavía no hay nada preparado. Elegí una herramienta arriba y tocá el diagrama: cada
            edición se junta acá y nada toca la base hasta que la apliques.
          </p>
        ) : (
          <div className={styles.tarjetas}>
            {cambios.map((v) => (
              <div
                key={v.change.id}
                className={cx(styles.tarjeta, v.statement.destructive && styles.tarjetaPeligro)}
              >
                <div className={styles.tarjetaCabeza}>
                  <span
                    className={cx(styles.op, v.statement.destructive && styles.opPeligro)}
                  >
                    {ETIQUETA[v.change.type] ?? v.change.type}
                  </span>
                  <span className={styles.objetivo}>{objetivo(v)}</span>
                  <span className={styles.grow} />
                  <button
                    type="button"
                    className={styles.quitar}
                    title="Sacar este cambio"
                    aria-label={`Sacar ${ETIQUETA[v.change.type] ?? v.change.type}`}
                    onClick={() => onQuitar(v.change.id)}
                  >
                    ✕
                  </button>
                </div>
                <div className={styles.sql}>{v.statement.sql}</div>
                {v.statement.note ? (
                  <div
                    className={cx(
                      styles.nota,
                      v.statement.destructive ? styles.notaPeligro : styles.notaAviso,
                    )}
                  >
                    {v.statement.note}
                  </div>
                ) : null}
              </div>
            ))}
          </div>
        )}

        {(vista?.order ?? []).length > 1 ? (
          <div className={styles.orden}>
            <div className={styles.titulo}>Orden de las sentencias</div>
            <ol className={styles.ordenLista}>
              {(vista?.order ?? []).map((v) => (
                <li key={v.change.id}>{objetivo(v)}</li>
              ))}
            </ol>
            <p className={styles.ordenNota}>
              Lo calcula Kaname por dependencias: una tabla nueva antes que la clave que la
              apunta, una restricción antes que la columna que la sostiene. No se puede reordenar
              a mano — un orden elegido a dedo puede no poder ejecutarse, y para dejar algo afuera
              está el destildado en la pantalla de cambios.
            </p>
          </div>
        ) : null}
      </div>

      <div className={styles.pie}>
        <Button size="sm" disabled={cambios.length === 0} onClick={onRevisar}>
          Revisar la SQL y aplicar
        </Button>
        <Button
          size="sm"
          variant="secondary"
          disabled={cambios.length === 0}
          onClick={onDescartar}
        >
          Descartar todo
        </Button>
      </div>
    </>
  );
}

function objetivo(v: ChangeView): string {
  const c = v.change;
  const detalle = c.column?.name ?? c.name ?? "";
  return detalle ? `${c.table}.${detalle}` : c.table;
}

const ETIQUETA: Record<string, string> = {
  createTable: "CREATE TABLE",
  dropTable: "DROP TABLE",
  renameTable: "RENAME",
  setTableComment: "COMMENT",
  addColumn: "ADD COLUMN",
  dropColumn: "DROP COLUMN",
  renameColumn: "RENAME",
  setColumnType: "ALTER TYPE",
  setNotNull: "SET NOT NULL",
  dropNotNull: "DROP NOT NULL",
  setDefault: "SET DEFAULT",
  dropDefault: "DROP DEFAULT",
  setColumnComment: "COMMENT",
  addPrimaryKey: "ADD PK",
  addForeignKey: "ADD FK",
  addCheck: "ADD CHECK",
  addUnique: "ADD UNIQUE",
  dropConstraint: "DROP CONSTRAINT",
  addIndex: "CREATE INDEX",
  dropIndex: "DROP INDEX",
};
