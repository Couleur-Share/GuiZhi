// 主题阅读生成管线差分基线：真实 v3-pipeline.ts / design.ts / v3-research.ts / v3-runtime-repair.ts，
// 模型、搜索、网页抓取、用量记录按「脚本规则」确定性回放；记录每次模型请求、每个钩子（含版本快照）、用量与最终版本。
// Go 侧 themed_pipeline_test.go 把同一份规则喂给 goja 里的打包产物，逐项对拍。
// 用法：run-golden-themed-pipeline.mts <输出目录>

/* eslint-disable @typescript-eslint/no-explicit-any */
export type ChatReply = { content?: string; deltas?: string[]; finishReason?: string; usage?: { promptTokens: number; completionTokens: number }; throw?: string; abort?: boolean; echoCandidates?: boolean };
export type ChatRule = { system?: "editor" | "design"; userIncludes?: string; reply: ChatReply; times?: number };
export type SearchSpec = { default?: { title: string; url: string }[]; byQuery?: Record<string, { title: string; url: string }[]>; throwOn?: Record<string, string> };
export type CaptureSpec = { default?: any; byUrl?: Record<string, any>; throwOn?: Record<string, string> };
export type Run = { rules: ChatRule[]; search?: SearchSpec; capture?: CaptureSpec; op?: "design" | "repair"; verify?: (string | null)[]; abortAfterHooks?: number };
export type Scenario = { name: string; version: any; config: any; runs: Run[] };

const mask = (v: any): any => {
  if (Array.isArray(v)) return v.map(mask);
  if (v && typeof v === "object") return Object.fromEntries(Object.entries(v).map(([k, x]) => [k, typeof x === "number" && x > 1e11 && /At$|updated/.test(k) ? "NOW" : mask(x)]));
  return v;
};
export const EDITOR_PREFIX = "你是专题文章编辑";
export const DESIGN_PREFIX = "你是阅读页设计师";
const SOURCE = "可核对的来源正文与明确适用条件。".repeat(25);
const BODY = "正文包含完整的知识解释与条件，不依赖脚本执行。".repeat(10);
export const design = (...records: unknown[]) => records.map(r => JSON.stringify(r) + "\n");
const REC_META = { type: "meta", css: "" };
const REC_INTRO = { type: "section", id: "intro", html: '<main><h1>标题</h1><h2>说明</h2><div data-reading-copy="s0b0"></div></main>' };
const REC_DONE = { type: "done" };
const OUTLINE_OK = { outline: { title: "标题", direction: "简洁", questions: [], sections: [{ title: "说明", brief: "完整解释" }] }, draft: [{ title: "说明", markdown: BODY, referenceIds: [] }] };

export function baseVersion(R: { renderThemedReadingBlocks: (md: string) => any[] }, over: Record<string, unknown> = {}, content = "# 标题\n\n" + BODY) {
  const blocks = R.renderThemedReadingBlocks(content);
  return {
    id: "pv-12345678", itemId: "it", sourceKind: "body", role: "working", formatVersion: 3,
    source: { title: "测试文章", content, sourceUri: null, fingerprint: "a".repeat(64), blocks },
    options: { style: "", research: false, action: "create", enhancedInteraction: true, generateImages: false, maxImages: 0 },
    reconstruction: { notes: [], queries: [], references: [], draft: [], interactions: [], visuals: [], animations: [] },
    design: null, assets: [], warnings: [], createdAt: 1, updatedAt: 1, ...over,
  };
}

export function scenarios(R: { renderThemedReadingBlocks: (md: string) => any[] }): Scenario[] {
  const cfg = { provider: "openai", apiProtocol: "openai", apiKey: "sk-test", apiUrl: "https://llm.test/v1", model: "test-model" };
  const editorOk: ChatRule = { system: "editor", reply: { content: JSON.stringify(OUTLINE_OK), usage: { promptTokens: 11, completionTokens: 22 } } };
  const designOk: ChatRule = { system: "design", reply: { deltas: design(REC_META, REC_INTRO, REC_DONE), finishReason: "stop" } };
  const research = (deep = false) => baseVersion(R, { options: { style: "", research: true, researchDepth: deep ? "deep" : "standard", action: "create", enhancedInteraction: true, generateImages: false, maxImages: 0 } });
  const list = [
    { name: "short-ok", version: baseVersion(R), config: cfg, runs: [{ rules: [editorOk, designOk] }] },
    { name: "short-extra-questions", version: baseVersion(R), config: cfg, runs: [{ rules: [
      { system: "editor", reply: { content: JSON.stringify({ outline: { ...OUTLINE_OK.outline, questions: ["一", "二", "三", "四", "五"] }, draft: OUTLINE_OK.draft }) } }, designOk] }] },
    { name: "truncated-then-resume", version: baseVersion(R), config: cfg, runs: [
      { rules: [editorOk, { system: "design", times: 1, reply: { deltas: design(REC_META, REC_INTRO), throw: "网络断开" } }] },
      { rules: [{ system: "design", reply: { deltas: design(REC_META, REC_DONE), finishReason: "stop" } }] },
    ] },
    { name: "abort-in-stream", version: baseVersion(R), config: cfg, runs: [{ rules: [editorOk, { system: "design", reply: { abort: true, deltas: design(REC_META), finishReason: "stop" } }] }] },
    { name: "editor-bad-json", version: baseVersion(R), config: cfg, runs: [{ rules: [{ system: "editor", reply: { content: "不是 JSON" } }] }] },
    { name: "editor-rate-limited", version: baseVersion(R), config: cfg, runs: [{ rules: [{ system: "editor", reply: { throw: "HTTP 429 请求过于频繁" } }] }] },
    { name: "design-invalid-section", version: baseVersion(R), config: cfg, runs: [{ rules: [editorOk,
      { system: "design", reply: { deltas: design(REC_META, { type: "section", id: "intro", html: "<main><h1>标题</h1><script>alert(1)</script></main>" }, REC_DONE), finishReason: "stop" } }] }] },
  ] as Scenario[];
  // 联网查证：规划带检索词；候选来源全选；证据摘录取自抓取到的正文。
  const planResearch = { title: "标题", direction: "简洁", questions: ["盒模型尺寸与绝对定位的包含块有什么关系？"], searchQueries: [" CSS box-sizing containing block "], sections: [{ title: "说明", brief: "完整解释" }] };
  const results = [{ title: "A", url: "https://example.com/a" }, { title: "B", url: "https://example.com/b" }];
  list.push({ name: "research-plan-then-search-fails", version: research(), config: cfg, runs: [{ rules: [{ system: "editor", reply: { content: JSON.stringify(planResearch) } }], search: { throwOn: { "CSS box-sizing containing block": "搜索服务不可用" } } }] });
  list.push({ name: "research-429-limits", version: research(true), config: cfg, runs: [{ rules: [{ system: "editor", reply: { content: JSON.stringify(planResearch) } }], search: { default: results, throwOn: { "CSS box-sizing containing block": "429" } } }] });
  list.push({ name: "research-irrelevant-results", version: research(), config: cfg, runs: [{ rules: [
    { system: "editor", userIncludes: "candidates", reply: { content: JSON.stringify({ urls: [] }) } },
    { system: "editor", reply: { content: JSON.stringify(planResearch) } }], search: { default: results } }] });
  list.push({ name: "research-capture-fails", version: research(), config: cfg, runs: [{ rules: [
    { system: "editor", userIncludes: "candidates", reply: { echoCandidates: true } },
    { system: "editor", reply: { content: JSON.stringify(planResearch) } }], search: { default: results }, capture: { default: { throw: "capture failed" } } }] });
  const quote = SOURCE.slice(0, 50);
  const researchFull: ChatRule[] = [
    { system: "editor", userIncludes: "candidates", reply: { echoCandidates: true } },
    { system: "editor", userIncludes: "判断正文是否足以支持", reply: { content: JSON.stringify({ adequate: true, missing: "", followUpQueries: [], evidence: [{ id: "R1", quotes: [quote] }] }) } },
    { system: "editor", userIncludes: "撰写指定章节", reply: { content: JSON.stringify({ sections: [{ title: "说明", markdown: BODY, referenceIds: ["R1"] }] }) } },
    { system: "editor", reply: { content: JSON.stringify(planResearch) } },
    designOk,
  ];
  list.push({ name: "research-full", version: research(), config: cfg, runs: [{ rules: researchFull, search: { default: results } }] });
  list.push({ name: "research-gap-then-limit", version: research(), config: cfg, runs: [{ rules: [
    researchFull[0], { system: "editor", userIncludes: "判断正文是否足以支持", reply: { content: JSON.stringify({ adequate: false, missing: "关键条件不足", followUpQueries: ["follow1"], evidence: [{ id: "R1", quotes: [quote] }] }) } },
    researchFull[3]], search: { default: results, byQuery: { follow1: [{ title: "C", url: "https://example.com/c" }] } } }] });
  const scriptDesign: ChatRule = { system: "design", reply: { deltas: design(REC_META, { type: "section", id: "intro", html: '<main><h1>标题</h1><h2>说明</h2><div id="tool"><button id="b">点我</button></div><div data-reading-copy="s0b0"></div></main>' }, { type: "script", id: "tool1", rootId: "tool", code: "document.getElementById('b').addEventListener('click',function(){});" }, REC_DONE), finishReason: "stop" } };
  list.push({ name: "design-with-script", version: baseVersion(R), config: cfg, runs: [{ rules: [editorOk, scriptDesign] }] });
  list.push({ name: "repair-model-fixes-script", version: baseVersion(R), config: cfg, runs: [
    { rules: [editorOk, scriptDesign] },
    { op: "repair", verify: ["交互 tool1 运行失败：x is not defined", null], rules: [{ userIncludes: "仅修复失败的经典JavaScript模块", reply: { content: JSON.stringify({ code: "document.getElementById('b').addEventListener('click',function(){ var x = 1; });" }) } }] },
  ] });
  list.push({ name: "repair-still-failing", version: baseVersion(R), config: cfg, runs: [
    { rules: [editorOk, scriptDesign] },
    { op: "repair", verify: ["交互 tool1 运行失败：x", "交互 tool1 运行失败：仍然失败"], rules: [{ userIncludes: "仅修复失败的经典JavaScript模块", reply: { content: JSON.stringify({ code: "document.getElementById('b').addEventListener('click',function(){});" }) } }] },
  ] });
  list.push({ name: "repair-bad-syntax", version: baseVersion(R), config: cfg, runs: [
    { rules: [editorOk, scriptDesign] },
    { op: "repair", verify: ["交互 tool1 运行失败：x", null], rules: [{ userIncludes: "仅修复失败的经典JavaScript模块", reply: { content: JSON.stringify({ code: "function(" }) } }] },
  ] });
  return list;
}

export async function runScenario(s: Scenario, stubs: { setScenario: (h: any) => void }, runPipeline: (version: any, config: any, signal: AbortSignal, hooks: any) => Promise<void>, runRepair: (...a: any[]) => Promise<void>) {
  const out: any[] = [];
  let version: any = JSON.parse(JSON.stringify(s.version));
  for (const run of s.runs) {
    const events: any[] = [], calls: any[] = [], usage: any[] = [], searches: string[] = [], captures: any[] = [];
    const ac = new AbortController();
    const used = new Map<ChatRule, number>();
    let lastCheckpoint: any = null;
    stubs.setScenario({
      chat: async (config: any, messages: any[], options: any = {}) => {
        const user = messages.find(m => m.role === "user")?.content ?? "";
        const sys = messages.find(m => m.role === "system")?.content ?? "";
        calls.push({ config, messages, options: { temperature: options.temperature, maxTokens: options.maxTokens, timeoutMs: options.timeoutMs, stream: options.stream, progress: !!options.onProgress, delta: !!options.onDelta } });
        const rule = run.rules.find(r => (!r.system || sys.startsWith(r.system === "editor" ? EDITOR_PREFIX : DESIGN_PREFIX)) && (!r.userIncludes || user.includes(r.userIncludes)) && (r.times === undefined || (used.get(r) ?? 0) < r.times));
        if (!rule) throw new Error("脚本规则已用尽");
        used.set(rule, (used.get(rule) ?? 0) + 1);
        const reply = rule.reply;
        if (reply.abort) ac.abort();
        let content = reply.content ?? "";
        if (reply.echoCandidates) content = JSON.stringify({ urls: JSON.parse(user).candidates.map((c: any) => c.url) });
        for (const d of reply.deltas ?? []) { options.onDelta?.(d); options.onProgress?.(d.length); }
        if (reply.throw) throw new Error(reply.throw);
        return { content, finishReason: reply.finishReason, usage: reply.usage };
      },
      usage: (u: any) => usage.push(u),
      search: async (query: string) => {
        searches.push(query);
        const spec = run.search ?? {};
        const bad = Object.entries(spec.throwOn ?? {}).find(([k]) => query.includes(k));
        if (bad) throw new Error(bad[1]);
        return spec.byQuery?.[query] ?? spec.default ?? [];
      },
      capture: async (request: any) => {
        captures.push({ url: request.url, purpose: request.purpose, taskId: request.taskId });
        const spec = run.capture ?? {};
        const bad = Object.entries(spec.throwOn ?? {}).find(([k]) => request.url.includes(k));
        if (bad) throw new Error(bad[1]);
        const r = spec.byUrl?.[request.url] ?? spec.default ?? { complete: true, markdown: SOURCE };
        if (r.throw) throw new Error(r.throw);
        return r;
      },
    });
    const snap = () => mask(JSON.parse(JSON.stringify(version)));
    const hooks = {
      checkpoint: () => { lastCheckpoint = JSON.parse(JSON.stringify(version)); events.push({ k: "checkpoint", v: snap() }); },
      stage: (stage: string, done?: number, total?: number) => events.push({ k: "stage", stage, done: done ?? 0, total: total ?? 0 }),
      request: (kind: string) => events.push({ k: "request", kind }),
      designProgress: (attempt: number, chars: number) => events.push({ k: "designProgress", attempt, chars }),
    };
    let error: string | null = null;
    try {
      if (run.op === "repair") await runRepair(version, s.config, ac.signal, run.verify ?? [], hooks);
      else await runPipeline(version, s.config, ac.signal, hooks);
    } catch (e) { error = e instanceof Error ? e.message : String(e); }
    out.push({ calls, events, usage, searches, captures, error, result: error ? null : snap() });
    // 失败后下一轮从最近一次保存的版本继续（运行时重新读库的效果）
    if (error) version = lastCheckpoint ?? version; else version = JSON.parse(JSON.stringify(version));
  }
  return out;
}
