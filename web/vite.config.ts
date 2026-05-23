import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";

function readApiTarget(): string {
  const rawPort = process.env.PORT?.trim();
  if (rawPort) return `http://127.0.0.1:${rawPort}`;

  const dataDirs = process.env.AO3HUB_DATA_DIR?.trim()
    ? [path.resolve(process.env.AO3HUB_DATA_DIR)]
    : [path.resolve(__dirname, "../data"), path.resolve(__dirname, "../server/data")];
  const configPath = dataDirs
    .map((dir) => path.join(dir, "config.json"))
    .find((file) => existsSync(file));

  if (configPath) {
    try {
      const cfg = JSON.parse(readFileSync(configPath, "utf8"));
      const port = Number(cfg?.server?.port);
      if (Number.isInteger(port) && port >= 1 && port <= 65535) {
        return `http://127.0.0.1:${port}`;
      }
    } catch {}
  }

  return "http://127.0.0.1:3000";
}

const apiTarget = readApiTarget();

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
      "@ao3hub/shared": path.resolve(__dirname, "../shared/schema.ts"),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: apiTarget,
        changeOrigin: true,
        ws: false,
      },
    },
  },
  build: {
    outDir: "dist",
    sourcemap: false,
    chunkSizeWarningLimit: 1200,
  },
});
