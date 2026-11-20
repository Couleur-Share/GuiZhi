// 生成 apps/desktop-go/viva/reader/assets/：阅读岛的页面（index.html + reader.js + reader.css [+ 按需加载的分块]）。
// 页面源码在 apps/desktop/src/renderer/reader/，复用渲染层的阅读组件与同一份 Tailwind / globals.css；
// 图片查看器换成垫片（lightbox-shim.tsx：交给宿主另开全窗口查看器）。
// 产物入库，CI 要求与重新生成的结果一致：文件名固定（reader.js / reader.css，分块按内容哈希），不带构建时间与路径。
// 用法（仓库根）：pnpm --filter @guizhi/desktop-go gen   或   pnpm dlx tsx apps/desktop-go/jsgen/build-reader.mts
import fs from "node:fs/promises";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, "..", "..", "..");
const desktop = path.join(repo, "apps", "desktop");
const readerDir = path.join(desktop, "src", "renderer", "reader");
const outDir = path.join(repo, "apps", "desktop-go", "viva", "reader", "assets");

const require = createRequire(path.join(desktop, "package.json"));
const { build } = await import(pathToFileURL(require.resolve("vite")).href);
const react = (await import(pathToFileURL(require.resolve("@vitejs/plugin-react")).href)).default;

// Tailwind / PostCSS 按当前目录找配置：与 scripts/build-shell.ps1 在 apps/desktop 下执行 vite 一致。
process.chdir(desktop);

await build({
  configFile: false,
  root: readerDir,
  base: "./",
  logLevel: "error",
  plugins: [react()],
  define: { "process.env.NODE_ENV": '"production"' },
  resolve: {
    alias: [
      // 渲染层的 ImageLightbox（全窗口浮层）在岛里换成垫片，所有写法的 import 都命中。
      { find: /^(?:.*\/)?ImageLightbox$/, replacement: path.join(readerDir, "lightbox-shim.tsx") },
      { find: "@guizhi/core", replacement: path.join(repo, "packages/core/src") },
      { find: "@guizhi/shared", replacement: path.join(repo, "packages/shared") },
      { find: "@guizhi/db", replacement: path.join(repo, "packages/db/src") },
      { find: "@shared", replacement: path.join(repo, "packages/shared") },
      { find: "@renderer", replacement: path.join(desktop, "src/renderer") },
      { find: "@", replacement: path.join(desktop, "src") },
    ],
  },
  build: {
    outDir,
    emptyOutDir: true,
    target: "chrome130",
    cssCodeSplit: false,
    chunkSizeWarningLimit: 8000,
    rollupOptions: {
      input: path.join(readerDir, "index.html"),
      output: {
        entryFileNames: "reader.js",
        chunkFileNames: "chunks/[name]-[hash].js",
        assetFileNames: (info: { names?: string[] }) =>
          info.names?.some((name) => name.endsWith(".css")) ? "reader[extname]" : "assets/[name]-[hash][extname]",
      },
    },
  },
});

const files = (await fs.readdir(outDir, { recursive: true })).map((name) => String(name).replace(/\\/g, "/")).sort();
console.log("ok", outDir, files);
