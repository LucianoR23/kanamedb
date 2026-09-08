import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";

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
  ],
});
