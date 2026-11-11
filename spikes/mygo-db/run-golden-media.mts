// 只生成 media 基线（独立运行，调试用）：用法 run-golden-media.mts <输出目录>
// 完整基线仍由 make-golden.mts 生成（它也会调用同一个 buildMediaGolden）。
import fs from "node:fs";
import Module from "node:module";
import path from "node:path";

const out = process.argv[2];
if (!out) throw new Error("用法: run-golden-media.mts <输出目录>");
fs.rmSync(out, { recursive: true, force: true });
const isolatedAppData = path.join(out, "isolated-appdata");
fs.mkdirSync(isolatedAppData, { recursive: true });
process.env.APPDATA = isolatedAppData;

const noop: unknown = new Proxy(function () {}, { get: (_t, p) => (p === "then" ? undefined : noop), apply: () => noop });
const electronStub = new Proxy(
  { app: { getPath: () => path.join(isolatedAppData, "GuiZhi"), getVersion: () => "0.28.0" } } as Record<string | symbol, unknown>,
  { get: (t, p) => (p in t ? t[p] : noop) },
);
const moduleLoader = Module as unknown as { _load: (request: string, ...rest: unknown[]) => unknown };
const originalLoad = moduleLoader._load;
moduleLoader._load = function (this: unknown, request: string, ...rest: unknown[]) {
  if (request === "virtual:reading-libraries") return { __esModule: true, default: { compiler: "", animation: "", components: {}, runtime: {} } };
  return request === "electron" ? electronStub : originalLoad.call(this, request, ...rest);
};

const { buildMediaGolden } = await import("./golden-media.mts");
const media = await buildMediaGolden(out);
fs.writeFileSync(path.join(out, "golden-media.json"), JSON.stringify({ media }, null, 1));
console.log("ok", path.join(out, "golden-media.json"));
