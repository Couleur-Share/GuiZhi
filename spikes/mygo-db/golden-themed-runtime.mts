// 主题阅读运行时差分基线：真实 runtime.ts（任务排队、前置校验、检查点、素材、发布、取消 / 继续 / 不联网继续 / 重生成图片），
// 模型、搜索、生图按「脚本规则」确定性回放；记录每一步的返回值、进度通知序列、任务表与各页面版本。
// Go 侧 themed_runtime_test.go 用同一份种子库、配置与脚本逐项对拍。时间戳抹成 "NOW"，毫秒耗时抹成 0，
// UUID 由两边各自按「第 n 次生成」的计数器产生，因此版本 / 任务 / 临时文件的生成次序也在对拍范围内。
// 用法：run-golden-themed-runtime.mts <输出目录>

/* eslint-disable @typescript-eslint/no-explicit-any */
import fs from "node:fs";
import path from "node:path";
import { EDITOR_PREFIX, DESIGN_PREFIX, design, type ChatRule, type SearchSpec } from "./golden-themed-pipeline.mts";

export type ImageSpec = { png?: string; throw?: string };
export type Step = {
  name: string;
  op: "generate" | "cancel" | "resume" | "continueOffline" | "regenerateAsset" | "removeConfig";
  itemId?: string; kind?: string; options?: unknown; raw?: unknown;
  rules?: ChatRule[]; search?: SearchSpec; images?: ImageSpec[];
  assetPick?: "generated";
  /** generate 后等什么：默认等任务全部结束；blocked = 等某次模型请求被脚本阻塞（用来演示取消 / 重复提交）。 */
  waitFor?: "idle" | "blocked";
};

export const pngBytes = (tag: string, extra = 0): Buffer => Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), Buffer.from(tag), Buffer.alloc(extra, 7)]);

export const runtimeItems = [
  { id: "a", title: "文章A", content: "# 文章A\n\n第一段正文。\n\n第二段正文。", sourceUri: "https://example.com/a" },
  { id: "b", title: "文章B", content: "文章B的正文，足够排版。\n\n再来一段。", sourceUri: null as string | null },
  { id: "c", title: "文章C", content: "文章C 讲盒模型与定位。\n\n另一段。", sourceUri: null },
  { id: "empty", title: "空文章", content: "", sourceUri: null },
  { id: "gone", title: "已删除", content: "x", sourceUri: null },
];

const BODY = "正文包含完整的知识解释与条件，不依赖脚本执行。".repeat(10);
const OUTLINE_OK = { outline: { title: "标题", direction: "简洁", questions: [], sections: [{ title: "说明", brief: "完整解释" }] }, draft: [{ title: "说明", markdown: BODY, referenceIds: [] }] };
const REC_META = { type: "meta", css: "" };
const REC_INTRO = { type: "section", id: "intro", html: '<main><h1>标题</h1><h2>说明</h2><div data-reading-copy="s0b0"></div></main>' };
const REC_DONE = { type: "done" };
export const editorOk: ChatRule = { system: "editor", reply: { content: JSON.stringify(OUTLINE_OK), usage: { promptTokens: 11, completionTokens: 22 } } };
export const designOk: ChatRule = { system: "design", reply: { deltas: design(REC_META, REC_INTRO, REC_DONE), finishReason: "stop" } };
// 设计请求中途断开：任务失败，之后手动继续
const designBad: ChatRule = { system: "design", reply: { deltas: design(REC_META, REC_INTRO), throw: "网络断开" } };
// 断开后的续写：只补收尾，不重复已完成章节
const designResume: ChatRule = { system: "design", reply: { deltas: design(REC_META, REC_DONE), finishReason: "stop" } };
const planImages: ChatRule = { userIncludes: "规划这篇文章的统一视觉方向", reply: { content: JSON.stringify({ direction: "简洁", assets: [{ id: "g0", purpose: "封面", prompt: "一幅抽象的几何插画", alt: "封面插画", aspectRatio: "4:3", blockId: "s0b0" }] }) } };
const reviseOk: ChatRule = { system: "editor", reply: { content: JSON.stringify({ outline: OUTLINE_OK.outline, updates: [{ index: 0, title: "说明", markdown: BODY, referenceIds: [] }] }) } };
const writeSections: ChatRule = { system: "editor", userIncludes: "撰写指定章节", reply: { content: JSON.stringify({ sections: [{ title: "说明", markdown: BODY, referenceIds: [] }] }) } };
const planResearch = { title: "标题", direction: "简洁", questions: ["盒模型尺寸与绝对定位的包含块有什么关系？"], searchQueries: [" CSS box-sizing containing block "], sections: [{ title: "说明", brief: "完整解释" }] };
const optsBase = { style: "", generateImages: false, maxImages: 0 };

export function runtimeSteps(): Step[] {
  const gen = (name: string, itemId: string, over: Partial<Step> = {}): Step => ({ name, op: "generate", itemId, kind: "body", options: optsBase, rules: [editorOk, designOk], ...over });
  return [
    gen("gen-short", "a"),
    gen("gen-bad-options-style", "a", { options: { style: 5, generateImages: false, maxImages: 0 } }),
    gen("gen-bad-options-max", "a", { options: { style: "", generateImages: false, maxImages: 6 } }),
    gen("gen-bad-options-action", "a", { options: { ...optsBase, action: "wild" } }),
    gen("gen-bad-options-depth", "a", { options: { ...optsBase, researchDepth: "x" } }),
    gen("gen-bad-request", "a", { raw: { itemId: "", sourceKind: "body", options: optsBase } }),
    gen("gen-missing-item", "nope"),
    gen("gen-trashed-item", "gone"),
    gen("gen-empty-body", "empty"),
    gen("gen-images-without-budget", "a", { options: { ...optsBase, generateImages: true, maxImages: 0 } }),
    // 取消：设计请求阻塞到收到取消；重复提交被拒；取消后继续
    { name: "gen-b-blocked", op: "generate", itemId: "b", kind: "body", options: optsBase, waitFor: "blocked", rules: [editorOk, { system: "design", reply: { block: true } as any }] },
    gen("gen-b-duplicate", "b", { rules: [editorOk, designOk] }),
    { name: "cancel-b", op: "cancel", itemId: "b", kind: "body" },
    { name: "resume-b", op: "resume", itemId: "b", kind: "body", rules: [editorOk, designOk] },
    // 生图：成功落盘，再对该图重新生成
    gen("gen-images-ok", "a", { options: { ...optsBase, generateImages: true, maxImages: 1 }, rules: [editorOk, planImages, designOk], images: [{ png: "gen1" }] }),
    { name: "regen-asset-ok", op: "regenerateAsset", itemId: "a", kind: "body", assetPick: "generated", images: [{ png: "gen2" }] },
    { name: "regen-asset-bad-id", op: "regenerateAsset", itemId: "a", kind: "body", raw: { itemId: "a", sourceKind: "body", assetId: "nope" }, images: [] },
    { name: "regen-asset-no-page", op: "regenerateAsset", itemId: "c", kind: "body", raw: { itemId: "c", sourceKind: "body", assetId: "x" }, images: [] },
    // 生图失败 → 页面带警告发布 → 继续补图
    gen("gen-images-fail", "c", { options: { ...optsBase, generateImages: true, maxImages: 1 }, rules: [editorOk, planImages, designOk], images: [{ throw: "图片服务异常" }] }),
    { name: "resume-published-images", op: "resume", itemId: "c", kind: "body", rules: [editorOk, designOk], images: [{ png: "gen3" }] },
    // 设计中途断开 → 失败 → 继续
    gen("gen-design-cut", "b", { rules: [editorOk, designBad] }),
    { name: "resume-design-cut", op: "resume", itemId: "b", kind: "body", rules: [editorOk, designOk] },
    // 调整 / 重排：沿用当前页
    gen("revise-a", "a", { options: { ...optsBase, action: "revise", fromCurrent: true }, rules: [reviseOk, designOk] }),
    gen("redesign-a", "a", { options: { ...optsBase, action: "redesign", fromCurrent: true }, rules: [designOk] }),
    // 联网查证失败 → 不联网继续
    gen("gen-research-fail", "c", { options: { ...optsBase, research: true }, rules: [{ system: "editor", reply: { content: JSON.stringify(planResearch) } }], search: { throwOn: { "CSS box-sizing containing block": "搜索服务不可用" } } }),
    { name: "continue-offline-c", op: "continueOffline", itemId: "c", kind: "body", rules: [writeSections, editorOk, designOk] },
    { name: "continue-offline-not-allowed", op: "continueOffline", itemId: "a", kind: "body", rules: [] },
    { name: "resume-unknown", op: "resume", itemId: "nope", kind: "body", rules: [] },
    // 没有可用的主文本模型
    { name: "remove-config", op: "removeConfig" },
    gen("gen-no-model", "a", { rules: [] }),
  ];
}

const mask = (v: any, key = ""): any => {
  if (Array.isArray(v)) return v.map(x => mask(x, key));
  if (v && typeof v === "object") return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, k === "timings" && x && typeof x === "object" ? Object.fromEntries(Object.keys(x as object).map(s => [s, 0])) : mask(x, k)]));
  if (typeof v === "number" && v > 1e11 && /At$|updated/.test(key)) return "NOW";
  return v;
};
const clean = (v: unknown) => mask(JSON.parse(JSON.stringify(v ?? null)));

export async function buildRuntimeGolden(outDir: string, hooks: { current: any }) {
  const { configureRuntimePaths, getImagesDir } = await import("../../apps/desktop/src/main/runtime-paths.ts");
  configureRuntimePaths({ userDataPath: path.join(outDir, "userdata") });
  fs.mkdirSync(getImagesDir(), { recursive: true });
  const { initDatabase, closeDatabase } = await import("../../packages/db/src/index.ts");
  // 与被测代码用同一份 @guizhi/core 实例（经 CJS require，走 Module._load），否则两份实例的运行时路径各配各的
  const { createRequire } = await import("node:module");
  const { coreAIConfigService, configureRuntimePaths: configureCorePaths } = createRequire(import.meta.url)("@guizhi/core");
  configureCorePaths({ userDataPath: path.join(outDir, "userdata") });
  const p = (id: string, url: string) => ({ id, provider: "OpenAI", name: "OpenAI", apiProtocol: "openai" as const, apiKey: "sk-test", apiUrl: url });
  const m = (id: string, providerId: string, name: string, url: string, extra: Record<string, unknown> = {}) => ({ id, providerId, provider: "OpenAI", apiProtocol: "openai" as const, apiKey: "sk-test", apiUrl: url, model: name, ...extra });
  coreAIConfigService.replace({
    providers: [p("p-main", "https://llm.test/v1")],
    models: [m("m-text", "p-main", "test-model", "https://llm.test/v1"), m("m-img", "p-main", "img-model", "https://llm.test/v1", { capabilities: { chat: false } })],
    modelRouteDefaults: { mainText: "m-text", imageGen: "m-img" },
  });
  const aiConfig = fs.readFileSync(path.join(outDir, "userdata", "config", "ai-models.json"), "utf8");
  let db: any = initDatabase(path.join(outDir, "seed.db"));
  const ins = db.prepare("INSERT INTO knowledge_items (id,title,content,transcript,item_type,deleted_at,created_at,updated_at) VALUES (?,?,?,?,?,?,1,1)");
  for (const it of runtimeItems) {
    ins.run(it.id, it.title, it.content, null, "note", null);
    if (it.sourceUri) db.prepare("INSERT INTO source_records (id,item_id,source_type,source_uri,normalized_uri,content_hash,captured_at,platform) VALUES (?,?,?,?,?,?,?,?)").run(`sr-${it.id}`, it.id, "url", it.sourceUri, it.sourceUri, "h", 1, null);
  }
  db.prepare("UPDATE knowledge_items SET deleted_at=? WHERE id='gone'").run(1700000000000);
  closeDatabase();
  fs.copyFileSync(path.join(outDir, "seed.db"), path.join(outDir, "rt.db"));
  db = initDatabase(path.join(outDir, "rt.db"));

  const { ThemedReadingRuntime } = await import("../../apps/desktop/src/main/services/themed-reading/runtime.ts");
  const { parseThemedRequest, parseThemedOptions } = await import("../../apps/desktop/src/main/ipc/themed-reading.ipc.ts");
  let events: any[] = [];
  const runtime = new ThemedReadingRuntime(db, task => { events.push(clean(task)); });
  const wait = async (until: () => boolean, label: string) => {
    const t0 = Date.now();
    while (!until()) {
      if (Date.now() - t0 > 20000) throw new Error(`等待超时：${label}`);
      await new Promise(r => setTimeout(r, 5));
    }
  };
  const idle = () => runtime.pages.listTasks().every((t: any) => t.state !== "queued" && t.state !== "running") && !(runtime as any).pumping;
  const latestTask = (itemId: string, kind: string) => runtime.pages.listTasks().filter((t: any) => t.itemId === itemId && t.sourceKind === kind).sort((a: any, b: any) => b.createdAt - a.createdAt)[0];
  const describe = (e: unknown) => (e instanceof Error ? e.message : String(e));
  const pageDump = () => {
    const out: any = {};
    for (const it of runtimeItems) for (const kind of ["body", "summary"]) for (const role of ["current", "previous"] as const) {
      const page = runtime.pages.get(it.id, kind, role);
      if (page) out[`${it.id}/${kind}/${role}`] = clean(page);
    }
    const rows: any[] = db.prepare("SELECT id,item_id,source_kind,role FROM themed_reading_versions ORDER BY item_id,source_kind,role,created_at,id").all();
    out.versionRows = rows.map(r => ({ item: r.item_id, kind: r.source_kind, role: r.role }));
    return out;
  };

  let blockedNow = false;
  const recs: any[] = [];
  for (const step of runtimeSteps()) {
    events = [];
    const calls: any[] = [], images: any[] = [], usage: any[] = [], searches: string[] = [];
    const used = new Map<ChatRule, number>();
    const imageQueue = [...(step.images ?? [])];
    hooks.current = {
      chat: async (config: any, messages: any[], options: any = {}) => {
        const user = messages.find(x => x.role === "user")?.content ?? "";
        const sys = messages.find(x => x.role === "system")?.content ?? "";
        calls.push({ config, sys: sys.slice(0, 20), user: user.slice(0, 60), options: { temperature: options.temperature, maxTokens: options.maxTokens, timeoutMs: options.timeoutMs, stream: options.stream } });
        const rule = (step.rules ?? []).find(r => (!r.system || sys.startsWith(r.system === "editor" ? EDITOR_PREFIX : DESIGN_PREFIX)) && (!r.userIncludes || user.includes(r.userIncludes)) && (r.times === undefined || (used.get(r) ?? 0) < r.times));
        if (!rule) throw new Error("脚本规则已用尽");
        used.set(rule, (used.get(rule) ?? 0) + 1);
        const reply: any = rule.reply;
        if (reply.block) {
          blockedNow = true;
          await new Promise((_, reject) => { const s: AbortSignal | undefined = options.signal; if (s?.aborted) reject(s.reason); s?.addEventListener("abort", () => reject(s.reason)); });
        }
        for (const d of reply.deltas ?? []) { options.onDelta?.(d); options.onProgress?.(d.length); }
        if (reply.throw) throw new Error(reply.throw);
        return { content: reply.content ?? "", finishReason: reply.finishReason, usage: reply.usage };
      },
      usage: (u: any) => usage.push(u),
      search: async (query: string) => {
        searches.push(query);
        const spec = step.search ?? {};
        const bad = Object.entries(spec.throwOn ?? {}).find(([k]) => query.includes(k));
        if (bad) throw new Error(bad[1]);
        return spec.byQuery?.[query] ?? spec.default ?? [];
      },
      capture: async () => { throw new Error("运行时基线不含网页抓取"); },
      image: async (prompt: string, aspect: string, config: any, options: any = {}) => {
        images.push({ prompt, aspect, model: config.model, scenario: options.scenario, timeoutMs: options.timeoutMs, retryDelays: options.retryDelaysMs });
        options.onRequest?.();
        const spec = imageQueue.shift();
        if (!spec) throw new Error("脚本里没有更多图片");
        if (spec.throw) throw new Error(spec.throw);
        return { data: pngBytes(spec.png ?? "x", 24), extension: ".png" };
      },
    };
    let error: string | null = null;
    let result: any = null;
    try {
      const raw: any = step.raw;
      if (step.op === "generate") {
        const r: any = raw ?? { itemId: step.itemId, sourceKind: step.kind, options: step.options };
        result = await runtime.generate({ ...parseThemedRequest(r), options: parseThemedOptions(r.options) });
        if (step.waitFor === "blocked") await wait(() => blockedNow, step.name); else await wait(idle, step.name);
      } else if (step.op === "cancel") {
        const task = latestTask(step.itemId!, step.kind!);
        result = runtime.cancel(task.id);
        blockedNow = false;
        await wait(idle, step.name);
      } else if (step.op === "resume" || step.op === "continueOffline") {
        const task = latestTask(step.itemId!, step.kind!);
        const id = task?.id ?? "no-such-task";
        result = step.op === "resume" ? await runtime.resume(id) : await runtime.continueOffline(id);
        await wait(idle, step.name);
      } else if (step.op === "regenerateAsset") {
        const r: any = raw ?? {};
        const input = parseThemedRequest(r.itemId ? r : { itemId: step.itemId, sourceKind: step.kind });
        const page = runtime.pages.get(input.itemId, input.sourceKind);
        const assetId = step.assetPick === "generated" ? page?.assets.find((a: any) => a.role === "generated")?.id : r.assetId;
        if (!page || typeof assetId !== "string") throw new Error("主题页或图片不存在");
        result = await runtime.generate({ ...input, options: { ...page.options, fromCurrent: true, generateImages: false } }, assetId);
        await wait(idle, step.name);
      } else if (step.op === "removeConfig") {
        fs.rmSync(path.join(outDir, "userdata", "config", "ai-models.json"));
      }
    } catch (e) { error = describe(e); }
    recs.push({
      name: step.name, error, result: clean(result), events, calls, images, usage, searches,
      tasks: clean(runtime.pages.listTasks().sort((a: any, b: any) => a.createdAt - b.createdAt || String(a.id).localeCompare(String(b.id)))),
      pages: pageDump(),
      files: fs.readdirSync(getImagesDir()).sort(),
    });
  }
  await runtime.shutdown();
  closeDatabase();
  const imageFiles: Record<string, string> = {};
  for (const f of fs.readdirSync(getImagesDir())) imageFiles[f] = fs.readFileSync(path.join(getImagesDir(), f)).toString("base64");
  return { recs, items: runtimeItems, steps: runtimeSteps(), aiConfig, imageFiles };
}
