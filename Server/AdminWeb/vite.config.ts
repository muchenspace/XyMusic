import { fileURLToPath, URL } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vite";

const developmentProxyTarget = process.env.VITE_DEV_PROXY_TARGET ?? "http://127.0.0.1:3000";

export default defineConfig({
  base: "/admin/",
  plugins: [vue(), tailwindcss()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    host: "0.0.0.0",
    port: 5173,
    proxy: {
      "/api": { target: developmentProxyTarget, changeOrigin: true },
      "/health": { target: developmentProxyTarget, changeOrigin: true },
    },
  },
  build: {
    outDir: "dist",
    assetsDir: "assets",
    sourcemap: false,
    manifest: true,
  },
});
