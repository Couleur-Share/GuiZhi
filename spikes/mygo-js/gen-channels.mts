// 生成 spikes/mygo-shell/ipc_channels_gen.go：渲染进程（preload）实际可以调用的频道清单。
// 用法（仓库根）：pnpm dlx tsx spikes/mygo-js/gen-channels.mts
// 判定：preload 里 ipcRenderer.invoke / send / sendSync 的目标；`ipcRenderer.on/once/off` 只是接收推送，不需要处理器。
// 经由包装函数转发的频道（如 web-capture.ts 的 invoke(channel, ...)）取该文件里出现的全部 IPC_CHANNELS.X。
import fs from "node:fs";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "../..");
const ts = fs.readFileSync(path.join(root, "packages/shared/constants/ipc-channels.ts"), "utf8");
const consts = new Map<string, string>();
for (const m of ts.matchAll(/^\s*([A-Z0-9_]+):\s*["']([^"']+)["']/gm)) consts.set(m[1], m[2]);

const preloadDir = path.join(root, "apps/desktop/src/preload");
const files: string[] = [];
(function walk(d: string) {
  for (const e of fs.readdirSync(d, { withFileTypes: true })) {
    const p = path.join(d, e.name);
    if (e.isDirectory()) walk(p);
    else if (p.endsWith(".ts") && !p.includes(".test.")) files.push(p);
  }
})(preloadDir);

const invoke = new Set<string>();
const send = new Set<string>();
const listen = new Set<string>();
for (const f of files) {
  const src = fs.readFileSync(f, "utf8");
  const wrapper = /ipcRenderer\.invoke\(\s*channel\b/.test(src);
  const re = /ipcRenderer\.(invoke|sendSync|send|on|once|off|removeListener)\(\s*(?:IPC_CHANNELS\.([A-Z0-9_]+)|["']([^"']+)["'])/g;
  for (const m of src.matchAll(re)) {
    const name = m[2] ? consts.get(m[2]) : m[3];
    if (!name) throw new Error(`${path.basename(f)}: 未知频道常量 ${m[2]}`);
    if (m[1] === "invoke") invoke.add(name);
    else if (m[1] === "send" || m[1] === "sendSync") send.add(name);
    else listen.add(name);
  }
  if (wrapper) for (const m of src.matchAll(/IPC_CHANNELS\.([A-Z0-9_]+)/g)) {
    const name = consts.get(m[1]);
    if (name) invoke.add(name);
  }
}
const sorted = (s: Set<string>) => [...s].sort();
const out = [
  "// 由 spikes/mygo-js/gen-channels.mts 生成，请勿手改。",
  "// 渲染进程（preload）能调用的频道清单，用于启动时自检「有多少频道还没有处理器」。",
  "package main",
  "",
  "var rendererInvokeChannels = []string{",
  ...sorted(invoke).map((c) => `\t${JSON.stringify(c)},`),
  "}",
  "",
  "var rendererSendChannels = []string{",
  ...sorted(send).map((c) => `\t${JSON.stringify(c)},`),
  "}",
  "",
].join("\n");
fs.writeFileSync(path.join(root, "spikes/mygo-shell/ipc_channels_gen.go"), out);
console.log(`ok invoke=${invoke.size} send=${send.size} listen=${listen.size}`);