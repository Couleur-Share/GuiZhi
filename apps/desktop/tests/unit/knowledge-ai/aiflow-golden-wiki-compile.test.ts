/**
 * aiflow Wiki 域前半段的 golden：页面草稿的解析与清洗（wiki-compile）、正文标题剥离（wiki-body）、证据链接脱敏（evidence）。
 * 都是纯函数，按用例逐项比对。Go 侧重放见 apps/desktop-go/aiflow/wiki_compile_golden_test.go；
 * 约定见 aiflow-golden-kit.ts / aiflow-golden-flow-kit.ts。驱动器（wiki-v2）见 aiflow-golden-wiki-driver.test.ts。
 *
 * 长字符串用 {"$text": 规格} 占位（规格语法同 flow-kit 的 Spec），golden 里存规格与指纹，Go 侧按同一生成器重建；
 * 输出里超过 300 个码元的文本只记指纹。孤立代理项不入 golden（见 flow-kit 的 safeText）。
 */
import { describe, it } from "vitest";
import type { WikiCatalogEntry } from "@guizhi/shared/types";
import { evidenceUrl } from "@guizhi/shared/utils/evidence";
import { stripDuplicateTitleHeading } from "@guizhi/shared/utils/wiki-body";
import {
  buildLinkResolver,
  cleanWikiLinks,
  normalizeWikiTitle,
  parseAliases,
  parseWikiResponse,
  sanitizePages,
  wikiKindLabel,
  type WikiPageDraft,
} from "../../../src/renderer/services/knowledge-ai/wiki-compile";
import { WIKI_COMPILER_VERSION } from "@guizhi/shared/types/wiki-compiler";
import { writeOrCompareGolden } from "./aiflow-golden-kit";
import { A, C, EMOJI, L, compactText, digest, render, type Digest, type Raw, type Spec } from "./aiflow-golden-flow-kit";

// ---- {"$text": 规格} 展开 ----

/** 遍历顺序：数组按下标，对象按键的码元序；每个占位按此顺序记一条指纹（Go 侧同序，键都是 ASCII）。 */
function expand(value: unknown, digests: Digest[]): unknown {
  if (Array.isArray(value)) return value.map((entry) => expand(entry, digests));
  if (value !== null && typeof value === "object") {
    const record = value as Record<string, unknown>;
    if (Object.keys(record).length === 1 && "$text" in record) {
      const text = render(record.$text as Spec);
      digests.push(digest(text));
      return text;
    }
    const expanded: Record<string, unknown> = {};
    for (const key of Object.keys(record).sort()) expanded[key] = expand(record[key], digests);
    return expanded;
  }
  return value;
}

const T = (spec: Spec): { $text: Spec } => ({ $text: spec });

// ---- normalizeWikiTitle ----

const NORMALIZE_INPUTS: string[] = [
  "",
  "   ",
  "  知识  管理 ",
  "React Hooks",
  "a\tb\nc\r\nd",
  "\u00a0A\u3000b\u2003C\ufeff",
  "\u2028x\u2029y",
  "straße",
  "ǆ ǈ ǌ",
  "ﬁ ﬂ ﬃ",
  "ŉ",
  "ΐ ΰ",
  "ς σ Σ",
  "ı i İ I",
  "ǰ ẖ ẗ",
  "Ａｂｃ　全角",
  "emoji 😀 mix",
  "𠀀 扩展汉字",
  "ａｂｃ ＡＢＣ",
  "ß ẞ",
];

// ---- parseAliases ----

const ALIAS_INPUTS: Array<string | null> = [
  null,
  "",
  "  ",
  "[]",
  '["a","b"]',
  '["a",1,null,"b",true,["n"],{"k":"v"}]',
  "{}",
  '"str"',
  "123",
  "not json",
  '["未闭合"',
  '[ "中文" , "😀" ]',
];

// ---- parseWikiResponse ----

const PARSE_INPUTS: Array<{ name: string; text: string }> = [
  { name: "规范 JSON 一页", text: '{"pages":[{"title":"归知","kind":"entity","summary":"s","aliases":[],"body":"b"}]}' },
  { name: "围栏包裹与前后缀文字", text: '好的：\n```json\n{"pages":[{"title":"归知","kind":"entity","summary":"s","aliases":[],"body":"b"}]}\n```\n以上。' },
  { name: "不是 JSON", text: "不是 JSON" },
  { name: "空串", text: "" },
  { name: "缺 pages 字段", text: '{"foo":1}' },
  { name: "pages 不是数组", text: '{"pages":"x"}' },
  { name: "pages 是对象", text: '{"pages":{"title":"x"}}' },
  { name: "空数组是合法结果", text: '{"pages":[]}' },
  { name: "前缀里的花括号让解析失败", text: '说明 {示例} 之后 {"pages":[{"title":"A","body":"b"}]}' },
  { name: "只有右花括号", text: "}" },
  { name: "只有左花括号", text: "{" },
  { name: "右花括号在左花括号之前", text: "} {" },
  { name: "一对空花括号", text: "{}" },
  { name: "元素类型混杂", text: '{"pages":[1,"a",null,[1],true,{"title":"T","body":"B"}]}' },
  { name: "重复键以后者为准", text: '{"pages":[],"pages":[{"title":"x","body":"y"}]}' },
  { name: "尾随逗号非法", text: '{"pages":[{"title":"x",}]}' },
  { name: "Unicode 与转义", text: '{"pages":[{"title":"中文😀","body":"\\u00e9\\n\\t\\"q\\""}]}' },
  { name: "多个对象取首尾花括号", text: '{"a":1} {"pages":[]}' },
  { name: "嵌套对象", text: '{"pages":[{"title":"T","body":"b","extra":{"x":[1,2,{"y":3}]}}]}' },
  { name: "前后空白", text: '\n\n  {"pages":[]}  \n' },
];

// ---- sanitizePages ----

const page = (over: Raw): Raw => ({ title: "页面", body: "正文", ...over });

interface SanitizeCase {
  name: string;
  pages: unknown[];
}

const SANITIZE_CASES: SanitizeCase[] = [
  {
    name: "过滤空标题与空正文，批内去重，上限 4 页",
    pages: [
      { title: "页面A", kind: "topic", summary: "s", body: "内容" },
      { title: "", body: "无标题被过滤" },
      { title: "页面A", body: "重复标题被过滤" },
      { title: "页面B", body: "" },
      { title: "页面C", kind: "entity", summary: "多行\n第二行", body: "c" },
      { title: "页面D", body: "d" },
      { title: "页面E", body: "e" },
      { title: "页面F", body: "f" },
    ],
  },
  { name: "空数组", pages: [] },
  {
    name: "类别解析：大小写、空白与非法值",
    pages: [
      page({ title: "一", kind: "Entity" }),
      page({ title: "二", kind: "  CONCEPT " }),
      page({ title: "三", kind: "unknown" }),
      page({ title: "四", kind: 5 }),
    ],
  },
  { name: "类别缺省与 null", pages: [page({ title: "一", kind: null }), page({ title: "二" }), page({ title: "三", kind: "topic" })] },
  {
    name: "标题净化：非字符串、空白、首尾空格",
    pages: [page({ title: 123 }), page({ title: "   " }), page({ title: "  带空格的标题  " }), page({ title: null }), page({ title: ["数组"] })],
  },
  {
    name: "标题去重按规范化形式：大小写与空白折叠",
    pages: [page({ title: "Abc Def" }), page({ title: "ABC   DEF" }), page({ title: " abc def " }), page({ title: "abc-def" })],
  },
  { name: "标题恰好 120 个码元", pages: [page({ title: T([C(120)]) })] },
  { name: "标题 121 个码元：截到 120", pages: [page({ title: T([C(121)]) })] },
  { name: "标题切口落在代理对中间：退一位", pages: [page({ title: T([C(119), L(EMOJI), C(5)]) })] },
  { name: "标题切口落在代理对之后：保留整字", pages: [page({ title: T([C(118), L(EMOJI), C(5)]) })] },
  { name: "正文恰好 8000 个码元", pages: [page({ body: T([C(8000)]) })] },
  { name: "正文 8001 个码元：截到 8000", pages: [page({ body: T([C(8001)]) })] },
  { name: "正文切口落在代理对中间", pages: [page({ body: T([C(7999), L(EMOJI), C(10)]) })] },
  { name: "正文非字符串按空：整页跳过", pages: [page({ title: "一", body: 123 }), page({ title: "二", body: null }), page({ title: "三", body: { a: 1 } }), page({ title: "四" })] },
  { name: "正文只有空白：跳过", pages: [page({ body: " \n\t " })] },
  { name: "正文首行与标题同名的一级标题被剥掉", pages: [page({ title: "页面A", body: "# 页面A\n\n正文第一段\n\n## 小节\n内容" })] },
  { name: "正文首行同名标题（全角括号与强调符）被剥掉", pages: [page({ title: "INP（下次绘制交互）", body: "## **INP** (下次绘制交互)\n正文" })] },
  { name: "正文只有同名标题：剥掉后为空，整页跳过", pages: [page({ title: "页面A", body: "# 页面A\n\n\n" })] },
  { name: "正文中间的同名标题不动", pages: [page({ title: "页面A", body: "引言\n\n# 页面A\n\n后文" })] },
  { name: "摘要：多行取首行并去空白", pages: [page({ title: "一", summary: "  第一行  \n第二行" }), page({ title: "二", summary: "\n\n前面有空行\n后面" }), page({ title: "三", summary: "带回车\r\n第二行" })] },
  { name: "摘要：非字符串与缺省", pages: [page({ title: "一", summary: 7 }), page({ title: "二", summary: null }), page({ title: "三" })] },
  { name: "摘要恰好 300 与 301 个码元", pages: [page({ title: "一", summary: T([C(300)]) }), page({ title: "二", summary: T([C(301)]) })] },
  { name: "摘要切口落在代理对中间", pages: [page({ title: "一", summary: T([C(299), L(EMOJI), C(5)]) })] },
  {
    name: "别名净化：去空白、去重、剔除与标题同名的、上限 5 个",
    pages: [page({ title: "归知", aliases: ["GuiZhi", "guizhi", "GuiZhi", "归知", "a", "b", "c", "d"] })],
  },
  {
    name: "别名：非字符串、空白、过长与规范化后等于标题",
    pages: [page({ title: "Alpha Beta", aliases: [1, null, "", "   ", "  keep  ", T([A(60)]), T([A(61)]), "ALPHA   beta", "alpha beta", ["x"], { a: 1 }, true] })],
  },
  { name: "别名恰好 60 个码元（含代理对）", pages: [page({ title: "T", aliases: [T([C(58), L(EMOJI)]), T([C(59), L(EMOJI)])] })] },
  { name: "别名不是数组：按无别名", pages: [page({ title: "一", aliases: "单个" }), page({ title: "二", aliases: { a: 1 } }), page({ title: "三", aliases: null })] },
  { name: "别名全部被过滤：aliasesJson 为 null", pages: [page({ title: "一", aliases: ["", "一", " 一 "] })] },
  {
    name: "别名 JSON 序列化：引号、反斜杠、换行、尖括号、行分隔符与中文",
    pages: [page({ title: "T", aliases: ['带"引号"', "反斜杠\\x", "换行\n尾", "<b>&", "\u2028行分隔\u2029段分隔", "中文😀", "\u0001控制"] })],
  },
  { name: "非对象元素：数字、字符串、数组、布尔都按空标题跳过", pages: [1, "a", [1, 2], true, 0, "", page({ title: "有效" })] },
  { name: "null 元素：读 .title 抛 TypeError", pages: [page({ title: "先有效" }), null, page({ title: "后有效" })] },
  { name: "已满 4 页后的 null 元素不会被读到", pages: [page({ title: "一" }), page({ title: "二" }), page({ title: "三" }), page({ title: "四" }), null] },
  { name: "第一个元素就是 null", pages: [null] },
  { name: "对象里的额外字段被忽略", pages: [page({ title: "一", extra: { x: 1 }, id: 5 })] },
];

// ---- buildLinkResolver / cleanWikiLinks ----

const entry = (title: string, over: Partial<WikiCatalogEntry> = {}): WikiCatalogEntry => ({
  id: `page-${title}`,
  title,
  normalizedTitle: normalizeWikiTitle(title),
  kind: "topic",
  summary: "",
  aliasesJson: null,
  manualEditedAt: null,
  updatedAt: 0,
  ...over,
});

const draft = (title: string, over: Partial<WikiPageDraft> = {}): WikiPageDraft => ({
  title,
  normalizedTitle: normalizeWikiTitle(title),
  kind: "topic",
  summary: "",
  body: "",
  aliasesJson: null,
  ...over,
});

interface LinkCase {
  name: string;
  catalog: WikiCatalogEntry[];
  batch: WikiPageDraft[];
  bodies: string[];
}

const BASE_CATALOG = [entry("知识管理", { aliasesJson: '["KM","PKM"]' }), entry("Electron")];

const LINK_CASES: LinkCase[] = [
  {
    name: "白名单内链接保留并归一目标标题（大小写差异的显示文字保留）",
    catalog: BASE_CATALOG,
    batch: [],
    bodies: ["参考 [[知识管理]] 与 [[electron]]。", "[[ELECTRON]] 与 [[ Electron ]] 与 [[electron|桌面框架]]"],
  },
  { name: "别名链接重写为规范标题（保留显示文字）", catalog: BASE_CATALOG, batch: [], bodies: ["详见 [[PKM|个人知识管理]]。", "[[km]] [[Pkm]] [[KM|KM]]"] },
  { name: "白名单外的链接降级为纯文本", catalog: BASE_CATALOG, batch: [], bodies: ["这个 [[不存在的页面]] 会降级。", "[[不存在|显示文字]] 与 [[另一个]]"] },
  {
    name: "空目标与空白目标",
    catalog: BASE_CATALOG,
    batch: [],
    bodies: ["[[|只有显示]]", "[[ ]]", "[[ |x]]", "[[知识管理|]]", "[[知识管理|知识管理]]", "[[知识管理| 知识管理 ]]", "前[[]]后"],
  },
  { name: "多个相同目标只记一次，目标按首次出现排序", catalog: BASE_CATALOG, batch: [], bodies: ["[[Electron]] [[知识管理]] [[electron]] [[PKM]] [[Electron|E]]"] },
  { name: "括号嵌套与相邻", catalog: BASE_CATALOG, batch: [], bodies: ["[[[知识管理]]]", "[[知识管理]][[Electron]]", "[[知识管理] ]", "[ [知识管理] ]", "[[知识\n管理]]"] },
  {
    name: "目标内含换行与多余空白：规范化后命中",
    catalog: [entry("知识 管理")],
    batch: [],
    bodies: ["[[知识\n   管理]]", "[[ 知识\t管理 ]]", "[[知识管理]]"],
  },
  {
    name: "标题优先于别名；批内页优先于目录页",
    catalog: [entry("目录页", { aliasesJson: '["共用名","批内标题"]' }), entry("共用名")],
    batch: [draft("批内标题", { aliasesJson: '["目录页别名","共用名"]' })],
    bodies: ["[[共用名]] [[批内标题]] [[目录页别名]] [[目录页]]"],
  },
  {
    name: "别名冲突先到先得：批内别名先于目录别名",
    catalog: [entry("甲", { aliasesJson: '["同名别名"]' })],
    batch: [draft("乙", { aliasesJson: '["同名别名"]' })],
    bodies: ["[[同名别名]]"],
  },
  {
    name: "别名 JSON 异常：非法、非数组、含非字符串",
    catalog: [entry("甲", { aliasesJson: "not json" }), entry("乙", { aliasesJson: '{"a":1}' }), entry("丙", { aliasesJson: '["有效",1,null]' })],
    batch: [draft("丁", { aliasesJson: "" })],
    bodies: ["[[有效]] [[1]] [[null]] [[丁]]"],
  },
  {
    name: "替换串里的 $ 不展开",
    catalog: [entry("价格$&说明"), entry("$1 与 $$")],
    batch: [],
    bodies: ["[[价格$&说明]] [[$1 与 $$]] [[价格$&说明|显示$`]]"],
  },
  {
    name: "目录标题带大小写与 Unicode 特殊字符",
    catalog: [entry("Straße"), entry("ǆ")],
    batch: [],
    bodies: ["[[STRASSE]] [[straße]] [[Ǆ]] [[ǅ]]"],
  },
  { name: "没有链接的正文原样返回", catalog: BASE_CATALOG, batch: [], bodies: ["", "普通正文 [x] [[ 与 ]] ]]", "单个 [中括号] 与 [[未闭合"] },
  {
    name: "显示文字含竖线：只在第一个竖线处切分",
    catalog: BASE_CATALOG,
    batch: [],
    bodies: ["[[Electron|a|b]]", "[[知识管理||x]]", "[[nope|a|b]]"],
  },
];

// ---- stripDuplicateTitleHeading ----

interface StripCase {
  name: string;
  body: Spec;
  title: string;
}

const STRIP_CASES: StripCase[] = [
  { name: "一级标题同名：剥掉并去掉紧随的空行", body: "# 页面A\n\n正文", title: "页面A" },
  { name: "任意级别的标题都算（1 到 6 个井号）", body: "###### 页面A\n正文", title: "页面A" },
  { name: "七个井号不是标题", body: "####### 页面A\n正文", title: "页面A" },
  { name: "标题文字不同：不动", body: "# 另一个标题\n正文", title: "页面A" },
  { name: "第一个非空行不是标题：不动", body: "引言\n# 页面A\n正文", title: "页面A" },
  { name: "标题前有空行与空白行", body: "\n  \n# 页面A\n\n正文", title: "页面A" },
  { name: "全角与半角括号视为相同", body: "# INP (下次绘制交互)\n正文", title: "INP（下次绘制交互）" },
  { name: "全角方括号与半角方括号视为相同", body: "# 【重点】说明\n正文", title: "[重点]说明" },
  { name: "强调符与反引号被忽略", body: "# **页面** _A_ `x`\n正文", title: "页面 A x" },
  { name: "忽略大小写", body: "# react hooks\n正文", title: "React Hooks" },
  { name: "标题里的空白被整个忽略", body: "# 页 面  A\n正文", title: "页面A" },
  { name: "CRLF 换行", body: "# 页面A\r\n\r\n正文\r\n第二行", title: "页面A" },
  { name: "标题之后没有正文：剥完为空串", body: "# 页面A", title: "页面A" },
  { name: "标题之后只有空行：剥完为空串", body: "# 页面A\n\n  \n", title: "页面A" },
  { name: "标题为空或只有空白：不动", body: "# 页面A\n正文", title: "  " },
  { name: "正文为空：不动", body: "", title: "页面A" },
  { name: "正文只有空白行：不动", body: "  \n\n", title: "页面A" },
  { name: "井号后没有空白不是标题", body: "#页面A\n正文", title: "页面A" },
  { name: "井号后只有制表符也算标题", body: "#\t页面A\n正文", title: "页面A" },
  { name: "井号后是不换行空格也算标题（JS 的 \\s 含 NBSP）", body: "#\u00a0页面A\n正文", title: "页面A" },
  { name: "标题只有井号与空白：文字为空，不等于标题", body: "# \n正文", title: "页面A" },
  { name: "封闭式井号不算同名", body: "# 页面A #\n正文", title: "页面A" },
  { name: "行内的行分隔符让 . 不匹配：不动", body: "# 页面A\u2028续\n正文", title: "页面A\u2028续" },
  { name: "行内的单独回车让 . 不匹配：不动", body: "# 页面A\r续\n正文", title: "页面A\r续" },
  { name: "缩进的标题行：先 trim 再匹配，仍被剥掉", body: "   # 页面A   \n正文", title: "页面A" },
  { name: "只剥一次：连续两个同名标题只剥第一个", body: "# 页面A\n# 页面A\n正文", title: "页面A" },
  { name: "标题与正文之间多个空行全部去掉", body: "# 页面A\n\n\n\n正文\n\n尾", title: "页面A" },
  { name: "长正文（保留尾部）", body: [L("# 页面A\n\n"), C(5000)], title: "页面A" },
];

// ---- evidenceUrl ----

const EVIDENCE_INPUTS: string[] = [
  "",
  "https://example.com",
  "https://example.com/path?x=1",
  "https://user:pass@example.com/a",
  "https://user@example.com/a",
  "https://example.com/a#frag",
  "https://example.com/?token=abc&x=1",
  "https://example.com/?x=1&api_key=SECRET",
  "https://example.com/?Authorization=Bearer+abc&y=2",
  "https://example.com/?sig=1&z=3",
  "https://example.com/?sigx=1",
  "https://example.com/?signature=abc",
  "https://example.com/?X-Amz-Credential=AKIA&X-Amz-Signature=abc&keep=1",
  "https://example.com/?x-goog-signature=abc",
  "https://example.com/?password=1&secret=2&credential=3",
  "https://example.com/?monkey=1&turkey=2&plain=3",
  // JS 非 Unicode 模式的 /i 不会把开尔文符号 K（U+212A）、长 s（U+017F）折叠成 ASCII 字母
  "https://example.com/?to\u212Aen=1&sig=3",
  "https://example.com/?\u017Fig=1&x=2",
  "https://example.com/?\u017Fig=1&sig=2",
  "https://example.com/?token=1",
  "https://example.com/?a=1&&b=%E4%B8%AD+x",
  "https://example.com/?a=b c&token=1&d=中",
  "https://example.com/?a=1&token=2&a=3",
  "https://example.com/?TOKEN=1&Key=2",
  "https://example.com/?=1&token=2&x",
  "https://example.com/?",
  "https://example.com/?#",
  "https://EXAMPLE.com:443/",
  "http://EXAMPLE.com:80/x",
  "http://example.com:8080/x",
  "https://例え.jp/パス?q=値",
  "https://example.com/a/../b/./c",
  "https://example.com/a b/c d",
  "https://example.com/中文?q=中文",
  " https://example.com/trim ",
  "https://exa\tmple.com/\nx",
  "HTTPS://EXAMPLE.COM/Path",
  "https:\\\\example.com\\x\\y",
  "http://0x7f.1/",
  "http://127.1/",
  "http://[::1]:8080/x",
  "http://[2001:DB8::1]/",
  "https://example.com./",
  "ftp://example.com/",
  "javascript:alert(1)",
  "mailto:a@b.c",
  "file:///C:/x",
  "data:text/plain,hello",
  "blob:https://example.com/uuid",
  "not a url",
  "//example.com/x",
  "http://",
  "http://a b.com/",
  "https://:80/",
  "https://example.com:99999/",
  "http://exa%41mple.com/",
  "https://example.com/%zz?q=%zz",
  "https://example.com/?q=%E4%B8",
  "https://example.com/?q=%E4%B8&token=1",
];

// ---- 输出整形 ----

const compactDraft = (d: WikiPageDraft): Raw => ({
  title: compactText(d.title),
  normalizedTitle: compactText(d.normalizedTitle),
  kind: d.kind,
  summary: compactText(d.summary),
  body: compactText(d.body),
  aliasesJson: d.aliasesJson === null ? null : compactText(d.aliasesJson),
});

describe("aiflow golden：Wiki 页面草稿的解析与清洗", () => {
  it("normalizeWikiTitle / parseAliases / parseWikiResponse / wikiKindLabel 与已提交的 golden 一致", () => {
    writeOrCompareGolden("wiki-parse", {
      version: WIKI_COMPILER_VERSION,
      normalize: NORMALIZE_INPUTS.map((input) => ({ input, output: normalizeWikiTitle(input) })),
      aliases: ALIAS_INPUTS.map((input) => ({ input, output: parseAliases(input) })),
      parse: PARSE_INPUTS.map((c) => ({ name: c.name, text: c.text, output: parseWikiResponse(c.text) })),
      kindLabels: { topic: wikiKindLabel("topic"), entity: wikiKindLabel("entity"), concept: wikiKindLabel("concept") },
    });
  });

  it("sanitizePages 与已提交的 golden 一致", () => {
    const cases = SANITIZE_CASES.map((c) => {
      const digests: Digest[] = [];
      const pages = expand(c.pages, digests) as unknown[];
      let output: Raw;
      try {
        output = { drafts: sanitizePages(pages as never).map(compactDraft) };
      } catch (error) {
        output = { error: error instanceof Error ? error.message : String(error) };
      }
      return { name: c.name, pages: c.pages, inputDigests: digests, output };
    });
    writeOrCompareGolden("wiki-sanitize", { cases });
  });

  it("buildLinkResolver / cleanWikiLinks 与已提交的 golden 一致", () => {
    const cases = LINK_CASES.map((c) => {
      const resolver = buildLinkResolver(c.catalog, c.batch);
      // 按码元序排序再写：JS Map 的遍历序是插入序，对拍只关心内容
      const entries = [...resolver.entries()].sort(([left], [right]) => (left < right ? -1 : left > right ? 1 : 0));
      return {
        name: c.name,
        catalog: c.catalog,
        batch: c.batch,
        bodies: c.bodies,
        resolver: entries,
        outputs: c.bodies.map((body) => cleanWikiLinks(body, resolver)),
      };
    });
    writeOrCompareGolden("wiki-links", { cases });
  });

  it("stripDuplicateTitleHeading 与已提交的 golden 一致", () => {
    const cases = STRIP_CASES.map((c) => {
      const body = render(c.body);
      return { name: c.name, body: c.body, title: c.title, input: digest(body), output: compactText(stripDuplicateTitleHeading(body, c.title)) };
    });
    writeOrCompareGolden("wiki-strip", { cases });
  });

  it("evidenceUrl 与已提交的 golden 一致", () => {
    writeOrCompareGolden("wiki-evidence", {
      cases: EVIDENCE_INPUTS.map((input) => ({ input, output: evidenceUrl(input) ?? null })),
    });
  });
});
