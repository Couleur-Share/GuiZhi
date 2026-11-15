/**
 * aiflow 配置与场景调用的 golden：
 *   - 设置归一化（settings-persistence 的 AI 部分）+ 场景/路由模型解析（ai-defaults、ai-invoke.resolveConfig、embeddings.resolveEmbeddingConfig）；
 *   - runScenarioChat 的请求、用量记录与截断日志。
 * Go 侧重放见 apps/desktop-go/aiflow/config_golden_test.go；约定见 aiflow-golden-kit.ts。
 */
import { describe, it } from "vitest";
import {
  isAiConfiguredForScenario,
  resolveConfig,
  runScenarioChat,
} from "../../../src/renderer/services/knowledge-ai/ai-invoke";
import { isSemanticConfigured, resolveEmbeddingConfig } from "../../../src/renderer/services/knowledge-ai/embeddings";
import { resolveRouteModel } from "../../../src/renderer/services/ai-defaults";
import { useSettingsStore } from "../../../src/renderer/stores/settings.store";
import { mergeSettingsState } from "../../../src/renderer/stores/settings/settings-persistence";
import {
  captureWarnings,
  describeError,
  installFakeTransport,
  writeOrCompareGolden,
  type ScriptStep,
} from "./aiflow-golden-kit";

const SCENARIOS = ["summary", "tagging", "qa", "wiki", "research", "transcription", "ocr"] as const;
const ROUTES = ["mainText", "fastText", "visionText", "embedding", "audioText", "imageGen"] as const;

type Raw = Record<string, unknown>;

const model = (id: string, over: Raw = {}): Raw => ({
  id,
  provider: "custom",
  apiProtocol: "openai",
  apiKey: `sk-${id}`,
  apiUrl: "https://api.example.com/v1",
  model: `model-${id}`,
  ...over,
});
const chat = (id: string, over: Raw = {}): Raw => model(id, { capabilities: { chat: true }, ...over });
const vision = (id: string, over: Raw = {}): Raw => model(id, { capabilities: { chat: true, vision: true }, ...over });
const embedding = (id: string, over: Raw = {}): Raw =>
  model(id, { capabilities: { chat: false, embedding: true }, ...over });
const whisper = (id: string, over: Raw = {}): Raw =>
  model(id, { capabilities: { chat: false, audioTranscription: true }, ...over });

interface ConfigCase {
  name: string;
  settings: Raw;
}

const CONFIG_CASES: ConfigCase[] = [
  { name: "空设置：什么都没配", settings: {} },
  {
    name: "只有旧版单模型字段：问答类场景兜底，视觉与转写不兜底",
    settings: { aiProvider: "custom", aiApiProtocol: "openai", aiApiKey: "sk-legacy", aiApiUrl: "https://legacy.example.com", aiModel: "legacy-1" },
  },
  {
    name: "旧版字段缺 key（全空白）视为未配置",
    settings: { aiProvider: "custom", aiApiKey: "   ", aiApiUrl: "https://legacy.example.com", aiModel: "legacy-1" },
  },
  {
    name: "旧版字段的协议按地址推断",
    settings: { aiProvider: "x", aiApiProtocol: "bogus", aiApiKey: "k", aiApiUrl: "https://api.anthropic.com/v1", aiModel: "claude-x" },
  },
  {
    name: "两个对话模型：isDefault 的优先，其次第一个",
    settings: { aiModels: [chat("a"), chat("b", { isDefault: true })] },
  },
  {
    name: "没有 isDefault：取第一个启用的对话模型",
    settings: { aiModels: [chat("a", { enabled: false }), chat("b"), chat("c", { isDefault: true, enabled: false })] },
  },
  {
    name: "路由默认优先于 isDefault；场景默认在路由默认缺失时生效",
    settings: {
      aiModels: [chat("a", { isDefault: true }), chat("b"), chat("c")],
      modelRouteDefaults: { mainText: "b" },
      scenarioModelDefaults: { summary: "c", qa: "a", tagging: "c" },
    },
  },
  {
    name: "只给场景默认：据此推导路由默认（qa→mainText，summary→fastText，ocr→visionText）",
    settings: {
      aiModels: [chat("a"), chat("b"), vision("v1"), vision("v2")],
      scenarioModelDefaults: { wiki: "b", tagging: "b", ocr: "v2" },
    },
  },
  {
    name: "场景默认指向不存在或已停用的模型：回退到默认",
    settings: {
      aiModels: [chat("a", { isDefault: true }), chat("off", { enabled: false })],
      scenarioModelDefaults: { qa: "gone", summary: "off" },
    },
  },
  {
    name: "视觉路由只认 vision 能力；chat:false 的模型不进对话候选",
    settings: {
      aiModels: [embedding("e1"), chat("a"), vision("v1")],
      modelRouteDefaults: { embedding: "e1" },
    },
  },
  {
    name: "vision 依附于 chat：chat:false + vision:true 不算视觉模型",
    settings: { aiModels: [model("odd", { capabilities: { chat: false, vision: true } }), chat("a")] },
  },
  {
    name: "embedding 路由：显式默认、缺省取第一个、未配置",
    settings: { aiModels: [embedding("e1"), embedding("e2", { isDefault: true })], modelRouteDefaults: { embedding: "e2" } },
  },
  {
    name: "embedding 缺省：没有 embedding 路由默认时取第一个候选（不看 isDefault 之外的优先）",
    settings: { aiModels: [embedding("e1"), embedding("e2", { isDefault: true })] },
  },
  {
    name: "音频路由：转写模型优先；只认显式设置，不回退",
    settings: {
      aiModels: [chat("a"), whisper("w1")],
      modelRouteDefaults: { audioText: "w1" },
    },
  },
  {
    name: "音频路由：没有显式设置就没有结果；图像生成同理",
    settings: {
      aiModels: [chat("a"), whisper("w1"), model("img", { capabilities: { chat: false, imageGeneration: true } })],
    },
  },
  {
    name: "音频路由：显式指向对话模型也可以",
    settings: { aiModels: [chat("a"), whisper("w1")], modelRouteDefaults: { audioText: "a", imageGen: "a" } },
  },
  {
    name: "供应商停用：带 providerId 的模型被排除（仅 embedding 解析传入供应商）",
    settings: {
      aiProviders: [{ id: "p1", name: "停用的", provider: "custom", apiProtocol: "openai", apiKey: "sk-p1", apiUrl: "https://api.example.com/v1", enabled: false }],
      aiModels: [chat("a", { providerId: "p1", isDefault: true }), embedding("e1", { providerId: "p1" })],
      modelRouteDefaults: { embedding: "e1" },
    },
  },
  {
    name: "供应商停用：没有 providerId 时按 provider/协议/地址/key 匹配",
    settings: {
      aiProviders: [{ id: "p1", provider: "custom", apiProtocol: "openai", apiKey: "sk-e1", apiUrl: "https://api.example.com/v1", enabled: false }],
      aiModels: [embedding("e1"), embedding("e2", { apiKey: "other" })],
    },
  },
  {
    name: "供应商匹配：provider.id 等于模型的 provider 字段",
    settings: {
      aiProviders: [{ id: "custom", provider: "other", apiProtocol: "openai", apiKey: "k", apiUrl: "https://z.example.com", enabled: false }],
      aiModels: [embedding("e1")],
    },
  },
  {
    name: "供应商匹配：providerId 指向不存在的供应商视为启用",
    settings: {
      aiProviders: [{ id: "p1", provider: "custom", apiKey: "k", apiUrl: "https://z.example.com", enabled: false }],
      aiModels: [embedding("e1", { providerId: "ghost" })],
    },
  },
  {
    name: "非法条目被丢弃：缺 id / 空白 id / 非字符串字段 / 数组 / null",
    settings: {
      aiModels: [
        null, 5, [], "x",
        chat("ok-1"),
        { ...chat("blank"), id: "   " },
        { ...chat("noprovider"), provider: "" },
        { ...chat("nourl"), apiUrl: "  " },
        { ...chat("nomodel"), model: "" },
        { ...chat("numid"), id: 7 },
        { ...chat("nokey"), apiKey: undefined },
        { ...chat("numkey"), apiKey: 12345 },
      ],
      aiProviders: [null, { id: "", provider: "x", apiUrl: "u" }, { id: "p", provider: "x", apiUrl: "https://z" }],
    },
  },
  {
    name: "字段被 trim，协议按需推断，enabled 只有 false 才停用",
    settings: {
      aiModels: [
        chat("  padded  ", { provider: "  Anthropic ", apiUrl: " https://x.example.com ", model: " m ", apiProtocol: "weird", enabled: "no" }),
        chat("gem", { provider: "google", apiProtocol: undefined, model: "models/gemini-x" }),
      ],
    },
  },
  {
    name: "能力缺省：capabilities 缺失或不是对象时按纯对话模型",
    settings: {
      aiModels: [model("nocap"), model("arrcap", { capabilities: [1] }), model("strcap", { capabilities: "chat" }), model("emb", { capabilities: { embedding: true } })],
    },
  },
  {
    name: "场景默认与路由默认的值会 trim，未知键与非字符串值被丢弃",
    settings: {
      aiModels: [chat("a"), chat("b")],
      scenarioModelDefaults: { qa: "  b  ", summary: 5, unknown: "a", wiki: "" },
      modelRouteDefaults: { mainText: " b ", fastText: null, nope: "a" },
    },
  },
  {
    name: "路由默认全是空/非法时重新从场景默认推导",
    settings: {
      aiModels: [chat("a"), chat("b")],
      scenarioModelDefaults: { summary: "b" },
      modelRouteDefaults: { mainText: "", nope: "a" },
    },
  },
];

function configJSON(config: unknown): unknown {
  return config ?? null;
}

describe("aiflow golden：配置归一化与场景模型解析", () => {
  it("与已提交的 golden 一致", () => {
    const initial = useSettingsStore.getState();
    const baseline = { ...initial };
    const cases = CONFIG_CASES.map((c) => {
      useSettingsStore.setState(mergeSettingsState(c.settings, baseline as never));
      const state = useSettingsStore.getState();
      return {
        name: c.name,
        settings: c.settings,
        expect: {
          scenarios: Object.fromEntries(SCENARIOS.map((s) => [s, configJSON(resolveConfig(s))])),
          configured: Object.fromEntries(SCENARIOS.map((s) => [s, isAiConfiguredForScenario(s)])),
          routes: Object.fromEntries(
            ROUTES.map((r) => [r, resolveRouteModel(state.aiModels, state.modelRouteDefaults, r)?.id ?? null]),
          ),
          routesWithProviders: Object.fromEntries(
            ROUTES.map((r) => [r, resolveRouteModel(state.aiModels, state.modelRouteDefaults, r, state.aiProviders)?.id ?? null]),
          ),
          embedding: configJSON(resolveEmbeddingConfig()),
          semantic: isSemanticConfigured(),
          normalized: {
            modelIds: state.aiModels.map((m) => m.id),
            providerIds: state.aiProviders.map((p) => p.id),
            routeDefaults: state.modelRouteDefaults,
            scenarioDefaults: state.scenarioModelDefaults,
            protocols: state.aiModels.map((m) => m.apiProtocol),
            legacyProtocol: state.aiApiProtocol,
          },
        },
      };
    });
    useSettingsStore.setState(baseline);
    writeOrCompareGolden("config", { cases });
  });
});

// ---- runScenarioChat ----

interface InvokeCase {
  name: string;
  settings: Raw;
  scenario: (typeof SCENARIOS)[number];
  options?: { temperature?: number; maxTokens?: number; timeoutMs?: number; stream?: boolean };
  script: ScriptStep[];
  abort?: "before" | "during";
}

const body = (content: string, finish: string | null = "stop", usage?: unknown): string =>
  JSON.stringify({ choices: [{ message: { content }, finish_reason: finish }], ...(usage ? { usage } : {}) });
const sse = (...events: object[]): string => `${events.map((e) => `data: ${JSON.stringify(e)}`).join("\n")}\n`;

const TWO_TIER: Raw = {
  aiModels: [chat("main", { isDefault: true, provider: "openai", apiUrl: "https://api.openai.com/v1" }), chat("fast", { apiUrl: "https://fast.example.com" })],
  modelRouteDefaults: { mainText: "main", fastText: "fast" },
};

const INVOKE_CASES: InvokeCase[] = [
  { name: "未配置抛 AI_NOT_CONFIGURED，不发请求也不记用量", settings: {}, scenario: "qa", script: [] },
  {
    name: "摘要走 fastText 模型，默认超时 120 秒，成功记 token",
    settings: TWO_TIER,
    scenario: "summary",
    options: { temperature: 0.3, maxTokens: 4096 },
    script: [{ body: body("摘要结果", "stop", { prompt_tokens: 100, completion_tokens: 20 }) }],
  },
  {
    name: "问答走 mainText 模型；timeoutMs 覆盖默认值",
    settings: TWO_TIER,
    scenario: "qa",
    options: { timeoutMs: 180_000 },
    script: [{ body: body("答案", "stop", { prompt_tokens: 7, completion_tokens: 8 }) }],
  },
  {
    name: "onDelta 走流式，用量来自流末尾事件",
    settings: TWO_TIER,
    scenario: "qa",
    options: { stream: true, maxTokens: 4096, temperature: 0.2 },
    script: [
      {
        chunks: [
          sse({ choices: [{ delta: { content: "流" } }] }, { choices: [{ delta: { content: "式" }, finish_reason: "stop" }] }, { choices: [], usage: { prompt_tokens: 11, completion_tokens: 2 } }),
        ],
      },
    ],
  },
  {
    name: "失败也记用量（failed）并原样抛错",
    settings: TWO_TIER,
    scenario: "wiki",
    script: [{ status: 500, body: JSON.stringify({ error: { message: "upstream down" } }) }],
  },
  {
    name: "输出被截断：记日志并带回 finishReason=length（有 maxTokens）",
    settings: TWO_TIER,
    scenario: "tagging",
    options: { maxTokens: 2048 },
    script: [{ body: body("被截断的半", "length", { prompt_tokens: 1, completion_tokens: 2 }) }],
  },
  {
    name: "输出被截断：没传 maxTokens 时日志写「默认」",
    settings: TWO_TIER,
    scenario: "research",
    script: [{ body: body("被截断的半", "length") }],
  },
  {
    name: "旧版单模型字段兜底；用量记录用兜底模型名",
    settings: { aiProvider: "custom", aiApiKey: "sk-legacy", aiApiUrl: "https://legacy.example.com", aiModel: "legacy-1" },
    scenario: "summary",
    script: [{ body: body("ok") }],
  },
  {
    name: "视觉场景没有模型时不兜底",
    settings: { aiProvider: "custom", aiApiKey: "sk-legacy", aiApiUrl: "https://legacy.example.com", aiModel: "legacy-1" },
    scenario: "ocr",
    script: [],
  },
  {
    name: "取消：请求前已取消也记一次失败用量",
    settings: TWO_TIER,
    scenario: "qa",
    abort: "before",
    script: [],
  },
  {
    name: "取消：进行中取消",
    settings: TWO_TIER,
    scenario: "qa",
    abort: "during",
    script: [{ abortDuring: true, status: 0, ok: false, error: "已取消" }],
  },
  {
    name: "没有 usage 时记 0",
    settings: TWO_TIER,
    scenario: "summary",
    script: [{ body: body("ok") }],
  },
];

describe("aiflow golden：runScenarioChat", () => {
  it("与已提交的 golden 一致", async () => {
    const initial = useSettingsStore.getState();
    const baseline = { ...initial };
    const cases = [];
    for (const c of INVOKE_CASES) {
      useSettingsStore.setState(mergeSettingsState(c.settings, baseline as never));
      const fake = installFakeTransport(c.script);
      const warnings = captureWarnings();
      if (c.abort === "before") fake.controller.abort();
      const deltas: string[] = [];
      let result: unknown = null;
      let error: ReturnType<typeof describeError> | null = null;
      try {
        const r = await runScenarioChat(c.scenario, [{ role: "user", content: "问" }], {
          temperature: c.options?.temperature,
          maxTokens: c.options?.maxTokens,
          timeoutMs: c.options?.timeoutMs,
          signal: fake.controller.signal,
          onDelta: c.options?.stream ? (chunk) => deltas.push(chunk) : undefined,
        });
        result = { content: r.content, model: r.model, ...(r.finishReason !== undefined ? { finishReason: r.finishReason } : {}) };
      } catch (e) {
        error = { ...describeError(e) };
        if (e instanceof Error && e.name === "AiNotConfiguredError") (error as Record<string, unknown>).notConfigured = true;
      } finally {
        warnings.restore();
      }
      // recordUsage 是 fire-and-forget 的 Promise，让它落地后再读
      await Promise.resolve();
      cases.push({
        name: c.name,
        settings: c.settings,
        scenario: c.scenario,
        options: c.options ?? {},
        script: c.script,
        abort: c.abort ?? null,
        expect: { requests: fake.requests, usage: fake.usage, deltas, warnings: warnings.messages, result, error },
      });
    }
    useSettingsStore.setState(baseline);
    writeOrCompareGolden("invoke", { cases });
  });
});
