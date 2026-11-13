// 构建归知现有渲染进程（不含 Electron 插件），产物给 MyGo 窗口加载。
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

const desktop = path.resolve(__dirname, "../desktop");

export default defineConfig({
  root: desktop,
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(desktop, "src"),
      "@guizhi/core": path.resolve(desktop, "../../packages/core/src"),
      "@shared": path.resolve(desktop, "../../packages/shared"),
      "@guizhi/shared": path.resolve(desktop, "../../packages/shared"),
      "@guizhi/db": path.resolve(desktop, "../../packages/db/src"),
      "@renderer": path.resolve(desktop, "src/renderer"),
    },
  },
  build: {
    outDir: path.resolve(__dirname, "dist/renderer"),
    emptyOutDir: true,
    chunkSizeWarningLimit: 4000,
  },
});
