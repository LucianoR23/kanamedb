import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import "./styles/tokens.css";

const root = document.getElementById("root");
if (!root) throw new Error("No se encontró #root");

// El tema oscuro es el default. El claro llega en la Iteración 9; hasta
// entonces la raíz no lleva data-theme y manda :root.
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
