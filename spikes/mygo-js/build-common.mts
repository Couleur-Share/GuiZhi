// goja 打包公共配置：node 内置模块换成 shim、css-tree 换成精简版、内置阅读库按需由 Go 读取。
import { build, type Plugin } from "esbuild";
import path from "node:path";
import { fileURLToPath } from "node:url";

export const here = path.dirname(fileURLToPath(import.meta.url));
const shimNames = ["fs", "path", "url", "util", "os", "tty", "stream", "events", "buffer", "assert", "crypto", "process", "vm", "module", "child_process", "worker_threads", "http", "https", "net", "zlib"];
const aliased = ["node:crypto", "node:vm", "node:path", "node:url", "node:process"];

export async function buildGoja(entry: string, outName: string, extra: Plugin[] = []) {
  const out = path.join(here, "..", "mygo-shell", outName);
  await build({
    entryPoints: [path.join(here, entry)],
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
      ...extra,
    ],
    external: shimNames.flatMap((n) => [n, `node:${n}`]).filter((n) => !aliased.includes(n)),
    banner: { js: "var process={env:{},argv:[],platform:'win32',versions:{},cwd:function(){return '/'},nextTick:function(f){f()},emitWarning:function(){}};" },
    logLevel: "info",
  });
  console.log("ok", out);
}
