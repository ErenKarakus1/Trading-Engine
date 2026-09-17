import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/orders": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
      },
      "/orderbook": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
      },
      "/trades": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
      },
      "/events": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
      },
      "/marketdata": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
      },
      "/ws": {
        target: "http://127.0.0.1:8080",
        changeOrigin: true,
        ws: true,
      },
    },
  },
});
