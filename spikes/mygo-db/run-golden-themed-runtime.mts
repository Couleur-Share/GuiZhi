// 生成主题阅读运行时基线：用法（仓库根）pnpm dlx tsx spikes/mygo-db/run-golden-themed-runtime.mts <输出目录>
import crypto from "node:crypto";
import fs from "node:fs";
import Module from "node:module";
import path from "node:path";

const out = process.argv[2];
if (!out) throw new Error("用法: run-golden-themed-runtime.mts <输出目录>");
fs.rmSync(out, { recursive: true, force: true });
fs.mkdirSync(out, { recursive: true });
const isolatedAppData = path.join(out, "isolated-appdata");
fs.mkdirSync(isolatedAppData, { recursive: true });
process.env.APPDATA = isolatedAppData;
const here = path.dirname(new URL(import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1"));
const noop: unknown = new Proxy(function () {}, { get: (_t, p) => (p === "then" ? undefined : noop), apply: () => noop });
const electronStub = new Proxy({ app: { getPath: () => path.join(out, "userdata"), getVersion: () => "0.28.0" } } as Record<string | symbol, unknown>, { get: (t, p) => (p in t ? t[p] : noop) });
const hooks: { current: any } = { current: null };
// UUID 计数器：两边按「第 n 次生成」对拍，生成次序也在差分范围内
const uuid = { n: 0 };
(crypto as any).randomUUID = () => `00000000-0000-4000-8000-${String(++uuid.n).padStart(12, "0")}`;
const moduleLoader = Module as unknown as { _load: (request: string, ...rest: unknown[]) => any };
const originalLoad = moduleLoader._load;
moduleLoader._load = function (this: unknown, request: string, ...rest: unknown[]) {
  if (request === "virtual:reading-libraries") {
    const libs = JSON.parse(fs.readFileSync(path.join(here, "..", "..", "apps", "desktop-go", "reading_libraries.json"), "utf8"));
    return { __esModule: true, default: { compiler: "", animation: "", components: libs.components, runtime: libs.runtime } };
  }
  if (request === "@guizhi/core") {
    const real = originalLoad.call(this, request, ...rest);
    return new Proxy(real, { get: (t, p) => (p === "chatCompletion" ? (...a: any[]) => hooks.current.chat(...a) : t[p]) });
  }
  if (/(^|\/)ai-usage$/.test(request)) return { recordMainAiUsage: (u: any) => hooks.current.usage(u) };
  if (/(^|\/)search-service$/.test(request)) return { searchReadingWeb: (q: string) => hooks.current.search(q) };
  if (/web-capture\/web-capture$/.test(request)) return { captureWebPage: (r: any) => hooks.current.capture(r) };
  if (/illustration\/image-gen$/.test(request)) {
    const real = originalLoad.call(this, request, ...rest);
    return new Proxy(real, { get: (t, p) => (p === "generateImage" ? (...a: any[]) => hooks.current.image(...a) : t[p]) });
  }
  if (request === "electron") return electronStub;
  return originalLoad.call(this, request, ...rest);
};

const { buildRuntimeGolden } = await import("./golden-themed-runtime.mts");
const g = await buildRuntimeGolden(out, hooks);
fs.writeFileSync(path.join(out, "golden-themed-runtime.json"), JSON.stringify(g, null, 1));
console.log("ok", g.recs.length, "steps");
for (const r of g.recs) console.log(r.name.padEnd(32), r.error ?? "OK", `events=${r.events.length} chat=${r.calls.length} img=${r.images.length}`);
