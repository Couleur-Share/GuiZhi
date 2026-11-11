// 生成主题阅读同步频道基线：用法（仓库根）pnpm dlx tsx spikes/mygo-db/run-golden-themed-ipc.mts <输出目录>
// electron 换桩：ipcMain / BrowserWindow / dialog 由 golden-themed-ipc.mts 注入，其余属性是空代理。
import fs from "node:fs";
import Module from "node:module";
import path from "node:path";

const out = process.argv[2];
if (!out) throw new Error("用法: run-golden-themed-ipc.mts <输出目录>");
fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(out, { recursive: true });
const isolatedAppData = path.join(out, "isolated-appdata");
fs.mkdirSync(isolatedAppData, { recursive: true });
process.env.APPDATA = isolatedAppData;

const noop: unknown = new Proxy(function () {}, { get: (_t, p) => (p === "then" ? undefined : noop), apply: () => noop });
const electronStub = new Proxy(
  { app: { getPath: () => path.join(out, "userdata"), getVersion: () => "0.28.0" } } as Record<string | symbol, unknown>,
  { get: (t, p) => (p in t ? t[p] : (globalThis as any).__themedIpcElectron?.[p] ?? noop) },
);
const moduleLoader = Module as unknown as { _load: (request: string, ...rest: unknown[]) => unknown };
const originalLoad = moduleLoader._load;
const here = path.dirname(new URL(import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1"));
moduleLoader._load = function (this: unknown, request: string, ...rest: unknown[]) {
  if (request === "virtual:reading-libraries") {
    const libs = JSON.parse(fs.readFileSync(path.join(here, "..", "mygo-shell", "reading_libraries.json"), "utf8"));
    return { __esModule: true, default: { compiler: "", animation: "", components: libs.components, runtime: libs.runtime } };
  }
  return request === "electron" ? electronStub : originalLoad.call(this, request, ...rest);
};

const { buildThemedIpcGolden } = await import("./golden-themed-ipc.mts");
const g = await buildThemedIpcGolden(out);
fs.writeFileSync(path.join(out, "golden-themed-ipc.json"), JSON.stringify(g, null, 1));
console.log("ok", path.join(out, "golden-themed-ipc.json"), "recs", g.recs.length);
