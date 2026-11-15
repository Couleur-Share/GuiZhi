/**
 * aiflow 全局问答纯函数的 golden：qa.ts 的 tryParseAction / extractCitedOrdinals / locateNormalized /
 * extractReadWindow / matchWikiPages / resolveBodyLinks。都是纯函数，按用例逐项比对。
 * Go 侧重放见 apps/desktop-go/aiflow/qa_golden_test.go；约定见 aiflow-golden-kit.ts / aiflow-golden-flow-kit.ts。
 * 循环本身（askKnowledgeBase）见 aiflow-golden-qa-engine.test.ts。
 *
 * 长字符串用 {"$text": 规格} 占位（规格语法同 flow-kit 的 Spec），Go 侧按同一生成器重建；
 * 输出里超过 300 个码元的文本只记指纹。孤立代理项不入 golden（见 flow-kit 的 safeText）。
 */
import { describe, it } from "vitest";
import type { WikiCatalogEntry } from "@guizhi/shared/types";
import {
  extractCitedOrdinals,
  extractReadWindow,
  locateNormalized,
  matchWikiPages,
  resolveBodyLinks,
  tryParseAction,
} from "../../../src/renderer/services/knowledge-ai/qa";
import { normalizeWikiTitle } from "../../../src/renderer/services/knowledge-ai/wiki-compile";
import { writeOrCompareGolden } from "./aiflow-golden-kit";
import { C, EMOJI, L, ascii, compactText, render, type Raw, type Spec } from "./aiflow-golden-flow-kit";

const T = (spec: Spec): { $text: Spec } => ({ $text: spec });

/** 把 {"$text": 规格} 展开成文本（遍历顺序无关，这里不需要指纹）。 */
function expand(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(expand);
  if (value !== null && typeof value === "object") {
    const record = value as Record<string, unknown>;
    if (Object.keys(record).length === 1 && "$text" in record) return render(record.$text as Spec);
    return Object.fromEntries(Object.entries(record).map(([key, entry]) => [key, expand(entry)]));
  }
  return value;
}

// ---- tryParseAction ----

type Text = string | { $text: Spec };

interface ParseCase {
  name: string;
  text: Text;
}

const act = (action: unknown, rest: Raw = {}): string => JSON.stringify({ action, ...rest });

const PARSE_CASES: ParseCase[] = [
  // 规范写法
  { name: "search", text: act("search", { query: "归知架构" }) },
  { name: "read", text: act("read", { target: 3 }) },
  { name: "answer", text: act("answer", { text: "归知基于 Electron 构建 [1]。" }) },
  // 围栏与前后缀
  { name: "代码围栏", text: "```json\n" + act("search", { query: "x" }) + "\n```" },
  { name: "前后缀文字", text: "好的，我来搜索：" + act("search", { query: "x" }) + " 以上。" },
  { name: "前缀里有花括号，解析失败", text: "说明 {示例} 之后 " + act("search", { query: "x" }) },
  { name: "两个对象，取首尾花括号后解析失败", text: act("search", { query: "a" }) + " " + act("read", { target: 1 }) },
  { name: "数组里的对象，取到内层对象", text: "[" + act("search", { query: "x" }) + "]" },
  { name: "空串", text: "" },
  { name: "只有左花括号", text: "{" },
  { name: "只有右花括号", text: "}" },
  { name: "右花括号在左花括号之前", text: "} {" },
  { name: "一对空花括号：没有 action", text: "{}" },
  { name: "前后空白", text: "\n\n  " + act("search", { query: "x" }) + "  \n" },
  // action 的写法
  { name: "action 大写并带空白", text: act(" SEARCH ", { query: "q" }) },
  { name: "action Read 首字母大写", text: act("Read", { target: 2 }) },
  { name: "action ANSWER", text: act("ANSWER", { text: "t" }) },
  { name: "action 是单元素数组（String(['search'])）", text: act(["search"], { query: "q" }) },
  { name: "action 是嵌套数组", text: act([["read"]], { target: 1 }) },
  { name: "action 是多元素数组", text: act(["search", "read"], { query: "q" }) },
  { name: "action 是含 null 的数组", text: act([null], { query: "q" }) },
  { name: "action 是数字", text: act(1, { query: "q" }) },
  { name: "action 是 true", text: act(true, { query: "q" }) },
  { name: "action 是 null", text: act(null, { query: "q" }) },
  { name: "action 是对象", text: act({ name: "search" }, { query: "q" }) },
  { name: "没有 action", text: JSON.stringify({ query: "q" }) },
  { name: "未知 action", text: act("lookup", { query: "q" }) },
  { name: "action 为空串", text: act("", { query: "q" }) },
  { name: "重复键以后者为准", text: '{"action":"search","action":"read","target":2}' },
  { name: "__proto__ 键不影响解析", text: '{"__proto__":{"action":"search"},"action":"read","target":1}' },
  // search 的 query
  { name: "query 首尾空白被去掉", text: act("search", { query: "  归知  架构  " }) },
  { name: "query 缺失", text: act("search") },
  { name: "query 为空串", text: act("search", { query: "" }) },
  { name: "query 只有空白", text: act("search", { query: " \u00a0\u3000\n" }) },
  { name: "query 是数字", text: act("search", { query: 123 }) },
  { name: "query 是数组", text: act("search", { query: ["a"] }) },
  { name: "query 含转义与 emoji", text: act("search", { query: `a\n"b"\t${EMOJI}` }) },
  { name: "query 用 \\u 转义写中文", text: '{"action":"search","query":"\\u4e2d\\u6587"}' },
  { name: "query 里有花括号", text: act("search", { query: "{x} 与 }" }) },
  // read 的 target
  { name: "target 整数", text: act("read", { target: 1 }) },
  { name: "target 小数向零取整", text: act("read", { target: 3.9 }) },
  { name: "target 负小数向零取整", text: act("read", { target: -2.5 }) },
  { name: "target 为 0", text: act("read", { target: 0 }) },
  { name: "target 为 -0", text: '{"action":"read","target":-0}' },
  { name: "target 科学计数法", text: '{"action":"read","target":1e3}' },
  { name: "target 超过 1e21（数字格式带指数）", text: '{"action":"read","target":1e21}' },
  { name: "target 溢出为 Infinity：不是有限数字，不当字符串处理", text: '{"action":"read","target":1e999}' },
  { name: "target 负溢出", text: '{"action":"read","target":-1e999}' },
  { name: "target 下溢为 0", text: '{"action":"read","target":1e-999}' },
  { name: "target 数字字符串", text: act("read", { target: "3" }) },
  { name: "target 字符串带空白", text: act("read", { target: " 3 " }) },
  { name: "target 字符串里夹中文（取数字）", text: act("read", { target: "第 3 条" }) },
  { name: "target 字符串多段数字拼接", text: act("read", { target: "a1b2" }) },
  { name: "target 字符串无数字", text: act("read", { target: "abc" }) },
  { name: "target 空串", text: act("read", { target: "" }) },
  { name: "target 全角数字不算数字", text: act("read", { target: "１２" }) },
  { name: "target 前导零", text: act("read", { target: "007" }) },
  { name: "target 23 位数字串（数字格式带指数）", text: act("read", { target: "99999999999999999999999" }) },
  { name: "target 400 位数字串（溢出为 Infinity）", text: T([L('{"action":"read","target":"'), L("9".repeat(400)), L('"}')]) },
  { name: "target 是 true", text: act("read", { target: true }) },
  { name: "target 是 null", text: act("read", { target: null }) },
  { name: "target 是数组", text: act("read", { target: [3] }) },
  { name: "target 是对象", text: act("read", { target: { n: 3 } }) },
  { name: "target 缺失", text: act("read") },
  // answer 的 text
  { name: "answer 文本原样保留（含首尾空白）", text: act("answer", { text: "  带空白的回答\n" }) },
  { name: "answer 文本为空串", text: act("answer", { text: "" }) },
  { name: "answer 文本只有空白", text: act("answer", { text: "   " }) },
  { name: "answer 文本是数字", text: act("answer", { text: 1 }) },
  { name: "answer 文本是 null", text: act("answer", { text: null }) },
  { name: "answer 文本缺失", text: act("answer") },
  { name: "answer 文本含转义", text: act("answer", { text: 'line1\nline2 "引用" \\ 反斜杠 ' + EMOJI }) },
  { name: "answer 长文本", text: T([L('{"action":"answer","text":"'), C(400), L('"}')]) },
  // JSON 语法
  { name: "尾随逗号非法", text: '{"action":"search","query":"x",}' },
  { name: "单引号非法", text: "{'action':'search','query':'x'}" },
  { name: "键没有引号非法", text: '{action:"search",query:"x"}' },
  { name: "注释非法", text: '{"action":"search", /* c */ "query":"x"}' },
  { name: "无关字段溢出为 Infinity 不影响解析", text: '{"action":"search","query":"x","extra":1e999}' },
  { name: "无关字段含深层嵌套", text: '{"action":"search","query":"x","extra":{"a":[1,2,{"b":[[[]]]}]}}' },
  { name: "无关字段是 NaN 写法，非法", text: '{"action":"search","query":"x","extra":NaN}' },
  { name: "数字前导零非法", text: '{"action":"read","target":01}' },
  { name: "数字写成 1.，非法", text: '{"action":"read","target":1.}' },
  { name: "根是字符串（没有花括号）", text: '"search"' },
  { name: "字符串值里有未转义换行，非法", text: '{"action":"search","query":"a\nb"}' },
];

// ---- extractCitedOrdinals ----

interface CitedCase {
  name: string;
  text: string;
  valid: number[];
}

const CITED_CASES: CitedCase[] = [
  { name: "单个与多个", text: "见 [1] 和 [2]。", valid: [1, 2, 3] },
  { name: "无标注", text: "没有任何引用。", valid: [1, 2] },
  { name: "全角方括号", text: "结论【1】成立，【2】也是。", valid: [1, 2] },
  { name: "复合标注：顿号", text: "见[1、3]。", valid: [1, 2, 3] },
  { name: "复合标注：半角逗号", text: "见[1,2]。", valid: [1, 2, 3] },
  { name: "复合标注：全角逗号", text: "见[1，2，3]。", valid: [1, 2, 3] },
  { name: "复合标注：全角方括号", text: "见【1、2】。", valid: [1, 2] },
  { name: "括号内空白", text: "见 [ 1 ] 与 [ 2 , 3 ]。", valid: [1, 2, 3] },
  { name: "括号内不间断空格与全角空格", text: "见[\u00a01\u3000,\u20032\ufeff]。", valid: [1, 2] },
  { name: "括号内换行", text: "见[\n1\n]。", valid: [1] },
  { name: "无效编号被忽略", text: "见 [9] 和 [1]。", valid: [1] },
  { name: "编号 0", text: "见 [0]。", valid: [0, 1] },
  { name: "前导零", text: "见 [001]。", valid: [1] },
  { name: "四位数不匹配", text: "见 [1234]。", valid: [1, 123, 234, 1234] },
  { name: "三位数", text: "见 [100] 与 [123]。", valid: [100, 123] },
  { name: "中英方括号混用", text: "见 [1】 与 【2]。", valid: [1, 2] },
  { name: "嵌套方括号", text: "见 [[1]]。", valid: [1] },
  { name: "紧邻", text: "见 [1][2]【3】。", valid: [1, 2, 3] },
  { name: "分隔符后没有数字", text: "见 [1、]。", valid: [1] },
  { name: "两个分隔符相连", text: "见 [1,,2]。", valid: [1, 2] },
  { name: "全角数字不算", text: "见 [１]。", valid: [1] },
  { name: "Markdown 链接样式", text: "见 [1](http://example.com) 与 [2]。", valid: [1, 2] },
  { name: "重复引用", text: "[1][1][1,1]", valid: [1] },
  { name: "多位数复合", text: "见 [10,20,30]。", valid: [10, 20, 30] },
  { name: "有效集合为空", text: "见 [1]。", valid: [] },
  { name: "括号里混了文字不匹配", text: "见 [1a] 与 [a1] 与 [1 2]。", valid: [1, 2] },
  { name: "未闭合", text: "见 [1 与 [2", valid: [1, 2] },
];

// ---- locateNormalized ----

interface LocateCase {
  name: string;
  haystack: Text;
  needle: Text;
}

const LOCATE_CASES: LocateCase[] = [
  { name: "空白被压过的片段", haystack: "前言。\n\n  关键结论   在这里。\n后记。", needle: "关键结论 在这里。" },
  { name: "找不到", haystack: "一段正文", needle: "不存在" },
  { name: "过短的片段不参与定位（3 个码元）", haystack: "一段正文内容", needle: "正文内" },
  { name: "恰好 4 个码元", haystack: "一段正文内容", needle: "正文内容" },
  { name: "片段首尾有空白，先归一再裁剪", haystack: "abc 关键结论在这里 def", needle: "  \n关键结论在这里\t " },
  { name: "片段内部多种空白折叠成一个空格", haystack: "甲乙 丙丁戊己", needle: "甲乙\u00a0\u3000\n丙丁" },
  { name: "片段只有空白", haystack: "abc def", needle: "   \n " },
  { name: "空原文", haystack: "", needle: "关键结论在这里" },
  { name: "空片段", haystack: "一段正文内容", needle: "" },
  { name: "原文开头的空白段被丢弃", haystack: "  \n\t开头的内容是这些", needle: "开头的内容" },
  { name: "原文末尾有空白段，不影响命中", haystack: "结尾的内容是这些 \n\n", needle: "内容是这些" },
  { name: "片段末尾的空白被裁掉后只剩 3 个码元", haystack: "结尾的内容是这些 \n\n", needle: "这些 \n" },
  { name: "命中跨越原文的空白段，下标取首个字符", haystack: "前缀前缀 一二三四\n\n\n五六七八 后缀", needle: "一二三四 五六七八" },
  { name: "空白段里混用各种空白字符", haystack: "一二三四\u00a0\u2003\u3000\ufeff\u2028五六七八", needle: "一二三四 五六七八" },
  { name: "多次出现取第一次", haystack: "重复的片段 重复的片段 重复的片段", needle: "重复的片段" },
  { name: "emoji 占两个码元，下标按码元", haystack: `${EMOJI}${EMOJI}前缀 命中的内容在这里`, needle: "命中的内容" },
  { name: "片段含 emoji，两个 emoji 恰好 4 个码元", haystack: `前缀${EMOJI}${EMOJI}后缀`, needle: `${EMOJI}${EMOJI}` },
  { name: "片段含一个 emoji 与一个字，3 个码元不够", haystack: `前缀${EMOJI}字后缀`, needle: `${EMOJI}字` },
  { name: "长原文中段命中", haystack: T([C(5000), L(" 目标 定位   片段 "), C(5000)]), needle: "目标 定位 片段" },
  { name: "长原文找不到", haystack: T([C(5000)]), needle: "不在其中的片段" },
  { name: "大小写敏感", haystack: "Hello World Example", needle: "hello world" },
  { name: "原文含 \\r\\n", haystack: "第一行内容\r\n第二行内容", needle: "第一行内容 第二行内容" },
];

// ---- extractReadWindow ----

interface WindowCase {
  name: string;
  text: Text;
  limit: number;
  hint?: Text;
}

const WINDOW_CASES: WindowCase[] = [
  { name: "短于上限原样返回", text: "很短的一段", limit: 100, hint: "任意" },
  { name: "恰好等于上限原样返回", text: T([C(300)]), limit: 300, hint: "不存在的片段" },
  { name: "没有线索：退回开头截断", text: T([L("起始标记"), C(2000)]), limit: 200 },
  { name: "线索为空串：同没有线索", text: T([L("起始标记"), C(2000)]), limit: 200, hint: "" },
  { name: "线索定位失败：退回开头截断", text: T([L("起始标记"), C(2000)]), limit: 200, hint: "库里根本没有这句话啊" },
  { name: "线索过短：不参与定位", text: T([L("起始标记"), C(2000), L("命中点")]), limit: 200, hint: "命中点" },
  { name: "命中在中段：留三成前文", text: T([C(3000), L("这里是真正的答案段落。"), C(3000)]), limit: 300, hint: "这里是真正的答案段落。" },
  { name: "命中在开头附近：没有前文标记", text: T([C(20), L("开头附近的命中片段"), C(3000)]), limit: 300, hint: "开头附近的命中片段" },
  { name: "命中恰在窗口前文边界", text: T([C(450), L("边界命中的片段"), C(3000)]), limit: 1500, hint: "边界命中的片段" },
  { name: "命中在末尾：窗口不越界，没有后文标记", text: T([C(3000), L("末尾的关键结论。")]), limit: 200, hint: "末尾的关键结论。" },
  { name: "命中在末尾附近：窗口右贴边", text: T([C(3000), L("接近末尾的结论片段"), C(30)]), limit: 200, hint: "接近末尾的结论片段" },
  { name: "线索的空白被压缩过", text: T([C(2000), L("第一段内容\n\n   第二段内容"), C(2000)]), limit: 500, hint: "第一段内容 第二段内容" },
  { name: "limit 为 1500（条目阅读上限）", text: T([C(6000), L("目标段落在这里出现"), C(6000)]), limit: 1500, hint: "目标段落在这里出现" },
  { name: "limit 为 2000", text: T([C(6000), L("目标段落在这里出现"), C(6000)]), limit: 2000, hint: "目标段落在这里出现" },
  { name: "limit 为奇数（三成取整）", text: T([C(3000), L("奇数上限下的命中"), C(3000)]), limit: 333, hint: "奇数上限下的命中" },
  { name: "limit 很小", text: T([C(100), L("命中的内容"), C(100)]), limit: 8, hint: "命中的内容" },
  { name: "窗口里含 emoji（切点避开代理对）", text: `${ascii(100)}${EMOJI}${EMOJI}${ascii(100)} 命中的这一句 ${ascii(100)}`, limit: 150, hint: "命中的这一句" },
  { name: "原文含空白段，下标按原文计", text: T([L("  \n\n  "), C(1000), L("   \n关键   片段  \n"), C(1000)]), limit: 300, hint: "关键 片段" },
  { name: "线索出现多次取第一次", text: T([C(1000), L("重复线索重复线索"), C(1000), L("重复线索重复线索"), C(1000)]), limit: 300, hint: "重复线索重复线索" },
];

// ---- matchWikiPages ----

const ENTRY = (id: string, title: string, over: Partial<WikiCatalogEntry> = {}): WikiCatalogEntry => ({
  id,
  title,
  normalizedTitle: normalizeWikiTitle(title),
  kind: "topic",
  summary: "",
  aliasesJson: null,
  manualEditedAt: null,
  updatedAt: 0,
  ...over,
});

interface MatchCase {
  name: string;
  catalog: WikiCatalogEntry[];
  query: string;
}

const BASE_CATALOG: WikiCatalogEntry[] = [
  ENTRY("w1", "Electron", { kind: "entity", summary: "跨平台桌面框架", aliasesJson: '["电子框架"]' }),
  ENTRY("w2", "知识管理", { summary: "PKM 方法论" }),
  ENTRY("w3", "采集工作流", { kind: "concept", summary: "网页与文件的采集流程，含 Electron 渲染进程" }),
];

const MATCH_CASES: MatchCase[] = [
  { name: "标题包含于查询", catalog: BASE_CATALOG, query: "Electron 是什么" },
  { name: "别名命中", catalog: BASE_CATALOG, query: "电子框架" },
  { name: "摘要 token 命中", catalog: BASE_CATALOG, query: "PKM" },
  { name: "无命中", catalog: BASE_CATALOG, query: "完全无关的词汇" },
  { name: "空查询", catalog: BASE_CATALOG, query: "" },
  { name: "只有空白的查询", catalog: BASE_CATALOG, query: " \u00a0\u3000\n" },
  { name: "多个 token 累计得分排序", catalog: BASE_CATALOG, query: "Electron 采集 流程" },
  { name: "同分保持目录顺序（稳定排序）", catalog: [ENTRY("a", "甲"), ENTRY("b", "甲乙"), ENTRY("c", "甲丙")], query: "甲" },
  { name: "标题得分高于摘要得分", catalog: [ENTRY("s", "无关", { summary: "归知 归知" }), ENTRY("t", "归知")], query: "归知" },
  { name: "大小写不敏感", catalog: BASE_CATALOG, query: "ELECTRON" },
  { name: "token 小写比较：土耳其语 İ", catalog: [ENTRY("i", "İstanbul"), ENTRY("j", "istanbul")], query: "İstanbul" },
  { name: "希腊字母 Σ 的小写（词尾 ς）", catalog: [ENTRY("g", "ΟΔΥΣΣΕΥΣ"), ENTRY("h", "ὀδυσσεύς")], query: "ΟΔΥΣΣΕΥΣ" },
  { name: "空标题的目录项总是被「查询包含标题」命中", catalog: [ENTRY("e", ""), ENTRY("f", "别的")], query: "任意问题" },
  { name: "空别名总是命中", catalog: [ENTRY("e", "标题", { aliasesJson: '[""]' })], query: "无关" },
  { name: "别名与 token 双向包含", catalog: [ENTRY("e", "标题", { aliasesJson: '["知识库管理","KM"]' })], query: "管理 km" },
  { name: "别名 JSON 非法按无别名", catalog: [ENTRY("e", "标题", { aliasesJson: "not json" })], query: "别名" },
  { name: "别名里混有非字符串", catalog: [ENTRY("e", "标题", { aliasesJson: '["有效",1,null,["x"]]' })], query: "有效" },
  { name: "查询里的多种空白分词", catalog: [ENTRY("e", "标题甲"), ENTRY("f", "标题乙")], query: "甲\u00a0\u3000\n乙" },
  { name: "摘要包含 token", catalog: [ENTRY("e", "标题", { summary: "这里讨论了向量检索的做法" })], query: "向量检索" },
  { name: "目录为空", catalog: [], query: "任意" },
  { name: "emoji 标题", catalog: [ENTRY("e", `${EMOJI}笔记`)], query: `${EMOJI}笔记 怎么写` },
  // JS 的 \s 含 U+FEFF、不含 U+0085，Go 的 unicode.IsSpace 恰好相反
  { name: "查询里的 U+FEFF 是空白，两侧各成一个 token", catalog: [ENTRY("e", "标题甲"), ENTRY("f", "标题乙")], query: "甲\uFEFF乙" },
  { name: "查询里的 U+0085 不是空白，整体是一个 token", catalog: [ENTRY("e", "标题甲"), ENTRY("f", "标题乙")], query: "甲\u0085乙" },
  // 权重（标题包含于查询 10、标题含词 6、别名包含于查询 5、别名含词 4、摘要含词 2）：每条各取一对只差一档的页面，
  // 目录里让得分低的排在前面，权重一变顺序就翻转或并列（并列按目录顺序）
  {
    name: "权重：标题包含于查询（10）高于别名全包含（5+4）",
    catalog: [ENTRY("b", "无关页面", { aliasesJson: '["归知检索"]' }), ENTRY("a", "归知")],
    query: "归知检索",
  },
  { name: "权重：标题含词（6）高于别名包含于查询（5）", catalog: [ENTRY("b", "无关", { aliasesJson: '["o b"]' }), ENTRY("a", "xfoox")], query: "foo bar" },
  {
    name: "权重：摘要含两个词（2+2）低于标题含一个词（6）",
    catalog: [ENTRY("x", "无关一", { summary: "foo bar 的说明" }), ENTRY("y", "xfoox")],
    query: "foo bar",
  },
  {
    name: "权重：别名包含于查询（5）高于摘要含两个词（2+2）",
    catalog: [ENTRY("b", "无关二", { summary: "foo bar 的说明" }), ENTRY("a", "无关三", { aliasesJson: '["o b"]' })],
    query: "foo bar",
  },
  {
    name: "权重：别名含词（4）高于摘要含一个词（2）",
    catalog: [ENTRY("b", "无关四", { summary: "foo 的说明" }), ENTRY("a", "无关五", { aliasesJson: '["xfoox"]' })],
    query: "foo bar",
  },
];

// ---- resolveBodyLinks ----

interface LinksCase {
  name: string;
  body: string;
  catalog: WikiCatalogEntry[];
}

const LINK_CATALOG: WikiCatalogEntry[] = [
  ENTRY("a", "Alpha"),
  ENTRY("b", "Beta 测试"),
  ENTRY("c", "知识管理"),
  ENTRY("d", "Alpha"), // 与 a 同名：只有第一个可被解析
];

const LINKS_CASES: LinksCase[] = [
  { name: "普通链接", body: "见 [[Alpha]] 与 [[知识管理]]。", catalog: LINK_CATALOG },
  { name: "显示文字：取竖线之前", body: "见 [[知识管理|知识库管理]]。", catalog: LINK_CATALOG },
  { name: "多个竖线只看第一个", body: "见 [[Beta 测试|甲|乙]]。", catalog: LINK_CATALOG },
  { name: "标题大小写与空白不敏感", body: "见 [[ alpha ]] 与 [[beta   测试]]。", catalog: LINK_CATALOG },
  { name: "链接里有换行，空白折叠", body: "见 [[Beta\n测试]]。", catalog: LINK_CATALOG },
  { name: "重复链接只出现一次", body: "[[Alpha]] [[alpha]] [[ALPHA|显示]]", catalog: LINK_CATALOG },
  { name: "同名目录项取第一个", body: "[[Alpha]]", catalog: LINK_CATALOG },
  { name: "目录里没有的链接被忽略", body: "[[不存在]] [[知识管理]]", catalog: LINK_CATALOG },
  { name: "空链接与只有显示文字的链接被忽略", body: "[[ ]] [[|只有显示]] [[Alpha]]", catalog: LINK_CATALOG },
  { name: "按正文出现顺序", body: "[[知识管理]] [[Alpha]] [[Beta 测试]]", catalog: LINK_CATALOG },
  { name: "残缺的括号", body: "[[Alpha] [Alpha]] [[[Alpha]]] [[Alpha]]", catalog: LINK_CATALOG },
  { name: "嵌套方括号不匹配外层", body: "[[a[b]c]] [[知识管理]]", catalog: LINK_CATALOG },
  { name: "没有链接", body: "普通正文 [单括号] 没有双括号链接。", catalog: LINK_CATALOG },
  { name: "空正文", body: "", catalog: LINK_CATALOG },
  { name: "空目录", body: "[[Alpha]]", catalog: [] },
  { name: "目录标题带特殊大小写映射（ß → SS）", body: "[[straße]] [[STRASSE]]", catalog: [ENTRY("s", "Straße")] },
];

// ---- 输出 ----

const describeAction = (action: ReturnType<typeof tryParseAction>): unknown => {
  if (action === null) return null;
  if (action.kind === "search") return { kind: "search", query: action.query };
  if (action.kind === "read") return { kind: "read", target: String(action.target) };
  return { kind: "answer", text: compactText(action.text) };
};

describe("aiflow golden：全局问答纯函数", () => {
  it("tryParseAction 与已提交的 golden 一致", () => {
    const cases = PARSE_CASES.map((c) => ({ name: c.name, text: c.text, output: describeAction(tryParseAction(expand(c.text) as string)) }));
    writeOrCompareGolden("qa-parse-action", { cases });
  });

  it("extractCitedOrdinals 与已提交的 golden 一致", () => {
    const cases = CITED_CASES.map((c) => ({
      ...c,
      output: [...extractCitedOrdinals(c.text, new Set(c.valid))].sort((a, b) => a - b),
    }));
    writeOrCompareGolden("qa-cited", { cases });
  });

  it("locateNormalized 与已提交的 golden 一致", () => {
    const cases = LOCATE_CASES.map((c) => ({
      ...c,
      output: locateNormalized(expand(c.haystack) as string, expand(c.needle) as string),
    }));
    writeOrCompareGolden("qa-locate", { cases });
  });

  it("extractReadWindow 与已提交的 golden 一致", () => {
    const cases = WINDOW_CASES.map((c) => {
      const out = extractReadWindow(expand(c.text) as string, c.limit, c.hint === undefined ? undefined : (expand(c.hint) as string));
      return { ...c, output: { text: compactText(out), length: out.length } };
    });
    writeOrCompareGolden("qa-window", { cases });
  });

  it("matchWikiPages 与已提交的 golden 一致", () => {
    const cases = MATCH_CASES.map((c) => ({ ...c, output: matchWikiPages(c.catalog, c.query).map((entry) => entry.id) }));
    writeOrCompareGolden("qa-match-wiki", { cases });
  });

  it("resolveBodyLinks 与已提交的 golden 一致", () => {
    const cases = LINKS_CASES.map((c) => ({ ...c, output: resolveBodyLinks(c.body, c.catalog).map((entry) => entry.id) }));
    writeOrCompareGolden("qa-body-links", { cases });
  });
});
