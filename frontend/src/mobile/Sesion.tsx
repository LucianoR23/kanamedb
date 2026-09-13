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
 * M05: la sesión abierta, con cuatro pestañas abajo. Cada una es dueña de su
 * estado mientras esté montada; cambiar de pestaña desmonta la anterior, así
 * que una consulta a medio escribir se conserva en el propio módulo
 * (Consulta) y no acá. Con una consulta corriendo, la pestaña SQL lleva un
 * punto que late.
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
  const [corriendo, setCorriendo] = useState(false);
  // Una tabla abierta ocupa la pantalla entera: la navegación se esconde.
  const [profunda, setProfunda] = useState(false);

  return (
    <div className={styles.pantalla}>
      {pestana === "tablas" ? (
        <Tablas sesion={sesion} onSesionCerrada={onSesionCerrada} onAviso={onAviso} onProfundidad={setProfunda} />
      ) : pestana === "sql" ? (
        <Consulta
          sesion={sesion}
          inicial={sqlPedida}
          onTomada={() => setSqlPedida(null)}
          onSesionCerrada={onSesionCerrada}
          onAviso={onAviso}
          onCorriendo={setCorriendo}
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

      {profunda && pestana === "tablas" ? null : (
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
            {p.id === "sql" && corriendo ? <span className={styles.navPunto} aria-label="consulta corriendo" /> : null}
          </button>
        ))}
      </nav>
      )}
    </div>
  );
}
