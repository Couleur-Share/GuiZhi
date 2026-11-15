/**
 * aiflow 知识域前半段的 golden：提示词与消息构造、摘要（单发 / map-reduce）、标签建议、处理中心 AI 归类。
 * Go 侧重放见 apps/desktop-go/aiflow/knowledge_golden_test.go；约定见 aiflow-golden-kit.ts。
 *
 * 比对范围：
 *   - 提示词常量与版本号逐字；构造函数的输出（长文只记长度 + sha256，避免 golden 体积失控）；
 *   - 流程层（generateSummary / suggestTags / classifyInboxItems）经真实的 runScenarioChat 与假传输：
 *     发出的请求（URL、头、请求体）、用量记录、截断警告、结果与错误文案；
 *   - 孤立代理项不入 golden：TS 能产生、Go 的 string 表示不了（见 native-ui-development.md §8.4），
 *     用例不会让切口落在代理对中间，safeText 在导出时强制检查。
 *
 * 长输入用「规格」描述（{cjk:n} / {ascii:n} / {lit:"…"} 的拼接），golden 里存规格与指纹，
 * Go 侧按同一生成器重建后先核对指纹，再喂给被测函数。
 */
import { describe, it } from "vitest";
import {
  classifyInboxItems,
  parseCollectionClassificationResponse,
} from "../../../src/renderer/services/knowledge-ai/classify-collections";
import {
  MAX_CONTENT_LENGTH,
  QA_AGENT_PROMPT_VERSION,
  QA_AGENT_SYSTEM_PROMPT,
  QA_PROMPT_VERSION,
  QA_SYSTEM_PROMPT,
  SUMMARY_CHUNK_SIZE,
  SUMMARY_MAP_SYSTEM_PROMPT,
  SUMMARY_MAX_CHUNKS,
  SUMMARY_PROMPT_VERSION,
  SUMMARY_SYSTEM_PROMPT,
  TAG_SUGGESTION_PROMPT_VERSION,
  TAG_SUGGESTION_SYSTEM_PROMPT,
  buildQaAgentPrompt,
  buildQaUserPrompt,
  buildSummaryMapPrompt,
  buildSummaryReducePrompt,
  buildUserPrompt,
  chunkContent,
  truncatePlain,
  truncateText,
} from "../../../src/renderer/services/knowledge-ai/prompts";
import { generateSummary } from "../../../src/renderer/services/knowledge-ai/summarize";
import { parseTagResponse, suggestTags } from "../../../src/renderer/services/knowledge-ai/suggest-tags";
import { useSettingsStore } from "../../../src/renderer/stores/settings.store";
import {
  aiCollectionNameKey,
  isReservedNewCollectionName,
  isValidAiCollectionName,
  normalizeAiCollectionName,
} from "@guizhi/shared/utils/inbox-classification";
import { writeOrCompareGolden, type ScriptStep } from "./aiflow-golden-kit";
import {
  A,
  C,
  L,
  EMOJI,
  HAN_EXT,
  LEGACY_ONLY,
  TWO_TIER,
  body,
  compactText,
  digest,
  range,
  render,
  runFlow,
  tokens,
  type FlowCase,
  type Raw,
  type Spec,
} from "./aiflow-golden-flow-kit";

// ---- 提示词 ----

interface TruncateCase {
  name: string;
  text: Spec;
  limit: number;
}

const TRUNCATE_CASES: TruncateCase[] = [
  { name: "空串", text: "", limit: 10 },
  { name: "短于上限", text: "abc", limit: 10 },
  { name: "恰好等于上限", text: "abcde", limit: 5 },
  { name: "超出一位", text: "abcdef", limit: 5 },
  { name: "上限为 0", text: "abc", limit: 0 },
  { name: "上限为 1 且首字是代理对：退到 0", text: [L(EMOJI), L("abc")], limit: 1 },
  { name: "上限为 2 且首字是代理对：整字保留", text: [L(EMOJI), L("abc")], limit: 2 },
  { name: "切口落在代理对中间：退一位", text: [L("ab"), L(EMOJI), L("cd")], limit: 3 },
  { name: "切口落在代理对之后：保留整字", text: [L("ab"), L(EMOJI), L("cd")], limit: 4 },
  { name: "扩展汉字同理", text: [C(5), L(HAN_EXT), C(4)], limit: 6 },
  { name: "含代理对且长度恰好等于上限时不截断", text: [A(5), L(EMOJI)], limit: 7 },
  { name: "5999 个码元（上限 6000）", text: [C(5999)], limit: 6000 },
  { name: "6000 个码元", text: [C(6000)], limit: 6000 },
  { name: "6001 个码元", text: [C(6001)], limit: 6000 },
  { name: "6001 个码元，切口在代理对中间", text: [C(5999), L(EMOJI), C(10)], limit: 6000 },
  { name: "6001 个码元，切口在代理对之后", text: [C(5998), L(EMOJI), C(10)], limit: 6000 },
  { name: "混合文本", text: [L("标题 "), L(EMOJI), L(" mixed 文字 "), L(HAN_EXT), L(" "), A(40)], limit: 20 },
];

interface UserPromptCase {
  name: string;
  title: string;
  content: Spec;
}

const USER_PROMPT_CASES: UserPromptCase[] = [
  { name: "普通", title: "标题", content: "正文" },
  { name: "空正文", title: "只有标题", content: "" },
  { name: "标题含换行与引号", title: '带"引号"\n换行', content: "内容\n第二行" },
  { name: "正文恰好 6000", title: "边界", content: [C(6000)] },
  { name: "正文 6001 被截断并附标注", title: "边界", content: [C(6001)] },
  { name: "截断点在代理对中间", title: "代理对", content: [C(5999), L(EMOJI), C(8)] },
  { name: "长英文", title: "english", content: [A(20000)] },
];

/** 长文分块的描述：块数、各块长度与指纹、拼回去是否等于被覆盖的前缀。 */
function describeChunks(content: string): Raw {
  const chunks = chunkContent(content);
  const covered = chunks.join("");
  return {
    count: chunks.length,
    lengths: chunks.map((chunk) => chunk.length),
    digests: chunks.map((chunk) => digest(chunk)),
    coveredLength: covered.length,
    coveredIsPrefix: content.startsWith(covered),
  };
}

const CHUNK_CASES: Array<{ name: string; content: Spec }> = [
  { name: "空串", content: "" },
  { name: "1 个码元", content: "a" },
  { name: "5999", content: [C(5999)] },
  { name: "6000", content: [C(6000)] },
  { name: "6001：第二块只有 1 个码元", content: [C(6001)] },
  { name: "12000", content: [A(12000)] },
  { name: "12001", content: [A(12001)] },
  { name: "47999", content: [C(47999)] },
  { name: "48000（恰好 8 块）", content: [C(48000)] },
  { name: "48001（超出部分丢弃）", content: [C(48001)] },
  { name: "100000（封顶 8 块）", content: [A(100000)] },
  { name: "块内含代理对（不在边界上）", content: [C(100), L(EMOJI), C(5000), L(HAN_EXT), C(7000)] },
];

describe("aiflow golden：提示词", () => {
  it("常量与构造函数与已提交的 golden 一致", () => {
    const constants = {
      SUMMARY_PROMPT_VERSION,
      SUMMARY_SYSTEM_PROMPT,
      SUMMARY_MAP_SYSTEM_PROMPT,
      TAG_SUGGESTION_PROMPT_VERSION,
      TAG_SUGGESTION_SYSTEM_PROMPT,
      QA_PROMPT_VERSION,
      QA_SYSTEM_PROMPT,
      QA_AGENT_PROMPT_VERSION,
      QA_AGENT_SYSTEM_PROMPT,
      MAX_CONTENT_LENGTH,
      SUMMARY_CHUNK_SIZE,
      SUMMARY_MAX_CHUNKS,
    };

    const truncate = TRUNCATE_CASES.map((c) => {
      const text = render(c.text);
      const plain = truncatePlain(text, c.limit);
      const withNote = truncateText(text, c.limit);
      return {
        name: c.name,
        text: c.text,
        input: digest(text),
        limit: c.limit,
        plain: digest(plain),
        withNote: digest(withNote),
        // 短结果同时给出原文，肉眼可校
        plainText: compactText(plain),
        withNoteText: compactText(withNote),
      };
    });

    const userPrompts = USER_PROMPT_CASES.map((c) => {
      const content = render(c.content);
      const prompt = buildUserPrompt(c.title, content);
      return {
        name: c.name,
        title: c.title,
        content: c.content,
        input: digest(content),
        prompt: digest(prompt),
        promptText: compactText(prompt),
      };
    });

    const chunks = CHUNK_CASES.map((c) => {
      const content = render(c.content);
      return { name: c.name, content: c.content, input: digest(content), ...describeChunks(content) };
    });

    const mapReduce = {
      map: [
        { title: "标题", chunk: "片段正文", index: 1, total: 3 },
        { title: "", chunk: "", index: 8, total: 8 },
        { title: "带\n换行", chunk: "多行\n片段", index: 2, total: 2 },
      ].map((c) => ({ ...c, prompt: buildSummaryMapPrompt(c.title, c.chunk, c.index, c.total) })),
      reduce: [
        { title: "标题", partials: ["- 一", "- 二\n- 三"] },
        { title: "单片", partials: ["- 只有一片"] },
        { title: "空", partials: [] as string[] },
      ].map((c) => ({ ...c, prompt: buildSummaryReducePrompt(c.title, c.partials) })),
    };

    const history = [
      { question: "第一问", answer: "第一答" },
      { question: "第二问\n含换行", answer: "第二答 [1]" },
    ];
    const qa = {
      user: [
        { name: "无资料无历史", question: "问题？", blocks: [] as string[], history: undefined },
        { name: "有资料无历史", question: "问题？", blocks: ["[1] 标题一\n内容一", "[2] 标题二\n内容二"], history: undefined },
        { name: "空历史数组", question: "问题？", blocks: ["[1] 甲"], history: [] as typeof history },
        { name: "有资料有历史", question: "问题？", blocks: ["[1] 甲", "[2] 乙"], history },
      ].map((c) => ({ ...c, prompt: buildQaUserPrompt(c.question, c.blocks, c.history) })),
      agent: [
        { name: "无历史无轨迹", question: "问题？", history: undefined, transcript: [] as string[] },
        { name: "空历史有轨迹", question: "问题？", history: [] as typeof history, transcript: ["搜索「甲」→ 命中 2 条", "阅读 [1]"] },
        { name: "有历史有轨迹", question: "问题？", history, transcript: ["动作 1", "动作 2"] },
      ].map((c) => ({ ...c, prompt: buildQaAgentPrompt(c.question, c.history, c.transcript) })),
    };

    writeOrCompareGolden("prompts", { constants, truncate, userPrompts, chunks, mapReduce, qa });
  });
});

// ---- 摘要 ----

interface SummaryCase extends FlowCase {
  title: string;
  content: Spec;
}

const MAP_OK = (n: number, finish: string | null = "stop"): ScriptStep => ({
  body: body(`  - 片段${n}要点甲\n- 片段${n}要点乙  \n`, finish, tokens(100 + n, 10 + n)),
});

const SUMMARY_CASES: SummaryCase[] = [
  {
    name: "短文单发：标题与正文先 trim，温度 0.3、max_tokens 4096",
    settings: TWO_TIER,
    title: "  我的标题  ",
    content: "  \n正文内容第一行\n第二行  \t",
    script: [{ body: body("\n- 要点一\n- 要点二\n", "stop", tokens(120, 30)) }],
  },
  {
    name: "标题为空白：落到「无标题」",
    settings: TWO_TIER,
    title: " \t ",
    content: "有正文",
    script: [{ body: body("- 只有一条", "stop", tokens(10, 5)) }],
  },
  {
    name: "正文只有空白：直接报错，不发请求",
    settings: TWO_TIER,
    title: "标题",
    content: " \n\t\u3000 ",
    script: [],
  },
  {
    name: "恰好 6000 个码元仍是单发",
    settings: TWO_TIER,
    title: "边界",
    content: [C(6000)],
    script: [{ body: body("- 单发", "stop", tokens(6100, 20)) }],
  },
  {
    name: "6001 个码元：map-reduce，两片",
    settings: TWO_TIER,
    title: "边界",
    content: [C(6001)],
    script: [MAP_OK(1), MAP_OK(2), { body: body("- 合并后的要点", "stop", tokens(300, 40)) }],
  },
  {
    name: "前后空白不计入长度：trim 后 6000 仍单发",
    settings: TWO_TIER,
    title: "边界",
    content: [L("   "), C(6000), L("   ")],
    script: [{ body: body("- 单发", "stop", tokens(1, 1)) }],
  },
  {
    name: "超长正文：封顶 8 片，超出部分丢弃",
    settings: TWO_TIER,
    title: "超长",
    content: [A(60000)],
    script: [...Array.from({ length: 8 }, (_, i) => MAP_OK(i + 1)), { body: body("- 综合", "stop", tokens(500, 50)) }],
  },
  {
    name: "含代理对的长文（切口不在代理对中间）",
    settings: TWO_TIER,
    title: "emoji 😀",
    content: [C(5998), L(EMOJI), C(30)],
    script: [MAP_OK(1), MAP_OK(2), { body: body("- 含 emoji 😀 的综合", "stop", tokens(2, 3)) }],
  },
  {
    name: "单发输出被截断：truncated=true，并留下截断警告",
    settings: TWO_TIER,
    title: "截断",
    content: "正文",
    script: [{ body: body("- 半句", "length", tokens(50, 4096)) }],
  },
  {
    name: "map 阶段有一片被截断：reduce 正常也算 truncated",
    settings: TWO_TIER,
    title: "截断",
    content: [C(13000)],
    script: [MAP_OK(1), MAP_OK(2, "length"), MAP_OK(3), { body: body("- 综合", "stop", tokens(1, 1)) }],
  },
  {
    name: "只有 reduce 被截断",
    settings: TWO_TIER,
    title: "截断",
    content: [C(7000)],
    script: [MAP_OK(1), MAP_OK(2), { body: body("- 综合半句", "length", tokens(1, 1)) }],
  },
  {
    name: "第二片请求失败：整体抛错，已发请求与失败用量都留痕",
    settings: TWO_TIER,
    title: "失败",
    content: [C(7000)],
    script: [MAP_OK(1), { status: 500, body: JSON.stringify({ error: { message: "upstream down" } }) }],
  },
  {
    name: "传输层失败（超时）",
    settings: TWO_TIER,
    title: "失败",
    content: "正文",
    script: [{ status: 0, ok: false, error: "请求超时" }],
  },
  {
    name: "模型只回空白：trim 后为空串，照常返回",
    settings: TWO_TIER,
    title: "空回答",
    content: "正文",
    script: [{ body: body("   \n ", "stop") }],
  },
  {
    name: "未配置 AI：抛 AI_NOT_CONFIGURED，不发请求",
    settings: {},
    title: "未配置",
    content: "正文",
    script: [],
  },
  {
    name: "旧版单模型字段兜底",
    settings: LEGACY_ONLY,
    title: "旧版",
    content: "正文",
    script: [{ body: body("- 兜底模型", "stop", tokens(3, 4)) }],
  },
  {
    name: "标题与正文含引号、换行与控制字符",
    settings: TWO_TIER,
    title: '带"引号"\n与换行',
    content: "正文\n含 <tag> & 符号 \u2028 与 \u0001 控制字符",
    script: [{ body: body("- 好", "stop", tokens(3, 4)) }],
  },
];

describe("aiflow golden：摘要", () => {
  it("generateSummary 与已提交的 golden 一致", async () => {
    const baseline = { ...useSettingsStore.getState() };
    const cases = [];
    for (const c of SUMMARY_CASES) {
      const content = render(c.content);
      const outcome = await runFlow(c, baseline as Raw, () => generateSummary(c.title, content));
      cases.push({
        name: c.name,
        settings: c.settings,
        title: c.title,
        content: c.content,
        input: digest(content),
        script: c.script,
        expect: outcome,
      });
    }
    useSettingsStore.setState(baseline);
    writeOrCompareGolden("summary", { cases });
  });
});

// ---- 标签 ----

const PARSE_TAG_CASES: string[] = [
  "",
  "   ",
  "AI，人工智能，知识库",
  "AI,人工智能,知识库",
  "AI、人工智能、知识库",
  "AI;人工智能；知识库",
  "AI\n人工智能\n知识库",
  "1. AI\n2. 人工智能\n3. 知识库",
  "1、AI，2、人工智能",
  "1) AI，2) 人工智能",
  "#AI，#人工智能，# 知识库",
  "  3.  #标签  ",
  "12. 十二号标签，007、前导零",
  "1.2 不是编号",
  "第1名，Top 3",
  '"AI"，\'人工智能\'，「知识库」，『笔记』，`代码`',
  "“弯引号保留”，‘单弯引号保留’",
  "重复，重复，重复，另一个，重复",
  "一，二，三，四，五，六，七，八",
  "一，二，三，四，一，二，五，六",
  "十二个字的标签正好十二个字啊，这个标签超过了十二个字的长度限制所以丢弃，短",
  "abcdefghijkl，abcdefghijklm",
  "😀😀😀😀😀😀，😀😀😀😀😀😀😀",
  "\u00a0标签\u00a0，\ufeff另一个\ufeff，\u3000全角空白\u3000，\u2028行分隔\u2029",
  "\u200b零宽空格，\u180e蒙古文元音分隔符",
  "标签一，，，标签二，，",
  "，，，",
  "。，！，？",
  "标签 带空格，tag with spaces，  两侧空白  ",
  "AI 人工智能 知识库",
  "- 列表项，* 星号项",
  "标签：AI，人工智能",
  "Tags: AI, ML, DL",
  "1\n2\n3\n4\n5\n6\n7",
  "\r\nAI\r\n人工智能\r\n",
  "#1，##2，#  3",
  "０１２全角数字，１．全角编号",
  "5. ",
  "#",
  "1.",
  "A#B，#A#B",
  "“引号内，逗号”，「「嵌套」」",
  "编程，Programming，PROGRAMMING，programming",
  "标\t签",
  "标签\t，\t标签二",
];

interface TagCase extends FlowCase {
  title: string;
  content: Spec;
}

const TAG_CASES: TagCase[] = [
  {
    name: "正常：解析逗号分隔的标签",
    settings: TWO_TIER,
    title: "  Rust 入门  ",
    content: "  Rust 是一门系统编程语言。  ",
    script: [{ body: body("编程语言，Rust，系统编程", "stop", tokens(80, 12)) }],
  },
  {
    name: "标题与正文都为空：报错，不发请求",
    settings: TWO_TIER,
    title: "  ",
    content: " \n ",
    script: [],
  },
  {
    name: "只有标题：正文留空",
    settings: TWO_TIER,
    title: "只有标题",
    content: "  ",
    script: [{ body: body("标题标签", "stop", tokens(5, 5)) }],
  },
  {
    name: "只有正文：标题落到「无标题」",
    settings: TWO_TIER,
    title: "",
    content: "只有正文",
    script: [{ body: body("1. 正文标签\n2. 第二个", "stop", tokens(5, 5)) }],
  },
  {
    name: "长正文被截断并附「已截断」标注",
    settings: TWO_TIER,
    title: "长文",
    content: [C(9000)],
    script: [{ body: body("长文，截断", "stop", tokens(6000, 6)) }],
  },
  {
    name: "模型输出无法解析出标签：报错",
    settings: TWO_TIER,
    title: "标题",
    content: "正文",
    script: [{ body: body("  \n  ", "stop", tokens(5, 1)) }],
  },
  {
    name: "输出被截断：警告 + 照常解析",
    settings: TWO_TIER,
    title: "标题",
    content: "正文",
    script: [{ body: body("半截，标签", "length", tokens(5, 2048)) }],
  },
  {
    name: "请求失败：抛错并记失败用量",
    settings: TWO_TIER,
    title: "标题",
    content: "正文",
    script: [{ status: 429, body: JSON.stringify({ error: { message: "rate limited" } }) }],
  },
  {
    name: "未配置 AI",
    settings: {},
    title: "标题",
    content: "正文",
    script: [],
  },
  {
    name: "标签超过 5 个只取前 5 个",
    settings: TWO_TIER,
    title: "标题",
    content: "正文",
    script: [{ body: body("一，二，三，四，五，六，七", "stop", tokens(1, 1)) }],
  },
];

describe("aiflow golden：标签建议", () => {
  it("parseTagResponse 与 suggestTags 与已提交的 golden 一致", async () => {
    const parse = PARSE_TAG_CASES.map((input) => ({ input, tags: parseTagResponse(input) }));

    const baseline = { ...useSettingsStore.getState() };
    const flows = [];
    for (const c of TAG_CASES) {
      const content = render(c.content);
      const outcome = await runFlow(c, baseline as Raw, () => suggestTags(c.title, content));
      flows.push({
        name: c.name,
        settings: c.settings,
        title: c.title,
        content: c.content,
        input: digest(content),
        script: c.script,
        expect: outcome,
      });
    }
    useSettingsStore.setState(baseline);
    writeOrCompareGolden("tags", { parse, flows });
  });
});

// ---- 归类 ----

const NAME_CASES: string[] = [
  "",
  "  ",
  "编程开发",
  "  编程   开发  ",
  "\t编程\n开发\r",
  "Ｒｕｓｔ　語言",
  "ＡＢＣ１２３",
  "ﬁle 与 ㈱ 与 ① 与 ㎏",
  "Café",
  "Cafe\u0301",
  "İstanbul",
  "ẞ",
  "ΑΣ",
  "ΑΣ ΑΣΑ",
  "ǅ",
  "全部",
  "未分类",
  "未归知识库",
  "收藏",
  "归档",
  "回收站",
  " 回收站 ",
  "回 收 站",
  "ＲＵＳＴ",
  "rust",
  "Rust",
  "RUST",
  "一二三四五六七八九十一二三四五六七八九十",
  "一二三四五六七八九十一二三四五六七八九十一",
  "😀😀😀😀😀😀😀😀😀😀",
  "😀😀😀😀😀😀😀😀😀😀😀",
  "a\u00a0b",
  "a\u2028b",
  "a\u200bb",
  "\ufeffBOM",
  "名称/带/斜杠",
  "名称\u0000空字符",
];

const EXPECTED_IDS = ["a", "b", "c"];

const PARSE_CLASSIFY_CASES: Array<{ name: string; text: string; ids: string[] }> = [
  {
    name: "标准对象",
    text: '{"assignments":[{"id":"a","collection":"编程"},{"id":"b","collection":"健康"},{"id":"c","collection":"编程"}]}',
    ids: EXPECTED_IDS,
  },
  {
    name: "代码块污染 + 乱序：按请求顺序返回",
    text: '好的，结果如下：\n```json\n{"assignments":[{"id":"c","collection":"C"},{"id":"a","collection":"A"},{"id":"b","collection":"B"}]}\n```\n以上。',
    ids: EXPECTED_IDS,
  },
  {
    name: "根是数组",
    text: '[{"id":"a","collection":"A"},{"id":"b","collection":"B"},{"id":"c","collection":"C"}]',
    ids: EXPECTED_IDS,
  },
  {
    name: "说明文字里先出现方括号：按数组去截取，解析失败",
    text: '说明[注意]：{"assignments":[{"id":"a","collection":"A"}]}',
    ids: ["a"],
  },
  {
    name: "collectionName 字段兜底",
    text: '{"assignments":[{"id":"a","collectionName":"A"},{"id":"b","collection":"B","collectionName":"忽略"}]}',
    ids: ["a", "b"],
  },
  {
    name: "collection 不是字符串时落到 collectionName",
    text: '{"assignments":[{"id":"a","collection":5,"collectionName":"A"}]}',
    ids: ["a"],
  },
  {
    name: "id 前后空白被 trim",
    text: '{"assignments":[{"id":"  a  ","collection":"A"}]}',
    ids: ["a"],
  },
  {
    name: "合集名被 NFKC 归一并折叠空白",
    text: '{"assignments":[{"id":"a","collection":"Ｒｕｓｔ　　語言"}]}',
    ids: ["a"],
  },
  {
    name: "null、标量项被跳过，其余照常",
    text: '{"assignments":[null,1,"x",true,{"id":"a","collection":"A"}]}',
    ids: ["a"],
  },
  {
    name: "数组项被当成空 ID",
    text: '{"assignments":[["a","A"]]}',
    ids: ["a"],
  },
  {
    name: "没有 JSON",
    text: "抱歉，我无法完成。",
    ids: ["a"],
  },
  {
    name: "只有左括号",
    text: "{ 不完整",
    ids: ["a"],
  },
  {
    name: "对象开头但结尾被截掉",
    text: '{"assignments":[{"id":"a","collection":"A"}]',
    ids: ["a"],
  },
  {
    name: "JSON 语法错误",
    text: '{"assignments":[{"id":"a","collection":}]}',
    ids: ["a"],
  },
  {
    name: "根是对象但没有 assignments",
    text: '{"result":[]}',
    ids: ["a"],
  },
  {
    name: "assignments 不是数组",
    text: '{"assignments":{"a":"A"}}',
    ids: ["a"],
  },
  {
    name: "对象夹在前后文字里，期望为空",
    text: 'x {"assignments":[]} y',
    ids: [],
  },
  {
    name: "空期望空结果",
    text: '{"assignments":[]}',
    ids: [],
  },
  {
    name: "漏项",
    text: '{"assignments":[{"id":"a","collection":"A"}]}',
    ids: EXPECTED_IDS,
  },
  {
    name: "重复分类",
    text: '{"assignments":[{"id":"a","collection":"A"},{"id":"a","collection":"B"}]}',
    ids: ["a"],
  },
  {
    name: "未知条目",
    text: '{"assignments":[{"id":"zzz","collection":"A"}]}',
    ids: ["a"],
  },
  {
    name: "空 ID 视为未知条目",
    text: '{"assignments":[{"collection":"A"}]}',
    ids: ["a"],
  },
  {
    name: "id 不是字符串视为空 ID",
    text: '{"assignments":[{"id":1,"collection":"A"}]}',
    ids: ["a"],
  },
  {
    name: "合集名为空白",
    text: '{"assignments":[{"id":"a","collection":"   "}]}',
    ids: ["a"],
  },
  {
    name: "合集名缺失",
    text: '{"assignments":[{"id":"a"}]}',
    ids: ["a"],
  },
  {
    name: "合集名超过 20",
    text: '{"assignments":[{"id":"a","collection":"一二三四五六七八九十一二三四五六七八九十一"}]}',
    ids: ["a"],
  },
  {
    name: "合集名恰好 20",
    text: '{"assignments":[{"id":"a","collection":"一二三四五六七八九十一二三四五六七八九十"}]}',
    ids: ["a"],
  },
  {
    name: "合集名带 emoji（按码元计长度）",
    text: '{"assignments":[{"id":"a","collection":"😀😀😀😀😀😀😀😀😀😀😀"}]}',
    ids: ["a"],
  },
  {
    name: "多个对象时取第一个 { 到最后一个 }",
    text: '前 {"assignments":[{"id":"a","collection":"A"}]} 后 {"x":1}',
    ids: ["a"],
  },
  {
    name: "空 ID 与空名称同时出现：先报未知条目",
    text: '{"assignments":[{"id":"","collection":""}]}',
    ids: ["a"],
  },
];

interface ClassifyCase extends FlowCase {
  items: Array<{ itemId: string; title: string; excerpt: string }>;
  existing: string[];
}

const item = (n: number, over: Partial<{ title: string; excerpt: string }> = {}): ClassifyCase["items"][number] => ({
  itemId: `item-${n}`,
  title: over.title ?? `条目 ${n}`,
  excerpt: over.excerpt ?? `摘要 ${n}`,
});

const assignmentsBody = (pairs: Array<[string, string]>): ScriptStep => ({
  body: body(JSON.stringify({ assignments: pairs.map(([id, collection]) => ({ id, collection })) }), "stop", tokens(10, 10)),
});


const CLASSIFY_CASES: ClassifyCase[] = [
  {
    name: "没有条目：直接返回空，不发请求",
    settings: TWO_TIER,
    items: [],
    existing: ["已有"],
    script: [],
  },
  {
    name: "单批：复用已有合集（大小写与全半角归一后命中原始名称）",
    settings: TWO_TIER,
    items: [item(1), item(2), item(3)],
    existing: ["Rust 语言", "健康"],
    script: [assignmentsBody([["item-1", "ＲＵＳＴ　语言"], ["item-2", "健康"], ["item-3", "新合集"]])],
  },
  {
    name: "21 条两批：前一批新建的合集进入后一批的目录",
    settings: TWO_TIER,
    items: range(21).map((n) => item(n)),
    existing: ["编程开发"],
    script: [
      assignmentsBody(range(20).map((n) => [`item-${n}`, n % 2 === 1 ? "健康养护" : "编程开发"] as [string, string])),
      assignmentsBody([["item-21", "健康养护"]]),
    ],
  },
  {
    name: "后一批用不同写法引用前一批的新合集：落到第一次出现的写法",
    settings: TWO_TIER,
    items: range(22).map((n) => item(n)),
    existing: [],
    script: [
      assignmentsBody(range(20).map((n) => [`item-${n}`, "Machine Learning"] as [string, string])),
      assignmentsBody([["item-21", "machine  learning"], ["item-22", "ＭＡＣＨＩＮＥ ＬＥＡＲＮＩＮＧ"]]),
    ],
  },
  {
    name: "已有合集里的无效名称与重复名称被忽略",
    settings: TWO_TIER,
    items: [item(1)],
    existing: ["", "  ", "一二三四五六七八九十一二三四五六七八九十一", "Dup", "dup", "ＤＵＰ"],
    script: [assignmentsBody([["item-1", "DUP"]])],
  },
  {
    name: "标题与摘要含需要转义的字符",
    settings: TWO_TIER,
    items: [
      item(1, { title: '带"引号"\\反斜杠\n换行\t制表', excerpt: "含 <b>标签</b> & 符号 \u2028 与 \u2029 与 \u0001 与 😀" }),
      item(2, { title: "", excerpt: "" }),
    ],
    existing: ['含"引号"的合集'],
    script: [assignmentsBody([["item-1", "A"], ["item-2", "B"]])],
  },
  {
    name: "新建合集超过安全上限（8）：整体抛错",
    settings: TWO_TIER,
    items: range(9).map((n) => item(n)),
    existing: [],
    script: [assignmentsBody(range(9).map((n) => [`item-${n}`, `合集${n}`] as [string, string]))],
  },
  {
    name: "恰好新建 8 个合集：允许",
    settings: TWO_TIER,
    items: range(8).map((n) => item(n)),
    existing: ["已有"],
    script: [assignmentsBody(range(8).map((n) => [`item-${n}`, `合集${n}`] as [string, string]))],
  },
  {
    name: "模型返回的 JSON 漏了条目：整体失败",
    settings: TWO_TIER,
    items: [item(1), item(2)],
    existing: [],
    script: [assignmentsBody([["item-1", "A"]])],
  },
  {
    name: "模型没返回 JSON",
    settings: TWO_TIER,
    items: [item(1)],
    existing: [],
    script: [{ body: body("我无法判断。", "stop", tokens(1, 1)) }],
  },
  {
    name: "第二批失败：第一批的结果不返回",
    settings: TWO_TIER,
    items: range(25).map((n) => item(n)),
    existing: [],
    script: [
      assignmentsBody(range(20).map((n) => [`item-${n}`, "A"] as [string, string])),
      { status: 500, body: JSON.stringify({ error: { message: "boom" } }) },
    ],
  },
  {
    name: "请求前已取消：不发请求",
    settings: TWO_TIER,
    items: [item(1)],
    existing: [],
    abort: "before",
    script: [],
  },
  {
    name: "进行中取消",
    settings: TWO_TIER,
    items: [item(1)],
    existing: [],
    abort: "during",
    script: [{ abortDuring: true, status: 0, ok: false, error: "已取消" }],
  },
  {
    name: "未配置 AI",
    settings: {},
    items: [item(1)],
    existing: [],
    script: [],
  },
  {
    name: "输出被截断：警告，但 JSON 完整时照常使用",
    settings: TWO_TIER,
    items: [item(1)],
    existing: [],
    script: [{ body: body(JSON.stringify({ assignments: [{ id: "item-1", collection: "A" }] }), "length", tokens(1, 4096)) }],
  },
];

describe("aiflow golden：处理中心 AI 归类", () => {
  it("名称工具、响应解析与 classifyInboxItems 与已提交的 golden 一致", async () => {
    const names = NAME_CASES.map((input) => ({
      input,
      normalized: normalizeAiCollectionName(input),
      key: aiCollectionNameKey(input),
      valid: isValidAiCollectionName(input),
      reserved: isReservedNewCollectionName(input),
    }));

    const parse = PARSE_CLASSIFY_CASES.map((c) => {
      let result: unknown = null;
      let error: string | null = null;
      try {
        result = parseCollectionClassificationResponse(c.text, c.ids);
      } catch (e) {
        error = (e as Error).message;
      }
      return { name: c.name, text: c.text, ids: c.ids, result, error };
    });

    const baseline = { ...useSettingsStore.getState() };
    const flows = [];
    for (const c of CLASSIFY_CASES) {
      const progress: Array<[number, number]> = [];
      const outcome = await runFlow(c, baseline as Raw, (signal) =>
        classifyInboxItems(c.items, c.existing, {
          signal,
          onProgress: (done, total) => progress.push([done, total]),
        }),
      );
      flows.push({
        name: c.name,
        settings: c.settings,
        items: c.items,
        existing: c.existing,
        abort: c.abort ?? null,
        script: c.script,
        expect: { ...outcome, progress },
      });
    }
    useSettingsStore.setState(baseline);
    writeOrCompareGolden("classify", { names, parse, flows });
  });
});
