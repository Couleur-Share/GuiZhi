/**
 * aiflow 全局问答依赖组装的 golden：真实的 createQaDeps 接上假的 window.api（knowledge / wiki / semantic）与假传输。
 * Go 侧重放见 apps/desktop-go/aiflow/qa_deps_golden_test.go（NewQADeps）。引擎本身见 aiflow-golden-qa-engine.test.ts。
 *
 * 这一层只做接线，golden 钉住的是接线细节：
 *   - readItem：已删除（deletedAt != null，含 0）与不存在返回 null、标题回落「无标题」、字段透传；
 *   - wiki 三个调用原样透传参数与结果（Go 的页面类型只含问答用到的字段，夹具也只放这些字段）；
 *   - chat 走 qa 场景（用 scenarioModelDefaults 把 qa 单独路由到 fast 模型来验证）、选项与流式透传、用量与截断日志；
 *   - searchItems 走 FTS + 语义混合，同一句降级提示在同一份依赖里只回调一次。
 *
 * readItem 的结果按「null 与缺省等价」归一（Go 的 *string 分不出 null 与缺省，引擎也不区分）。
 */
import { afterEach, describe, it, vi } from "vitest";
import { createQaDeps } from "../../../src/renderer/services/knowledge-ai/qa";
import { resetQueryVectors } from "../../../src/renderer/services/knowledge-ai/query-vectors";
import { useSettingsStore } from "../../../src/renderer/stores/settings.store";
import { writeOrCompareGolden, type ScriptStep } from "./aiflow-golden-kit";
import { TWO_TIER, WITH_EMBEDDING, body, errorInfo, runFlow, tokens, type FlowCase, type Raw } from "./aiflow-golden-flow-kit";

afterEach(() => {
  resetQueryVectors();
  vi.restoreAllMocks();
});

interface Fail {
  throws: string;
}
const isFail = (value: unknown): value is Fail => value !== null && typeof value === "object" && "throws" in (value as Raw);

type SemanticSpec = { hits: Raw[] } | Fail | { hang: true };

type WikiCall =
  | { call: "catalog"; result: unknown }
  | { call: "search"; query: string; limit: number; result: unknown }
  | { call: "getPage"; id: string; result: unknown };

interface DepsCase extends FlowCase {
  op: "readItem" | "wiki" | "chat" | "search";
  // readItem：knowledge.get 的应答；"$undefined" 表示返回 undefined
  id?: string;
  get?: Raw | null | "$undefined" | Fail;
  // wiki
  wiki?: WikiCall;
  // chat
  messages?: { role: "system" | "user"; content: string }[];
  options?: { temperature?: number; maxTokens?: number; stream?: boolean };
  // search
  queries?: string[];
  limit?: number;
  fts?: { entries: Raw[] } | Fail;
  /** sequence：按语义检索被调用的次序依次给出行为（第 n 次调用取第 n 项） */
  semantic?: SemanticSpec | { sequence: SemanticSpec[] };
  /** 每个检索词各用一份新的依赖（警告去重按依赖实例计） */
  freshDeps?: boolean;
}

const embedBody = (vectors: number[][], promptTokens = 7): string =>
  JSON.stringify({
    object: "list",
    data: vectors.map((embedding, index) => ({ object: "embedding", index, embedding })),
    usage: { prompt_tokens: promptTokens, total_tokens: promptTokens },
  });
const EMB_OK: ScriptStep = { body: embedBody([[3, 4]]) };
const EMB_HTTP_500: ScriptStep = { status: 500, body: JSON.stringify({ error: { message: "upstream down" } }) };

const sse = (...events: object[]): string => `${events.map((e) => `data: ${JSON.stringify(e)}`).join("\n")}\n`;
const delta = (content: string, finish?: string): object => ({ choices: [{ index: 0, delta: { content }, ...(finish ? { finish_reason: finish } : {}) }] });

const base = (name: string, over: Partial<DepsCase> & Pick<DepsCase, "op">): DepsCase => ({ name, settings: TWO_TIER, script: [], ...over });

const ITEM: Raw = {
  id: "a",
  title: "归知架构",
  content: "正文内容",
  transcript: "口播转写",
  reviewStatus: "needs_review",
  reviewReasons: ["OCR 未完成"],
  updatedAt: 1_700_000_000_123,
  deletedAt: null,
};
const getItem = (name: string, get: DepsCase["get"], id = "a"): DepsCase => base(name, { op: "readItem", id, get });

/** 路由默认优先于场景默认（resolveScenarioModel），所以要让 qa 的场景默认生效，必须清掉 mainText 路由。 */
const QA_ROUTED: Raw = { aiModels: TWO_TIER.aiModels, modelRouteDefaults: {}, scenarioModelDefaults: { qa: "fast" } };
const MESSAGES: DepsCase["messages"] = [
  { role: "system", content: "你是知识库助手。" },
  { role: "user", content: "归知用什么技术栈？" },
];

const FTS = (...entries: Raw[]): DepsCase["fts"] => ({ entries });
const SEM = (...hits: Raw[]): SemanticSpec => ({ hits });
const ftsEntry = (id: string): Raw => ({ id, title: `关键词标题-${id}`, snippet: `关键词摘录-${id}` });
const semHit = (itemId: string, score: number): Raw => ({ itemId, title: `语义标题-${itemId}`, snippet: `命中段-${itemId}`, score });

const search = (name: string, over: Partial<DepsCase>): DepsCase =>
  base(name, { op: "search", settings: WITH_EMBEDDING, script: [EMB_OK], queries: ["归知怎么做检索"], limit: 5, fts: FTS(ftsEntry("a")), semantic: SEM(semHit("b", 0.9)), ...over });

const CASES: DepsCase[] = [
  // ---- readItem ----
  getItem("readItem：字段透传", ITEM),
  getItem("readItem：标题为空回落「无标题」", { ...ITEM, title: "" }),
  getItem("readItem：标题只有空白时原样保留", { ...ITEM, title: "  " }),
  getItem("readItem：条目不存在（返回 null）", null),
  getItem("readItem：条目不存在（返回 undefined）", "$undefined"),
  getItem("readItem：已删除（deletedAt 有值）返回 null", { ...ITEM, deletedAt: 1_700_000_000_999 }),
  getItem("readItem：deletedAt 为 0 也算已删除", { ...ITEM, deletedAt: 0 }),
  getItem("readItem：deletedAt 缺省按未删除", { id: "a", title: "T", content: "C" }),
  getItem("readItem：转写稿为 null", { ...ITEM, transcript: null }),
  getItem("readItem：审核信息与版本缺省", { id: "a", title: "T", content: "C", transcript: "转写" }),
  getItem("readItem：reviewReasons 为空数组", { ...ITEM, reviewStatus: "clear", reviewReasons: [] }),
  getItem("readItem：正文为空也原样返回（空判断由引擎做）", { ...ITEM, content: "", transcript: null }),
  getItem("readItem：读取失败向上抛出", { throws: "knowledge.get 失败" }),

  // ---- wiki ----
  base("wiki：目录原样透传", {
    op: "wiki",
    wiki: {
      call: "catalog",
      result: [
        { id: "w1", title: "采集工作流", normalizedTitle: "采集工作流", kind: "topic", summary: "摘要", aliasesJson: '["采集"]', manualEditedAt: null, updatedAt: 1 },
        { id: "w2", title: "检索设计", normalizedTitle: "检索设计", kind: "concept", summary: "", aliasesJson: null, manualEditedAt: 5, updatedAt: 2 },
      ],
    },
  }),
  base("wiki：页面检索透传检索词与条数", {
    op: "wiki",
    wiki: { call: "search", query: "采集 流程", limit: 4, result: [{ id: "w1", title: "采集工作流", kind: "entity", summary: "摘要" }] },
  }),
  base("wiki：页面检索无命中", { op: "wiki", wiki: { call: "search", query: "无", limit: 4, result: [] } }),
  base("wiki：页面详情透传 id 与来源条目", {
    op: "wiki",
    wiki: {
      call: "getPage",
      id: "w1",
      result: {
        page: { id: "w1", title: "采集工作流", body: "正文 [[检索设计]]", updatedAt: 1_700_000_000_001 },
        sources: [
          { itemId: "a", title: "归知架构" },
          { itemId: "b", title: "采集管线", reviewStatus: "needs_review", reviewReasons: ["正文缺失"] },
        ],
      },
    },
  }),
  base("wiki：页面不存在", { op: "wiki", wiki: { call: "getPage", id: "gone", result: null } }),
  base("wiki：目录读取失败", { op: "wiki", wiki: { call: "catalog", result: { throws: "目录读取失败" } } }),
  base("wiki：页面检索失败", { op: "wiki", wiki: { call: "search", query: "x", limit: 4, result: { throws: "页面检索失败" } } }),
  base("wiki：页面读取失败", { op: "wiki", wiki: { call: "getPage", id: "w1", result: { throws: "页面读取失败" } } }),

  // ---- chat ----
  base("chat：走 qa 场景（被单独路由到 fast），选项透传", {
    op: "chat",
    settings: QA_ROUTED,
    messages: MESSAGES,
    options: { temperature: 0.2, maxTokens: 4096 },
    script: [{ body: body("回答 [1]", "stop", tokens(12, 5)) }],
  }),
  base("chat：qa 没有单独路由时用主模型", {
    op: "chat",
    messages: MESSAGES,
    options: { temperature: 0.2, maxTokens: 4096 },
    script: [{ body: body("回答 [1]", "stop", tokens(12, 5)) }],
  }),
  base("chat：不传选项用默认值", {
    op: "chat",
    settings: QA_ROUTED,
    messages: MESSAGES,
    options: {},
    script: [{ body: body("回答", "stop") }],
  }),
  base("chat：流式逐块回调", {
    op: "chat",
    settings: QA_ROUTED,
    messages: MESSAGES,
    options: { temperature: 0.2, maxTokens: 4096, stream: true },
    script: [{ chunks: [sse(delta("归知")), sse(delta("基于 Electron", "stop"))] }],
  }),
  base("chat：撞上 max_tokens 记截断日志并带回 finishReason", {
    op: "chat",
    settings: QA_ROUTED,
    messages: MESSAGES,
    options: { temperature: 0.2, maxTokens: 4096 },
    script: [{ body: body("没写完", "length", tokens(30, 4096)) }],
  }),
  base("chat：未配置模型", { op: "chat", settings: { aiModels: [], modelRouteDefaults: {} }, messages: MESSAGES, options: { temperature: 0.2, maxTokens: 4096 } }),
  base("chat：请求失败记一笔失败用量", {
    op: "chat",
    settings: QA_ROUTED,
    messages: MESSAGES,
    options: { temperature: 0.2, maxTokens: 4096 },
    script: [{ status: 500, body: JSON.stringify({ error: { message: "boom" } }) }],
  }),
  base("chat：调用前已取消", { op: "chat", settings: QA_ROUTED, messages: MESSAGES, options: { temperature: 0.2, maxTokens: 4096 }, abort: "before" }),
  base("chat：请求进行中取消", {
    op: "chat",
    settings: QA_ROUTED,
    messages: MESSAGES,
    options: { temperature: 0.2, maxTokens: 4096 },
    script: [{ abortDuring: true, status: 499, body: "", error: "已取消" }],
  }),

  // ---- searchItems ----
  search("search：未配置 embedding 时只返回关键词命中", { settings: TWO_TIER, script: [] }),
  search("search：关键词与语义融合", {}),
  search("search：语义抛错告警，同一句话两次检索只回调一次", {
    queries: ["检索词一", "检索词二"],
    script: [EMB_OK, EMB_OK],
    semantic: { throws: "semantic index is rebuilding" },
  }),
  search("search：每个检索词各用新依赖，警告各回调一次", {
    queries: ["检索词一", "检索词二"],
    script: [EMB_OK, EMB_OK],
    semantic: { throws: "semantic index is rebuilding" },
    freshDeps: true,
  }),
  search("search：查询向量失败与语义抛错是同一句提示，只回调一次", {
    queries: ["检索词一", "检索词二", "检索词三"],
    script: [EMB_OK, EMB_HTTP_500, EMB_OK],
    semantic: { throws: "semantic index is rebuilding" },
  }),
  search("search：超时与不可用是两句不同的提示，各回调一次", {
    queries: ["检索词一", "检索词二", "检索词三"],
    script: [EMB_OK, EMB_OK, EMB_OK],
    semantic: { sequence: [{ hang: true }, { throws: "semantic index is rebuilding" }, { hang: true }] },
  }),
  // 未配置 embedding：关键词这一路先失败时，语义这一路是否已经发出请求是竞态，会让记录不稳定
  search("search：关键词检索失败整体失败，后面的检索词照常尝试", {
    settings: TWO_TIER,
    queries: ["检索词一", "检索词二"],
    script: [],
    fts: { throws: "fts index corrupted" },
  }),
  search("search：调用前已取消", { script: [], abort: "before" }),
];

// ---- 运行 ----

interface TestWindow {
  api: Record<string, unknown>;
}
const testWindow = (): TestWindow => (globalThis as unknown as { window: TestWindow }).window;

/** readItem 的结果归一：null 与缺省等价。 */
function normalizeItem(item: unknown): unknown {
  if (item === null) return null;
  const copy = { ...(item as Raw) };
  if (copy.transcript === null || copy.transcript === undefined) delete copy.transcript;
  return JSON.parse(JSON.stringify(copy));
}

async function runDepsCase(c: DepsCase, baseline: Raw): Promise<Raw> {
  const records = {
    getCalls: [] as Raw[],
    wikiCalls: [] as Raw[],
    ftsCalls: [] as Raw[],
    semanticCalls: [] as Raw[],
    onWarnings: [] as string[],
    deltas: [] as string[],
  };
  resetQueryVectors();
  // 3 秒的语义超时在测试里缩成 150 毫秒；其余定时器原样（与 aiflow-golden-semantic.test.ts 同一做法）
  const realSetTimeout = globalThis.setTimeout;
  vi.spyOn(globalThis, "setTimeout").mockImplementation(((handler: TimerHandler, ms?: number, ...args: unknown[]) =>
    realSetTimeout(handler as () => void, ms === 3000 ? 150 : ms, ...args)) as typeof setTimeout);

  const outcome = await runFlow(
    c,
    baseline,
    async (signal) => {
      const wiki = c.wiki;
      testWindow().api = {
        ...testWindow().api,
        knowledge: {
          get: async (id: string) => {
            records.getCalls.push({ id });
            if (isFail(c.get)) throw new Error(c.get.throws);
            return c.get === "$undefined" || c.get === undefined ? undefined : structuredClone(c.get);
          },
          list: async (params: Raw) => {
            records.ftsCalls.push(params);
            if (!c.fts) return { entries: [], total: 0 };
            if (isFail(c.fts)) throw new Error(c.fts.throws);
            return { entries: c.fts.entries, total: c.fts.entries.length };
          },
        },
        wiki: {
          catalog: async () => {
            records.wikiCalls.push({ call: "catalog" });
            if (isFail(wiki?.result)) throw new Error(wiki.result.throws);
            return structuredClone(wiki?.result);
          },
          search: async (query: string, limit: number) => {
            records.wikiCalls.push({ call: "search", query, limit });
            if (isFail(wiki?.result)) throw new Error(wiki.result.throws);
            return structuredClone(wiki?.result);
          },
          getPage: async (id: string) => {
            records.wikiCalls.push({ call: "getPage", id });
            if (isFail(wiki?.result)) throw new Error(wiki.result.throws);
            return structuredClone(wiki?.result);
          },
        },
        semantic: {
          search: async (params: { model: string; vector: number[]; limit: number }) => {
            records.semanticCalls.push({ model: params.model, vector: params.vector, limit: params.limit });
            const spec = c.semantic;
            const behavior: SemanticSpec | undefined = spec && "sequence" in spec ? spec.sequence[records.semanticCalls.length - 1] : spec;
            if (!behavior) return [];
            if (isFail(behavior)) throw new Error(behavior.throws);
            if ("hang" in behavior) return new Promise(() => undefined);
            return behavior.hits;
          },
        },
      };

      const make = () => createQaDeps({ onWarning: (message) => records.onWarnings.push(message) });
      const deps = make();
      switch (c.op) {
        case "readItem":
          return normalizeItem(await deps.readItem(c.id as string));
        case "wiki": {
          if (!wiki) throw new Error("缺少 wiki 参数");
          if (wiki.call === "catalog") return deps.getWikiCatalog?.();
          if (wiki.call === "search") return deps.searchWikiPages?.(wiki.query, wiki.limit);
          return deps.readWikiPage?.(wiki.id);
        }
        case "chat": {
          const options = c.options ?? {};
          const result = await deps.chat(c.messages ?? [], {
            ...(options.temperature === undefined ? {} : { temperature: options.temperature }),
            ...(options.maxTokens === undefined ? {} : { maxTokens: options.maxTokens }),
            signal,
            onDelta: options.stream ? (chunk) => records.deltas.push(chunk) : undefined,
          });
          return result;
        }
        case "search": {
          const results: Raw[] = [];
          for (const query of c.queries ?? []) {
            const target = c.freshDeps ? make() : deps;
            try {
              results.push({ ok: true, hits: await target.searchItems(query, c.limit ?? 5, signal) });
            } catch (error) {
              results.push({ ok: false, error: errorInfo(error, { abortMessage: false }) });
            }
          }
          return results;
        }
      }
    },
    { abortMessage: false },
  );
  return { ...outcome, ...records };
}

describe("aiflow golden：全局问答依赖组装", () => {
  it("createQaDeps 与已提交的 golden 一致", async () => {
    const baseline = { ...useSettingsStore.getState() };
    const cases: Raw[] = [];
    const names = new Set<string>();
    try {
      for (const c of CASES) {
        if (names.has(c.name)) throw new Error(`用例重名：${c.name}`);
        names.add(c.name);
        const expect = await runDepsCase(c, baseline as unknown as Raw);
        cases.push({ ...c, expect });
        vi.restoreAllMocks();
      }
    } finally {
      useSettingsStore.setState(baseline);
    }
    writeOrCompareGolden("qa-deps", { cases });
  });
});
