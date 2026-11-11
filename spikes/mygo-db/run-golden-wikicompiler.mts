// 只生成 wiki:compiler 基线（独立运行）：用法 run-golden-wikicompiler.mts <输出目录>
// 完整基线由 make-golden.mts 生成（它也会调用同一个 buildWikiCompilerGolden）。
import fs from "node:fs";
import Module from "node:module";
import path from "node:path";

const out = process.argv[2];
if (!out) throw new Error("用法: run-golden-wikicompiler.mts <输出目录>");
fs.rmSync(out, { recursive: true, force: true });
const isolatedAppData = path.join(out, "isolated-appdata");
fs.mkdirSync(isolatedAppData, { recursive: true });
process.env.APPDATA = isolatedAppData;

const ipcHandlers = new Map<string, (...a: unknown[]) => Promise<unknown>>();
const noop: unknown = new Proxy(function () {}, { get: (_t, p) => (p === "then" ? undefined : noop), apply: () => noop });
const electronStub = new Proxy(
  {
    ipcMain: { handle: (ch: string, fn: (...a: unknown[]) => Promise<unknown>) => ipcHandlers.set(ch, fn), on() {}, removeAllListeners() {}, removeHandler() {} },
    app: { getPath: () => path.join(isolatedAppData, "GuiZhi"), getVersion: () => "0.28.0" },
  } as Record<string | symbol, unknown>,
  { get: (t, p) => (p in t ? t[p] : noop) },
);
const moduleLoader = Module as unknown as { _load: (request: string, ...rest: unknown[]) => unknown };
const originalLoad = moduleLoader._load;
moduleLoader._load = function (this: unknown, request: string, ...rest: unknown[]) {
  if (request === "virtual:reading-libraries") return { __esModule: true, default: { compiler: "", animation: "", components: {}, runtime: {} } };
  return request === "electron" ? electronStub : originalLoad.call(this, request, ...rest);
};

const { buildWikiCompilerGolden } = await import("./golden-wikicompiler.mts");
const golden = await buildWikiCompilerGolden(out, ipcHandlers as never);
fs.writeFileSync(path.join(out, "golden-wikicompiler.json"), JSON.stringify(golden, null, 1));
console.log("ok", path.join(out, "golden-wikicompiler.json"));
