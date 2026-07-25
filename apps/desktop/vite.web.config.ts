import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "path";

process.env.BROWSERSLIST_IGNORE_OLD_DATA = "1";

// 纯 Web 开发配置（不包含 Electron）
export default defineConfig({
  plugins: [react()],
  root: "src/renderer",
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
      "@guizhi/core": path.resolve(__dirname, "../../packages/core/src"),
      "@shared": path.resolve(__dirname, "../../packages/shared"),
      "@guizhi/shared": path.resolve(__dirname, "../../packages/shared"),
      "@guizhi/db": path.resolve(__dirname, "../../packages/db/src"),
      "@renderer": path.resolve(__dirname, "src/renderer"),
    },
  },
  server: {
    port: 5173,
  },
});
