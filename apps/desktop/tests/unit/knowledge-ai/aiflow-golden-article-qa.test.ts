/**
 * aiflow 本文问答的 golden：article-qa.ts 的 askArticle（读取上下文、证据快照、联网查证、流式回答）。
 * 真实 TS 实现 + 假的 window.api（knowledge.get / articleAsk.context / search / cancelSearch 的脚本化应答）+ 假的 AI 传输，
 * 把 IPC 调用序列、模型请求、界面补丁序列、用量、日志与错误一并导出；
 * Go 侧重放见 apps/desktop-go/aiflow/article_qa_golden_test.go。
 *
 * 约定见 aiflow-golden-kit.ts / aiflow-golden-flow-kit.ts；IPC 脚本的写法同 aiflow-golden-wiki-driver.test.ts：
 * 按频道排队，每一步是 {result} 或 {throws}，可选 during 钩子在应答前触发中止信号（确定性地复现并发时序）。
 *
 * 补丁（patch 回调收到的 Partial<ArticleMessage>）逐条记录：
 *   - 顶层值为 undefined 的键记为 "$undefined"（对应 `step: undefined`，含义是「清掉步骤提示」，与「没有这个键」不同）；
 *   - 其余内容经 JSON 往返（嵌套的 undefined 与 JSON.stringify 一致，被丢弃）；
 *   - 长文本（回答、来源正文、证据正文）只记指纹。
 * 来源对象的键序决定提示词里资料的键序，这里按生产端（主包 articleSource）的顺序构造，
 * Go 侧用固定顺序输出，未知字段会在构造时报错，免得 Go 悄悄丢掉。
 *
 * 取不到的对拍项（不入 golden）：搜索词不是合法 JSON 时，TS 的错误文案是 V8 的 SyntaxError 原文
 * （且会进入系统提示），Go 不复刻，见 article_qa_test.go 里的单独用例。
 */
import { describe, it, vi } from "vitest";
import { askArticle } from "../../../src/renderer/services/knowledge-ai/article-qa";
import { useSettingsStore } from "../../../src/renderer/stores/settings.store";
import { writeOrCompareGolden, type ScriptStep } from "./aiflow-golden-kit";
import {
  A,
  C,
  TWO_TIER,
  body,
  compactText,
  render,
  runFlow,
  type FlowCase,
  type Raw,
  type Spec,
} from "./aiflow-golden-flow-kit";

// ---- 用例形状 ----

type Channel = "get" | "context" | "search";

interface IpcStep {
  result?: unknown;
  throws?: string;
  during?: { run: "abortSignal" };
}

interface AskInput {
  target: Raw;
  question: string;
  history?: Raw[];
  webEnabled?: boolean;
  requestId?: string;
  context?: Raw;
}

interface ArticleCase extends FlowCase {
  input: AskInput;
  ipc: Partial<Record<Channel, IpcStep[]>>;
  /** Date.now() 的固定值：给没有采集时间的来源补证据快照用。 */
  now?: number;
  /** 收到匹配的补丁之后中止信号：按 step 文案匹配，或第一个带 answer 的补丁。 */
  abortOnPatch?: { step?: string; answer?: boolean };
}

// ---- 夹具 ----

const NOW = 1_700_000_000_000;

const TARGET_KEYS = ["itemId", "view", "sourceKind", "versionId", "selection", "floor"];
const SOURCE_KEYS = ["ordinal", "kind", "title", "text", "url", "target", "fingerprint", "capturedAt", "cleared", "evidence"];

/** 按固定键序输出（跳过 undefined）；出现未知键直接报错。 */
function ordered(source: Raw, keys: string[]): Raw {
  for (const key of Object.keys(source)) {
    if (source[key] !== undefined && !keys.includes(key)) throw new Error(`夹具里有未知字段：${key}`);
  }
  return Object.fromEntries(keys.filter((key) => source[key] !== undefined).map((key) => [key, source[key]]));
}

const TGT = (over: Raw = {}): Raw => ordered({ itemId: "item-1", view: "body", ...over }, TARGET_KEYS);

const SRC = (over: Raw = {}): Raw =>
  ordered(
    {
      ordinal: 1,
      kind: "article",
      title: "归知设计笔记",
      text: "归知把资料保存在本地知识库，并用语义检索回答问题。",
      target: TGT({ sourceKind: "body", versionId: "v1" }),
      fingerprint: "fp-article-1",
      capturedAt: 1_699_000_000_000,
      ...over,
    },
    SOURCE_KEYS,
  );

const WEB = (n: number, over: Raw = {}): Raw =>
  SRC({
    ordinal: n,
    kind: "web",
    title: `网页${n}`,
    text: `网页${n}的摘录`,
    url: `https://example.com/page${n}`,
    target: undefined,
    fingerprint: undefined,
    capturedAt: 1_699_500_000_000 + n,
    ...over,
  });

const EVID = (over: Raw = {}): Raw => ({
  version: 1,
  kind: "article",
  sourceId: "item-1",
  title: "归知设计笔记",
  text: "证据里的文本",
  fingerprint: "fp-evidence-1",
  sourceVersion: "v1",
  capturedAt: 1_698_000_000_000,
  reviewStatus: "clear",
  reviewReasons: [],
  ...over,
});

const CTX = (over: Raw = {}): Raw => ({
  target: TGT(),
  title: "归知设计笔记",
  fingerprint: "ctx-fp-1",
  sources: [SRC()],
  clipped: false,
  ...over,
});

const MSG = (n: number, over: Raw = {}): Raw => ({
  id: `m${n}`,
  question: `问题${n}`,
  answer: `回答${n}`,
  status: "done",
  sources: [],
  webEnabled: false,
  webStatus: "off",
  warnings: [],
  ...over,
});

const ABORT = { run: "abortSignal" } as const;

const ctxOk = (over: Raw = {}): IpcStep => ({ result: { success: true, context: CTX(over) } });
const getItem = (over: Raw = {}): IpcStep => ({ result: { id: "item-1", ...over } });
const NO_ITEM: IpcStep = { result: null };
const searchOk = (sources: Raw[], warnings?: string[]): IpcStep => ({ result: { success: true, sources, ...(warnings ? { warnings } : {}) } });
const searchFail = (error?: string): IpcStep => ({ result: error === undefined ? { success: false } : { success: false, error } });

const sse = (...events: Array<object | string>): string =>
  `${events.map((e) => (typeof e === "string" ? e : `data: ${JSON.stringify(e)}`)).join("\n")}\n`;
const delta = (d: Record<string, unknown>, finish?: string): object => ({
  choices: [{ index: 0, delta: d, ...(finish ? { finish_reason: finish } : {}) }],
});

/** 流式回答：每块一个 SSE 分块，最后一块带 finish_reason。 */
const answer = (parts: string[] = ["结论见[1]。"], finish = "stop", extra: ScriptStep = {}): ScriptStep => ({
  chunks: parts.map((part, i) => sse(delta({ content: part }, i === parts.length - 1 ? finish : undefined))),
  ...extra,
});
const planner = (content: string): ScriptStep => ({ body: body(content) });
const PLAN = planner('["归知 语义检索","embedding 召回"]');
const HTTP_500: ScriptStep = { status: 500, body: JSON.stringify({ error: { message: "boom" } }) };

const inp = (over: Partial<AskInput> = {}): AskInput => ({ target: TGT(), question: "归知怎么做检索？", ...over });

const LINKS_TEXT =
  "参考 https://example.com/a?token=abc&page=2#frag 以及 (https://user:pw@example.com/p?id=1&api_key=k) 和 https://[bad 还有 http://ok.example.com/x，结尾";

/** 一个用例：默认配置两档模型、不联网。 */
const ask = (name: string, ipc: ArticleCase["ipc"], script: ScriptStep[], over: Partial<ArticleCase> = {}): ArticleCase => ({
  name,
  settings: TWO_TIER,
  script,
  input: inp(),
  ipc,
  ...over,
});

/** 联网问答：webEnabled 打开，其余同 ask。 */
const web = (name: string, ipc: ArticleCase["ipc"], script: ScriptStep[], over: Partial<ArticleCase> & { input?: AskInput } = {}): ArticleCase =>
  ask(name, ipc, script, { ...over, input: { ...inp({ webEnabled: true }), ...(over.input ?? {}) } });

const OK = { context: [ctxOk()], get: [getItem()] };
const withSources = (sources: Raw[], get: IpcStep = getItem()): ArticleCase["ipc"] => ({ context: [ctxOk({ sources })], get: [get] });

// ---- 用例 ----

const CASES: ArticleCase[] = [
  // 基本流程
  ask("基本：一份本文来源，流式回答", OK, [answer(["归知把资料保存在", "本地知识库[1]。"])]),
  ask("基本：没有任何来源也照常回答", withSources([]), [answer()]),
  ask("基本：回答分成很多小块", OK, [answer(Array.from({ length: 12 }, (_, i) => `第${i + 1}段。`))]),
  ask("选段：上下文目标里的选段进入提示（输入里的选段只给搜索词用）", { context: [ctxOk({ target: TGT({ selection: "选中的这一段 <b>&" }) })], get: [getItem()] }, [answer()], {
    input: inp({ target: TGT({ selection: "输入里的选段" }) }),
  }),
  ask("多份来源：本文片段、回复楼层、选段", withSources([
    SRC({ ordinal: 1 }),
    SRC({ ordinal: 2, title: "楼层 3", text: "楼主补充了测试数据。", target: TGT({ view: "replies", floor: 3 }), fingerprint: undefined }),
    SRC({ ordinal: 3, title: "选段", text: "选中的文字", target: TGT({ selection: "选中的文字" }), capturedAt: undefined }),
  ]), [answer()]),
  ask("问题里有 <、>、&、U+2028 与引号：序列化不转义", OK, [answer()], { input: inp({ question: "含 <b> & \u2028 的\n问题 \"引号\" \\ 反斜杠" }) }),
  ask("来源很长：请求体与补丁只记指纹", withSources([SRC({ text: { $text: [C(4000)] } })]), [answer([render([C(400)])])]),

  // 条目的复核状态
  ask("条目待复核（带原因）：警告进入补丁", withSources([SRC()], getItem({ reviewStatus: "needs_review", reviewReasons: ["OCR 缺页", "字幕不完整"] })), [answer()]),
  ask("条目待复核（没有原因）：用默认说明", withSources([SRC()], getItem({ reviewStatus: "needs_review" })), [answer()]),
  ask("条目读不到（null）：复核状态按 clear", withSources([SRC()], NO_ITEM), [answer()]),
  ask("条目标为 clear 但带原因：原因照样进入证据与提示", withSources([SRC()], getItem({ reviewStatus: "clear", reviewReasons: ["旧原因"] })), [answer()]),
  ask("联网来源类型的资料不看条目的复核状态", withSources([SRC({ ordinal: 1, kind: "web", title: "旧网页", text: "旧摘录", url: "https://example.com/old", target: undefined, fingerprint: undefined })], getItem({ reviewStatus: "needs_review", reviewReasons: ["不该影响联网来源"] })), [answer()]),

  // 证据快照
  ask("来源已带证据快照：不重新采集，文本与链接取证据里的", withSources([
    SRC({ text: "原文本（会被替换）", url: "https://example.com/old?token=zzz", evidence: EVID({ text: "证据里的文本", url: "https://example.com/e" }) }),
  ], getItem({ reviewStatus: "needs_review", reviewReasons: ["不影响已有证据的复核状态"] })), [answer()]),
  ask("两份来源：第一份已带证据快照，第二份现场采集", withSources([
    SRC({ ordinal: 1, evidence: EVID({ text: "第一份证据的文本" }) }),
    SRC({ ordinal: 2, title: "第二份", text: "第二份的正文", target: TGT({ view: "summary", sourceKind: "summary" }), fingerprint: undefined }),
  ]), [answer()]),
  ask("来源已带证据快照：证据里没有链接时来源的链接被清掉", withSources([SRC({ url: "https://example.com/old", evidence: EVID() })]), [answer()]),
  ask("来源已带证据快照且标记待复核：警告取自证据", withSources([SRC({ evidence: EVID({ reviewStatus: "needs_review", reviewReasons: ["证据里的原因"] }) })]), [answer()]),
  ask("来源文本里的链接被脱敏（凭证、敏感参数、片段、无法解析）", withSources([SRC({ text: LINKS_TEXT })]), [answer()]),
  ask("链接被 JS 的空白截断（NBSP、全角空格、U+2028 都算空白）", withSources([SRC({ text: "见 https://example.com/a\u00a0紧跟 与 https://example.com/b\u3000全角 以及 http://example.com/c\u2028行分隔符 还有 https://example.com/d?token=1\u200b零宽" })]), [answer()]),
  ask("链接被全角右括号、右方括号、尖括号与引号截断", withSources([SRC({ text: "见（https://example.com/a?key=1）和[https://example.com/b?x=2]及<https://example.com/c?sig=3>与\"https://example.com/d?auth=4\"结尾" })]), [answer()]),
  ask("来源的 url 字段被脱敏", withSources([SRC({ url: "https://user:pw@example.com/a?token=abc&page=2#frag" })]), [answer()]),
  ask("来源的 url 不是 http(s)：链接丢弃", withSources([SRC({ url: "ftp://files.example.com/a" })]), [answer()]),
  ask("来源没有指纹与采集时间：指纹取正文的 SHA-256，采集时间取当前时间", withSources([SRC({ fingerprint: undefined, capturedAt: undefined })]), [answer()]),
  ask("来源的指纹为空串：原样保留（?? 只认 undefined）", withSources([SRC({ fingerprint: "" })]), [answer()]),
  ask("来源的采集时间为 0：原样保留", withSources([SRC({ capturedAt: 0 })]), [answer()]),
  ask("来源没有 target：证据里没有 sourceId 与 sourceVersion", withSources([SRC({ target: undefined })]), [answer()]),
  ask("来源带 cleared 标记：随来源进入提示", withSources([SRC({ cleared: true })]), [answer()]),
  ask("采集时间取当前时间：换一个时钟值", withSources([SRC({ capturedAt: undefined })]), [answer()], { now: 1_800_000_000_123 }),

  // 历史
  ask("历史：只取最近三轮已完成的问答，过滤掉出错与进行中的", OK, [answer()], {
    input: inp({ history: [MSG(1), MSG(2, { status: "error" }), MSG(3), MSG(4, { status: "running" }), MSG(5), MSG(6)] }),
  }),
  ask("历史：少于三轮", OK, [answer()], { input: inp({ history: [MSG(1)] }) }),
  ask("历史：长问题与长回答被截断（1000 / 1800 个码元）", OK, [answer()], {
    input: inp({ history: [MSG(1, { question: { $text: [C(1500)] }, answer: { $text: [C(2500)] } })] }),
  }),
  ask("历史：问题与回答恰好在截断边界上", OK, [answer()], {
    input: inp({ history: [MSG(1, { question: { $text: [C(1000)] }, answer: { $text: [C(1800)] } }), MSG(2, { question: { $text: [C(1001)] }, answer: { $text: [C(1801)] } })] }),
  }),

  // 重试：沿用保存的上下文
  ask("重试：沿用保存的上下文，只核验文章仍在", { get: [getItem(), getItem()] }, [answer()], {
    input: inp({ context: CTX({ sources: [SRC({ evidence: EVID() })] }) }),
  }),
  ask("重试：保存的上下文没有证据：按现在的条目状态重新采集", { get: [getItem(), getItem({ reviewStatus: "needs_review", reviewReasons: ["现在才发现的问题"] })] }, [answer()], {
    input: inp({ context: CTX() }),
  }),
  web("重试并联网：保存的上下文里已有联网来源，新来源接着往后编号", { get: [getItem(), getItem()], search: [searchOk([WEB(1)])] }, [PLAN, answer()], {
    input: inp({
      webEnabled: true,
      context: CTX({ sources: [SRC({ evidence: EVID() }), WEB(2, { evidence: EVID({ kind: "web", sourceId: undefined, sourceVersion: undefined, title: "网页2", text: "网页2的摘录", url: "https://example.com/page2" }) })] }),
    }),
  }),
  ask("重试：文章不存在", { get: [NO_ITEM] }, [], { input: inp({ context: CTX() }) }),
  ask("重试：文章已删除", { get: [getItem({ deletedAt: 1_699_999_999_000 })] }, [], { input: inp({ context: CTX() }) }),
  ask("重试：deletedAt 为 0 也算已删除（!= null）", { get: [getItem({ deletedAt: 0 })] }, [], { input: inp({ context: CTX() }) }),
  ask("重试：核验文章时 IPC 抛错", { get: [{ throws: "IPC 断开" }] }, [], { input: inp({ context: CTX() }) }),
  ask("重试：第二次读条目时 IPC 抛错", { get: [getItem(), { throws: "IPC 断开" }] }, [], { input: inp({ context: CTX() }) }),

  // 上下文读取失败
  ask("上下文：返回失败与原因", { context: [{ result: { success: false, error: "文章已被删除" } }] }, []),
  ask("上下文：返回失败、没有原因", { context: [{ result: { success: false } }] }, []),
  ask("上下文：成功但没有 context", { context: [{ result: { success: true } }] }, []),
  ask("上下文：IPC 抛错", { context: [{ throws: "IPC 断开" }] }, []),
  ask("上下文：读取成功后读条目时 IPC 抛错", { context: [ctxOk()], get: [{ throws: "条目读取失败" }] }, []),

  // 联网查证
  web("联网成功：拟出两个搜索词，搜到两份资料（续编号）", { ...OK, search: [searchOk([WEB(1), WEB(2)])] }, [PLAN, answer()]),
  web("联网部分成功：搜索带警告", { ...OK, search: [searchOk([WEB(1)], ["第二个链接读取失败"])] }, [PLAN, answer()]),
  web("联网：搜索成功但没有来源", { ...OK, search: [searchOk([])] }, [PLAN, answer()]),
  web("联网：搜索成功但没给 sources 字段", { ...OK, search: [{ result: { success: true } }] }, [PLAN, answer()]),
  web("联网：没有来源时搜索警告照样追加", { ...OK, search: [searchOk([], ["全部被拦截"])] }, [PLAN, answer()]),
  web("联网：没有任何警告时系统提示用默认说明", { ...OK, search: [searchOk([WEB(1)])] }, [PLAN, answer()]),
  web("联网：本文待复核、被裁剪、搜索有警告：系统提示里按分号连接", {
    context: [ctxOk({ clipped: true })],
    get: [getItem({ reviewStatus: "needs_review", reviewReasons: ["缺页"] })],
    search: [searchOk([WEB(1)], ["一个链接超时"])],
  }, [PLAN, answer()]),
  web("联网：模型输出带围栏与前后缀文字，只取前两个词", { ...OK, search: [searchOk([WEB(1)])] }, [planner('好的：\n```json\n["甲","乙","丙"]\n```\n以上'), answer()]),
  web("联网：非字符串、空白与过长的搜索词被过滤", { ...OK, search: [searchOk([WEB(1)])] }, [
    planner(JSON.stringify([1, "  ", "x".repeat(401), "有效词", null, "y".repeat(400)])),
    answer(),
  ]),
  web("联网：嵌套数组里的元素不算搜索词", { ...OK, search: [searchOk([WEB(1)])] }, [planner('[["嵌套"],"有效"]'), answer()]),
  web("联网：模型回的是含数组片段的对象：取方括号之间的部分", { ...OK, search: [searchOk([WEB(1)])] }, [planner('{"queries":["甲","乙"]}'), answer()]),
  web("联网：搜索词全被过滤：未能生成有效搜索词", OK, [planner('["  ", 3]'), answer()]),
  web("联网：模型输出里没有方括号", OK, [planner("不需要搜索"), answer()]),
  web("联网：括号里是空数组", OK, [planner("[]"), answer()]),
  web("联网：右方括号在左方括号之前", OK, [planner("] 先 [ 后"), answer()]),
  web("联网：选段与上一轮问题进入搜索词请求（选段只取前 800 个码元）", { context: [ctxOk()], get: [getItem()], search: [searchOk([WEB(1)])] }, [PLAN, answer()], {
    input: inp({ webEnabled: true, target: TGT({ selection: { $text: [A(900)] } as unknown as string }), history: [MSG(1, { status: "error", question: "上一轮出错的问题" })] }),
  }),
  web("联网：搜索失败（success=false 带原因）", { ...OK, search: [searchFail("搜索服务不可用")] }, [PLAN, answer()]),
  web("联网：搜索失败、没有原因", { ...OK, search: [searchFail()] }, [PLAN, answer()]),
  web("联网：搜索的 IPC 抛错", { ...OK, search: [{ throws: "IPC 断开" }] }, [PLAN, answer()]),
  web("联网：来源里的链接被脱敏，不可用的 url 被丢弃", { ...OK, search: [searchOk([
    WEB(1, { text: LINKS_TEXT, url: "https://u:p@example.com/a?token=1&q=2#f" }),
    WEB(2, { url: "ftp://files.example.com/x" }),
  ])] }, [PLAN, answer()]),
  web("联网：来源带指纹、目标与 cleared：来源保留，证据的指纹按正文重算", { ...OK, search: [searchOk([
    WEB(1, { fingerprint: "web-fp-1", target: TGT({ itemId: "web-1", view: "snapshot" }), cleared: false }),
  ])] }, [PLAN, answer()]),
  web("联网：来源没有采集时间：证据取当前时间，来源本身不补", { ...OK, search: [searchOk([WEB(1, { capturedAt: undefined })])] }, [PLAN, answer()]),
  web("联网：拟搜索词的请求失败（HTTP 500）：记警告后仍回答", OK, [HTTP_500, answer()]),
  web("联网：拟搜索词时未配置 AI：警告之后回答同样失败", OK, [], { settings: {} }),
  ask("未配置 AI（不联网）：读完上下文后失败", OK, [], { settings: {} }),

  // 回答阶段
  ask("回答：被截断（length）：truncated 为 true，并留下日志", OK, [answer(["写到一半"], "length")]),
  ask("回答：内容为空串：由客户端报错", OK, [answer([""])]),
  ask("回答：只有空白：同样由客户端报错（askArticle 自己的空回答检查在客户端之后，实际到不了）", OK, [answer([" ", "\n"])]),
  ask("回答：请求失败（HTTP 500）", OK, [HTTP_500]),
  ask("回答：流读取中断", OK, [{ chunks: [sse(delta({ content: "半" }))], streamError: "流读取中断" }]),

  // 取消
  ask("取消：调用前信号已中止：照常读完上下文，在第一个检查点结束", OK, [], { abort: "before" }),
  ask("取消：取上下文时中止", { context: [{ ...ctxOk(), during: ABORT }], get: [getItem()] }, []),
  ask("取消：第二次读条目时中止：上下文补丁已发出，随后在回答前结束", { context: [ctxOk()], get: [{ ...getItem(), during: ABORT }] }, []),
  ask("取消：重试核验文章时中止：下一个检查点结束，不再读条目", { get: [{ ...getItem(), during: ABORT }, getItem()] }, [], { input: inp({ context: CTX({ sources: [SRC({ evidence: EVID() })] }) }) }),
  web("取消：拟搜索词的请求进行中中止", OK, [{ abortDuring: true, status: 0, ok: false, error: "已取消" }], { abort: "during" }),
  web("取消：整理搜索词的补丁之后中止", OK, [PLAN, answer()], { abortOnPatch: { step: "整理搜索词" } }),
  web("取消：联网搜索的补丁之后中止：不通知取消搜索，也不发搜索请求", { ...OK, search: [searchOk([WEB(1)])] }, [PLAN, answer()], { abortOnPatch: { step: "联网搜索与读取资料" } }),
  web("取消：搜索进行中中止：通知存储取消搜索", { ...OK, search: [{ ...searchOk([WEB(1)]), during: ABORT }] }, [PLAN, answer()]),
  web("取消：搜索抛错的同时已中止：抛出取消而不是记成警告", { ...OK, search: [{ throws: "IPC 断开", during: ABORT }] }, [PLAN, answer()]),
  web("取消：搜索返回失败的同时已中止：先判断取消", { ...OK, search: [{ ...searchFail("搜索服务不可用"), during: ABORT }] }, [PLAN, answer()]),
  ask("取消：生成回答的补丁之后中止", OK, [answer()], { abortOnPatch: { step: "生成回答" } }),
  ask("取消：回答进行中（已收到几块）中止", OK, [answer(["前半", "后半"], "stop", { abortDuring: true })], { abort: "during" }),
  ask("取消：第一个回答补丁之后中止：后面的块不再产生补丁", OK, [answer(["甲", "乙", "丙"])], { abortOnPatch: { answer: true } }),
];

// ---- 重放 ----

interface TestWindow {
  api: Record<string, unknown>;
}
const testWindow = (): TestWindow => (globalThis as unknown as { window: TestWindow }).window;

/** 展开用例数据里的 {"$text": 规格}。 */
function resolveTexts(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(resolveTexts);
  if (value !== null && typeof value === "object") {
    const record = value as Record<string, unknown>;
    if (Object.keys(record).length === 1 && "$text" in record) return render(record.$text as Spec);
    return Object.fromEntries(Object.entries(record).map(([key, entry]) => [key, resolveTexts(entry)]));
  }
  return value;
}

/** 补丁快照：顶层 undefined 记为 "$undefined"，其余经 JSON 往返；长文本只留指纹。 */
function snapshotPatch(value: Raw): Raw {
  const marked = Object.fromEntries(Object.entries(value).map(([key, entry]) => [key, entry === undefined ? "$undefined" : entry]));
  const copy = JSON.parse(JSON.stringify(marked)) as Raw;
  if (typeof copy.answer === "string") copy.answer = compactText(copy.answer);
  const compactSources = (list: unknown): void => {
    if (!Array.isArray(list)) return;
    for (const source of list as Raw[]) {
      if (typeof source.text === "string") source.text = compactText(source.text);
      const evidence = source.evidence as Raw | undefined;
      if (evidence && typeof evidence.text === "string") evidence.text = compactText(evidence.text);
    }
  };
  compactSources(copy.sources);
  compactSources((copy.context as Raw | undefined)?.sources);
  return copy;
}

async function runArticleCase(c: ArticleCase, baseline: Raw): Promise<Raw> {
  const patches: Raw[] = [];
  const calls: Raw[] = [];
  const queues: Record<string, IpcStep[]> = {};
  for (const [channel, steps] of Object.entries(c.ipc)) queues[channel] = (steps ?? []).map((step) => resolveTexts(step) as IpcStep);
  const input = resolveTexts(c.input) as AskInput;
  vi.spyOn(Date, "now").mockReturnValue(c.now ?? NOW);

  const outcome = await runFlow(
    c,
    baseline,
    async (signal, fake) => {
      const step = async (channel: Channel, record: Raw): Promise<unknown> => {
        calls.push(record);
        const next = queues[channel]?.shift();
        if (!next) throw new Error(`IPC 脚本已用尽：${channel}`);
        if (next.during) fake.controller.abort();
        if (next.throws !== undefined) throw new Error(next.throws);
        return next.result === undefined ? undefined : structuredClone(next.result);
      };

      testWindow().api = {
        ...testWindow().api,
        knowledge: { get: (id: string) => step("get", { channel: "knowledge.get", id }) },
        articleAsk: {
          context: (request: Raw) => step("context", { channel: "articleAsk.context", input: structuredClone(request) }),
          search: (request: Raw) => step("search", { channel: "articleAsk.search", input: structuredClone(request) }),
          cancelSearch: (requestId: string) => {
            calls.push({ channel: "articleAsk.cancelSearch", requestId });
            return Promise.resolve({ success: true });
          },
        },
      };

      const hook = c.abortOnPatch;
      await askArticle({
        target: input.target as never,
        question: input.question,
        history: (input.history ?? []) as never,
        webEnabled: input.webEnabled ?? false,
        requestId: input.requestId ?? "req-article-1",
        signal,
        context: input.context as never,
        patch: (value) => {
          patches.push(snapshotPatch(value as Raw));
          if (hook && ((hook.step !== undefined && value.step === hook.step) || (hook.answer && value.answer !== undefined))) {
            fake.controller.abort();
          }
        },
      });
      return null;
    },
    { abortMessage: false },
  );

  const unusedIpc: Raw = {};
  for (const [channel, steps] of Object.entries(queues)) if (steps.length) unusedIpc[channel] = steps.length;
  return { ...outcome, patches, calls, unusedIpc };
}

describe("aiflow golden：本文问答", () => {
  it("askArticle 与已提交的 golden 一致", async () => {
    const baseline = { ...useSettingsStore.getState() };
    const cases: Raw[] = [];
    try {
      for (const c of CASES) {
        const expect = await runArticleCase(c, baseline as unknown as Raw);
        cases.push({ name: c.name, settings: c.settings, abort: c.abort ?? null, script: c.script, input: c.input, ipc: c.ipc, now: c.now ?? NOW, abortOnPatch: c.abortOnPatch ?? null, expect });
        vi.restoreAllMocks();
      }
    } finally {
      useSettingsStore.setState(baseline);
    }
    writeOrCompareGolden("article-qa", { cases });
  });
});
