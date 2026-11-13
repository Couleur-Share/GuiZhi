// 只生成主题阅读渲染层基线：用法 run-golden-themed.mts <输出目录>（从 D:\Project\GuiZhi-mygo-spike 目录运行 tsx）
// 与 run-golden-media.mts 同样的 electron 桩与隔离 APPDATA：video-url.ts 的依赖链会拉进数据库与主进程模块。
import fs from "node:fs";
import Module from "node:module";
import path from "node:path";

const out = process.argv[2];
if (!out) throw new Error("用法: run-golden-themed.mts <输出目录>");
fs.mkdirSync(out, { recursive: true });
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
  if (request === "virtual:reading-libraries") { const libs = JSON.parse(fs.readFileSync(path.join(path.dirname(new URL(import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1")), "..", "..", "apps", "desktop-go", "reading_libraries.json"), "utf8")); return { __esModule: true, default: { compiler: "", animation: "", components: libs.components, runtime: libs.runtime } }; }
  return request === "electron" ? electronStub : originalLoad.call(this, request, ...rest);
};

const { buildThemedGolden } = await import("./golden-themed.mts");
const g = await buildThemedGolden();
fs.writeFileSync(path.join(out, "golden-themed.json"), JSON.stringify(g, null, 1));
console.log("ok", path.join(out, "golden-themed.json"), "recs", g.recs.length);
