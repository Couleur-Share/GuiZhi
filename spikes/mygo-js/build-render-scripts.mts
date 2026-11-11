// 生成 spikes/mygo-shell/render_scripts_gen.go：渲染窗口里执行的页面侧脚本（沉降探针、DOM 快照）。
// 与 Electron 版同源：直接取 web-settle.ts / web-rendered-document.ts 里自包含函数的 toString()，
// 用 esbuild 关闭 keepNames 编译（tsx 默认带 keepNames 会注入 __name，序列化后在页面里不存在）。
// 用法（仓库根）：pnpm dlx tsx spikes/mygo-js/build-render-scripts.mts
import { build } from "esbuild";
import fs from "node:fs";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const root = path.join(here, "..", "..");
const cap = path.join(root, "apps", "desktop", "src", "main", "services", "web-capture");
const entry = `export { installSettleProbe, readSettleProbe } from ${JSON.stringify(path.join(cap, "web-settle.ts"))};
export { captureRenderedDocument } from ${JSON.stringify(path.join(cap, "web-rendered-document.ts"))};`;
const res = await build({
  stdin: { contents: entry, resolveDir: cap, loader: "ts" },
  bundle: true, write: false, format: "iife", globalName: "G", target: "es2020", keepNames: false, platform: "neutral",
});
const ctx: Record<string, unknown> = { performance: { now: () => 0 }, process: { env: {} } };
vm.createContext(ctx);
const G = vm.runInContext(res.outputFiles[0].text + ";G", ctx) as Record<string, () => unknown>;
const lit = (name: string) => "`" + `(${G[name].toString()})()`.replace(/\\/g, "\\\\").replace(/`/g, "` + \"`\" + `").replace(/\$\{/g, "${\"$\"}{") + "`";
const go = `// 代码生成，勿手改：pnpm dlx tsx spikes/mygo-js/build-render-scripts.mts
package main

// renderScriptInstallProbe 对应 installSettleProbe：安装 DOM 变化观察，返回 true/false。
const renderScriptInstallProbe = ${lit("installSettleProbe")}

// renderScriptReadProbe 对应 readSettleProbe：返回 {domQuietMs,textLength,costMs} 或 null。
const renderScriptReadProbe = ${lit("readSettleProbe")}

// renderScriptSnapshot 对应 captureRenderedDocument：返回 {html,restoredEditors,flattenedShadowRoots?,error?}。
const renderScriptSnapshot = ${lit("captureRenderedDocument")}
`;
fs.writeFileSync(path.join(here, "..", "mygo-shell", "render_scripts_gen.go"), go);
console.log("ok");