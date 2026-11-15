/**
 * aiflow 语义域的 golden：分块、索引循环、查询向量缓存、混合检索（RRF 融合）。
 * Go 侧重放见 apps/desktop-go/aiflow/semantic_golden_test.go；约定见 aiflow-golden-kit.ts / aiflow-golden-flow-kit.ts。
 *
 * 比对范围：
 *   - 分块器 buildEmbeddingChunks 与 RRF 融合 mergeHybridResults 是纯函数，按用例逐项比对；
 *   - runSemanticIndexing / queryVector / hybridSearchItems 经真实的 embedTexts 与假传输、假的 window.api.semantic /
 *     window.api.knowledge：请求、用量、警告、回调序列、结果与错误；
 *   - 取消类错误只比 aborted、不比文案：signal.throwIfAborted() 抛的 reason 在 Node 与 Chromium 里文案不同。
 *
 * 并发语义（多个调用者合并同一次请求、一方取消不影响其他等待者）无法在 TS 里确定性地复现——
 * 摘要计算的完成顺序不由我们控制——这部分由 Go 侧 queryvec_test.go 用可阻塞的假传输单独验证，
 * 对应 TS 侧的 heavy-user-search.test.ts。
 */
import { afterEach, describe, it, vi } from "vitest";
import type { AIConfig } from "../../../src/renderer/services/ai";
import {
  SEMANTIC_CHUNK_OVERLAP,
  SEMANTIC_CHUNK_SIZE,
  SEMANTIC_MAX_CHUNKS,
  buildEmbeddingChunks,
} from "../../../src/renderer/services/knowledge-ai/semantic-chunk";
import { hybridSearchItems, mergeHybridResults } from "../../../src/renderer/services/knowledge-ai/hybrid-search";
import { queryVector, resetQueryVectors } from "../../../src/renderer/services/knowledge-ai/query-vectors";
import { runSemanticIndexing } from "../../../src/renderer/services/knowledge-ai/semantic-index";
import { useSettingsStore } from "../../../src/renderer/stores/settings.store";
import { installFakeTransport, writeOrCompareGolden, type ScriptStep } from "./aiflow-golden-kit";
import {
  A,
  C,
  EMOJI,
  HAN_EXT,
  L,
  LEGACY_ONLY,
  TWO_TIER,
  WITH_EMBEDDING,
  compactText,
  digest,
  errorInfo,
  render,
  runFlow,
  type FlowCase,
  type Raw,
  type Spec,
} from "./aiflow-golden-flow-kit";

afterEach(() => {
  resetQueryVectors();
  vi.restoreAllMocks();
});

/** embeddings 响应体：每个向量一项，index 与位置一致。 */
const embedBody = (vectors: number[][], promptTokens?: number): string =>
  JSON.stringify({
    object: "list",
    data: vectors.map((embedding, index) => ({ object: "embedding", index, embedding })),
    ...(promptTokens === undefined ? {} : { usage: { prompt_tokens: promptTokens, total_tokens: promptTokens } }),
  });

/** 一次 embeddings 调用成功：n 个向量，值取 [3, 4] 或指定值（归一化后为 [0.6, 0.8]）。 */
const EMB_OK = (n = 1, vector: number[] = [3, 4], promptTokens = 7): ScriptStep => ({
  body: embedBody(Array.from({ length: n }, () => vector), promptTokens),
});

const EMB_HTTP_ERROR = (status: number, message: string): ScriptStep => ({
  status,
  body: JSON.stringify({ error: { message } }),
});

// ---- 分块 ----

interface ChunkCase {
  name: string;
  title: Spec;
  content: Spec;
  transcript: Spec | null;
}

// 窗口起点为 620 的倍数、终点为 620k+700：用例里的代理对都避开这些位置（孤立代理项不入 golden）。
const CHUNK_CASES: ChunkCase[] = [
  { name: "全空", title: "", content: "", transcript: null },
  { name: "只有空白", title: "  ", content: " \n\t ", transcript: "\n" },
  { name: "只有标题", title: "仅标题", content: "", transcript: null },
  { name: "标题与正文首尾空白被修剪", title: "  标题  ", content: "  正文  ", transcript: null },
  { name: "无标题短正文", title: "", content: "短正文", transcript: null },
  { name: "短正文加转写稿", title: "T", content: "正文", transcript: "转写内容" },
  { name: "只有转写稿", title: "T", content: "", transcript: "只有转写" },
  { name: "转写稿全是空白被忽略", title: "T", content: "正文", transcript: "   " },
  { name: "转写稿首尾空白被修剪", title: "", content: "正文", transcript: "\n 转写 \n" },
  { name: "JS 空白集：NBSP、BOM、全角空格", title: "\u00A0标题\uFEFF", content: "\uFEFF\u00A0正文\u3000", transcript: null },
  { name: "恰好 700 码元不分块", title: "", content: [C(SEMANTIC_CHUNK_SIZE)], transcript: null },
  { name: "701 码元分两块", title: "", content: [C(SEMANTIC_CHUNK_SIZE + 1)], transcript: null },
  { name: "带标题的 700 码元（标题不占窗口）", title: "标题", content: [C(SEMANTIC_CHUNK_SIZE)], transcript: null },
  { name: "1320 码元：两窗恰好覆盖", title: "", content: [C(1320)], transcript: null },
  { name: "1321 码元：三块", title: "标题", content: [C(1321)], transcript: null },
  {
    name: "恰好 24 块的边界",
    title: "",
    content: [C(SEMANTIC_CHUNK_SIZE + (SEMANTIC_MAX_CHUNKS - 1) * (SEMANTIC_CHUNK_SIZE - SEMANTIC_CHUNK_OVERLAP))],
    transcript: null,
  },
  {
    name: "25 块被截断到 24 块",
    title: "",
    content: [C(SEMANTIC_CHUNK_SIZE + (SEMANTIC_MAX_CHUNKS - 1) * (SEMANTIC_CHUNK_SIZE - SEMANTIC_CHUNK_OVERLAP) + 1)],
    transcript: null,
  },
  { name: "超长内容截断", title: "长文", content: [C(30000)], transcript: null },
  {
    name: "含表情与扩展汉字（窗口避开代理对）",
    title: [L(`${EMOJI}标题`)],
    content: [A(100), L(EMOJI), A(547), L(HAN_EXT), C(700)],
    transcript: null,
  },
  { name: "转写稿拼接后跨窗口", title: "访谈", content: [C(500)], transcript: [C(500)] },
  { name: "正文很长而转写稿很短", title: "", content: [A(900)], transcript: "短" },
];

function chunkCaseOutput(c: ChunkCase): Raw {
  const content = render(c.content);
  const transcript = c.transcript === null ? null : render(c.transcript);
  const title = render(c.title);
  const chunks = buildEmbeddingChunks(title, content, transcript);
  return {
    name: c.name,
    title: c.title,
    content: c.content,
    transcript: c.transcript,
    inputs: { title: digest(title), content: digest(content), transcript: transcript === null ? null : digest(transcript) },
    count: chunks.length,
    lengths: chunks.map((chunk) => chunk.length),
    chunks: chunks.map(compactText),
  };
}

// ---- RRF 融合 ----

interface MergeHit {
  id: string;
  title: string;
  snippet: string;
  matchText?: string;
  reviewStatus?: "clear" | "needs_review";
  reviewReasons?: string[];
}

const hit = (id: string, over: Partial<MergeHit> = {}): MergeHit => ({
  id,
  title: `标题-${id}`,
  snippet: `摘录-${id}`,
  ...over,
});
const ids = (...list: string[]): MergeHit[] => list.map((id) => hit(id));

const MANY_FTS = Array.from({ length: 25 }, (_, i) => hit(`f${i + 1}`));
const MANY_SEMANTIC = Array.from({ length: 25 }, (_, i) => hit(i % 3 === 0 ? `f${i + 1}` : `s${i + 1}`, { matchText: `片段-${i + 1}` }));

const MERGE_CASES: Array<{ name: string; fts: MergeHit[]; semantic: MergeHit[]; limit: number }> = [
  { name: "两路都空", fts: [], semantic: [], limit: 10 },
  { name: "只有 FTS", fts: ids("a", "b", "c"), semantic: [], limit: 10 },
  { name: "只有语义（元数据取语义自身）", fts: [], semantic: [hit("x", { matchText: "命中段" }), hit("y")], limit: 10 },
  { name: "两路顺序相同", fts: ids("a", "b", "c"), semantic: ids("a", "b", "c"), limit: 10 },
  { name: "两路顺序相反：a 与 c 同分，按先出现排前", fts: ids("a", "b", "c"), semantic: ids("c", "b", "a"), limit: 10 },
  {
    name: "元数据归 FTS、matchText 取语义",
    fts: [hit("a", { title: "FTS 标题", snippet: "FTS 摘录" })],
    semantic: [hit("a", { title: "语义标题", snippet: "语义摘录", matchText: "语义命中段" })],
    limit: 10,
  },
  {
    name: "两路都带 matchText 时取先到的（FTS）",
    fts: [hit("a", { matchText: "FTS 命中" })],
    semantic: [hit("a", { matchText: "语义命中" })],
    limit: 10,
  },
  // 元数据归 FTS，所以结果里没有 matchText 字段（语义那份是空串，不算数）
  { name: "matchText 为空串不算数", fts: [hit("a")], semantic: [hit("a", { matchText: "" })], limit: 10 },
  { name: "limit 截断", fts: ids("a", "b", "c", "d"), semantic: ids("d", "c"), limit: 2 },
  { name: "limit 为 0 按 1 算", fts: ids("a", "b"), semantic: ids("b"), limit: 0 },
  { name: "limit 为负数按 1 算", fts: ids("a", "b"), semantic: ids("b"), limit: -3 },
  { name: "limit 大于结果数", fts: ids("a"), semantic: ids("b"), limit: 100 },
  {
    name: "同一路内重复 id：分数累加、元数据取后者",
    fts: [hit("a", { title: "第一次" }), hit("b"), hit("a", { title: "第二次" })],
    semantic: [],
    limit: 10,
  },
  {
    name: "审核状态与原因透传",
    fts: [hit("a", { reviewStatus: "needs_review", reviewReasons: ["来源不可信", "内容过短"] })],
    semantic: [hit("b", { reviewStatus: "clear", reviewReasons: [] }), hit("a", { reviewStatus: "clear" })],
    limit: 10,
  },
  { name: "25 对 25 的交叠", fts: MANY_FTS, semantic: MANY_SEMANTIC, limit: 10 },
];

// ---- 索引循环 ----

interface IndexItem {
  id: string;
  title: string;
  content: Spec;
  transcript: Spec | null;
  contentHash: string;
}

type Batch = IndexItem[] | { throws: string };

interface IndexCase extends FlowCase {
  batches: Batch[];
  /** 落库结果：缺省 true；false 表示资料已变；{throws} 表示 IPC 失败 */
  applies?: Record<string, false | { throws: string }>;
  /** 第 n 条落库成功后取消（在进度回调里触发） */
  abortAfterIndexed?: number;
}

const item = (n: number, over: Partial<IndexItem> = {}): IndexItem => ({
  id: `item-${n}`,
  title: `标题${n}`,
  content: [C(40 + n)],
  transcript: null,
  contentHash: `hash-${n}`,
  ...over,
});

const FAIL_500 = EMB_HTTP_ERROR(500, "upstream down");

const INDEX_CASES: IndexCase[] = [
  { name: "未配置 embedding 模型：静默跳过", settings: TWO_TIER, script: [], batches: [[item(1)]] },
  { name: "只有旧版单模型：不算配置了 embedding", settings: LEGACY_ONLY, script: [], batches: [[item(1)]] },
  {
    name: "单批两条目成功",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1), EMB_OK(1, [1, 2, 2], 11)],
    batches: [[item(1), item(2)]],
  },
  {
    name: "多批：第二批取到新条目，第三批空",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1), EMB_OK(1), EMB_OK(1)],
    batches: [[item(1), item(2)], [item(3)]],
  },
  {
    name: "落库返回 false 的条目仍 pending：去重后停止，不死循环",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1)],
    batches: [[item(1)], [item(1)]],
    applies: { "item-1": false },
  },
  {
    name: "没有可嵌入文本的条目被静默跳过（不计成功也不计失败）",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1)],
    batches: [[item(1, { title: "", content: "  ", transcript: null }), item(2)]],
  },
  {
    name: "长正文：多块一次请求，向量按 L2 归一化",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(4, [1, 2, 2], 321)],
    batches: [[item(1, { title: "长文", content: [C(2000)] })]],
  },
  {
    name: "带转写稿的条目",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(2)],
    batches: [[item(1, { title: "访谈", content: [C(500)], transcript: [C(500)] })]],
  },
  {
    name: "嵌入失败一次后继续下一条",
    settings: WITH_EMBEDDING,
    script: [FAIL_500, EMB_OK(1)],
    batches: [[item(1), item(2)]],
  },
  {
    name: "连续 3 次失败即中止本轮",
    settings: WITH_EMBEDDING,
    script: [FAIL_500, FAIL_500, FAIL_500, EMB_OK(1)],
    batches: [[item(1), item(2), item(3), item(4)]],
  },
  {
    name: "成功会重置连续失败计数：失败 2 次、成功、失败 2 次不中止",
    settings: WITH_EMBEDDING,
    script: [FAIL_500, FAIL_500, EMB_OK(1), FAIL_500, FAIL_500],
    batches: [[item(1), item(2), item(3), item(4), item(5)]],
  },
  {
    name: "落库返回 false 不计入连续失败：4 条都不中止",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1), EMB_OK(1), EMB_OK(1), EMB_OK(1)],
    batches: [[item(1), item(2), item(3), item(4)]],
    applies: { "item-1": false, "item-2": false, "item-3": false, "item-4": false },
  },
  {
    name: "落库抛错计入连续失败：3 条后中止",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1), EMB_OK(1), EMB_OK(1), EMB_OK(1)],
    batches: [[item(1), item(2), item(3), item(4)]],
    applies: {
      "item-1": { throws: "database is locked" },
      "item-2": { throws: "database is locked" },
      "item-3": { throws: "database is locked" },
    },
  },
  {
    name: "嵌入响应数量不匹配按条目失败处理",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1), EMB_OK(1)],
    batches: [[item(1, { title: "长", content: [C(1000)] }), item(2)]],
  },
  {
    name: "鉴权失败：错误原因带回结果",
    settings: WITH_EMBEDDING,
    script: [EMB_HTTP_ERROR(401, "Incorrect API key provided"), EMB_HTTP_ERROR(401, "Incorrect API key provided"), EMB_HTTP_ERROR(401, "Incorrect API key provided")],
    batches: [[item(1), item(2), item(3)]],
  },
  { name: "开始前已取消", settings: WITH_EMBEDDING, script: [], batches: [[item(1)]], abort: "before" },
  {
    name: "第 1 条落库后取消：不再处理后面的条目",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1), EMB_OK(1), EMB_OK(1)],
    batches: [[item(1), item(2), item(3)]],
    abortAfterIndexed: 1,
  },
  {
    name: "嵌入请求进行中取消：该条按失败计，随后退出",
    settings: WITH_EMBEDDING,
    script: [{ ...EMB_OK(1), abortDuring: true }, EMB_OK(1)],
    batches: [[item(1), item(2)]],
  },
  {
    name: "listPending 抛错直接向上抛",
    settings: WITH_EMBEDDING,
    script: [],
    batches: [{ throws: "database is locked" }],
  },
  {
    name: "第二次 listPending 抛错：已处理的不回滚，错误向上抛",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1)],
    batches: [[item(1)], { throws: "disk I/O error" }],
  },
];

interface IndexRecords {
  listCalls: Raw[];
  applyCalls: Raw[];
  progress: number[];
}

async function runIndexCase(c: IndexCase, baseline: Raw): Promise<Raw> {
  const records: IndexRecords = { listCalls: [], applyCalls: [], progress: [] };
  const batches = [...c.batches];
  const target = globalThis as unknown as { window: { api: Record<string, unknown> } };
  const outcome = await runFlow(
    c,
    baseline,
    async (signal, fake) => {
      target.window.api = {
        ...target.window.api,
        semantic: {
          listPending: async (params: { model: string; limit: number }) => {
            records.listCalls.push({ model: params.model, limit: params.limit });
            const next = batches.shift();
            if (next === undefined) return [];
            if (!Array.isArray(next)) throw new Error(next.throws);
            return next.map((entry) => ({
              id: entry.id,
              title: entry.title,
              content: render(entry.content),
              transcript: entry.transcript === null ? null : render(entry.transcript),
              contentHash: entry.contentHash,
            }));
          },
          applyEmbeddings: async (input: {
            itemId: string;
            contentHash: string;
            model: string;
            dims: number;
            chunks: Array<{ text: string; vector: number[] }>;
          }) => {
            records.applyCalls.push({
              itemId: input.itemId,
              contentHash: input.contentHash,
              model: input.model,
              dims: input.dims,
              chunks: input.chunks.map((chunk) => ({ text: compactText(chunk.text), vector: chunk.vector })),
            });
            const behavior = c.applies?.[input.itemId];
            if (behavior === false) return false;
            if (behavior) throw new Error(behavior.throws);
            return true;
          },
        },
      };
      return runSemanticIndexing((indexed) => {
        records.progress.push(indexed);
        if (c.abortAfterIndexed !== undefined && indexed === c.abortAfterIndexed) fake.controller.abort();
      }, signal);
    },
    { abortMessage: false },
  );
  return { ...outcome, ...records };
}

// ---- 查询向量 ----

const EMBED_CONFIG: AIConfig = {
  id: "embed",
  provider: "custom",
  apiProtocol: "openai",
  apiUrl: "https://embed.example.com/v1",
  model: "model-embed",
  apiKey: "sk-embed",
};

const QV_CONFIGS: Record<string, AIConfig> = {
  A: EMBED_CONFIG,
  B: { ...EMBED_CONFIG, apiKey: "sk-other" },
  C: { ...EMBED_CONFIG, model: "model-embed-large" },
  D: { ...EMBED_CONFIG, apiUrl: "https://embed2.example.com/v1" },
};

type QvStep =
  | { op: "query"; id: string; config?: string; query: string; abort?: "before" | "during" }
  | { op: "concurrent"; id: string; config?: string; queries: string[] }
  | { op: "advance"; ms: number }
  | { op: "reset" };

interface QvCase {
  name: string;
  script: ScriptStep[];
  steps: QvStep[];
  /** 并发发出的请求顺序取决于摘要计算的完成顺序：记录时按请求体排序，Go 侧同样处理 */
  unorderedRequests?: boolean;
}

const q = (id: string, query: string, config = "A", abort?: "before" | "during"): QvStep => ({ op: "query", id, config, query, abort });

const MANY_QUERIES = Array.from({ length: 101 }, (_, i) => `查询-${i + 1}`);

const QV_CASES: QvCase[] = [
  { name: "同一查询第二次走缓存", script: [EMB_OK(1, [3, 4], 5)], steps: [q("1", "你好"), q("2", "你好")] },
  { name: "不同查询各自请求", script: [EMB_OK(1, [3, 4]), EMB_OK(1, [0, 2])], steps: [q("1", "甲"), q("2", "乙"), q("3", "甲")] },
  {
    name: "配置的任一字段不同都不共用缓存",
    script: [EMB_OK(1), EMB_OK(1), EMB_OK(1), EMB_OK(1)],
    steps: [q("a", "同一问题", "A"), q("b", "同一问题", "B"), q("c", "同一问题", "C"), q("d", "同一问题", "D"), q("a2", "同一问题", "A")],
  },
  {
    name: "5 分钟过期：299999 毫秒仍命中，300000 毫秒起过期",
    script: [EMB_OK(1), EMB_OK(1), EMB_OK(1)],
    steps: [
      q("t0", "问题"),
      { op: "advance", ms: 299_999 },
      q("t299999", "问题"),
      { op: "advance", ms: 1 },
      q("t300000", "问题"),
      { op: "advance", ms: 300_000 },
      q("t600000", "问题"),
    ],
  },
  {
    name: "命中不续期：只刷新 LRU 位置，过期时间沿用首次写入",
    script: [EMB_OK(1), EMB_OK(1)],
    steps: [q("t0", "问题"), { op: "advance", ms: 200_000 }, q("t200000", "问题"), { op: "advance", ms: 100_000 }, q("t300000", "问题")],
  },
  {
    name: "容量 100 条，LRU 淘汰：命中会刷新位置",
    // 101 次填充 + 「查询-1」「查询-3」两次重新请求
    script: Array.from({ length: 103 }, () => EMB_OK(1)),
    steps: [
      ...MANY_QUERIES.map((query, i) => q(`fill-${i + 1}`, query)),
      // 写入第 101 条时淘汰了最久未用的「查询-1」
      q("hit-2", "查询-2"), // 命中并刷新：「查询-2」变最新
      q("miss-1", "查询-1"), // 已被淘汰：重新请求，写入后淘汰最久未用的「查询-3」
      q("miss-3", "查询-3"),
      q("hit-2-again", "查询-2"),
    ],
  },
  {
    name: "并发相同查询只发一次请求",
    script: [EMB_OK(1)],
    steps: [{ op: "concurrent", id: "c", queries: ["同一个", "同一个", "同一个", "同一个"] }],
  },
  {
    // 两个请求的发出顺序取决于摘要计算的完成顺序，所以两份响应用同一个向量，请求按请求体排序后再记
    name: "并发不同查询各发一次",
    script: [EMB_OK(1), EMB_OK(1)],
    steps: [{ op: "concurrent", id: "c", queries: ["甲", "乙"] }],
    unorderedRequests: true,
  },
  { name: "reset 清空缓存", script: [EMB_OK(1), EMB_OK(1)], steps: [q("1", "你好"), { op: "reset" }, q("2", "你好")] },
  {
    name: "失败不入缓存，重试会再次请求",
    script: [EMB_HTTP_ERROR(500, "upstream down"), EMB_OK(1)],
    steps: [q("fail", "你好"), q("retry", "你好"), q("cached", "你好")],
  },
  { name: "嵌入响应畸形", script: [{ body: JSON.stringify({ data: [{ index: 0 }] }) }], steps: [q("bad", "你好")] },
  { name: "调用前已取消：不发请求", script: [], steps: [q("pre", "你好", "A", "before")] },
  {
    name: "请求进行中取消：不缓存，下次重新请求",
    script: [{ ...EMB_OK(1), abortDuring: true }, EMB_OK(1)],
    steps: [q("during", "你好", "A", "during"), q("again", "你好")],
  },
  {
    name: "取消后其他查询不受影响",
    script: [{ ...EMB_OK(1), abortDuring: true }, EMB_OK(1), EMB_OK(1)],
    steps: [q("during", "甲", "A", "during"), q("other", "乙"), q("again", "甲")],
  },
];

async function runQvCase(c: QvCase): Promise<Raw> {
  resetQueryVectors();
  let clock = 1_700_000_000_000;
  vi.spyOn(Date, "now").mockImplementation(() => clock);
  const fake = installFakeTransport(c.script);
  const stepResults: Raw[] = [];
  const requestCounts: number[] = [];

  const settle = async (config: AIConfig, query: string, signal?: AbortSignal): Promise<Raw> => {
    try {
      return { ok: true, vector: await queryVector(config, query, signal) };
    } catch (error) {
      return { ok: false, error: errorInfo(error, { abortMessage: false }) };
    }
  };

  for (const step of c.steps) {
    if (step.op === "advance") {
      clock += step.ms;
      continue;
    }
    if (step.op === "reset") {
      resetQueryVectors();
      continue;
    }
    const config = QV_CONFIGS[step.config ?? "A"];
    if (step.op === "concurrent") {
      const results = await Promise.all(step.queries.map((query) => settle(config, query)));
      stepResults.push({ id: step.id, results });
      requestCounts.push(fake.requests.length);
      continue;
    }
    fake.controller = new AbortController();
    if (step.abort === "before") fake.controller.abort();
    const result = await settle(config, step.query, step.abort ? fake.controller.signal : undefined);
    stepResults.push({ id: step.id, ...result });
    requestCounts.push(fake.requests.length);
  }
  // recordUsage 是 fire-and-forget 的 Promise
  await Promise.resolve();
  const requests = fake.requests.map((request) => ({ url: request.url, headers: request.headers, body: request.body, timeoutMs: request.timeoutMs }));
  // 按码元序而不是 localeCompare：后者随 ICU 与系统区域而异
  if (c.unorderedRequests) requests.sort((left, right) => (String(left.body) < String(right.body) ? -1 : String(left.body) > String(right.body) ? 1 : 0));
  return {
    steps: stepResults,
    requestCounts,
    requests,
    unusedScript: c.script.length - fake.requests.length,
    usage: fake.usage,
  };
}

// ---- 混合检索 ----

interface FtsEntry {
  id: string;
  title?: string;
  snippet?: string | null;
  reviewStatus?: "clear" | "needs_review";
  reviewReasons?: string[];
}

interface SemanticHitInput {
  itemId: string;
  title: string;
  snippet: string;
  score: number;
  reviewStatus?: "clear" | "needs_review";
  reviewReasons?: string[];
}

interface HybridCase extends FlowCase {
  query: string;
  limit: number;
  fts: { entries: FtsEntry[] } | { throws: string };
  semantic: { hits: SemanticHitInput[] } | { throws: string } | { hang: true } | { abortThenHang: true };
}

const fts = (...entries: FtsEntry[]): HybridCase["fts"] => ({ entries });
const sem = (...hits: SemanticHitInput[]): HybridCase["semantic"] => ({ hits });
const semHit = (itemId: string, score: number, over: Partial<SemanticHitInput> = {}): SemanticHitInput => ({
  itemId,
  title: `语义标题-${itemId}`,
  snippet: `命中段-${itemId}`,
  score,
  ...over,
});

const HYBRID_CASES: HybridCase[] = [
  {
    name: "未配置 embedding：只返回 FTS，不做语义调用",
    settings: TWO_TIER,
    script: [],
    query: "知识库怎么用",
    limit: 5,
    fts: fts({ id: "a", title: "甲", snippet: "甲的摘录" }, { id: "b", title: "乙", snippet: "乙的摘录" }),
    semantic: sem(semHit("never", 0.9)),
  },
  {
    name: "FTS 为空、语义命中：用语义结果（matchText 等于命中段）",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1)],
    query: "知识库怎么用",
    limit: 5,
    fts: fts(),
    semantic: sem(semHit("s1", 0.8), semHit("s2", 0.6)),
  },
  {
    name: "两路融合：FTS 元数据优先，语义独有命中追加",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1, [1, 2, 2])],
    query: "知识库怎么用",
    limit: 5,
    fts: fts({ id: "a", title: "甲", snippet: "甲的摘录" }, { id: "b", title: "乙", snippet: "乙的摘录" }, { id: "c", title: "丙", snippet: null }),
    semantic: sem(semHit("c", 0.9), semHit("d", 0.7), semHit("a", 0.5)),
  },
  {
    name: "语义分数低于 0.25 的被过滤，恰为 0.25 的保留",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1)],
    query: "q",
    limit: 5,
    fts: fts({ id: "a", title: "甲", snippet: "s" }),
    semantic: sem(semHit("keep-edge", 0.25), semHit("drop", 0.2499), semHit("keep", 0.9), semHit("drop-neg", -0.5)),
  },
  {
    name: "语义全被过滤：返回 FTS 原样（不经融合与截断）",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1)],
    query: "q",
    limit: 1,
    fts: fts({ id: "a", title: "甲", snippet: "s" }, { id: "b", title: "乙", snippet: "t" }),
    semantic: sem(semHit("x", 0.1), semHit("y", 0.2)),
  },
  {
    name: "limit 同时作用于两路与融合结果",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1)],
    query: "q",
    limit: 2,
    fts: fts({ id: "a", title: "甲", snippet: "1" }, { id: "b", title: "乙", snippet: "2" }, { id: "c", title: "丙", snippet: "3" }),
    semantic: sem(semHit("d", 0.9), semHit("c", 0.8), semHit("e", 0.7)),
  },
  {
    name: "标题缺省补「无标题」，摘录缺省补空串，审核信息透传",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1)],
    query: "q",
    limit: 5,
    fts: fts(
      { id: "a", title: "", snippet: null, reviewStatus: "needs_review", reviewReasons: ["来源不可信"] },
      { id: "b", snippet: "无 title 字段" },
    ),
    semantic: sem(
      semHit("c", 0.9, { title: "", reviewStatus: "clear", reviewReasons: [] }),
      semHit("a", 0.8, { reviewStatus: "clear" }),
    ),
  },
  {
    name: "语义检索抛错：告警并退回 FTS",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1)],
    query: "q",
    limit: 5,
    fts: fts({ id: "a", title: "甲", snippet: "s" }),
    semantic: { throws: "semantic index is rebuilding" },
  },
  {
    name: "查询向量失败（HTTP 500）：告警并退回 FTS，不调用语义检索",
    settings: WITH_EMBEDDING,
    script: [EMB_HTTP_ERROR(500, "upstream down")],
    query: "q",
    limit: 5,
    fts: fts({ id: "a", title: "甲", snippet: "s" }),
    semantic: sem(semHit("never", 0.9)),
  },
  {
    name: "语义检索挂起超过 3 秒：告警并退回 FTS",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1)],
    query: "q",
    limit: 5,
    fts: fts({ id: "a", title: "甲", snippet: "s" }),
    semantic: { hang: true },
  },
  {
    name: "FTS 抛错：整体失败（此时未配置 embedding，避免后台任务干扰记录）",
    settings: TWO_TIER,
    script: [],
    query: "q",
    limit: 5,
    fts: { throws: "fts index corrupted" },
    semantic: sem(),
  },
  {
    name: "调用前已取消",
    settings: WITH_EMBEDDING,
    script: [],
    query: "q",
    limit: 5,
    fts: fts({ id: "a", title: "甲", snippet: "s" }),
    semantic: sem(semHit("never", 0.9)),
    abort: "before",
  },
  {
    name: "语义检索进行中取消：整体抛取消",
    settings: WITH_EMBEDDING,
    script: [EMB_OK(1)],
    query: "q",
    limit: 5,
    fts: fts({ id: "a", title: "甲", snippet: "s" }),
    semantic: { abortThenHang: true },
  },
];

interface HybridRecords {
  ftsCalls: Raw[];
  semanticCalls: Raw[];
  onWarnings: string[];
}

async function runHybridCase(c: HybridCase, baseline: Raw): Promise<Raw> {
  const records: HybridRecords = { ftsCalls: [], semanticCalls: [], onWarnings: [] };
  const target = globalThis as unknown as { window: { api: Record<string, unknown> } };
  resetQueryVectors();
  // 3 秒的语义超时在测试里缩成 150 毫秒；其余定时器原样
  const realSetTimeout = globalThis.setTimeout;
  vi.spyOn(globalThis, "setTimeout").mockImplementation(((handler: TimerHandler, ms?: number, ...args: unknown[]) =>
    realSetTimeout(handler as () => void, ms === 3000 ? 150 : ms, ...args)) as typeof setTimeout);

  const outcome = await runFlow(
    c,
    baseline,
    async (signal, fake) => {
      target.window.api = {
        ...target.window.api,
        knowledge: {
          list: async (params: Raw) => {
            records.ftsCalls.push(params);
            if ("throws" in c.fts) throw new Error(c.fts.throws);
            return { entries: c.fts.entries, total: c.fts.entries.length };
          },
        },
        semantic: {
          search: async (params: { model: string; vector: number[]; limit: number }) => {
            records.semanticCalls.push({ model: params.model, vector: params.vector, limit: params.limit });
            if ("throws" in c.semantic) throw new Error(c.semantic.throws);
            if ("hang" in c.semantic) return new Promise(() => undefined);
            if ("abortThenHang" in c.semantic) {
              fake.controller.abort();
              return new Promise(() => undefined);
            }
            return c.semantic.hits;
          },
        },
      };
      return hybridSearchItems(c.query, c.limit, { signal, onWarning: (message) => records.onWarnings.push(message) });
    },
    { abortMessage: false },
  );
  return { ...outcome, ...records };
}

// ---- 用例入口 ----

describe("aiflow golden：语义索引与检索", () => {
  it("分块器与 RRF 融合与已提交的 golden 一致", () => {
    const chunks = CHUNK_CASES.map(chunkCaseOutput);
    const merge = MERGE_CASES.map((c) => ({
      name: c.name,
      fts: c.fts,
      semantic: c.semantic,
      limit: c.limit,
      result: mergeHybridResults(c.fts, c.semantic, c.limit),
    }));
    writeOrCompareGolden("semantic", {
      constants: { chunkSize: SEMANTIC_CHUNK_SIZE, chunkOverlap: SEMANTIC_CHUNK_OVERLAP, maxChunks: SEMANTIC_MAX_CHUNKS },
      chunks,
      merge,
    });
  });

  it("runSemanticIndexing 与已提交的 golden 一致", async () => {
    const baseline = { ...useSettingsStore.getState() };
    const cases: Raw[] = [];
    try {
      for (const c of INDEX_CASES) {
        const inputs = c.batches.flatMap((batch) =>
          Array.isArray(batch) ? batch.map((entry) => ({ id: entry.id, content: digest(render(entry.content)), transcript: entry.transcript === null ? null : digest(render(entry.transcript)) })) : [],
        );
        const expect = await runIndexCase(c, baseline as unknown as Raw);
        cases.push({
          name: c.name,
          settings: c.settings,
          batches: c.batches,
          applies: c.applies ?? {},
          abort: c.abort ?? null,
          abortAfterIndexed: c.abortAfterIndexed ?? null,
          script: c.script,
          inputs,
          expect,
        });
      }
    } finally {
      useSettingsStore.setState(baseline);
    }
    writeOrCompareGolden("semantic-index", { cases });
  });

  it("queryVector 缓存与过期与已提交的 golden 一致", async () => {
    const baseline = { ...useSettingsStore.getState() };
    const cases: Raw[] = [];
    try {
      for (const c of QV_CASES) {
        const expect = await runQvCase(c);
        cases.push({ name: c.name, script: c.script, steps: c.steps, unorderedRequests: c.unorderedRequests ?? false, expect });
        vi.restoreAllMocks();
      }
    } finally {
      useSettingsStore.setState(baseline);
    }
    writeOrCompareGolden("query-vectors", { configs: QV_CONFIGS, cases });
  });

  it("hybridSearchItems 与已提交的 golden 一致", async () => {
    const baseline = { ...useSettingsStore.getState() };
    const cases: Raw[] = [];
    try {
      for (const c of HYBRID_CASES) {
        const expect = await runHybridCase(c, baseline as unknown as Raw);
        cases.push({
          name: c.name,
          settings: c.settings,
          query: c.query,
          limit: c.limit,
          fts: c.fts,
          semantic: c.semantic,
          abort: c.abort ?? null,
          script: c.script,
          expect,
        });
        vi.restoreAllMocks();
      }
    } finally {
      useSettingsStore.setState(baseline);
    }
    writeOrCompareGolden("hybrid", { cases });
  });
});
