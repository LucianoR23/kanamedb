import { useState } from "react";
import type { SessionView } from "../../bindings/github.com/LucianoR23/kanamedb/internal/service";
import type { ToastItem } from "../components/ui";
import { cx } from "../lib/cx";
import { Tablas } from "./Tablas";
import { Consulta } from "./Consulta";
import { Historial } from "./Historial";
import { Ajustes } from "./Ajustes";
import styles from "./mobile.module.css";

type Pestana = "tablas" | "sql" | "historial" | "ajustes";

const PESTANAS: { id: Pestana; icono: string; nombre: string }[] = [
  { id: "tablas", icono: "▤", nombre: "Tablas" },
  { id: "sql", icono: "›_", nombre: "SQL" },
  { id: "historial", icono: "↺", nombre: "Historial" },
  { id: "ajustes", icono: "⚙", nombre: "Ajustes" },
];

/**
 * La sesión abierta: cuatro pestañas abajo. Cada una es dueña de su estado
 * mientras esté montada; cambiar de pestaña desmonta la anterior, así que una
 * consulta a medio escribir se conserva en el propio componente (Consulta) y
 * no acá.
 */
export function Sesion({
  sesion,
  onDesconectar,
  onSesionCerrada,
  onAviso,
}: {
  sesion: SessionView;
  onDesconectar: () => void;
  onSesionCerrada: (motivo: string) => void;
  onAviso: (t: ToastItem) => void;
}) {
  const [pestana, setPestana] = useState<Pestana>("tablas");
  // Una consulta que el historial quiere volver a correr, esperando que la
  // pestaña SQL se monte y la tome.
  const [sqlPedida, setSqlPedida] = useState<string | null>(null);

  return (
    <>
      {pestana === "tablas" ? (
        <Tablas sesion={sesion} onSesionCerrada={onSesionCerrada} onAviso={onAviso} />
      ) : pestana === "sql" ? (
        <Consulta
          sesion={sesion}
          inicial={sqlPedida}
          onTomada={() => setSqlPedida(null)}
          onSesionCerrada={onSesionCerrada}
          onAviso={onAviso}
        />
      ) : pestana === "historial" ? (
        <Historial
          sesion={sesion}
          onRepetir={(sql) => {
            setSqlPedida(sql);
            setPestana("sql");
          }}
        />
      ) : (
        <Ajustes sesion={sesion} onDesconectar={onDesconectar} onAviso={onAviso} />
      )}

      <nav className={styles.nav} aria-label="Secciones">
        {PESTANAS.map((p) => (
          <button
            key={p.id}
            type="button"
            className={cx(styles.navItem, pestana === p.id && styles.navActivo)}
            onClick={() => setPestana(p.id)}
            aria-current={pestana === p.id ? "page" : undefined}
          >
            <span aria-hidden="true">{p.icono}</span>
            <span>{p.nombre}</span>
          </button>
        ))}
      </nav>
    </>
  );
}

/** La barra de arriba de una pestaña, con el nombre de la sesión. */
export function BarraDeSesion({
  sesion,
  titulo,
  atras,
  derecha,
}: {
  sesion: SessionView;
  titulo?: string;
  atras?: () => void;
  derecha?: React.ReactNode;
}) {
  const prod = sesion.environment === "production";
  return (
    <header className={cx(styles.barra, prod && styles.prod)}>
      {atras ? (
        <button type="button" className={styles.icono} onClick={atras} aria-label="Volver">
          ‹
        </button>
      ) : null}
      <div className={styles.titulo}>
        <strong>{titulo ?? sesion.name}</strong>
        <small>
          {sesion.describe}
          {sesion.readOnly ? " · solo lectura" : ""}
        </small>
      </div>
      {derecha}
    </header>
  );
}
