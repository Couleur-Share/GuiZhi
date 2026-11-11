// 生成 spikes/mygo-shell/themed_engine.js：主题阅读渲染层（TS 原文件 + linkedom / sanitize-html / unified 等依赖）的 goja 打包产物。
// 用法（仓库根）：pnpm dlx tsx spikes/mygo-js/build-themed.mts
import { build } from "esbuild";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const out = path.join(here, "..", "mygo-shell", "themed_engine.js");
const shimNames = ["fs", "path", "url", "util", "os", "tty", "stream", "events", "buffer", "assert", "crypto", "process", "vm", "module", "child_process", "worker_threads", "http", "https", "net", "zlib"];
await build({
  entryPoints: [path.join(here, "themed-entry.ts")],
  outfile: out,
  bundle: true,
  format: "iife",
  platform: "node",
  target: "es2018",
  minify: process.env.GZ_NOMIN ? false : true,
  legalComments: "none",
  alias: { "node:crypto": path.join(here, "crypto-shim.ts"), "node:vm": path.join(here, "vm-shim.ts"), "node:path": path.join(here, "path-shim.ts"), "node:url": path.join(here, "url-shim.ts"), "node:process": path.join(here, "process-shim.ts") },
  plugins: [
    {
      name: "css-tree-lite",
      setup(b) {
        b.onResolve({ filter: /^css-tree$/ }, () => ({ path: path.join(here, "css-tree-lite.ts") }));
        // 内置库脚本体积大，按需由 Go 侧读取（getter 保持惰性）。
        b.onResolve({ filter: /^virtual:reading-libraries$/ }, () => ({ path: "reading-libraries", namespace: "gz-virtual" }));
        b.onLoad({ filter: /.*/, namespace: "gz-virtual" }, () => ({
          loader: "js",
          contents: `export default { compiler: "", animation: "", components: JSON.parse(__gzLibrary("components")), runtime: { get echarts(){return __gzLibrary("echarts")}, get mermaid(){return __gzLibrary("mermaid")}, get animation(){return __gzLibrary("animation")} } };`,
        }));
      },
    },
  ],
  external: shimNames.flatMap((n) => [n, `node:${n}`]).filter((n) => !["node:crypto", "node:vm", "node:path", "node:url", "node:process"].includes(n)),
  banner: { js: "var process={env:{},argv:[],platform:'win32',versions:{},cwd:function(){return '/'},nextTick:function(f){f()},emitWarning:function(){}};" },
  logLevel: "info",
});
console.log("ok", out);