/**
 * aiflow 全局问答引擎的 golden：真实的 askKnowledgeBase（Agent 工具循环 + 单发兜底管线）+ 脚本化的假 QaDeps。
 * Go 侧重放见 apps/desktop-go/aiflow/qa_engine_golden_test.go；纯函数部分见 aiflow-golden-qa-pure.test.ts。
 *
 * 每个用例给出：问题与历史、对话脚本（每次 chat 调用依次取一步）、条目检索与 Wiki 检索的应答队列、
 * 条目与页面全文、目录，以及取消时机。导出的 expect 记录全部可观察行为：
 *   - calls：对依赖的每次调用（含对话的消息与选项、检索词与条数、是否透传取消信号），按发生顺序；
 *   - steps / answerTexts / evidence：onStep、onAnswerText、onEvidence 的每次回调；
 *   - answer / error：最终结果（长文本只留指纹）；
 *   - unused：脚本里没被用到的步数（多出来说明循环比预期少走了）。
 *
 * 长字符串用 {"$text": 规格} 占位（语法同 flow-kit 的 Spec）；对话的系统提示不进 golden 全文，记成 "$agent" / "$qa"。
 * 取消靠 abortOn（某类调用的第 n 次开始时取消）/ abortOnStep / abortOnText / chat 步的 abortDuring 精确落在检查点之间。
 */
import { describe, it, vi } from "vitest";
import type { QaDeps, QaSourceRef } from "../../../src/renderer/services/knowledge-ai/qa";
import { QaNoSourceError, askKnowledgeBase } from "../../../src/renderer/services/knowledge-ai/qa";
import { QA_AGENT_SYSTEM_PROMPT, QA_SYSTEM_PROMPT } from "../../../src/renderer/services/knowledge-ai/prompts";
import { normalizeWikiTitle } from "../../../src/renderer/services/knowledge-ai/wiki-compile";
import { alignDomExceptionWithChromium, writeOrCompareGolden } from "./aiflow-golden-kit";
import { A, BODY_LIMIT, C, EMOJI, L, compactText, digest, errorInfo, render, safeText, type Raw, type Spec } from "./aiflow-golden-flow-kit";

type Text = string | { $text: Spec };
const T = (spec: Spec): { $text: Spec } => ({ $text: spec });

interface Fail {
  throws: string;
}
type Hits = Raw[] | Fail;
type Pages = Record<string, Raw | null | Fail>;

interface ChatStep {
  content?: Text;
  model?: string;
  finish?: string;
  /** 传了 onDelta 时按这个码点数分块回调模型输出 */
  chunk?: number;
  /** 对话抛错；"$abort" 表示抛出取消错误 */
  throws?: string;
  /** 分块回调之后、返回之前触发取消 */
  abortDuring?: boolean;
}

type CallKind = "chat" | "searchItems" | "readItem" | "getWikiCatalog" | "searchWikiPages" | "readWikiPage";

interface EngineCase {
  name: string;
  question: Text;
  history?: { question: string; answer: Text }[];
  chat: ChatStep[];
  /** searchItems 的应答队列（每次调用取一项）；缺省为空队列 */
  items?: Hits[];
  /** readItem 按 id 取；null 表示条目不存在 */
  read?: Pages;
  /** getWikiCatalog；缺省表示没有这个依赖 */
  catalog?: Raw[] | Fail;
  /** searchWikiPages 的应答队列；缺省表示没有这个依赖 */
  wikiSearch?: Hits[];
  /** readWikiPage 按 id 取；缺省表示没有这个依赖 */
  wikiPages?: Pages;
  /** 传 onAnswerText */
  stream?: boolean;
  /** 传 onEvidence，默认传 */
  evidence?: boolean;
  abort?: "before";
  abortOn?: { call: CallKind; index: number };
  abortOnStep?: string;
  abortOnText?: boolean;
  now?: number;
}

const NOW = 1_700_000_000_000;
const QUESTION = "归知用什么技术栈？";

// ---- 夹具 ----

const act = (value: Raw): ChatStep => ({ content: JSON.stringify(value) });
const search = (query: string, over: Partial<ChatStep> = {}): ChatStep => ({ ...act({ action: "search", query }), ...over });
const read = (target: unknown, over: Partial<ChatStep> = {}): ChatStep => ({ ...act({ action: "read", target }), ...over });
const answer = (text: string, over: Partial<ChatStep> = {}): ChatStep => ({ ...act({ action: "answer", text }), ...over });
const junk = (): ChatStep => ({ content: "我不会输出 JSON" });
/** 兜底管线的纯文本回答 */
const plain = (text: Text, over: Partial<ChatStep> = {}): ChatStep => ({ content: text, ...over });
const answerOf = (spec: Spec): ChatStep => ({ content: T([L('{"action":"answer","text":"'), ...(typeof spec === "string" ? [L(spec)] : spec), L('"}')]) });

/** 条目检索命中 */
const H = (id: string, over: Raw = {}): Raw => ({ id, title: `条目${id}`, snippet: `${id} 的摘录`, ...over });
/** 条目全文 */
const IT = (title: string, content: Text, over: Raw = {}): Raw => ({ title, content, ...over });

const LONG_HINT = "命中片段的内容在这里出现";
const ITEMS: Pages = {
  a: IT("归知架构", "归知是本地优先的知识库，采用 Electron + React 构建。"),
  b: IT("采集管线", "导入队列并发为 2，支持断点恢复。"),
  c: IT("检索设计", "检索由全文与语义两路并行，再做融合排序。"),
  d: IT("备份策略", "备份使用 VACUUM INTO，不携带 AI 配置。"),
  f: IT("视频条目", "视频的简介文字。", { transcript: "口播转写：今天讲讲归知的检索。" }),
  g: IT("纯转写", "", { transcript: "只有转写的内容。" }),
  h: IT("空白条目", "  \n", { transcript: "  " }),
  i: null,
  j: IT("待复核条目", "这份资料的正文缺了一段。", { reviewStatus: "needs_review", reviewReasons: ["正文缺失", "OCR 未完成"] }),
  k: IT("待复核无原因", "这份资料待复核。", { reviewStatus: "needs_review" }),
  l: IT("带版本条目", "带 updatedAt 的条目。", { updatedAt: 1_699_999_999_123 }),
  m: IT("带链接条目", "官网 https://example.com/a?token=secret#frag 与坏链接 http://bad host/x 都要脱敏。"),
  n: IT("长文档", T([C(1500), L(LONG_HINT), C(2500)])),
  o: IT("转写为 null", "只有正文。", { transcript: null }),
  q: IT("", "标题为空的条目。"),
  p: IT("空串转写", "只有正文。", { transcript: "" }),
  r: IT("空白转写", "只有正文。", { transcript: "  " }),
  // 兜底管线的总预算 6000：前三份各 1500，第四份 1300 时剩 200（刚好够读下一份），1301 时剩 199（停止）
  e1: IT("预算边界一", T([C(1500)])),
  e2: IT("预算边界二", T([C(1500)])),
  e3: IT("预算边界三", T([C(1500)])),
  e4: IT("预算边界四", T([C(1300)])),
  e4x: IT("预算边界四加一", T([C(1301)])),
  e5: IT("预算边界五", "第五份资料的内容。"),
  l1: IT("预算文档一", T([C(2000)])),
  l2: IT("预算文档二", T([C(2000)])),
  l3: IT("预算文档三", T([C(2000)])),
  l4: IT("预算文档四", T([C(2000)])),
  l5: IT("预算文档五", T([C(2000)])),
};

const WK = (id: string, title: string, over: Raw = {}): Raw => ({
  id,
  title,
  normalizedTitle: normalizeWikiTitle(title),
  kind: "topic",
  summary: `${title}的摘要`,
  aliasesJson: null,
  manualEditedAt: null,
  updatedAt: 0,
  ...over,
});
/** Wiki 页面检索命中 */
const WH = (id: string, title: string, kind = "topic", summary: string = `${title}的摘要`): Raw => ({ id, title, kind, summary });
const SRC = (itemId: string, title: string, over: Raw = {}): Raw => ({ itemId, title, ...over });
const PAGE = (id: string, title: string, body: Text, sources: Raw[] = [], over: Raw = {}): Raw => ({
  page: { id, title, body, updatedAt: 1_700_000_000_001, ...over },
  sources,
});

const CATALOG: Raw[] = [WK("w1", "采集工作流"), WK("w2", "检索设计"), WK("w3", "发布流程")];

const ask = (name: string, chat: ChatStep[], over: Partial<EngineCase> = {}): EngineCase => ({
  name,
  question: QUESTION,
  chat,
  items: [[H("a"), H("b")]],
  read: ITEMS,
  ...over,
});

/** 6 轮工具调用（3 次检索 + 3 次阅读）：预算用尽的前奏 */
const SIX_ROUNDS: ChatStep[] = [search("q1"), read(1), search("q2"), read(1), search("q3"), read(1)];
const THREE_HITS: Hits[] = [[H("a")], [H("a")], [H("a")]];

const WIKI_BODY = "总览：见 [[检索设计]] 与 [[不存在的页]]。";
const WIKI_ENV: Partial<EngineCase> = {
  catalog: CATALOG,
  wikiSearch: [[WH("w1", "采集工作流")]],
  wikiPages: { w1: PAGE("w1", "采集工作流", WIKI_BODY, [SRC("a", "归知架构"), SRC("b", "采集管线")]) },
  items: [[]],
};

const LINK_CATALOG: Raw[] = [
  WK("p0", "自己"),
  WK("p1", "页面一"),
  WK("p2", "页面二"),
  WK("p3", "页面三"),
  WK("p4", "页面四"),
  WK("p5", "页面五"),
  WK("p6", "页面六"),
];

const FOUR_PAGES = ["w1", "w2", "w3", "w4"];

const CASES: EngineCase[] = [
  // ---- 基本路径 ----
  ask("基本：检索、阅读、回答，引用对齐到被引用的来源", [search("技术栈"), read(1), answer("归知基于 Electron 构建 [1]。")]),
  ask("基本：两份都读了，只引用第二份", [search("技术栈"), read(1), read(2), answer("采集用队列处理 [2]。")]),
  ask("基本：没有标注引用，保留全部已读来源", [search("技术栈"), read(1), read(2), answer("没有标注引用的回答。")]),
  ask("基本：引用了没读过的编号，视为没有有效引用，保留已读来源", [search("技术栈"), read(1), answer("见 [2]。")]),
  ask("基本：复合标注与全角括号", [search("技术栈"), read(1), read(2), read(3), answer("结论见【1、3】。")], { items: [[H("a"), H("b"), H("c")]] }),
  ask("基本：回答首尾空白被去掉", [search("x"), read(1), answer("  \n回答 [1]\n  ")]),
  ask("基本：finishReason 为 length 标记截断，模型名取自最后一轮", [
    search("x", { model: "m-search" }),
    read(1, { model: "m-read" }),
    answer("被截断的回答 [1]", { model: "m-final", finish: "length" }),
  ]),
  ask("基本：中间轮的 finishReason 不影响最终标记", [search("x", { finish: "length" }), read(1), answer("[1]", { finish: "stop" })]),
  ask("基本：没有注入 onEvidence 也能完成", [search("x"), read(1), answer("[1]")], { evidence: false }),

  // ---- 入参 ----
  ask("入参：问题为空", [], { question: "", items: [] }),
  ask("入参：问题只有空白（含全角空格）", [], { question: " \u3000\n ", items: [] }),
  ask("入参：问题首尾空白被去掉后进入提示", [search("x"), read(1), answer("[1]")], { question: "  归知怎么检索？ \n" }),
  // JS 的 trim 与 Go 的 unicode.IsSpace 在这两个字符上相反：U+FEFF 在 JS 里是空白，U+0085 不是
  ask("入参：问题首尾的 BOM（U+FEFF）按 JS 的 trim 被去掉", [search("x"), read(1), answer("[1]")], { question: "\uFEFF归知怎么检索？\uFEFF" }),
  ask("入参：问题首尾的 U+0085 不是 JS 的空白，原样保留", [search("x"), read(1), answer("[1]")], { question: "\u0085归知怎么检索？\u0085" }),
  ask("历史：只取最近 3 轮", [search("x"), read(1), answer("[1]")], {
    history: [
      { question: "第一问", answer: "第一答" },
      { question: "第二问", answer: "第二答" },
      { question: "第三问", answer: "第三答" },
      { question: "第四问", answer: "第四答" },
    ],
  }),
  ask("历史：空数组", [search("x"), read(1), answer("[1]")], { history: [] }),
  ask("历史：不足 3 轮全部保留", [search("x"), read(1), answer("[1]")], {
    history: [
      { question: "第一问", answer: "第一答" },
      { question: "第二问", answer: "第二答" },
    ],
  }),
  ask("历史：回答恰好 400 个码元不截断", [search("x"), read(1), answer("[1]")], { history: [{ question: "问", answer: T([C(400)]) }] }),
  ask("历史：回答 401 个码元时截断", [search("x"), read(1), answer("[1]")], { history: [{ question: "问", answer: T([C(401)]) }] }),
  ask("历史：截断点落在代理对中间时少取一个码元", [search("x"), read(1), answer("[1]")], {
    history: [{ question: "问", answer: T([C(399), L(EMOJI), C(10)]) }],
  }),

  // ---- 阅读 ----
  ask("阅读：视频条目附口播转写稿", [search("x"), read(1), answer("[1]")], { items: [[H("f")]] }),
  ask("阅读：正文为空只有转写稿也可读", [search("x"), read(1), answer("[1]")], { items: [[H("g")]] }),
  ask("阅读：转写稿为 null 按没有转写稿处理", [search("x"), read(1), answer("[1]")], { items: [[H("o")]] }),
  ask("阅读：转写稿为空串按没有转写稿处理，不拼接标记", [search("x"), read(1), answer("[1]")], { items: [[H("p")]] }),
  ask("阅读：转写稿只有空白但不是空串，仍然拼接", [search("x"), read(1), answer("[1]")], { items: [[H("r")]] }),
  ask("阅读：正文与转写稿都只有空白，提示为空且没有证据，随后改读别的", [search("x"), read(1), answer("直接回答"), search("y"), read(2), answer("[2]")], {
    items: [[H("h")], [H("a")]],
  }),
  ask("阅读：条目已不存在（读取返回 null）", [search("x"), read(1), answer("直接回答"), search("y"), read(2), answer("[2]")], {
    items: [[H("i")], [H("a")]],
  }),
  ask("阅读：条目标题为空", [search("x"), read(1), answer("[1]")], { items: [[H("q", { title: "" })]] }),
  ask("阅读：编号不存在", [search("x"), read(99), read(1), answer("[1]")]),
  ask("阅读：编号 0、负数与小数（小数向零取整）", [search("x"), read(0), read(-1), read(1.9), answer("[1]")]),
  ask("阅读：编号写成数字字符串", [search("x"), read("2"), answer("[2]")]),
  ask("阅读：编号字符串里夹中文", [search("x"), read("第 2 条"), answer("[2]")]),
  ask("阅读：编号超大，提示按 JS 数字格式", [search("x"), { content: '{"action":"read","target":1e21}' }, read(1), answer("[1]")]),
  ask("阅读：编号溢出为 Infinity，整个动作无法解析", [search("x"), { content: '{"action":"read","target":1e999}' }, read(1), answer("[1]")]),
  ask("阅读：窗口取命中片段附近", [search("x"), read(1), answer("[1]")], { items: [[H("n", { matchText: LONG_HINT })]] }),
  ask("阅读：命中片段缺失时从头截取", [search("x"), read(1), answer("[1]")], { items: [[H("n")]] }),
  ask("阅读：同一文档再次命中，用最新的定位线索", [search("x"), search("y"), read(1), answer("[1]")], {
    read: { ...ITEMS, n: IT("长文档", T([C(1500), L("第一处命中线索的内容"), C(1500), L("第二处命中线索的内容"), C(1500)])) },
    items: [[H("n", { matchText: "第一处命中线索的内容" })], [H("n", { matchText: "第二处命中线索的内容" })]],
  }),
  ask("阅读：后一次命中没有线索时沿用之前的", [search("x"), search("y"), read(1), answer("[1]")], {
    items: [[H("n", { matchText: LONG_HINT })], [H("n")]],
  }),
  ask("阅读：重复阅读同一编号，证据重新留存", [search("x"), read(1), read(1), answer("[1]")]),
  ask("阅读：待复核条目，原因进入工具结果与证据", [search("x"), read(1), answer("[1]")], { items: [[H("j")]] }),
  ask("阅读：待复核但没有原因", [search("x"), read(1), answer("[1]")], { items: [[H("k")]] }),
  ask("阅读：updatedAt 进入证据版本", [search("x"), read(1), answer("[1]")], { items: [[H("l")]] }),
  ask("阅读：链接脱敏", [search("x"), read(1), answer("[1]")], { items: [[H("m")]] }),
  ask("阅读：同一次检索重复命中同一条目只登记一次", [search("x"), read(1), answer("[1]")], {
    items: [[H("a"), H("a", { title: "另一个标题" })]],
  }),
  ask("检索：摘录换行压成空格并截到 80", [search("x"), read(1), answer("[1]")], {
    items: [[H("a", { snippet: T([L("第一行\r\n第二行\n"), A(200)]) })]],
  }),
  ask("检索：无命中后换词检索", [search("无结果"), search("有结果"), read(1), answer("[1]")], { items: [[], [H("a")]] }),
  ask("检索：连续两次无命中追加系统提示", [search("q1"), search("q2"), search("q3"), read(1), answer("[1]")], { items: [[], [], [H("a")]] }),
  ask("检索：有命中后计数清零，再次无命中不追加提示", [search("q1"), search("q2"), read(1), search("q3"), answer("[1]")], {
    items: [[], [H("a")], []],
  }),
  ask("检索：检索词里有引号与换行", [search('他说"归知"\n换行'), read(1), answer("[1]")]),

  // ---- 纪律与预算 ----
  ask("纪律：未读先答被纠正后完成", [answer("我直接回答"), search("x"), read(1), answer("[1]")]),
  ask("纪律：读过之后回答内容为空", [search("x"), read(1), answer("   "), answer("正式回答 [1]")]),
  ask("纪律：输出无法解析被纠正", [junk(), search("x"), read(1), answer("[1]")]),
  ask("纪律：两次违规后仍可完成", [junk(), junk(), search("x"), read(1), answer("[1]")]),
  ask("纪律：三次违规回退单发管线", [junk(), junk(), junk(), plain("兜底回答 [1]")]),
  ask("纪律：不同种类的违规共用同一个计数", [answer("没读就答"), junk(), answer("还是没读"), plain("兜底回答 [2]")]),
  ask("预算：6 轮工具调用后强制回答，模型还想检索则计违规", [...SIX_ROUNDS, search("还想搜"), answer("[1]")], { items: THREE_HITS }),
  ask("预算：强制回答轮直接回答不算违规", [...SIX_ROUNDS, answer("[1]")], { items: THREE_HITS }),
  ask("预算：强制回答轮输出无法解析也计违规", [...SIX_ROUNDS, junk(), answer("[1]")], { items: THREE_HITS }),
  ask("预算：强制回答轮违规超限回退单发管线", [...SIX_ROUNDS, search("a"), search("b"), search("c"), plain("兜底回答 [1]")], {
    items: [...THREE_HITS, [H("a"), H("b")]],
  }),
  ask("预算：5 轮时尚未强制，第 6 轮之后才强制", [search("q1"), read(1), search("q2"), read(1), search("q3"), search("q4"), answer("[1]")], {
    items: [[H("a")], [H("a")], [H("a")], [H("a")]],
  }),

  // ---- Wiki ----
  ask("Wiki：命中页面，阅读后登记关联页面与来源条目", [search("采集"), read(1), answer("[1]")], WIKI_ENV),
  ask("Wiki：来源条目接着阅读", [search("采集"), read(1), read(3), answer("[1][3]")], WIKI_ENV),
  ask("Wiki：关联页面接着阅读，它自己的来源条目继续登记", [search("采集"), read(1), read(2), answer("[1][2]")], {
    ...WIKI_ENV,
    wikiPages: {
      w1: PAGE("w1", "采集工作流", WIKI_BODY, [SRC("a", "归知架构"), SRC("b", "采集管线")]),
      w2: PAGE("w2", "检索设计", "检索页的正文，回链 [[采集工作流]]。", [SRC("c", "检索设计")]),
    },
  }),
  ask("Wiki：待复核来源进入证据与提示", [search("采集"), read(1), answer("[1]")], {
    ...WIKI_ENV,
    wikiPages: {
      w1: PAGE("w1", "采集工作流", WIKI_BODY, [SRC("a", "归知架构", { reviewStatus: "needs_review", reviewReasons: ["忽略"] }), SRC("b", "采集管线", { reviewStatus: "clear" })]),
    },
  }),
  ask("Wiki：页面已不存在（读取返回 null），改读条目", [search("采集"), read(1), search("y"), read(2), answer("[2]")], {
    ...WIKI_ENV,
    wikiPages: { w1: null },
    wikiSearch: [[WH("w1", "采集工作流")], []],
    items: [[], [H("a")]],
  }),
  ask("Wiki：没有页面读取依赖按页面已不存在处理", [search("采集"), read(1), search("y"), read(2), answer("[2]")], {
    ...WIKI_ENV,
    wikiPages: undefined,
    wikiSearch: [[WH("w1", "采集工作流")], []],
    items: [[], [H("a")]],
  }),
  ask("Wiki：没有页面检索依赖，退回目录子串计分", [search("采集工作流是什么"), read(1), answer("[1]")], {
    ...WIKI_ENV,
    wikiSearch: undefined,
    catalog: [WK("w1", "采集工作流", { summary: "导入队列与断点恢复" }), WK("w2", "检索设计")],
  }),
  ask("Wiki：目录子串计分最多取前 4 个", [search("页面"), read(1), answer("[1]")], {
    wikiSearch: undefined,
    catalog: LINK_CATALOG,
    wikiPages: { p1: PAGE("p1", "页面一", "页面一的正文") },
    items: [[]],
  }),
  ask("Wiki：没有目录依赖，页面检索仍可用，正文链接无法解析", [search("采集"), read(1), answer("[1]")], { ...WIKI_ENV, catalog: undefined }),
  ask("Wiki：既没有目录也没有页面检索，只有条目检索", [search("技术栈"), read(1), answer("[1]")], { catalog: undefined }),
  ask("Wiki：出链最多 4 个、来源最多 4 个，自链接与重复链接被排除", [search("x"), read(1), answer("[1]")], {
    catalog: LINK_CATALOG,
    wikiSearch: [[WH("p0", "自己")]],
    wikiPages: {
      p0: PAGE("p0", "自己", "[[页面一]] [[页面二]] [[页面二|别名]] [[自己]] [[页面三]] [[页面四]] [[页面五]] [[页面六]]", [
        SRC("a", "来源甲"),
        SRC("b", "来源乙"),
        SRC("c", "来源丙"),
        SRC("d", "来源丁"),
        SRC("a", "来源戊"),
        SRC("b", "来源己"),
      ]),
    },
    items: [[]],
  }),
  ask("Wiki：关联页面与检索命中是同一页，编号复用", [search("x"), read(1), answer("[1]")], {
    catalog: CATALOG,
    wikiSearch: [[WH("w1", "采集工作流"), WH("w2", "检索设计")]],
    wikiPages: { w1: PAGE("w1", "采集工作流", "见 [[检索设计]]") },
    items: [[]],
  }),
  ask("Wiki：页面正文超过 2000 个码元时截断", [search("x"), read(1), answer("[1]")], {
    ...WIKI_ENV,
    wikiPages: { w1: PAGE("w1", "采集工作流", T([C(2400)])) },
  }),
  ask("Wiki：目录读取失败直接报错", [search("x")], { catalog: { throws: "目录不可用" } }),
  ask("Wiki：页面检索失败直接报错", [search("x")], { ...WIKI_ENV, wikiSearch: [{ throws: "页面检索失败" }] }),
  ask("Wiki：页面读取失败直接报错", [search("x"), read(1)], { ...WIKI_ENV, wikiPages: { w1: { throws: "页面读取失败" } } }),
  ask("Wiki：类别标签", [search("x"), read(1), answer("[1]")], {
    ...WIKI_ENV,
    wikiSearch: [[WH("w1", "页面甲", "topic"), WH("w2", "页面乙", "entity"), WH("w3", "页面丙", "concept")]],
    wikiPages: { w1: PAGE("w1", "页面甲", "页面甲的正文") },
    items: [[]],
  }),
  ask("Wiki：命中在前条目在后，编号连续", [search("x"), read(3), answer("[3]")], {
    ...WIKI_ENV,
    wikiSearch: [[WH("w1", "采集工作流"), WH("w2", "检索设计")]],
    items: [[H("a")]],
  }),
  ask("Wiki：命中没有摘要", [search("x"), read(1), answer("[1]")], { ...WIKI_ENV, wikiSearch: [[WH("w1", "采集工作流", "topic", "")]] }),
  ask("Wiki：页面正文为空，读取结果去掉末尾的空白", [search("x"), read(1), answer("[1]")], {
    ...WIKI_ENV,
    wikiPages: { w1: PAGE("w1", "采集工作流", "") },
  }),
  ask("Wiki：同一页重复命中只登记一次", [search("x"), read(1), answer("[1]")], {
    ...WIKI_ENV,
    wikiSearch: [[WH("w1", "采集工作流"), WH("w1", "改名后的标题")]],
  }),
  ask("Wiki：四个长页面读完，转写超过 8000 触发强制回答", [search("x"), read(1), read(2), read(3), read(4), answer("[1]")], {
    catalog: CATALOG,
    wikiSearch: [FOUR_PAGES.map((id, index) => WH(id, `长页面${index + 1}`))],
    wikiPages: Object.fromEntries(FOUR_PAGES.map((id, index) => [id, PAGE(id, `长页面${index + 1}`, T([C(2400)]))])),
    items: [[]],
  }),
  ask("Wiki：转写超过预算后模型仍要阅读，计违规", [search("x"), read(1), read(2), read(3), read(4), read(1), answer("[1]")], {
    catalog: CATALOG,
    wikiSearch: [FOUR_PAGES.map((id, index) => WH(id, `长页面${index + 1}`))],
    wikiPages: Object.fromEntries(FOUR_PAGES.map((id, index) => [id, PAGE(id, `长页面${index + 1}`, T([C(2400)]))])),
    items: [[]],
  }),

  // ---- 单发兜底管线 ----
  ask("兜底：基本", [junk(), junk(), junk(), plain("根据资料，答案是 Electron [1]。")]),
  ask("兜底：检索文本带上一轮的问题", [junk(), junk(), junk(), plain("回答 [1]")], {
    history: [
      { question: "更早的问题", answer: "更早的回答" },
      { question: "上一个问题", answer: "上一个回答" },
    ],
  }),
  ask("兜底：上一轮问题为空时检索文本只有新问题", [junk(), junk(), junk(), plain("回答 [1]")], { history: [{ question: "", answer: "上一个回答" }] }),
  ask("兜底：没有命中，抛出无相关资料", [junk(), junk(), junk()], { items: [[]] }),
  ask("兜底：命中的条目全是空内容，抛出无相关资料", [junk(), junk(), junk()], { items: [[H("i"), H("h")]] }),
  ask("兜底：跳过空条目，序号接续", [junk(), junk(), junk(), plain("回答 [1]")], { items: [[H("i"), H("a")]] }),
  ask("兜底：总预算耗尽即停，后面的条目不再读取", [junk(), junk(), junk(), plain("回答")], {
    items: [[H("l1"), H("l2"), H("l3"), H("l4"), H("l5")]],
  }),
  ask("兜底：剩余预算恰好 200 时仍读取下一份", [junk(), junk(), junk(), plain("回答")], {
    items: [[H("e1"), H("e2"), H("e3"), H("e4"), H("e5")]],
  }),
  ask("兜底：剩余预算 199 时停止读取", [junk(), junk(), junk(), plain("回答")], {
    items: [[H("e1"), H("e2"), H("e3"), H("e4x"), H("e5")]],
  }),
  ask("兜底：命中片段定位窗口", [junk(), junk(), junk(), plain("回答 [1]")], { items: [[H("n", { matchText: LONG_HINT })]] }),
  ask("兜底：待复核条目提示", [junk(), junk(), junk(), plain("回答 [1]")], { items: [[H("j"), H("a")]] }),
  ask("兜底：视频条目带转写稿", [junk(), junk(), junk(), plain("回答 [1]")], { items: [[H("f")]] }),
  ask("兜底：回答引用其中一份，来源只保留被引用的", [junk(), junk(), junk(), plain("只引用第二份 [2]")]),
  ask("兜底：回答没有引用，保留全部来源", [junk(), junk(), junk(), plain("没有引用")]),
  ask("兜底：引用的编号不存在，保留全部来源", [junk(), junk(), junk(), plain("见 [9]")]),
  ask("兜底：回答原样返回，不去首尾空白", [junk(), junk(), junk(), plain("  回答 [1]  \n")]),
  ask("兜底：finishReason 为 length 标记截断", [junk(), junk(), junk(), plain("没说完 [1]", { model: "m-fallback", finish: "length" })]),
  ask("兜底：流式回答逐块累计", [junk(), junk(), junk(), plain("流式回答第一段，第二段 [1]", { chunk: 4 })], { stream: true }),
  ask("兜底：开启流式但对话没有分块，不回调", [junk(), junk(), junk(), plain("整段回答 [1]")], { stream: true }),
  ask("兜底：对话失败向上抛出", [junk(), junk(), junk(), { throws: "兜底对话失败" }]),
  ask("兜底：条目读取失败向上抛出", [junk(), junk(), junk()], { read: { ...ITEMS, a: { throws: "条目读取失败" } } }),
  ask("兜底：条目检索失败向上抛出", [junk(), junk(), junk()], { items: [{ throws: "检索失败" }] }),
  ask("兜底：目录与页面检索只在循环里用，兜底不再使用", [junk(), junk(), junk(), plain("回答 [1]")], {
    ...WIKI_ENV,
    items: [[H("a")]],
  }),

  // ---- 流式（Agent 循环）----
  ask(
    "流式：读过资料之后才开启，回答轮逐块累计",
    [search("x", { chunk: 4 }), read(1, { chunk: 4 }), answer("归知基于 Electron [1]", { chunk: 4 })],
    { stream: true },
  ),
  ask(
    "流式：读过之后的检索轮开启流式，但动作不是回答，不吐字",
    [search("x", { chunk: 4 }), read(1, { chunk: 4 }), search("再查", { chunk: 4 }), read(2, { chunk: 4 }), answer("回答 [1][2]", { chunk: 4 })],
    { stream: true, items: [[H("a")], [H("b")]] },
  ),
  ask(
    "流式：未读先答的回合不开启流式",
    [answer("未读先答", { chunk: 2 }), search("x", { chunk: 2 }), read(1, { chunk: 2 }), answer("正式回答 [1]", { chunk: 2 })],
    { stream: true },
  ),
  ask("流式：不传 onAnswerText 时不开启流式", [search("x", { chunk: 4 }), read(1, { chunk: 4 }), answer("回答 [1]", { chunk: 4 })]),
  ask(
    "流式：转义跨块",
    [search("x"), read(1), { content: '{"action":"answer","text":"第一行\\n第二行 \\"引号\\" \\u4e2d [1]"}', chunk: 2 }],
    { stream: true },
  ),
  ask(
    "流式：回答 JSON 前有围栏与前缀文字",
    [search("x"), read(1), { content: '好的：\n```json\n{"action":"answer","text":"围栏里的回答 [1]"}\n```', chunk: 3 }],
    { stream: true },
  ),
  ask(
    "流式：回答轮只有空白（违规）也会先吐出空白，随后被后一轮覆盖",
    [search("x"), read(1), answer("   ", { chunk: 1 }), answer("正式回答 [1]", { chunk: 3 })],
    { stream: true },
  ),
  ask(
    "流式：强制回答轮也流式",
    [...SIX_ROUNDS.map((step) => ({ ...step, chunk: 5 })), answer("预算后的回答 [1]", { chunk: 4 })],
    { stream: true, items: THREE_HITS },
  ),
  ask(
    "流式：回答首尾空白，流出原样，结果去空白",
    [search("x"), read(1), answer("  带空白 [1]  ", { chunk: 3 })],
    { stream: true },
  ),
  ask(
    "流式：长回答",
    [search("x"), read(1), { ...answerOf([C(600), L(" [1]")]), chunk: 50 }],
    { stream: true },
  ),

  // ---- 取消 ----
  ask("取消：开始前已取消，没有目录依赖，循环入口直接抛出", [search("x")], { abort: "before" }),
  ask("取消：开始前已取消，有目录依赖，目录照常读取", [search("x")], { abort: "before", catalog: CATALOG }),
  ask("取消：对话开始时取消，对话照常返回，返回后抛出且不解析输出", [search("x")], { abortOn: { call: "chat", index: 0 } }),
  ask("取消：对话抛出取消错误，原样向上", [{ throws: "$abort" }]),
  ask("取消：条目检索进行中取消，检索照常完成，下一轮入口抛出", [search("x"), read(1)], { abortOn: { call: "searchItems", index: 0 } }),
  ask("取消：阅读进行中取消，阅读完成并回调证据，下一轮入口抛出", [search("x"), read(1), answer("[1]")], { abortOn: { call: "readItem", index: 0 } }),
  ask("取消：检索的步骤回调里取消，检索照常进行", [search("架构"), read(1)], { abortOnStep: "检索：架构" }),
  ask("取消：阅读的步骤回调里取消，阅读照常进行并回调证据", [search("x"), read(1), answer("[1]")], { abortOnStep: "阅读：条目a" }),
  ask("取消：Wiki 目录读取中取消，目录读完后循环入口抛出", [search("x")], { abortOn: { call: "getWikiCatalog", index: 0 }, catalog: CATALOG }),
  ask("取消：Wiki 页面检索中取消，条目检索照常进行", [search("采集"), read(1)], { ...WIKI_ENV, abortOn: { call: "searchWikiPages", index: 0 } }),
  ask("取消：Wiki 页面读取中取消，读完后下一轮入口抛出", [search("采集"), read(1), answer("[1]")], { ...WIKI_ENV, abortOn: { call: "readWikiPage", index: 0 } }),
  ask("取消：回答轮对话返回前取消，不返回答案", [search("x"), read(1), answer("[1]", { abortDuring: true })]),
  ask("取消：兜底管线的检索进行中取消，检索返回后立即抛出", [junk(), junk(), junk(), plain("不会用到")], { abortOn: { call: "searchItems", index: 0 } }),
  ask("取消：兜底管线检索进行中取消且没有命中，先判取消", [junk(), junk(), junk()], { items: [[]], abortOn: { call: "searchItems", index: 0 } }),
  ask("取消：兜底管线读取中取消，读完后检查", [junk(), junk(), junk(), plain("不会用到")], { abortOn: { call: "readItem", index: 0 } }),
  ask("取消：兜底管线读取中取消且没有可用来源，先判取消", [junk(), junk(), junk()], { items: [[H("i")]], abortOn: { call: "readItem", index: 0 } }),
  ask("取消：兜底管线对话进行中取消，对话返回后不检查，照常返回回答", [junk(), junk(), junk(), plain("照常返回的回答 [1]")], {
    abortOn: { call: "chat", index: 3 },
  }),
  ask("取消：流式回答的第一块之后取消，分块照常回调，返回后抛出", [search("x"), read(1), answer("取消前的回答 [1]", { chunk: 4 })], {
    stream: true,
    abortOnText: true,
  }),
  ask("取消：兜底流式回答的第一块之后取消，分块照常回调，返回后不检查", [junk(), junk(), junk(), plain("取消前的回答 [1]", { chunk: 4 })], {
    stream: true,
    abortOnText: true,
  }),
  // ---- 错误 ----
  ask("错误：对话失败向上抛出", [{ throws: "模型服务不可用" }]),
  ask("错误：条目检索失败向上抛出", [search("x")], { items: [{ throws: "检索失败" }] }),
  ask("错误：条目读取失败向上抛出，之前的证据回调保留", [search("x"), read(1), read(2)], {
    read: { ...ITEMS, b: { throws: "条目读取失败" } },
  }),
  ask("错误：第二轮对话才失败", [search("x"), { throws: "第二轮失败" }]),
];

// ---- 运行 ----

/** 展开用例数据里的 {"$text": 规格}。 */
function expand(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(expand);
  if (value !== null && typeof value === "object") {
    const record = value as Record<string, unknown>;
    if (Object.keys(record).length === 1 && "$text" in record) return render(record.$text as Spec);
    return Object.fromEntries(Object.entries(record).map(([key, entry]) => [key, expand(entry)]));
  }
  return value;
}

const str = (value: Text | undefined): string => (value === undefined ? "" : typeof value === "string" ? value : render(value.$text));

function pieces(text: string, size: number): string[] {
  const points = Array.from(text);
  const out: string[] = [];
  for (let i = 0; i < points.length; i += size) out.push(points.slice(i, i + size).join(""));
  return out;
}

/**
 * 提示词比普通文本长得多：2000 个码元以内保留原文便于肉眼核对；更长的留指纹，
 * 再附最后 300 个码点（动作轨迹的最新几条与「[系统]」纠正说明都在末尾，用来肉眼确认走到了哪个分支）。
 */
const compactPrompt = (text: string): unknown =>
  text.length <= BODY_LIMIT ? safeText(text) : { ...digest(text), last: Array.from(text).slice(-300).join("") };

const compactMessage = (message: { role: string; content: string }): Raw => ({
  role: message.role,
  content: message.content === QA_AGENT_SYSTEM_PROMPT ? "$agent" : message.content === QA_SYSTEM_PROMPT ? "$qa" : compactPrompt(message.content),
});

function compactRef(ref: QaSourceRef): Raw {
  const copy = JSON.parse(JSON.stringify(ref)) as Raw;
  const evidence = copy.evidence as Raw | undefined;
  if (evidence && typeof evidence.text === "string") evidence.text = compactText(evidence.text);
  return copy;
}

const isFail = (value: unknown): value is Fail => value !== null && typeof value === "object" && "throws" in (value as Raw);

async function runEngine(c: EngineCase): Promise<Raw> {
  alignDomExceptionWithChromium();
  vi.spyOn(Date, "now").mockReturnValue(c.now ?? NOW);
  const x = expand(c) as EngineCase;
  const controller = new AbortController();
  if (x.abort === "before") controller.abort();

  const calls: Raw[] = [];
  const steps: string[] = [];
  const answerTexts: unknown[] = [];
  const evidence: Raw[][] = [];
  const counts = new Map<CallKind, number>();
  const chatQueue = [...x.chat];
  const itemQueue = [...(x.items ?? [])];
  const wikiQueue = [...(x.wikiSearch ?? [])];

  const begin = (kind: CallKind, detail: Raw): void => {
    const index = counts.get(kind) ?? 0;
    counts.set(kind, index + 1);
    calls.push({ call: kind, ...detail });
    if (x.abortOn && x.abortOn.call === kind && x.abortOn.index === index) controller.abort();
  };
  const lookup = (table: Pages | undefined, id: string, what: string): Raw | null => {
    if (!table || !(id in table)) throw new Error(`脚本里没有${what} ${id}`);
    const value = table[id];
    if (isFail(value)) throw new Error(value.throws);
    return value === null ? null : (structuredClone(value) as Raw);
  };
  const queued = (queue: Hits[], what: string): Raw[] => {
    const next = queue.shift();
    if (next === undefined) throw new Error(`脚本已用尽：${what}`);
    if (!Array.isArray(next)) throw new Error(next.throws);
    return structuredClone(next);
  };

  const deps: QaDeps = {
    chat: async (messages, options) => {
      begin("chat", {
        messages: messages.map(compactMessage),
        temperature: options.temperature,
        maxTokens: options.maxTokens,
        streaming: options.onDelta !== undefined,
        signal: options.signal === controller.signal,
      });
      const step = chatQueue.shift();
      if (!step) throw new Error("脚本已用尽：chat");
      const content = str(step.content);
      if (options.onDelta && step.chunk) for (const piece of pieces(content, step.chunk)) options.onDelta(piece);
      if (step.abortDuring) controller.abort();
      if (step.throws !== undefined) throw step.throws === "$abort" ? new DOMException("已取消", "AbortError") : new Error(step.throws);
      return { content, model: step.model ?? "test-model", ...(step.finish ? { finishReason: step.finish } : {}) };
    },
    searchItems: async (query, limit, signal) => {
      begin("searchItems", { query, limit, signal: signal === controller.signal });
      return queued(itemQueue, "searchItems") as never;
    },
    readItem: async (id) => {
      begin("readItem", { id });
      return lookup(x.read, id, "条目") as never;
    },
  };
  if (x.catalog !== undefined) {
    deps.getWikiCatalog = async () => {
      begin("getWikiCatalog", {});
      if (isFail(x.catalog)) throw new Error(x.catalog.throws);
      return structuredClone(x.catalog as Raw[]) as never;
    };
  }
  if (x.wikiSearch !== undefined) {
    deps.searchWikiPages = async (query, limit) => {
      begin("searchWikiPages", { query, limit });
      return queued(wikiQueue, "searchWikiPages") as never;
    };
  }
  if (x.wikiPages !== undefined) {
    deps.readWikiPage = async (id) => {
      begin("readWikiPage", { id });
      return lookup(x.wikiPages, id, "页面") as never;
    };
  }
  if (x.evidence !== false) deps.onEvidence = (refs) => void evidence.push(refs.map(compactRef));

  let outcome: Raw;
  try {
    const result = await askKnowledgeBase(
      str(x.question),
      x.history?.map((turn) => ({ question: turn.question, answer: str(turn.answer) })),
      deps,
      (description) => {
        steps.push(description);
        if (x.abortOnStep !== undefined && description === x.abortOnStep) controller.abort();
      },
      controller.signal,
      x.stream
        ? (text) => {
            answerTexts.push(compactText(text));
            if (x.abortOnText) controller.abort();
          }
        : undefined,
    );
    outcome = {
      answer: {
        text: compactText(result.text),
        model: result.model,
        usedFallback: result.usedFallback,
        truncated: result.truncated,
        sources: result.sources.map(compactRef),
      },
    };
  } catch (error) {
    outcome = { error: { ...errorInfo(error, { abortMessage: false }), ...(error instanceof QaNoSourceError ? { noSource: true } : {}) } };
  }

  const unused: Raw = {};
  if (chatQueue.length) unused.chat = chatQueue.length;
  if (itemQueue.length) unused.items = itemQueue.length;
  if (wikiQueue.length) unused.wikiSearch = wikiQueue.length;
  return { ...outcome, calls, steps, answerTexts, evidence, unused };
}

// ---- 转写预算的边界（总长恰好 8000 / 8001 码元）----
// 转写稿总长 = 各条目码元数之和，取决于提示词里每条的固定文案，手算容易错一位。
// 所以先跑一次探针量出实际长度，再反推末页正文长度；用例跑完后再从提示词里复核一次，
// 将来文案变了边界对不上，这里会直接报错，而不是悄悄让用例失去边界意义。

const BUDGET_NOTE = "[系统] 检索预算已用尽，请立即基于已阅读的资料给出最终回答（action=answer），资料不足就说明缺少什么。";
const NO_ACTION = "（尚未执行任何动作）";
const TRANSCRIPT_BUDGET = 8000;

const promptLength = (call: Raw): number => {
  const content = (call.messages as Raw[])[1]!.content as string | { length: number };
  return typeof content === "string" ? content.length : content.length;
};

/** 从第 chatNo 次对话的提示反推当时转写稿的总码元数（entries 是提示里转写条目的个数）。 */
function transcriptUnits(expect: Raw, chatNo: number, entries: number): number {
  const chats = (expect.calls as Raw[]).filter((call) => call.call === "chat");
  const skeleton = promptLength(chats[0]!) - NO_ACTION.length;
  return promptLength(chats[chatNo]!) - skeleton - (entries - 1);
}

/** 四个页面读完（加上检索共 5 条转写）；末页正文长度可调，之后再读一次，观察是否已被强制回答。 */
const budgetCase = (name: string, lastBody: number): EngineCase =>
  ask(name, [search("x"), read(1), read(2), read(3), read(4), read(1), answer("[1]")], {
    catalog: CATALOG,
    wikiSearch: [FOUR_PAGES.map((id, index) => WH(id, `长页面${index + 1}`))],
    wikiPages: Object.fromEntries(FOUR_PAGES.map((id, index) => [id, PAGE(id, `长页面${index + 1}`, T([C(index === 3 ? lastBody : 2000)]))])),
    items: [[]],
  });

async function budgetBoundaryCases(): Promise<{ at: EngineCase; over: EngineCase }> {
  const probeBody = 500;
  const probed = transcriptUnits(await runEngine(budgetCase("探针", probeBody)), 5, 5);
  vi.restoreAllMocks();
  const lastBody = probeBody + (TRANSCRIPT_BUDGET - probed);
  if (lastBody <= 0 || lastBody > 2000) throw new Error(`末页正文长度 ${lastBody} 超出可调范围（页面读取上限 2000）`);
  return {
    at: budgetCase("预算：转写正好 8000 码元时不强制，下一次阅读照常", lastBody),
    over: budgetCase("预算：转写 8001 码元时强制回答，下一次阅读计违规", lastBody + 1),
  };
}

describe("aiflow golden：全局问答引擎", () => {
  it("askKnowledgeBase 与已提交的 golden 一致", async () => {
    const boundary = await budgetBoundaryCases();
    const cases: Raw[] = [];
    const names = new Set<string>();
    for (const c of [...CASES, boundary.at, boundary.over]) {
      if (names.has(c.name)) throw new Error(`用例重名：${c.name}`);
      names.add(c.name);
      const expect = await runEngine(c);
      cases.push({ ...c, now: c.now ?? NOW, expect });
      vi.restoreAllMocks();

      // 复核边界：恰好 8000 时第 6 次对话的提示里没有预算说明（5 条转写）；8001 时多一条预算说明（6 条），扣掉它还是 8001
      if (c === boundary.at) {
        const units = transcriptUnits(expect, 5, 5);
        if (units !== TRANSCRIPT_BUDGET) throw new Error(`边界用例的转写稿应恰好 ${TRANSCRIPT_BUDGET} 码元，实际 ${units}`);
      }
      if (c === boundary.over) {
        const units = transcriptUnits(expect, 5, 6) - BUDGET_NOTE.length;
        if (units !== TRANSCRIPT_BUDGET + 1) throw new Error(`越界用例的转写稿应恰好 ${TRANSCRIPT_BUDGET + 1} 码元，实际 ${units}`);
      }
    }
    writeOrCompareGolden("qa-engine", { cases });
  });
});
