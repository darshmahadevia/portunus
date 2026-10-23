import { defineConfig } from "vite";
export default defineConfig({
  build: { outDir: "../internal/dashboard/dist", emptyOutDir: true },
  server: {
    proxy: { "/api": "http://localhost:8080", "/v1": "http://localhost:8080" },
  },
});
