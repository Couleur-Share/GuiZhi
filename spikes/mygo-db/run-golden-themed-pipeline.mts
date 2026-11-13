// 生成主题阅读生成管线基线：用法（仓库根）pnpm dlx tsx spikes/mygo-db/run-golden-themed-pipeline.mts <输出目录>
import fs from "node:fs";
import Module from "node:module";
import path from "node:path";

const out = process.argv[2];
if (!out) throw new Error("用法: run-golden-themed-pipeline.mts <输出目录>");
fs.mkdirSync(out, { recursive: true });
const here = path.dirname(new URL(import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1"));
const noop: unknown = new Proxy(function () {}, { get: (_t, p) => (p === "then" ? undefined : noop), apply: () => noop });
let hooks: any = null;
const moduleLoader = Module as unknown as { _load: (request: string, ...rest: unknown[]) => unknown };
const originalLoad = moduleLoader._load;
moduleLoader._load = function (this: unknown, request: string, ...rest: unknown[]) {
  if (request === "virtual:reading-libraries") {
    const libs = JSON.parse(fs.readFileSync(path.join(here, "..", "..", "apps", "desktop-go", "reading_libraries.json"), "utf8"));
    return { __esModule: true, default: { compiler: "", animation: "", components: libs.components, runtime: libs.runtime } };
  }
  if (request === "@guizhi/core") return { chatCompletion: (...a: any[]) => hooks.chat(...a) };
  if (/(^|\/)ai-usage$/.test(request)) return { recordMainAiUsage: (u: any) => hooks.usage(u) };
  if (/(^|\/)search-service$/.test(request)) return { searchReadingWeb: (q: string) => hooks.search(q) };
  if (/web-capture\/web-capture$/.test(request)) return { captureWebPage: (r: any) => hooks.capture(r) };
  if (request === "electron") return noop;
  return originalLoad.call(this, request, ...rest);
};

const { runReadingV3 } = await import("../../apps/desktop/src/main/services/themed-reading/v3-pipeline.ts");
const { repairReadingRuntime } = await import("../../apps/desktop/src/main/services/themed-reading/v3-runtime-repair.ts");
const { renderThemedReadingBlocks } = await import("../../apps/desktop/src/main/services/themed-reading/content.ts");
const { scenarios, runScenario } = await import("./golden-themed-pipeline.mts");
const list = scenarios({ renderThemedReadingBlocks });
const recs: any[] = [];
for (const s of list) {
  const runs = await runScenario(s, { setScenario: h => { hooks = h; } },
    (version, config, signal, h) => runReadingV3(version, config, signal, h),
    async (version, config, signal, verify, h) => {
      let i = 0;
      await repairReadingRuntime(version, config, signal, async () => { const r = (verify as any[])[i++]; return r ?? undefined; }, h.checkpoint, () => h.request("textCalls"));
    });
  recs.push({ kind: "thmPipeline", name: s.name, input: { version: s.version, config: s.config, runs: s.runs }, output: runs });
}
fs.writeFileSync(path.join(out, "golden-themed-pipeline.json"), JSON.stringify({ recs }, null, 1));
console.log("ok", recs.length, "scenarios");
for (const r of recs) console.log(r.name, r.output.map((o: any) => `${o.error ?? "OK"} calls=${o.calls.length} ev=${o.events.length}`).join(" | "));
