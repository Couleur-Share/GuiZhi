/**
 * 论坛讨论总结：把一帖多人的零散回复提炼成按方案聚类的结论。
 *
 * 与视频总结（media-summary.ts）刻意分开：口播稿是单人按时间线叙述，
 * 顺着讲一遍就是好总结；论坛帖是几十上百人各说各的，同一个方案散落在
 * 十几个楼层里，还夹着灌水与玩梗。按楼层复述等于没总结——所以这里的
 * 提示词要求按「方案 / 观点」归类，并如实交代共识与分歧。
 *
 * 模型走 mainText 路由（与视频总结同一解析函数，回退 fastText → 默认
 * chat 模型）。未配置模型时调用方静默跳过，只入库原始讨论。
 */
import {
  chatCompletion,
  type AIChatMessage,
  type AIClientConfig,
} from "@guizhi/core";

/**
 * 总结只用得上这三个字段。刚抓下来的 ForumReply 与从已入库正文解析回来的
 * ForumReplyEntry 都结构兼容，重新生成时不必再抓一次网页。
 */
export interface ForumSummaryReply {
  floor: number;
  author: string;
  content: string;
}

/** 单发上限（字符）：超过则按回复边界分块走 map-reduce */
const SINGLE_SHOT_MAX_CHARS = 12_000;
/** map 阶段每块字符数 */
const CHUNK_MAX_CHARS = 12_000;
/** 最大块数：约束超长帖的调用次数与成本，超出部分截断 */
const MAX_CHUNKS = 8;
const SUMMARY_MAX_TOKENS = 6144;
const SUMMARY_TIMEOUT_MS = 180_000;
const SUMMARY_TEMPERATURE = 0.3;

const SYSTEM_PROMPT =
  "你是论坛讨论总结助手。用户会提供一个论坛帖子的标题、主楼内容与回复，" +
  "请把讨论里真正有用的信息提炼出来，让用户不看原帖也能拿到结论。要求：\n" +
  "1. 开头用一段话（不加标题）说明：帖子在讨论什么问题、讨论整体倾向于什么结论。" +
  "这段之后正文里不要再出现成段的大段落；\n" +
  "2. 主体按「方案 / 观点」归类，不要按楼层顺序复述。每一类用「### 方案名称」" +
  "作为独立一行的小标题；\n" +
  "3. 小标题下一律用「- 」无序列表展开，一条讲清一件事，不要把几件事塞进一条长句。" +
  "按实际有的内容取舍这些角度：具体做法、多少人支持（如「多人推荐」「2 人提到」）、" +
  "给出的理由与实测数据、缺点或适用前提。原帖没提到的角度就不写，不要为凑格式硬编；\n" +
  "4. 工具名、软件名、参数、价格、延迟等关键信息用 **加粗** 标出，方便扫读；\n" +
  "5. 末尾用「### 共识与分歧」小节收尾，同样用列表分别写清：哪些结论大家看法一致，" +
  "哪些存在明显争论；\n" +
  "6. 忠实于原帖，不要编造原帖没有的内容；灌水、玩梗、纯附和与人身攻击的回复直接忽略；\n" +
  "7. 篇幅与信息量匹配：讨论充分的长帖可写 400~900 字，回复稀少的短帖精简即可；\n" +
  "8. 输出简体中文 Markdown；小标题一律用 ###，不要用 # 或 ##，" +
  "不要 --- 分隔线、表格或代码块；不要输出「讨论总结」这类总标题，" +
  "也不要任何前言或结尾说明。";

const MAP_SYSTEM_PROMPT =
  "你是论坛讨论总结助手。下面是某个论坛帖子回复的一个片段，" +
  "请按「方案 / 观点」归类提取这一段里的有效信息：每类用「### 方案名称」独立成行，" +
  "其下用「- 」列表写要点，保留具体的工具名、参数、理由与踩坑经验，忽略灌水与玩梗。" +
  "输出简体中文 Markdown，小标题一律用 ###，不要用 # 或 ##，不要 --- 分隔线或代码块；" +
  "只输出笔记本身，不要前言或结尾说明。";

/** 首行若是「讨论总结」这类冗余总标题则丢弃（小节标题由代码统一写入） */
const REDUNDANT_TITLE_PATTERN =
  /^#{0,6}\s*\*{0,2}(?:讨论|内容)?总结\*{0,2}[:：]?$/;
/** 整行只有一段加粗文本（可带尾冒号），模型用它当小标题 */
const BOLD_ONLY_LINE = /^\s*\*\*(.+?)\*\*[：:]?\s*$/;
const HASH_HEADING_LINE = /^\s*#{1,6}\s+(.+?)\s*$/;

/** 去掉包裹整段输出的 ``` 围栏（部分模型无视指令输出代码块） */
function stripWrappingCodeFence(text: string): string {
  const lines = text.split("\n");
  if (
    lines.length >= 2 &&
    /^```[a-zA-Z]*\s*$/.test(lines[0]) &&
    lines[lines.length - 1].trim() === "```"
  ) {
    return lines.slice(1, -1).join("\n").trim();
  }
  return text;
}

/**
 * 小节标题一律规范成 `###`。
 *
 * 两件事都是必须的：
 * - `#` / `##` 要拉平。`## 正文`、`## 讨论` 是条目正文的分段锚点，
 *   模型吐出同级标题会把详情页的分段切错。
 * - 独占一行的「**加粗**」要转成真标题。Markdown 里它后面只跟一个换行时，
 *   会和下一行正文渲染进同一个段落，标题就白写了——实测踩到过，
 *   页面上「**方案名** 多人推荐此方案…」黏成一坨。
 */
function normalizeHeadingLine(line: string): string {
  const hash = HASH_HEADING_LINE.exec(line);
  if (hash) {
    return `### ${cleanHeadingText(hash[1])}`;
  }
  const bold = BOLD_ONLY_LINE.exec(line);
  // 列表项里的加粗（`- **X**：…`）不以 ** 开头，不会命中
  return bold ? `### ${cleanHeadingText(bold[1])}` : line;
}

/** 标题文本去掉残留的加粗标记与尾冒号（冒号写在 ** 内外的都有） */
function cleanHeadingText(text: string): string {
  return text
    .replace(/\*\*/g, "")
    .trim()
    .replace(/[：:]$/, "")
    .trim();
}

/**
 * 输出协议的确定性清洗：标题规范化、删分隔线、去冗余总标题、折叠空行。
 * 提示词管不住模型的排版，渲染效果得靠这一层兜底。
 */
export function sanitizeForumSummary(raw: string): string {
  const lines = stripWrappingCodeFence(raw.trim())
    .split("\n")
    .filter((line) => !/^\s*-{3,}\s*$/.test(line))
    .map(normalizeHeadingLine);

  while (lines.length > 0 && lines[0].trim() === "") {
    lines.shift();
  }
  if (lines.length > 0 && REDUNDANT_TITLE_PATTERN.test(lines[0].trim())) {
    lines.shift();
  }
  return lines.join("\n").replace(/\n{3,}/g, "\n\n").trim();
}

export interface ForumSummaryInput {
  title: string;
  /** 主楼正文 */
  content: string;
  replies: ForumSummaryReply[];
}

export interface ForumSummaryOptions {
  signal?: AbortSignal;
  /** 测试注入：底层 chat 调用 */
  chat?: typeof chatCompletion;
}

/** 单条回复的素材行 */
function formatReply(reply: ForumSummaryReply): string {
  return `${reply.floor} 楼 ${reply.author}：${reply.content}`;
}

/**
 * 按回复边界分块：一条回复不能被切成两半，
 * 否则模型会把半句话当成完整观点。单条超长的回复自成一块。
 */
export function splitReplyChunks(
  replies: ForumSummaryReply[],
  maxChars: number,
): string[] {
  const chunks: string[] = [];
  let current: string[] = [];
  let length = 0;

  for (const reply of replies) {
    const line = formatReply(reply);
    if (current.length > 0 && length + line.length > maxChars) {
      chunks.push(current.join("\n"));
      current = [];
      length = 0;
    }
    current.push(line);
    length += line.length + 1;
  }
  if (current.length > 0) {
    chunks.push(current.join("\n"));
  }
  return chunks;
}

function buildHeader(input: ForumSummaryInput): string {
  const parts = [`帖子标题：《${input.title.trim() || "无标题"}》`];
  if (input.content.trim()) {
    parts.push(`主楼内容：\n${input.content.trim()}`);
  }
  return parts.join("\n");
}

async function runChat(
  config: AIClientConfig,
  messages: AIChatMessage[],
  options: ForumSummaryOptions | undefined,
  label: string,
): Promise<string> {
  const chat = options?.chat ?? chatCompletion;
  const result = await chat(config, messages, {
    temperature: SUMMARY_TEMPERATURE,
    maxTokens: SUMMARY_MAX_TOKENS,
    signal: options?.signal,
    timeoutMs: SUMMARY_TIMEOUT_MS,
  });
  if (result.finishReason === "length") {
    console.warn(`[import] 论坛讨论总结${label}输出被 max_tokens 截断`);
  }
  const content = result.content.trim();
  if (!content) {
    throw new Error(`模型未返回讨论总结（${label}输出为空）`);
  }
  return content;
}

/**
 * 生成讨论总结。没有回复时返回 null——只有主楼的帖子等同于一篇短文，
 * 让「讨论总结」小节空着比硬凑一段废话好。
 */
export async function generateForumSummary(
  input: ForumSummaryInput,
  config: AIClientConfig,
  options?: ForumSummaryOptions,
): Promise<string | null> {
  if (input.replies.length === 0) {
    return null;
  }

  const header = buildHeader(input);
  const replyText = input.replies.map(formatReply).join("\n");

  if (header.length + replyText.length <= SINGLE_SHOT_MAX_CHARS) {
    const content = await runChat(
      config,
      [
        { role: "system", content: SYSTEM_PROMPT },
        {
          role: "user",
          content: `${header}\n\n以下是全部 ${input.replies.length} 条回复：\n${replyText}`,
        },
      ],
      options,
      "",
    );
    return sanitizeForumSummary(content) || null;
  }

  // map：按回复边界分块提取要点
  const chunks = splitReplyChunks(input.replies, CHUNK_MAX_CHARS);
  const truncated = chunks.length > MAX_CHUNKS;
  if (truncated) {
    console.warn(
      `[import] 讨论过长（${chunks.length} 块），仅总结前 ${MAX_CHUNKS} 块`,
    );
    chunks.length = MAX_CHUNKS;
  }

  const notes: string[] = [];
  for (const [index, chunk] of chunks.entries()) {
    options?.signal?.throwIfAborted();
    notes.push(
      await runChat(
        config,
        [
          { role: "system", content: MAP_SYSTEM_PROMPT },
          {
            role: "user",
            content: `${header}\n\n回复片段 ${index + 1}/${chunks.length}：\n${chunk}`,
          },
        ],
        options,
        `（片段 ${index + 1}/${chunks.length}）`,
      ),
    );
  }

  // reduce：跨片段归并同类方案
  options?.signal?.throwIfAborted();
  const noteBlocks = notes
    .map((note, index) => `【片段 ${index + 1} 要点】\n${note}`)
    .join("\n\n");
  const reduced = await runChat(
    config,
    [
      { role: "system", content: SYSTEM_PROMPT },
      {
        role: "user",
        content:
          `${header}\n\n以下是各回复片段的要点笔记` +
          `${truncated ? "（回复过多，仅覆盖前一部分）" : ""}，` +
          `请跨片段归并同类方案后，按要求输出完整的讨论总结：\n\n${noteBlocks}`,
      },
    ],
    options,
    "（综合）",
  );
  return sanitizeForumSummary(reduced) || null;
}
