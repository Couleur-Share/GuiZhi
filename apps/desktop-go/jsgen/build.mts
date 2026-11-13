// 生成 apps/desktop-go/websnap_sanitize.js：用 esbuild 把快照净化相关 TS 与其依赖（sanitize-html、css-tree 等）打成单文件 IIFE，
// 由 Go 侧 goja 加载。用法（仓库根）：pnpm dlx tsx apps/desktop-go/jsgen/build.mts
import { build } from "esbuild";
import path from "node:path";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const out = path.join(here, "..", "websnap_sanitize.js");
const shimNames = ["fs", "path", "url", "util", "os", "tty", "stream", "events", "buffer", "assert", "crypto", "process"];
await build({
  entryPoints: [path.join(here, "sanitizer-entry.ts")],
  outfile: out,
  bundle: true,
  format: "iife",
  platform: "node",
  target: "es2018",
  minify: process.env.GZ_NOMIN ? false : true,
  legalComments: "none",
  alias: { "node:crypto": path.join(here, "crypto-shim.ts") },
  plugins: [
    {
      // 只重定向裸的 "css-tree"（子路径 css-tree/parser 等照常解析）
      name: "css-tree-lite",
      setup(b) {
        b.onResolve({ filter: /^css-tree$/ }, () => ({ path: path.join(here, "css-tree-lite.ts") }));
      },
    },
  ],
  external: shimNames.flatMap((n) => [n, `node:${n}`]).filter((n) => n !== "node:crypto"),
  banner: { js: "var process={env:{},argv:[],platform:'win32',versions:{},cwd:function(){return '/'},nextTick:function(f){f()},emitWarning:function(){}};" },
  logLevel: "info",
});
console.log("ok", out);
