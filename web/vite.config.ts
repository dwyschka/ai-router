import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Der Dev-Server proxyt /api auf das Go-Backend, damit die WebUI im Entwicklungsmodus
// gegen denselben Router läuft wie im Betrieb — inklusive WebSocket-Upgrade.
export default defineConfig({
  plugins: [react()],
  build: { outDir: "dist", emptyOutDir: true },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:7777",
        changeOrigin: true,
        ws: true,
      },
    },
  },
});
