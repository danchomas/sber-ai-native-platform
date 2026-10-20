import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// В разработке фронтенд ходит в API соседнего процесса: платформа
// поднимается через `make run` или docker compose, Vite проксирует к ней
// /api, чтобы не возиться с CORS.
const apiTarget = process.env.VITE_API_TARGET || "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: apiTarget, changeOrigin: true },
    },
  },
  build: {
    outDir: "dist",
    sourcemap: true,
  },
});
