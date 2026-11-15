/**
 * aiflow 后台任务执行器的 golden：渲染层的 hooks/useBackgroundJobClient（连同它调用的 wiki.store 的
 * runBackgroundWikiCompile 与 semantic.store 的 runIndexing）。
 *
 * 真实的 hook 挂在 jsdom 里，假的 window.api.backgroundJob（syncRenderer / renew / complete / fail）、
 * window.api.on/off、window.api.log.appError，两个耗时的下游（compileReadyWiki、runSemanticIndexing）换成脚本化的假实现——
 * 它们本身由 wiki-driver、semantic 两份 golden 覆盖，这里只看任务层怎么领取、执行、续租、回报。
 * 虚拟时间由假定时器驱动（续租间隔 30 秒）。把每一步操作之后发生的调用序列写成 golden；
 * Go 侧重放见 apps/desktop-go/aiflow/background_golden_test.go。
 *
 * 步骤：mount（挂载 hook：同步一次、注册领取回调）/ settings（换成一份完整设置，必然改变 hook 的某个依赖）/
 * available（调度器推来一个任务）/ advance（走 n 个续租间隔）/ release（放行一个卡住的下游调用）/
 * busy（模拟界面上同类任务正在手动执行）/ manualIndexing（界面上手动触发一轮语义索引，记录它得到的回执）/ unmount。
 * 下游调用的行为按次序排队，block 表示卡住直到 release（manualIndexing 不能用卡住的行为）；用完退回「什么都没做」的空结果。
 *
 * 约束（用例构造时自检）：
 *   - 每个 settings 步骤恰好触发一次同步（Go 侧由设置层在对应输入变化时调用 SyncSettings）；
 *   - 同时在跑的任务必须在同一虚拟时刻开始（续租相位一致），Go 侧的假 ticker 一次性触发全部活跃的 ticker；
 *   - unmount 时不能有任务在跑（hook 卸载后 TS 的在途任务继续跑完，Go 的 Close 会取消它们，两者语义不同，不对拍）。
 */
import { act, renderHook } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, describe, it, vi } from "vitest";
import { IPC_CHANNELS } from "@guizhi/shared/constants/ipc-channels";
import { useSettingsStore } from "../../../src/renderer/stores/settings.store";
import { mergeSettingsState } from "../../../src/renderer/stores/settings/settings-persistence";
import { useBackgroundJobClient } from "../../../src/renderer/hooks/useBackgroundJobClient";
import { useWikiStore } from "../../../src/renderer/stores/wiki.store";
import { useSemanticStore } from "../../../src/renderer/stores/semantic.store";
import { writeOrCompareGolden } from "./aiflow-golden-kit";
import { TWO_TIER, WITH_EMBEDDING, embeddingModel, type Raw } from "./aiflow-golden-flow-kit";

// ---- 下游的脚本化假实现 ----

const harness = vi.hoisted(() => ({
  compileReadyWiki: undefined as undefined | (() => Promise<unknown>),
  runSemanticIndexing: undefined as undefined | (() => Promise<unknown>),
}));

vi.mock("../../../src/renderer/services/knowledge-ai/wiki-v2", async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  compileReadyWiki: () => harness.compileReadyWiki!(),
}));
vi.mock("../../../src/renderer/services/knowledge-ai/semantic-index", async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  runSemanticIndexing: () => harness.runSemanticIndexing!(),
}));

// ---- 用例形状 ----

type Runner = "wiki" | "semantic";

interface Job {
  id: string;
  kind: string;
}

type Behavior =
  | { result: Raw; block?: boolean }
  | { throws: string; block?: boolean };

type CompleteOutcome = "ok" | "lost" | { rejects: string };

type Step =
  | { op: "mount" }
  | { op: "settings"; settings: Raw }
  | { op: "available"; job: Job }
  | { op: "advance"; ticks: number }
  | { op: "release"; runner: Runner }
  | { op: "busy"; wiki?: boolean; semantic?: boolean }
  | { op: "manualIndexing" }
  | { op: "unmount" };

interface Case {
  name: string;
  settings: Raw;
  wiki?: Behavior[];
  semantic?: Behavior[];
  complete?: CompleteOutcome[];
  steps: Step[];
}

type Call =
  | { call: "syncRenderer"; wikiEnabled: boolean; semanticEnabled: boolean }
  | { call: "renew"; id: string }
  | { call: "complete"; id: string }
  | { call: "fail"; id: string; error: string; pause?: unknown }
  | { call: "appError"; scope: string; action: string; message: string }
  | { call: "compileReadyWiki" }
  | { call: "runSemanticIndexing" };

// ---- 夹具 ----

const HEARTBEAT_MS = 30_000;

const spec = (ai: Raw, bg = true, wiki = true): Raw => ({ ...ai, backgroundTasksEnabled: bg, wikiCompileEnabled: wiki });

const EMBED_NO_KEY: Raw = {
  aiModels: [...(TWO_TIER.aiModels as Raw[]), embeddingModel("embed", { apiKey: "" })],
  modelRouteDefaults: { mainText: "main", fastText: "fast", embedding: "embed" },
};
const EMBED_PROVIDER_OFF: Raw = {
  ...WITH_EMBEDDING,
  aiProviders: [
    { id: "p1", name: "停用的", provider: "custom", apiProtocol: "openai", apiKey: "sk-embed", apiUrl: "https://embed.example.com/v1", enabled: false },
  ],
};
const EMBED_NO_ROUTE: Raw = {
  aiModels: WITH_EMBEDDING.aiModels,
  modelRouteDefaults: { mainText: "main", fastText: "fast" },
};

const ALL_ON = spec(WITH_EMBEDDING);

const wikiJob = (id = "job-w"): Job => ({ id, kind: "wiki-compile" });
const semJob = (id = "job-s"): Job => ({ id, kind: "semantic-index" });

const mount: Step = { op: "mount" };
const avail = (job: Job): Step => ({ op: "available", job });
const tick = (ticks = 1): Step => ({ op: "advance", ticks });
const release = (runner: Runner): Step => ({ op: "release", runner });
const settings = (value: Raw): Step => ({ op: "settings", settings: value });

const wikiResult = (over: Raw = {}): Behavior => ({ result: { compiled: 3, pending: 3, skipped: 0, failures: [], ...over } });
const wikiThrows = (message: string): Behavior => ({ throws: message });
const semResult = (indexed: number, failed: number, over: Raw = {}): Behavior => ({ result: { indexed, failed, ...over } });
const semThrows = (message: string): Behavior => ({ throws: message });
const blocked = (behavior: Behavior): Behavior => ({ ...behavior, block: true });

const CASES: Case[] = [
  // ---- 同步：开关 = 总开关 && 各自的前提 ----
  { name: "同步：全部开启，Wiki 与语义索引都启用", settings: ALL_ON, steps: [mount] },
  { name: "同步：总开关关闭时两者都关", settings: spec(WITH_EMBEDDING, false, true), steps: [mount] },
  { name: "同步：Wiki 编译关闭，语义索引仍启用", settings: spec(WITH_EMBEDDING, true, false), steps: [mount] },
  { name: "同步：没有配置 embedding 时语义索引关闭", settings: spec(TWO_TIER), steps: [mount] },
  { name: "同步：embedding 模型缺密钥视为未配置", settings: spec(EMBED_NO_KEY), steps: [mount] },
  { name: "同步：embedding 路由没有默认值但模型本身已配置", settings: spec(EMBED_NO_ROUTE), steps: [mount] },
  { name: "同步：embedding 模型所属供应商被停用", settings: spec(EMBED_PROVIDER_OFF), steps: [mount] },
  { name: "同步：总开关与 Wiki 开关都关", settings: spec(TWO_TIER, false, false), steps: [mount] },
  {
    name: "同步：先没有 embedding，后来配置了，语义索引随之启用",
    settings: spec(TWO_TIER),
    steps: [mount, settings(spec(WITH_EMBEDDING))],
  },
  {
    name: "同步：embedding 配置被换成缺密钥的模型，语义索引随之关闭",
    settings: spec(WITH_EMBEDDING),
    steps: [mount, settings(spec(EMBED_NO_KEY))],
  },
  {
    name: "同步：运行中翻转总开关，每次变化各同步一次",
    settings: spec(WITH_EMBEDDING, false, true),
    steps: [mount, settings(spec(WITH_EMBEDDING, true, true)), settings(spec(WITH_EMBEDDING, false, true))],
  },
  {
    name: "同步：只翻转 Wiki 开关",
    settings: spec(WITH_EMBEDDING, true, true),
    steps: [mount, settings(spec(WITH_EMBEDDING, true, false)), settings(spec(WITH_EMBEDDING, true, true))],
  },

  // ---- Wiki 编译任务 ----
  {
    name: "Wiki：一轮没有失败，执行后回报完成",
    settings: ALL_ON,
    wiki: [wikiResult()],
    steps: [mount, avail(wikiJob())],
  },
  {
    name: "Wiki：一轮里没有可编译的条目也回报完成",
    settings: ALL_ON,
    wiki: [wikiResult({ compiled: 0, pending: 0 })],
    steps: [mount, avail(wikiJob())],
  },
  {
    name: "Wiki：一轮里有失败条目，逐条留痕后仍回报完成",
    settings: ALL_ON,
    wiki: [
      wikiResult({
        compiled: 0,
        pending: 10,
        skipped: 10,
        failures: [
          { title: "全文编译", reason: "已完成 4/10 块；请在编译任务中继续" },
          { title: "第二条：带全角冒号", reason: "原因里也有：冒号" },
        ],
      }),
    ],
    steps: [mount, avail(wikiJob())],
  },
  {
    name: "Wiki：失败条目的标题与原因为空串",
    settings: ALL_ON,
    wiki: [wikiResult({ failures: [{ title: "", reason: "" }] })],
    steps: [mount, avail(wikiJob())],
  },
  {
    name: "Wiki：编译抛错，留痕并回报失败",
    settings: ALL_ON,
    wiki: [wikiThrows("AI_NOT_CONFIGURED")],
    steps: [mount, avail(wikiJob())],
  },
  {
    name: "Wiki：编译抛错的消息为空串",
    settings: ALL_ON,
    wiki: [wikiThrows("")],
    steps: [mount, avail(wikiJob())],
  },
  {
    name: "Wiki：编译抛错的消息含中文与换行",
    settings: ALL_ON,
    wiki: [wikiThrows("请求失败：\n第二行 HTTP 502")],
    steps: [mount, avail(wikiJob())],
  },
  {
    name: "Wiki：界面上正在手动编译，后台这一轮直接跳过并回报完成",
    settings: ALL_ON,
    wiki: [wikiResult()],
    steps: [mount, { op: "busy", wiki: true }, avail(wikiJob())],
  },
  {
    name: "Wiki：手动编译结束后下一轮照常执行",
    settings: ALL_ON,
    wiki: [wikiResult()],
    steps: [mount, { op: "busy", wiki: true }, avail(wikiJob("job-1")), { op: "busy", wiki: false }, avail(wikiJob("job-2"))],
  },
  {
    name: "Wiki：语义索引正在手动执行不影响 Wiki 任务",
    settings: ALL_ON,
    wiki: [wikiResult()],
    steps: [mount, { op: "busy", semantic: true }, avail(wikiJob())],
  },
  {
    name: "Wiki：失败之后下一个任务照常执行",
    settings: ALL_ON,
    wiki: [wikiThrows("boom"), wikiResult()],
    steps: [mount, avail(wikiJob("job-1")), avail(wikiJob("job-2"))],
  },
  {
    name: "Wiki：任务 id 为空串照样执行",
    settings: ALL_ON,
    wiki: [wikiResult()],
    steps: [mount, avail(wikiJob(""))],
  },

  // ---- 语义索引任务 ----
  {
    name: "语义：索引了若干条，回报完成",
    settings: ALL_ON,
    semantic: [semResult(7, 0)],
    steps: [mount, avail(semJob())],
  },
  {
    name: "语义：没有待索引的条目，回报完成",
    settings: ALL_ON,
    semantic: [semResult(0, 0)],
    steps: [mount, avail(semJob())],
  },
  {
    name: "语义：embedding 未配置（跳过），回报完成",
    settings: ALL_ON,
    semantic: [{ result: { skipped: true, indexed: 0, failed: 0 } }],
    steps: [mount, avail(semJob())],
  },
  {
    name: "语义：部分失败，用最后一次失败的原因回报失败",
    settings: ALL_ON,
    semantic: [semResult(3, 2, { lastError: "嵌入接口返回 429" })],
    steps: [mount, avail(semJob())],
  },
  {
    name: "语义：部分失败且没有原因，用失败条数回报",
    settings: ALL_ON,
    semantic: [semResult(3, 2)],
    steps: [mount, avail(semJob())],
  },
  {
    name: "语义：全部失败，原因为空串时用失败条数回报",
    settings: ALL_ON,
    semantic: [semResult(0, 4, { lastError: "" })],
    steps: [mount, avail(semJob())],
  },
  {
    name: "语义：全部失败，带原因",
    settings: ALL_ON,
    semantic: [semResult(0, 4, { lastError: "资料已改或已删，向量未保存" })],
    steps: [mount, avail(semJob())],
  },
  {
    name: "语义：索引抛错，用错误消息回报失败",
    settings: ALL_ON,
    semantic: [semThrows("ListPending 失败")],
    steps: [mount, avail(semJob())],
  },
  {
    name: "语义：索引抛错的消息为空串，失败条数记 0",
    settings: ALL_ON,
    semantic: [semThrows("")],
    steps: [mount, avail(semJob())],
  },
  {
    name: "语义：界面上正在手动索引，后台这一轮直接跳过并回报完成",
    settings: ALL_ON,
    semantic: [semResult(5, 0)],
    steps: [mount, { op: "busy", semantic: true }, avail(semJob())],
  },
  {
    name: "语义：手动索引结束后下一轮照常执行",
    settings: ALL_ON,
    semantic: [semResult(5, 0)],
    steps: [mount, { op: "busy", semantic: true }, avail(semJob("job-1")), { op: "busy", semantic: false }, avail(semJob("job-2"))],
  },
  {
    name: "语义：Wiki 正在手动编译不影响语义任务",
    settings: ALL_ON,
    semantic: [semResult(2, 0)],
    steps: [mount, { op: "busy", wiki: true }, avail(semJob())],
  },
  {
    name: "语义：一轮之后接着一轮，各自独立回报",
    settings: ALL_ON,
    semantic: [semResult(2, 1, { lastError: "第一轮" }), semResult(4, 0)],
    steps: [mount, avail(semJob("job-1")), avail(semJob("job-2"))],
  },

  // ---- 界面上手动触发语义索引：同一套执行与归类，回执交还给界面 ----
  { name: "手动语义索引：索引了若干条，回执为 done", settings: ALL_ON, semantic: [semResult(7, 0)], steps: [mount, { op: "manualIndexing" }] },
  { name: "手动语义索引：没有待索引的条目，回执为 nothing", settings: ALL_ON, semantic: [semResult(0, 0)], steps: [mount, { op: "manualIndexing" }] },
  {
    name: "手动语义索引：embedding 未配置（跳过），回执为 not-configured",
    settings: ALL_ON,
    semantic: [{ result: { skipped: true, indexed: 0, failed: 0 } }],
    steps: [mount, { op: "manualIndexing" }],
  },
  {
    name: "手动语义索引：跳过时即使带着条数与原因，回执仍是 not-configured 且不带数字",
    settings: ALL_ON,
    semantic: [{ result: { skipped: true, indexed: 2, failed: 1, lastError: "x" } }],
    steps: [mount, { op: "manualIndexing" }],
  },
  {
    name: "手动语义索引：部分失败带原因，回执为 partial",
    settings: ALL_ON,
    semantic: [semResult(3, 2, { lastError: "嵌入接口返回 429" })],
    steps: [mount, { op: "manualIndexing" }],
  },
  { name: "手动语义索引：部分失败没有原因，回执为 partial 且不带消息", settings: ALL_ON, semantic: [semResult(3, 2)], steps: [mount, { op: "manualIndexing" }] },
  {
    name: "手动语义索引：全部失败带原因，回执为 failed",
    settings: ALL_ON,
    semantic: [semResult(0, 4, { lastError: "资料已改或已删，向量未保存" })],
    steps: [mount, { op: "manualIndexing" }],
  },
  {
    name: "手动语义索引：全部失败原因为空串，回执为 failed 且不带消息",
    settings: ALL_ON,
    semantic: [semResult(0, 4, { lastError: "" })],
    steps: [mount, { op: "manualIndexing" }],
  },
  {
    name: "手动语义索引：执行抛错，回执为 failed 并带错误消息",
    settings: ALL_ON,
    semantic: [semThrows("ListPending 失败")],
    steps: [mount, { op: "manualIndexing" }],
  },
  {
    name: "手动语义索引：执行抛错的消息为空串，回执为 failed 且不带消息",
    settings: ALL_ON,
    semantic: [semThrows("")],
    steps: [mount, { op: "manualIndexing" }],
  },
  {
    name: "手动语义索引：已有索引在跑时直接返回没有回执，也不执行",
    settings: ALL_ON,
    semantic: [semResult(5, 0)],
    steps: [mount, { op: "busy", semantic: true }, { op: "manualIndexing" }],
  },
  {
    name: "手动语义索引：抛错之后守卫复位，再手动一次照常执行",
    settings: ALL_ON,
    semantic: [semThrows("boom"), semResult(2, 0)],
    steps: [mount, { op: "manualIndexing" }, { op: "manualIndexing" }],
  },
  // 各档位的边界：恰好 1 条已索引、恰好 1 条失败
  { name: "手动语义索引：恰好索引 1 条，回执为 done", settings: ALL_ON, semantic: [semResult(1, 0)], steps: [mount, { op: "manualIndexing" }] },
  {
    name: "手动语义索引：已索引 1 条且失败 1 条，回执为 partial",
    settings: ALL_ON,
    semantic: [semResult(1, 1, { lastError: "x" })],
    steps: [mount, { op: "manualIndexing" }],
  },
  {
    name: "手动语义索引：没有索引成功且恰好失败 1 条，回执为 failed",
    settings: ALL_ON,
    semantic: [semResult(0, 1, { lastError: "y" })],
    steps: [mount, { op: "manualIndexing" }],
  },
  {
    name: "手动语义索引：手动之后后台任务照常执行",
    settings: ALL_ON,
    semantic: [semResult(2, 0), semResult(4, 1, { lastError: "后台" })],
    steps: [mount, { op: "manualIndexing" }, avail(semJob())],
  },

  // ---- 不是执行器的任务 ----
  { name: "忽略：备份任务", settings: ALL_ON, steps: [mount, avail({ id: "job-b", kind: "backup" })] },
  { name: "忽略：平台发现任务", settings: ALL_ON, steps: [mount, avail({ id: "job-p", kind: "platform-discovery" })] },
  { name: "忽略：种类为空串", settings: ALL_ON, steps: [mount, avail({ id: "job-e", kind: "" })] },
  { name: "忽略：种类大小写不同", settings: ALL_ON, steps: [mount, avail({ id: "job-c", kind: "WIKI-COMPILE" })] },
  {
    name: "忽略：被忽略的任务不影响随后的有效任务",
    settings: ALL_ON,
    wiki: [wikiResult()],
    steps: [mount, avail({ id: "job-b", kind: "backup" }), avail(wikiJob())],
  },

  // ---- 续租 ----
  {
    name: "续租：任务运行 95 秒，每 30 秒续租一次，结束后回报完成",
    settings: ALL_ON,
    wiki: [blocked(wikiResult())],
    steps: [mount, avail(wikiJob()), tick(3), release("wiki")],
  },
  {
    name: "续租：任务运行不足一个间隔不续租",
    settings: ALL_ON,
    semantic: [blocked(semResult(1, 0))],
    steps: [mount, avail(semJob()), release("semantic")],
  },
  {
    name: "续租：完成之后不再续租",
    settings: ALL_ON,
    wiki: [blocked(wikiResult())],
    steps: [mount, avail(wikiJob()), tick(1), release("wiki"), tick(3)],
  },
  {
    name: "续租：失败之后不再续租",
    settings: ALL_ON,
    semantic: [blocked(semThrows("中途失败"))],
    steps: [mount, avail(semJob()), tick(2), release("semantic"), tick(3)],
  },
  {
    name: "续租：两个任务同时运行，各自续租、各自回报",
    settings: ALL_ON,
    wiki: [blocked(wikiResult())],
    semantic: [blocked(semResult(5, 0))],
    steps: [mount, avail(wikiJob()), avail(semJob()), tick(2), release("semantic"), tick(2), release("wiki"), tick(1)],
  },
  {
    name: "续租：同类 Wiki 任务并发时第二个直接跳过并回报完成，第一个照常续租",
    settings: ALL_ON,
    wiki: [blocked(wikiResult())],
    steps: [mount, avail(wikiJob("job-1")), avail(wikiJob("job-2")), tick(1), release("wiki"), tick(1)],
  },
  {
    name: "续租：同类语义任务并发时第二个直接跳过并回报完成，第一个照常续租",
    settings: ALL_ON,
    semantic: [blocked(semResult(2, 0))],
    steps: [mount, avail(semJob("job-1")), avail(semJob("job-2")), tick(1), release("semantic"), tick(1)],
  },
  {
    name: "续租：任务运行期间设置变化触发同步，调用与续租交错",
    settings: ALL_ON,
    wiki: [blocked(wikiResult())],
    steps: [mount, avail(wikiJob()), tick(1), settings(spec(TWO_TIER)), tick(1), release("wiki")],
  },
  {
    name: "续租：下游卡住期间界面手动任务的开关不影响续租",
    settings: ALL_ON,
    wiki: [blocked(wikiResult())],
    steps: [mount, avail(wikiJob()), { op: "busy", semantic: true }, tick(2), release("wiki")],
  },

  // ---- 回报的边角 ----
  {
    name: "回报：完成时租约已丢失（返回 false），不再有后续调用",
    settings: ALL_ON,
    wiki: [wikiResult()],
    complete: ["lost"],
    steps: [mount, avail(wikiJob())],
  },
  {
    name: "回报：完成调用本身抛错，改用同一条消息回报失败",
    settings: ALL_ON,
    wiki: [wikiResult()],
    complete: [{ rejects: "IPC 通道已断开" }],
    steps: [mount, avail(wikiJob())],
  },
  {
    name: "回报：完成调用抛错的消息为空串",
    settings: ALL_ON,
    semantic: [semResult(1, 0)],
    complete: [{ rejects: "" }],
    steps: [mount, avail(semJob())],
  },
  {
    name: "回报：完成调用抛错期间续租持续到回报失败之后才停止",
    settings: ALL_ON,
    wiki: [blocked(wikiResult())],
    complete: [{ rejects: "写库失败" }],
    steps: [mount, avail(wikiJob()), tick(1), release("wiki"), tick(2)],
  },
  {
    name: "回报：任务失败时不调用完成",
    settings: ALL_ON,
    wiki: [wikiThrows("boom")],
    complete: [{ rejects: "不应被调用" }],
    steps: [mount, avail(wikiJob())],
  },

  // ---- 卸载 ----
  {
    name: "卸载：卸载之后推来的任务被忽略",
    settings: ALL_ON,
    wiki: [wikiResult()],
    steps: [mount, { op: "unmount" }, avail(wikiJob())],
  },
  {
    name: "卸载：任务跑完之后卸载，之后的任务被忽略",
    settings: ALL_ON,
    wiki: [wikiResult(), wikiResult()],
    steps: [mount, avail(wikiJob("job-1")), { op: "unmount" }, avail(wikiJob("job-2"))],
  },
  {
    name: "卸载：卸载之后设置变化不再同步",
    settings: ALL_ON,
    steps: [mount, { op: "unmount" }, settings(spec(TWO_TIER))],
  },
];

// ---- 重放 ----

const flushMacrotask = (): Promise<void> => new Promise((resolve) => setImmediate(resolve));

interface BlockedCall {
  behavior: Behavior;
  resolve: (value: unknown) => void;
  reject: (reason: unknown) => void;
}

function settleBehavior(behavior: Behavior, call: Pick<BlockedCall, "resolve" | "reject">): void {
  if ("throws" in behavior) call.reject(new Error(behavior.throws));
  else call.resolve(behavior.result);
}

async function runCase(c: Case, baseline: Raw): Promise<Raw> {
  const calls: Call[] = [];
  let recording = true;
  const record = (call: Call): void => {
    if (recording) calls.push(call);
  };

  const wikiQueue = [...(c.wiki ?? [])];
  const semanticQueue = [...(c.semantic ?? [])];
  const completeQueue = [...(c.complete ?? [])];
  const pending: Record<Runner, BlockedCall[]> = { wiki: [], semantic: [] };
  /** 正在续租的任务 id → 开始的虚拟时刻 */
  const active = new Map<string, number>();

  const downstream = (queue: Behavior[], runner: Runner, empty: Raw): Promise<unknown> => {
    const behavior = queue.shift() ?? { result: empty };
    if (!behavior.block) {
      return "throws" in behavior ? Promise.reject(new Error(behavior.throws)) : Promise.resolve(behavior.result);
    }
    return new Promise((resolve, reject) => pending[runner].push({ behavior, resolve, reject }));
  };
  harness.compileReadyWiki = () => {
    record({ call: "compileReadyWiki" });
    return downstream(wikiQueue, "wiki", { compiled: 0, pending: 0, skipped: 0, failures: [] });
  };
  harness.runSemanticIndexing = () => {
    record({ call: "runSemanticIndexing" });
    return downstream(semanticQueue, "semantic", { indexed: 0, failed: 0 });
  };

  const handlers = new Map<string, Set<(value: unknown) => void>>();
  const win = window as unknown as { api: Raw };
  const savedApi = win.api;
  win.api = {
    ...savedApi,
    on: (channel: string, handler: (value: unknown) => void) => {
      if (!handlers.has(channel)) handlers.set(channel, new Set());
      handlers.get(channel)!.add(handler);
    },
    off: (channel: string, handler: (value: unknown) => void) => {
      handlers.get(channel)?.delete(handler);
    },
    backgroundJob: {
      syncRenderer: async (input: { wikiEnabled: boolean; semanticEnabled: boolean }) => {
        record({ call: "syncRenderer", wikiEnabled: input.wikiEnabled, semanticEnabled: input.semanticEnabled });
        return [];
      },
      renew: async (id: string) => {
        record({ call: "renew", id });
        return true;
      },
      complete: async (id: string) => {
        record({ call: "complete", id });
        active.delete(id);
        const outcome = completeQueue.shift() ?? "ok";
        if (outcome === "ok") return true;
        if (outcome === "lost") return false;
        throw new Error(outcome.rejects);
      },
      fail: async (input: { id: string; error: string; pause?: unknown }) => {
        record({ call: "fail", id: input.id, error: input.error, ...("pause" in input ? { pause: input.pause } : {}) });
        active.delete(input.id);
        return true;
      },
    },
    log: {
      appError: (input: { scope: string; action: string; message: string }) => {
        record({ call: "appError", scope: input.scope, action: input.action, message: input.message });
      },
    },
    // runIndexing 结束后会刷新状态：只要不抛错即可，不计入对拍的调用
    semantic: { status: async () => ({}) },
  };

  useSettingsStore.setState(mergeSettingsState(c.settings, baseline as never));
  useWikiStore.setState({ isCompiling: false });
  useSemanticStore.setState({ isIndexing: false });

  let hook: ReturnType<typeof renderHook> | null = null;
  const steps: Raw[] = [];

  const settle = async (): Promise<void> => {
    let stable = 0;
    let last = -1;
    for (let i = 0; i < 200 && stable < 4; i++) {
      await vi.advanceTimersByTimeAsync(0);
      await flushMacrotask();
      if (calls.length === last) stable += 1;
      else {
        stable = 0;
        last = calls.length;
      }
    }
  };

  const syncCount = (from: number): number => calls.slice(from).filter((call) => call.call === "syncRenderer").length;

  try {
    for (const step of c.steps) {
      const before = calls.length;
      let notice: Raw | null | undefined;
      switch (step.op) {
        case "mount": {
          if (hook) throw new Error(`用例「${c.name}」重复挂载`);
          hook = renderHook(() => useBackgroundJobClient());
          await settle();
          if (syncCount(before) !== 1) throw new Error(`用例「${c.name}」：挂载应同步恰好一次`);
          if (handlers.get(IPC_CHANNELS.BACKGROUND_JOB_AVAILABLE)?.size !== 1) throw new Error(`用例「${c.name}」：挂载应注册一个领取回调`);
          break;
        }
        case "settings": {
          await act(async () => {
            useSettingsStore.setState(mergeSettingsState(step.settings, baseline as never));
          });
          await settle();
          const expected = hook ? 1 : 0;
          if (syncCount(before) !== expected) throw new Error(`用例「${c.name}」：设置步骤应同步 ${expected} 次`);
          break;
        }
        case "available": {
          const valid = step.job.kind === "wiki-compile" || step.job.kind === "semantic-index";
          if (valid && handlers.get(IPC_CHANNELS.BACKGROUND_JOB_AVAILABLE)?.size) {
            const now = Date.now();
            for (const startedAt of active.values()) {
              if (startedAt !== now) throw new Error(`用例「${c.name}」：同时在跑的任务必须在同一虚拟时刻开始`);
            }
            active.set(step.job.id, now);
          }
          for (const handler of [...(handlers.get(IPC_CHANNELS.BACKGROUND_JOB_AVAILABLE) ?? [])]) handler(step.job);
          await settle();
          break;
        }
        case "advance": {
          if (new Set(active.values()).size > 1) throw new Error(`用例「${c.name}」：活跃任务的续租相位不一致`);
          for (let i = 0; i < step.ticks; i++) {
            await vi.advanceTimersByTimeAsync(HEARTBEAT_MS);
            await settle();
          }
          break;
        }
        case "release": {
          const next = pending[step.runner].shift();
          if (!next) throw new Error(`用例「${c.name}」：没有卡住的 ${step.runner} 调用可放行`);
          settleBehavior(next.behavior, next);
          await settle();
          break;
        }
        case "busy": {
          if (step.wiki !== undefined) useWikiStore.setState({ isCompiling: step.wiki });
          if (step.semantic !== undefined) useSemanticStore.setState({ isIndexing: step.semantic });
          break;
        }
        case "manualIndexing": {
          if (semanticQueue[0]?.block) throw new Error(`用例「${c.name}」：手动索引不能用卡住的下游行为`);
          const result = await useSemanticStore.getState().runIndexing(false);
          await settle();
          // message 为空串与没有原因等价（Go 侧的 LastError 没有「未设置」这一态）
          notice = result === null ? null : { kind: result.kind, indexed: result.indexed, failed: result.failed, ...(result.message ? { message: result.message } : {}) };
          break;
        }
        case "unmount": {
          if (!hook) throw new Error(`用例「${c.name}」：还没挂载`);
          if (active.size) throw new Error(`用例「${c.name}」：卸载时不能有任务在跑`);
          hook.unmount();
          hook = null;
          await settle();
          if (handlers.get(IPC_CHANNELS.BACKGROUND_JOB_AVAILABLE)?.size) throw new Error(`用例「${c.name}」：卸载应移除领取回调`);
          break;
        }
      }
      steps.push({ ...step, calls: calls.slice(before), ...(notice !== undefined ? { notice } : {}) });
    }

    const blockedAtEnd = { wiki: pending.wiki.length, semantic: pending.semantic.length };
    // 收尾：放行卡住的调用让在途任务结束，其间的调用不入 golden
    recording = false;
    for (const runner of ["wiki", "semantic"] as const) {
      for (const next of pending[runner].splice(0)) settleBehavior(next.behavior, next);
    }
    await settle();
    return { steps, blockedAtEnd };
  } finally {
    hook?.unmount();
    vi.clearAllTimers();
    win.api = savedApi;
    harness.compileReadyWiki = undefined;
    harness.runSemanticIndexing = undefined;
  }
}

describe("aiflow golden：后台任务执行器", () => {
  const baseline = { ...useSettingsStore.getState() };

  beforeAll(async () => {
    // 预先加载 hook 里动态 import 的模块，免得首次加载的真实 I/O 拖进虚拟时间的结算
    await import("../../../src/renderer/stores/wiki.store");
    await import("../../../src/renderer/stores/semantic.store");
    await import("../../../src/renderer/stores/ui.store");
    await import("../../../src/renderer/services/knowledge-ai/wiki-v2");
    vi.spyOn(console, "error").mockImplementation(() => undefined);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  afterAll(() => {
    vi.restoreAllMocks();
    useSettingsStore.setState(baseline);
  });

  it("useBackgroundJobClient 的同步、领取、执行、续租与回报与已提交的 golden 一致", async () => {
    const cases: Raw[] = [];
    for (const c of CASES) {
      vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout", "setInterval", "clearInterval", "Date"], now: 1_700_000_000_000 });
      const expect = await runCase(c, baseline as unknown as Raw);
      cases.push({ name: c.name, settings: c.settings, wiki: c.wiki ?? [], semantic: c.semantic ?? [], complete: c.complete ?? [], steps: c.steps, expect });
      vi.useRealTimers();
    }
    writeOrCompareGolden("background-client", { heartbeatMs: HEARTBEAT_MS, cases });
  });
});
