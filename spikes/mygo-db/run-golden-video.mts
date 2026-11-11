// 只生成视频连接器基线：用法 run-golden-video.mts <输出目录>（从 D:\Project\GuiZhi-mygo-spike 目录运行 tsx）
// 与 run-golden-media.mts 同样的 electron 桩与隔离 APPDATA：video-url.ts 的依赖链会拉进数据库与主进程模块。
import fs from "node:fs";
import Module from "node:module";
import path from "node:path";

const out = process.argv[2];
if (!out) throw new Error("用法: run-golden-video.mts <输出目录>");
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
  if (request === "virtual:reading-libraries") return { __esModule: true, default: { compiler: "", animation: "", components: {}, runtime: {} } };
  return request === "electron" ? electronStub : originalLoad.call(this, request, ...rest);
};

const { buildVideoGolden } = await import("./golden-video.mts");
const g = await buildVideoGolden();
fs.writeFileSync(path.join(out, "golden-video.json"), JSON.stringify(g, null, 1));
console.log("ok", path.join(out, "golden-video.json"), "recs", g.recs.length);
