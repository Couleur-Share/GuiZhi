// 生成 apps/desktop-go/viva/reader/assets/：阅读岛的页面（index.html + reader.js + reader.css [+ 按需加载的分块]）
// 与图片查看岛的页面（viewer/viewer.html + viewer.js + viewer.css）。
// 页面源码在 apps/desktop/src/renderer/reader/，复用渲染层的阅读组件与同一份 Tailwind / globals.css；
// 阅读页里的图片查看器换成垫片（lightbox-shim.tsx：交给宿主另开全窗口的查看岛），查看岛的页面（ViewerApp）渲染渲染层自己的 ImageLightbox。
// 阅读岛的样式表在构建时把 min-width 媒体查询改写成按窗口宽度判定（岛的视口只有阅读区那么宽，见 reader/breakpoints.ts）；查看岛盖满整个窗口，不改写。
// 产物入库，CI 要求与重新生成的结果一致：文件名固定（reader.js / reader.css / viewer/viewer.js，分块按内容哈希），不带构建时间与路径。
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
const postcss = (await import(pathToFileURL(require.resolve("postcss")).href)).default;
const { windowWidthMedia } = await import(pathToFileURL(path.join(readerDir, "window-width-media.ts")).href);

// 岛的视口只有阅读区那么宽，样式表里按视口宽度判定的 min-width 媒体查询要改成按窗口宽度判定（原因与做法见 reader/breakpoints.ts）。
// 在打包完成之后处理最终的 css 资源（enforce: post），不碰渲染层自己的构建配置。
const windowWidthCss = {
  name: "guizhi-window-width-media",
  enforce: "post" as const,
  async generateBundle(_options: unknown, bundle: Record<string, { type: string; fileName: string; source?: string | Uint8Array }>) {
    for (const file of Object.values(bundle)) {
      if (file.type !== "asset" || !file.fileName.endsWith(".css")) continue;
      const result = await postcss([windowWidthMedia()]).process(String(file.source), { from: undefined });
      file.source = result.css;
    }
  },
};

// Tailwind / PostCSS 按当前目录找配置：与 scripts/build-shell.ps1 在 apps/desktop 下执行 vite 一致。
process.chdir(desktop);

// 两次构建共用的别名（与 apps/desktop 的 tsconfig / vite 配置一致）。
const sharedAliases = [
  { find: "@guizhi/core", replacement: path.join(repo, "packages/core/src") },
  { find: "@guizhi/shared", replacement: path.join(repo, "packages/shared") },
  { find: "@guizhi/db", replacement: path.join(repo, "packages/db/src") },
  { find: "@shared", replacement: path.join(repo, "packages/shared") },
  { find: "@renderer", replacement: path.join(desktop, "src/renderer") },
  { find: "@", replacement: path.join(desktop, "src") },
];

await build({
  configFile: false,
  root: readerDir,
  base: "./",
  logLevel: "error",
  plugins: [react(), windowWidthCss],
  define: { "process.env.NODE_ENV": '"production"' },
  resolve: {
    alias: [
      // 渲染层的 ImageLightbox（全窗口浮层）在阅读岛里换成垫片，所有写法的 import 都命中。
      { find: /^(?:.*\/)?ImageLightbox$/, replacement: path.join(readerDir, "lightbox-shim.tsx") },
      ...sharedAliases,
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

// 图片查看岛：不换垫片，ViewerApp 渲染渲染层自己的 ImageLightbox。只清自己的子目录（上面的阅读岛构建已经清空过 outDir）。
// 不加 windowWidthCss：查看岛盖满整个窗口，视口就是窗口，媒体查询（含 yarl 自带的 min-width: 800px）本来就按窗口宽度判定。
await build({
  configFile: false,
  root: readerDir,
  base: "./",
  logLevel: "error",
  plugins: [react()],
  define: { "process.env.NODE_ENV": '"production"' },
  resolve: { alias: sharedAliases },
  build: {
    outDir: path.join(outDir, "viewer"),
    emptyOutDir: true,
    target: "chrome130",
    cssCodeSplit: false,
    chunkSizeWarningLimit: 8000,
    rollupOptions: {
      input: path.join(readerDir, "viewer.html"),
      output: {
        entryFileNames: "viewer.js",
        chunkFileNames: "chunks/[name]-[hash].js",
        assetFileNames: (info: { names?: string[] }) =>
          info.names?.some((name) => name.endsWith(".css")) ? "viewer[extname]" : "assets/[name]-[hash][extname]",
      },
    },
  },
});

const files = (await fs.readdir(outDir, { recursive: true })).map((name) => String(name).replace(/\\/g, "/")).sort();
console.log("ok", outDir, files);
