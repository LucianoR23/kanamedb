import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./styles/tokens.css";

const root = document.getElementById("root");
if (!root) throw new Error("No se encontró #root");

// El tema oscuro es el default y es el de :root. El claro lo pone Ajustes
// con data-theme="light" en la raíz; ver lib/preferencias.
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
