/**
 * aiflow Wiki 域后半段的 golden：全文编译驱动器（wiki-v2：模型身份、预览、控制、逐块驱动、后台一轮）。
 * 真实 TS 实现 + 假的 window.api.wiki（脚本化的 IPC 应答）+ 假的 AI 传输，把 IPC 调用序列、模型请求、进度回调、
 * 事件、结果与错误一并导出；Go 侧重放见 apps/desktop-go/aiflow/wiki_driver_golden_test.go。
 *
 * 约定见 aiflow-golden-kit.ts / aiflow-golden-flow-kit.ts。纯函数部分（解析、清洗、链接）在 aiflow-golden-wiki-compile.test.ts。
 *
 * IPC 脚本按频道排队：preview / start / next / finish / control / catalog；每一步是 {result} 或 {throws}，
 * 可选 during 钩子在应答前执行（嵌套再跑一遍、经 control 取消、中止信号、中途换模型……），用来确定性地复现并发时序。
 * 任务里的 model 写 "$identity" 表示用当前设置算出的模型身份串（golden 里同时记下这串的字面值）。
 */
import { describe, it, vi } from "vitest";
import type { WikiCatalogEntry } from "@guizhi/shared/types";
import { useSettingsStore } from "../../../src/renderer/stores/settings.store";
import { mergeSettingsState } from "../../../src/renderer/stores/settings/settings-persistence";
import {
  compileReadyWiki,
  controlWikiJob,
  runWikiJob,
  wikiModelIdentity,
  wikiPreview,
} from "../../../src/renderer/services/knowledge-ai/wiki-v2";
import { writeOrCompareGolden, type ScriptStep } from "./aiflow-golden-kit";
import {
  C,
  LEGACY_ONLY,
  TWO_TIER,
  body,
  chat,
  compactText,
  errorInfo,
  render,
  runFlow,
  type FlowCase,
  type Raw,
  type Spec,
} from "./aiflow-golden-flow-kit";

// ---- 用例形状 ----

type Channel = "preview" | "start" | "next" | "finish" | "control" | "catalog";

type Hook =
  | { run: "runJobAgain" }
  | { run: "cancelViaControl"; id: string }
  | { run: "compileReadyAgain" }
  | { run: "abortSignal" }
  | { run: "swapSettings"; settings: Raw };

interface IpcStep {
  result?: unknown;
  throws?: string;
  during?: Hook;
}

type Call =
  | { kind: "identity" }
  | { kind: "preview"; ids?: string[] }
  | { kind: "control"; id: string; status: "paused" | "cancelled" }
  | { kind: "runJob"; job: Raw }
  | { kind: "compileReady" };

interface WikiCase extends FlowCase {
  call: Call;
  ipc: Partial<Record<Channel, IpcStep[]>>;
}

// ---- 夹具 ----

const ID = "$identity";

const OTHER_MODEL: Raw = {
  aiModels: [chat("other", { isDefault: true, apiUrl: "https://other.example.com/v1", model: "model-other" })],
  modelRouteDefaults: { mainText: "other", fastText: "other" },
};

const SECRET_URL: Raw = {
  aiModels: [
    chat("main", {
      isDefault: true,
      provider: "openai",
      apiUrl: "https://user:pw@api.example.com/v1?token=abc&x=1&api_key=k#frag",
    }),
  ],
  modelRouteDefaults: { mainText: "main", fastText: "main" },
};

const urlSettings = (apiUrl: string): Raw => ({
  aiModels: [chat("main", { isDefault: true, apiUrl })],
  modelRouteDefaults: { mainText: "main", fastText: "main" },
});

const job = (over: Raw = {}): Raw => ({
  id: "job-1",
  model: ID,
  status: "running",
  total: 3,
  completed: 0,
  failed: 0,
  createdAt: 1_700_000_000_000,
  ...over,
});

const block = (n: number, over: Raw = {}): Raw => ({
  key: `b${n}`,
  hash: `h${n}`,
  field: "content",
  section: `第 ${n} 节`,
  start: 0,
  end: 20,
  text: `分块 ${n} 的正文`,
  ...over,
});

const work = (n: number, over: Raw = {}): Raw => ({
  jobId: "job-1",
  itemId: `item-${n}`,
  title: `资料 ${n}`,
  fingerprint: `fp-${n}`,
  block: block(n),
  ...over,
});

const nextWork = (n: number, jobOver: Raw = {}, workOver: Raw = {}): IpcStep => ({
  result: { ok: true, job: job({ completed: n - 1, ...jobOver }), work: work(n, workOver) },
});
const nextDone = (jobOver: Raw = {}): IpcStep => ({
  result: { ok: true, job: job({ status: "completed", completed: 3, ...jobOver }) },
});
const OK: IpcStep = { result: { ok: true } };
const FAIL = (error?: string): IpcStep => ({ result: error === undefined ? { ok: false } : { ok: false, error } });

const entry = (title: string, over: Partial<WikiCatalogEntry> = {}): WikiCatalogEntry => ({
  id: `page-${title}`,
  title,
  normalizedTitle: title.split(/\s+/).filter(Boolean).join(" ").toUpperCase(),
  kind: "topic",
  summary: "",
  aliasesJson: null,
  manualEditedAt: null,
  updatedAt: 0,
  ...over,
});

const CATALOG: IpcStep = { result: [entry("知识管理", { aliasesJson: '["KM","PKM"]' }), entry("Electron")] };
const BIG_CATALOG: IpcStep = { result: Array.from({ length: 150 }, (_, i) => entry(`页面${i + 1}`)) };

const page = (title: string, over: Raw = {}): Raw => ({
  title,
  kind: "topic",
  summary: `${title}的摘要`,
  body: `${title}的正文`,
  aliases: [],
  ...over,
});
const pagesBody = (pages: unknown[], finish: string | null = "stop"): ScriptStep => ({ body: body(JSON.stringify({ pages }), finish) });
const textBody = (text: string, finish: string | null = "stop"): ScriptStep => ({ body: body(text, finish) });

const LINKED = page("协作笔记", {
  kind: "concept",
  summary: "多人协作记录\n第二行被丢弃",
  body: "与 [[km]] 和 [[Electron|桌面框架]] 相关，另见 [[不存在的页]]。",
  aliases: ["协作", " 共享笔记 "],
});

const cand = (n: number, state: string, over: Raw = {}): Raw => ({
  id: `item-${n}`,
  title: `资料 ${n}`,
  fingerprint: `fp-${n}`,
  state,
  blocks: 2,
  reusable: 0,
  ...over,
});

const preview = (entries: Raw[], counts: Raw = {}): IpcStep => ({
  result: { ok: true, preview: { entries, counts, requests: { min: entries.length, max: entries.length * 2 } } },
});

const ipcStatus = (id: string, status: string): IpcStep => ({ result: { ok: true, job: job({ id, status }) } });

const run = (name: string, ipc: WikiCase["ipc"], script: ScriptStep[] = [], over: Partial<WikiCase> = {}): WikiCase => ({
  name,
  settings: TWO_TIER,
  script,
  call: { kind: "runJob", job: job() },
  ipc,
  ...over,
});

// ---- 用例 ----

const CASES: WikiCase[] = [
  // 模型身份
  { name: "身份：两档模型", settings: TWO_TIER, script: [], call: { kind: "identity" }, ipc: {} },
  { name: "身份：只有旧版单模型设置", settings: LEGACY_ONLY, script: [], call: { kind: "identity" }, ipc: {} },
  { name: "身份：没有任何 AI 设置", settings: {}, script: [], call: { kind: "identity" }, ipc: {} },
  { name: "身份：接口地址的凭证、敏感参数与片段被脱敏", settings: SECRET_URL, script: [], call: { kind: "identity" }, ipc: {} },
  { name: "身份：接口地址无法解析时记为 null", settings: urlSettings("not a url"), script: [], call: { kind: "identity" }, ipc: {} },
  { name: "身份：接口地址不是 http(s) 时记为 null", settings: urlSettings("ftp://files.example.com/v1"), script: [], call: { kind: "identity" }, ipc: {} },
  { name: "身份：另一个模型", settings: OTHER_MODEL, script: [], call: { kind: "identity" }, ipc: {} },

  // 预览
  { name: "预览：全库", settings: TWO_TIER, script: [], call: { kind: "preview" }, ipc: { preview: [preview([cand(1, "ready"), cand(2, "current")], { ready: 1, current: 1 })] } },
  { name: "预览：指定条目", settings: TWO_TIER, script: [], call: { kind: "preview", ids: ["item-1", "item-2"] }, ipc: { preview: [preview([cand(1, "ready")])] } },
  { name: "预览：空的指定列表", settings: TWO_TIER, script: [], call: { kind: "preview", ids: [] }, ipc: { preview: [preview([])] } },
  { name: "预览：存储返回失败与原因", settings: TWO_TIER, script: [], call: { kind: "preview" }, ipc: { preview: [FAIL("库繁忙")] } },
  { name: "预览：存储返回失败、没有原因", settings: TWO_TIER, script: [], call: { kind: "preview" }, ipc: { preview: [FAIL()] } },
  { name: "预览：成功但没有预览内容", settings: TWO_TIER, script: [], call: { kind: "preview" }, ipc: { preview: [OK] } },
  { name: "预览：成功但没有预览内容、带原因", settings: TWO_TIER, script: [], call: { kind: "preview" }, ipc: { preview: [{ result: { ok: true, error: "奇怪的原因" } }] } },
  { name: "预览：IPC 通道抛错", settings: TWO_TIER, script: [], call: { kind: "preview" }, ipc: { preview: [{ throws: "IPC 断开" }] } },
  { name: "预览：未配置 AI 时身份串为空也照常发出", settings: {}, script: [], call: { kind: "preview" }, ipc: { preview: [preview([])] } },
  {
    name: "预览：带结果里的候选原因",
    settings: TWO_TIER,
    script: [],
    call: { kind: "preview" },
    ipc: { preview: [preview([cand(1, "review", { reason: "内容疑似缺失" }), cand(2, "waiting", { reason: "等待摘要完成" })], { review: 1, waiting: 1 })] },
  },

  // 控制
  { name: "控制：暂停", settings: TWO_TIER, script: [], call: { kind: "control", id: "job-1", status: "paused" }, ipc: { control: [ipcStatus("job-1", "paused")] } },
  { name: "控制：取消（没有在驱动的任务）", settings: TWO_TIER, script: [], call: { kind: "control", id: "job-1", status: "cancelled" }, ipc: { control: [ipcStatus("job-1", "cancelled")] } },
  { name: "控制：存储返回失败与原因", settings: TWO_TIER, script: [], call: { kind: "control", id: "job-9", status: "paused" }, ipc: { control: [FAIL("无此任务")] } },
  { name: "控制：存储返回失败、没有原因", settings: TWO_TIER, script: [], call: { kind: "control", id: "job-9", status: "cancelled" }, ipc: { control: [FAIL()] } },
  { name: "控制：IPC 通道抛错", settings: TWO_TIER, script: [], call: { kind: "control", id: "job-1", status: "paused" }, ipc: { control: [{ throws: "IPC 断开" }] } },
  { name: "控制：成功但没有返回任务", settings: TWO_TIER, script: [], call: { kind: "control", id: "job-1", status: "paused" }, ipc: { control: [OK] } },

  // 逐块驱动：正常路径
  run("驱动：没有可做的分块，直接返回当前任务", { next: [nextDone()] }),
  run("驱动：存储没给任务也没给分块，返回传入的任务", { next: [{ result: { ok: true } }] }),
  run("驱动：一个分块成功（链接按目录与别名清洗，别名与摘要净化）", { next: [nextWork(1), nextDone({ completed: 1, total: 1 })], catalog: [CATALOG], finish: [OK] }, [pagesBody([LINKED])]),
  run("驱动：模型输出带围栏与前后缀文字", { next: [nextWork(1), nextDone({ completed: 1 })], catalog: [CATALOG], finish: [OK] }, [
    textBody("好的，结果如下：\n```json\n" + JSON.stringify({ pages: [page("甲"), page("乙", { kind: "entity" })] }) + "\n```\n以上。"),
  ]),
  run("驱动：三个分块依次成功，进度逐次回调", { next: [nextWork(1), nextWork(2, { completed: 1 }), nextWork(3, { completed: 2 }), nextDone()], catalog: [CATALOG, CATALOG, CATALOG], finish: [OK, OK, OK] }, [
    pagesBody([page("甲")]),
    pagesBody([page("乙")]),
    pagesBody([page("丙")]),
  ]),
  run("驱动：模型回空 pages，照常落盘空贡献", { next: [nextWork(1), nextDone({ completed: 1 })], catalog: [CATALOG], finish: [OK] }, [pagesBody([])]),
  run("驱动：来源标题与分块字段进入提示", { next: [nextWork(1, {}, { title: "含\"引号\"与\n换行的标题", block: block(1, { field: "transcript", section: "字幕：开场", text: "第一行\n第二行 <b>&" }) }), nextDone()], catalog: [CATALOG], finish: [OK] }, [pagesBody([page("甲")])]),
  run("驱动：目录超过 100 条时只带前 100 个标题", { next: [nextWork(1), nextDone()], catalog: [BIG_CATALOG], finish: [OK] }, [pagesBody([page("甲")])]),
  run("驱动：目录为空", { next: [nextWork(1), nextDone()], catalog: [{ result: [] }], finish: [OK] }, [pagesBody([page("甲", { body: "没有 [[任何页]] 可链" })])]),
  run("驱动：长分块文本（请求体只记指纹）", { next: [nextWork(1, {}, { block: block(1, { text: { $text: [C(3000)] } as unknown as string }) }), nextDone()], catalog: [CATALOG], finish: [OK] }, [pagesBody([page("甲")])]),

  // 逐块驱动：单块失败只记账
  run("驱动：第一次输出被截断（length），带纠正说明重试后成功", { next: [nextWork(1), nextDone({ completed: 1 })], catalog: [CATALOG], finish: [OK] }, [
    textBody('{"pages":[{"title":"甲"', "length"),
    pagesBody([page("甲")]),
  ]),
  run("驱动：第一次被截断（length）但内容恰好是合法 JSON：不解析，直接重试", { next: [nextWork(1), nextDone({ completed: 1 })], catalog: [CATALOG], finish: [OK] }, [
    pagesBody([page("半截")], "length"),
    pagesBody([page("完整")]),
  ]),
  run("驱动：两次都被截断且都是合法 JSON：记账为无效或不完整", { next: [nextWork(1), nextDone({ failed: 1 })], catalog: [CATALOG], finish: [OK] }, [
    pagesBody([page("甲")], "length"),
    pagesBody([page("乙")], "length"),
  ]),
  run("驱动：第一次不是 JSON，重试后成功", { next: [nextWork(1), nextDone({ completed: 1 })], catalog: [CATALOG], finish: [OK] }, [textBody("抱歉，我无法处理。"), pagesBody([page("甲")])]),
  run("驱动：两次都被截断：记账后继续下一块", { next: [nextWork(1), nextWork(2, { completed: 0, failed: 1 }), nextDone({ failed: 1, completed: 1 })], catalog: [CATALOG, CATALOG], finish: [OK, OK] }, [
    textBody("{", "length"),
    textBody("{", "length"),
    pagesBody([page("乙")]),
  ]),
  run("驱动：两次都不是 JSON：记账后继续", { next: [nextWork(1), nextDone({ failed: 1 })], catalog: [CATALOG], finish: [OK] }, [textBody("不是 JSON"), textBody("仍然不是 JSON")]),
  run("驱动：截断后的重试里 pages 不是数组", { next: [nextWork(1), nextDone()], catalog: [CATALOG], finish: [OK] }, [textBody("{}", "length"), textBody('{"pages":"x"}')]),
  run("驱动：pages 全部没通过校验", { next: [nextWork(1), nextDone({ failed: 1 })], catalog: [CATALOG], finish: [OK] }, [pagesBody([{ title: "", body: "无标题" }, { title: "无正文", body: "" }])]),
  run("驱动：pages 里有 null 元素（TypeError 文案进入失败原因）", { next: [nextWork(1), nextDone({ failed: 1 })], catalog: [CATALOG], finish: [OK] }, [pagesBody([null])]),
  run("驱动：模型请求失败（HTTP 500）：记账后继续", { next: [nextWork(1), nextDone({ failed: 1 })], catalog: [CATALOG], finish: [OK] }, [
    { status: 500, body: JSON.stringify({ error: { message: "boom" } }) },
  ]),
  run("驱动：模型请求被限流（HTTP 429）", { next: [nextWork(1), nextDone({ failed: 1 })], catalog: [CATALOG], finish: [OK] }, [
    { status: 429, body: JSON.stringify({ error: { message: "rate limited" } }) },
  ]),
  run("驱动：传输层抛错", { next: [nextWork(1), nextDone({ failed: 1 })], catalog: [CATALOG], finish: [OK] }, [{ throws: "网络不可达" }]),
  run("驱动：目录读取抛错：记账后继续", { next: [nextWork(1), nextDone({ failed: 1 })], catalog: [{ throws: "目录读取失败" }], finish: [OK] }),
  run("驱动：落盘返回失败与原因：记为分块失败后继续", { next: [nextWork(1), nextDone({ failed: 1 })], catalog: [CATALOG], finish: [FAIL("磁盘已满"), OK] }, [pagesBody([page("甲")])]),
  run("驱动：落盘返回失败、没有原因", { next: [nextWork(1), nextDone({ failed: 1 })], catalog: [CATALOG], finish: [FAIL(), OK] }, [pagesBody([page("甲")])]),
  run("驱动：落盘的 IPC 抛错：记为分块失败后继续", { next: [nextWork(1), nextDone({ failed: 1 })], catalog: [CATALOG], finish: [{ throws: "IPC 断开" }, OK] }, [pagesBody([page("甲")])]),

  // 逐块驱动：整体失败
  run("驱动：连失败原因都存不下：暂停任务并抛出", { next: [nextWork(1)], catalog: [CATALOG], finish: [FAIL("记账失败")], control: [ipcStatus("job-1", "paused")] }, [textBody("不是 JSON"), textBody("不是 JSON")]),
  run("驱动：失败记账也失败、没有原因", { next: [nextWork(1)], catalog: [CATALOG], finish: [FAIL()], control: [ipcStatus("job-1", "paused")] }, [textBody("不是 JSON"), textBody("不是 JSON")]),
  run("驱动：失败记账的 IPC 抛错", { next: [nextWork(1)], catalog: [CATALOG], finish: [{ throws: "IPC 断开" }], control: [ipcStatus("job-1", "paused")] }, [textBody("不是 JSON"), textBody("不是 JSON")]),
  run("驱动：落盘失败后记账也失败", { next: [nextWork(1)], catalog: [CATALOG], finish: [FAIL("磁盘已满"), FAIL("账本损坏")], control: [ipcStatus("job-1", "paused")] }, [pagesBody([page("甲")])]),
  run("驱动：读检查点返回失败与原因", { next: [FAIL("检查点损坏")], control: [ipcStatus("job-1", "paused")] }),
  run("驱动：读检查点返回失败、没有原因", { next: [FAIL()], control: [ipcStatus("job-1", "paused")] }),
  run("驱动：读检查点的 IPC 抛错", { next: [{ throws: "IPC 断开" }], control: [ipcStatus("job-1", "paused")] }),
  run("驱动：第二轮读检查点失败：已落盘的块不受影响，任务暂停", { next: [nextWork(1), FAIL("检查点损坏")], catalog: [CATALOG], finish: [OK], control: [ipcStatus("job-1", "paused")] }, [pagesBody([page("甲")])]),
  run("驱动：暂停任务本身也失败：仍抛出原错误", { next: [FAIL("检查点损坏")], control: [{ throws: "面板繁忙" }] }),
  run("驱动：暂停任务返回业务失败：仍抛出原错误", { next: [FAIL("检查点损坏")], control: [FAIL("无此任务")] }),

  // 入口检查
  run("驱动：未配置 AI", {}, [], { settings: {} }),
  run("驱动：任务的模型身份与当前不一致", { next: [nextDone()] }, [], { call: { kind: "runJob", job: job({ model: "[\"fulltext-v2\",\"old\",\"custom\",\"openai\",null,\"old-model\"]" }) } }),
  run("驱动：任务的模型身份是空串", { next: [nextDone()] }, [], { call: { kind: "runJob", job: job({ model: "" }) } }),
  run("驱动：已有任务在跑：嵌套再跑一遍被拒绝，原任务不受影响", { next: [{ ...nextWork(1), during: { run: "runJobAgain" } }, nextDone({ completed: 1 })], catalog: [CATALOG], finish: [OK] }, [pagesBody([page("甲")])]),
  run("驱动：编译途中换了模型：未继续调用，记账后继续", {
    next: [{ ...nextWork(1), during: { run: "swapSettings", settings: OTHER_MODEL } }, nextDone({ failed: 1 })],
    finish: [OK],
  }),
  run("驱动：途中重设了相同的模型：身份不变，照常调用", {
    next: [{ ...nextWork(1), during: { run: "swapSettings", settings: TWO_TIER } }, nextDone({ completed: 1 })],
    catalog: [CATALOG],
    finish: [OK],
  }, [pagesBody([page("甲")])]),

  // 取消
  run("取消：调用前信号已中止：只通知存储取消，不读检查点", { control: [ipcStatus("job-1", "cancelled")] }, [], { abort: "before" }),
  run("取消：调用前信号已中止，通知存储取消时抛错（被吞掉）", { control: [{ throws: "IPC 断开" }] }, [], { abort: "before" }),
  run("取消：调用前信号已中止，存储返回业务失败（被吞掉）", { control: [FAIL("无此任务")] }, [], { abort: "before" }),
  run("取消：模型请求进行中中止信号", { next: [nextWork(1)], catalog: [CATALOG], control: [ipcStatus("job-1", "cancelled")] }, [{ abortDuring: true, status: 0, ok: false, error: "已取消" }], { abort: "during" }),
  run("取消：读检查点时中止信号：继续取目录但不再发模型请求", {
    next: [{ ...nextWork(1), during: { run: "abortSignal" } }],
    catalog: [CATALOG],
    control: [ipcStatus("job-1", "cancelled")],
  }),
  run("取消：取目录时中止信号", { next: [nextWork(1)], catalog: [{ ...CATALOG, during: { run: "abortSignal" } }], control: [ipcStatus("job-1", "cancelled")] }),
  run("取消：落盘时中止信号：落盘照常完成，随后结束", {
    next: [nextWork(1)],
    catalog: [CATALOG],
    finish: [{ ...OK, during: { run: "abortSignal" } }],
    control: [ipcStatus("job-1", "cancelled")],
  }, [pagesBody([page("甲")])]),
  run("取消：经 control 取消正在驱动的任务", {
    next: [{ ...nextWork(1), during: { run: "cancelViaControl", id: "job-1" } }],
    catalog: [CATALOG],
    control: [ipcStatus("job-1", "cancelled")],
  }),
  run("取消：经 control 取消别的任务不影响正在驱动的任务", {
    next: [{ ...nextWork(1), during: { run: "cancelViaControl", id: "job-2" } }, nextDone({ completed: 1 })],
    catalog: [CATALOG],
    finish: [OK],
    control: [ipcStatus("job-2", "cancelled")],
  }, [pagesBody([page("甲")])]),

  // 后台一轮
  { name: "一轮：未配置 AI", settings: {}, script: [], call: { kind: "compileReady" }, ipc: {} },
  { name: "一轮：没有可执行条目", settings: TWO_TIER, script: [], call: { kind: "compileReady" }, ipc: { preview: [preview([cand(1, "waiting"), cand(2, "review"), cand(3, "current"), cand(4, "upgrade"), cand(5, "excluded")])] } },
  { name: "一轮：预览为空", settings: TWO_TIER, script: [], call: { kind: "compileReady" }, ipc: { preview: [preview([])] } },
  {
    name: "一轮：可执行条目超过 10 个只取前 10 个，任务完成",
    settings: TWO_TIER,
    script: [pagesBody([page("甲")])],
    call: { kind: "compileReady" },
    ipc: {
      preview: [preview([...Array.from({ length: 6 }, (_, i) => cand(i + 1, "ready")), cand(7, "waiting"), ...Array.from({ length: 6 }, (_, i) => cand(i + 8, "ready"))], { ready: 12 })],
      start: [{ result: { ok: true, job: job({ total: 1 }) } }],
      next: [nextWork(1, { total: 1 }), nextDone({ total: 1, completed: 1 })],
      catalog: [CATALOG],
      finish: [OK],
    },
  },
  {
    name: "一轮：任务没跑完（暂停）：汇总给出继续的提示",
    settings: TWO_TIER,
    script: [],
    call: { kind: "compileReady" },
    ipc: {
      preview: [preview([cand(1, "ready"), cand(2, "ready")])],
      start: [{ result: { ok: true, job: job({ total: 4 }) } }],
      next: [{ result: { ok: true, job: job({ status: "paused", total: 4, completed: 1 }) } }],
    },
  },
  {
    name: "一轮：任务失败并带原因：原因原样作为失败说明",
    settings: TWO_TIER,
    script: [],
    call: { kind: "compileReady" },
    ipc: {
      preview: [preview([cand(1, "ready")])],
      start: [{ result: { ok: true, job: job({ total: 2 }) } }],
      next: [{ result: { ok: true, job: job({ status: "failed", total: 2, completed: 1, failed: 1, error: "模型额度用尽" }) } }],
    },
  },
  {
    name: "一轮：启动失败与原因",
    settings: TWO_TIER,
    script: [],
    call: { kind: "compileReady" },
    ipc: { preview: [preview([cand(1, "ready")])], start: [FAIL("无法创建任务")] },
  },
  {
    name: "一轮：启动失败、没有原因",
    settings: TWO_TIER,
    script: [],
    call: { kind: "compileReady" },
    ipc: { preview: [preview([cand(1, "ready")])], start: [FAIL()] },
  },
  {
    name: "一轮：启动成功但没有任务",
    settings: TWO_TIER,
    script: [],
    call: { kind: "compileReady" },
    ipc: { preview: [preview([cand(1, "ready")])], start: [OK] },
  },
  {
    name: "一轮：预览失败",
    settings: TWO_TIER,
    script: [],
    call: { kind: "compileReady" },
    ipc: { preview: [FAIL()] },
  },
  {
    name: "一轮：驱动中读检查点失败：错误向上抛，任务被暂停",
    settings: TWO_TIER,
    script: [],
    call: { kind: "compileReady" },
    ipc: {
      preview: [preview([cand(1, "ready")])],
      start: [{ result: { ok: true, job: job({ total: 1 }) } }],
      next: [FAIL("检查点损坏")],
      control: [ipcStatus("job-1", "paused")],
    },
  },
  {
    name: "一轮：调用前信号已中止：任务被取消，汇总为未完成",
    settings: TWO_TIER,
    script: [],
    abort: "before",
    call: { kind: "compileReady" },
    ipc: {
      preview: [preview([cand(1, "ready"), cand(2, "ready")])],
      start: [{ result: { ok: true, job: job({ total: 2 }) } }],
      control: [ipcStatus("job-1", "cancelled")],
    },
  },
  {
    name: "一轮：已有任务在跑：嵌套的一轮已建好任务才发现占用而失败",
    settings: TWO_TIER,
    script: [pagesBody([page("甲")])],
    call: { kind: "runJob", job: job() },
    ipc: {
      next: [{ ...nextWork(1), during: { run: "compileReadyAgain" } }, nextDone({ completed: 1 })],
      catalog: [CATALOG],
      finish: [OK],
      preview: [preview([cand(1, "ready")])],
      start: [{ result: { ok: true, job: job({ id: "job-2", total: 1 }) } }],
    },
  },
];

// ---- 重放 ----

interface TestWindow {
  api: Record<string, unknown>;
  addEventListener(type: string, listener: () => void): void;
  removeEventListener(type: string, listener: () => void): void;
}
const testWindow = (): TestWindow => (globalThis as unknown as { window: TestWindow }).window;

interface Recorder {
  calls: Raw[];
  nested: Raw[];
  progress: Raw[];
  events: number;
}

/** 调用记录里的长文本只留指纹，其余原样。 */
function compactCall(input: Raw): Raw {
  const copy = JSON.parse(JSON.stringify(input)) as Raw;
  const blockOf = (copy.work as Raw | undefined)?.block as Raw | undefined;
  if (blockOf && typeof blockOf.text === "string") blockOf.text = compactText(blockOf.text);
  if (Array.isArray(copy.contributions)) {
    for (const c of copy.contributions as Raw[]) {
      for (const field of ["title", "summary", "body"]) if (typeof c[field] === "string") c[field] = compactText(c[field] as string);
    }
  }
  return copy;
}

/** 展开用例数据里的占位：字符串 "$identity" → 当前模型身份串；{"$text": 规格} → 规格生成的文本。 */
function resolveValue(value: unknown, identity: string): unknown {
  if (value === ID) return identity;
  if (Array.isArray(value)) return value.map((entry) => resolveValue(entry, identity));
  if (value !== null && typeof value === "object") {
    const record = value as Record<string, unknown>;
    if (Object.keys(record).length === 1 && "$text" in record) return render(record.$text as Spec);
    return Object.fromEntries(Object.entries(record).map(([key, entry]) => [key, resolveValue(entry, identity)]));
  }
  return value;
}

/** 记录输出里把恰好等于模型身份串的字符串换回 "$identity"：golden 可读，且仍能发现身份串用错的地方。身份为空时不替换。 */
function maskIdentity(value: unknown, identity: string): unknown {
  if (identity === "") return value;
  if (value === identity) return ID;
  if (Array.isArray(value)) return value.map((entry) => maskIdentity(entry, identity));
  if (value !== null && typeof value === "object") {
    return Object.fromEntries(Object.entries(value as Record<string, unknown>).map(([key, entry]) => [key, maskIdentity(entry, identity)]));
  }
  return value;
}

const settle = async (name: string, runner: () => Promise<unknown>): Promise<Raw> => {
  try {
    const value = await runner();
    return { hook: name, value: value ?? null };
  } catch (error) {
    return { hook: name, error: errorInfo(error, { abortMessage: false }) };
  }
};

async function runWikiCase(c: WikiCase, baseline: Raw): Promise<Raw> {
  const rec: Recorder = { calls: [], nested: [], progress: [], events: 0 };
  const queues: Record<string, IpcStep[]> = {};
  let identity = "";
  const onEvent = (): void => {
    rec.events += 1;
  };

  const resolveJob = (source: Raw): Raw => resolveValue(source, identity) as Raw;

  const outcome = await runFlow(
    c,
    baseline,
    async (signal, fake) => {
      identity = wikiModelIdentity();
      for (const [channel, steps] of Object.entries(c.ipc)) queues[channel] = (steps ?? []).map((step) => resolveValue(step, identity) as IpcStep);
      const win = testWindow();

      const runHook = async (hook: Hook): Promise<void> => {
        switch (hook.run) {
          case "runJobAgain":
            rec.nested.push(await settle("runJobAgain", () => runWikiJob(resolveJob(job()) as never)));
            return;
          case "cancelViaControl":
            rec.nested.push(await settle("cancelViaControl", () => controlWikiJob(hook.id, "cancelled")));
            return;
          case "compileReadyAgain":
            rec.nested.push(await settle("compileReadyAgain", () => compileReadyWiki()));
            return;
          case "abortSignal":
            fake.controller.abort();
            rec.nested.push({ hook: "abortSignal" });
            return;
          case "swapSettings":
            useSettingsStore.setState(mergeSettingsState(hook.settings, baseline as never));
            rec.nested.push({ hook: "swapSettings" });
            return;
        }
      };

      const step = async (channel: Channel, record: Raw): Promise<unknown> => {
        rec.calls.push(record);
        const next = queues[channel]?.shift();
        if (!next) throw new Error(`IPC 脚本已用尽：${channel}`);
        if (next.during) await runHook(next.during);
        if (next.throws !== undefined) throw new Error(next.throws);
        return next.result;
      };

      win.api = {
        ...win.api,
        wiki: {
          compiler: (input: Raw) => step(input.action as Channel, { channel: "compiler", input: compactCall(input) }),
          catalog: () => step("catalog", { channel: "catalog" }),
        },
      };
      win.addEventListener("wiki-contributions-updated", onEvent);
      try {
        const call = c.call;
        switch (call.kind) {
          case "identity":
            return identity;
          case "preview":
            return await wikiPreview(call.ids);
          case "control":
            return (await controlWikiJob(call.id, call.status)) ?? null;
          case "runJob":
            return await runWikiJob(
              resolveJob(call.job) as never,
              (progressed) => rec.progress.push(JSON.parse(JSON.stringify(progressed)) as Raw),
              signal,
            );
          case "compileReady":
            return await compileReadyWiki((title, current, total) => rec.progress.push({ title, current, total }), signal);
        }
      } finally {
        win.removeEventListener("wiki-contributions-updated", onEvent);
      }
    },
    { abortMessage: false },
  );

  const unusedIpc: Raw = {};
  for (const [channel, steps] of Object.entries(queues)) if (steps.length) unusedIpc[channel] = steps.length;
  const masked = (value: unknown): unknown => maskIdentity(value, identity);
  return {
    identity,
    ...outcome,
    result: masked(outcome.result),
    calls: masked(rec.calls),
    nested: masked(rec.nested),
    progress: masked(rec.progress),
    events: rec.events,
    unusedIpc,
  };
}

describe("aiflow golden：Wiki 全文编译驱动器", () => {
  it("wikiModelIdentity / wikiPreview / controlWikiJob / runWikiJob / compileReadyWiki 与已提交的 golden 一致", async () => {
    const baseline = { ...useSettingsStore.getState() };
    const cases: Raw[] = [];
    try {
      for (const c of CASES) {
        const expect = await runWikiCase(c, baseline as unknown as Raw);
        cases.push({ name: c.name, settings: c.settings, abort: c.abort ?? null, script: c.script, call: c.call, ipc: c.ipc, expect });
        vi.restoreAllMocks();
      }
    } finally {
      useSettingsStore.setState(baseline);
    }
    writeOrCompareGolden("wiki-driver", { cases });
  });
});
