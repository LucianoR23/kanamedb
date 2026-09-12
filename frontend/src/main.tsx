import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import { Mobile } from "./mobile/Mobile";
import "./styles/tokens.css";

const root = document.getElementById("root");
if (!root) throw new Error("No se encontró #root");

/**
 * Qué interfaz se dibuja.
 *
 * En Android, la del teléfono: mismo backend, otra forma (ver
 * kaname-android.md). Se decide por el user agent del WebView y no por un
 * binding, porque es una decisión de dibujo que tiene que tomarse antes de
 * que llegue ninguna respuesta; nada de seguridad depende de ella. `?movil`
 * en la URL fuerza la del teléfono en escritorio, para probarla en la PC con
 * las herramientas de desarrollo en modo dispositivo.
 */
const movil =
  /\bAndroid\b/.test(navigator.userAgent) || new URLSearchParams(location.search).has("movil");

// El tema oscuro es el default y es el de :root. El claro lo pone Ajustes
// con data-theme="light" en la raíz; ver lib/preferencias.
createRoot(root).render(<StrictMode>{movil ? <Mobile /> : <App />}</StrictMode>);
