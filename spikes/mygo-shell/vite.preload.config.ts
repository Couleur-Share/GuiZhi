// 把归知现有的 preload/index.ts 打成一个 IIFE，由 MyGo 作为 PreloadScript 注入。
import { defineConfig } from "vite";
import path from "node:path";

const desktop = path.resolve(__dirname, "../../apps/desktop");

export default defineConfig({
  root: desktop,
  resolve: {
    alias: [
      { find: /^electron$/, replacement: path.resolve(__dirname, "shim/electron.ts") },
      { find: "@guizhi/core", replacement: path.resolve(desktop, "../../packages/core/src") },
      { find: "@shared", replacement: path.resolve(desktop, "../../packages/shared") },
      { find: "@guizhi/shared", replacement: path.resolve(desktop, "../../packages/shared") },
      { find: "@guizhi/db", replacement: path.resolve(desktop, "../../packages/db/src") },
      { find: "@", replacement: path.resolve(desktop, "src") },
    ],
  },
  build: {
    outDir: path.resolve(__dirname, "dist/preload"),
    emptyOutDir: true,
    lib: {
      entry: path.resolve(desktop, "src/preload/index.ts"),
      formats: ["iife"],
      name: "guizhiPreload",
      fileName: () => "preload.js",
    },
    minify: false,
  },
});
