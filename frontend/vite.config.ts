import { defineConfig } from "vite";
import type { Plugin } from "vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";

/**
 * La Content-Security-Policy del webview, solo en el build.
 *
 * Es la segunda capa que no existía (K-15 de la auditoría del 2026-09-11):
 * hoy no hay ningún sumidero de HTML ni recurso remoto —`logs_test.go` lo
 * exige—, pero el día que aparezca una inyección desde un valor de celda, los
 * bindings que escriben y leen rutas arbitrarias son «escribí esto en
 * Inicio\Programas». Con `script-src 'self'` un script inyectado no corre.
 *
 * Solo en el build porque en desarrollo Vite inyecta un script inline (el
 * preámbulo de React Refresh) y habla por websocket con HMR, y una política
 * que los permita no protegería nada. `style-src 'unsafe-inline'` sí hace
 * falta en producción: CodeMirror y xyflow inyectan sus propias hojas de
 * estilo. `connect-src 'self'` alcanza para el puente de Wails, que es un
 * fetch al mismo origen.
 */
const CSP = [
  "default-src 'self'",
  "script-src 'self'",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data: blob:",
  "font-src 'self' data:",
  "connect-src 'self'",
  "object-src 'none'",
  "base-uri 'none'",
  "form-action 'none'",
  "frame-ancestors 'none'",
].join("; ");

function csp(): Plugin {
  return {
    name: "kaname-csp",
    apply: "build",
    transformIndexHtml(html) {
      return html.replace(
        '<meta charset="UTF-8" />',
        `<meta charset="UTF-8" />\n    <meta http-equiv="Content-Security-Policy" content="${CSP}" />`,
      );
    },
  };
}

export default defineConfig({
  server: {
    // Solo loopback y puerto fijo: el dev server nunca debe quedar expuesto.
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [
    react({
      // React Compiler: memoiza automáticamente. Ver CLAUDE.md — no usar
      // useMemo/useCallback/React.memo por defecto.
      babel: { plugins: [["babel-plugin-react-compiler", {}]] },
    }),
    wails("./bindings"),
    csp(),
  ],
});
