/**
 * 归知 AI 提示词（移植自 .NET 版 AiPrompts.cs，版本号随内容变更递增）。
 */

export const SUMMARY_PROMPT_VERSION = "summary-v1";

export const SUMMARY_SYSTEM_PROMPT =
  "你是一个知识管理助手。请用简体中文将用户提供的内容总结为 2~4 条要点：" +
  "每条独立一行并以「- 」开头，总字数不超过 200 字，只输出要点本身，" +
  "不要输出任何前言、标题或结尾说明。";

/** 分块摘要的 map 阶段提示词：从单个片段提取要点。 */
export const SUMMARY_MAP_SYSTEM_PROMPT =
  "你是知识管理助手。下面是长文的一个片段，请提取其中的关键信息，" +
  "用简体中文列成 3~6 条要点，每条独立一行以「- 」开头，只输出要点本身，不要输出前言或结尾。";

export const TAG_SUGGESTION_PROMPT_VERSION = "autotag-v2";

export const TAG_SUGGESTION_SYSTEM_PROMPT =
  "你是一个知识管理助手。请为用户提供的内容生成 2~3 个简短的中文标签，" +
  "每个标签 2~6 个字、概括最核心的主题或领域。只输出标签本身，标签之间用中文逗号分隔，" +
  "不要输出编号、井号或任何其他文字。";

export const QA_PROMPT_VERSION = "qa-v3";

/** 单发 RAG 管线：宽召回可能混入无关条目，要求模型甄别并只引用支撑回答的资料。 */
export const QA_SYSTEM_PROMPT =
  "你是一个个人知识库问答助手。用户会提供编号资料与问题。资料由自动检索获得，" +
  "可能包含与问题无关的条目：请先甄别，只依据真正相关的资料用简体中文回答，无关资料直接忽略。" +
  "引用资料时在句末标注编号（如 [1]），只标注支撑该句结论的资料，编号必须来自资料列表，不要编造编号；" +
  "资料不足以回答时明确说明缺少哪方面的信息，不要编造资料中不存在的内容；" +
  "回答保持简洁，不要复述资料原文。" +
  "回答可使用 Markdown 排版（如要点列表、**加粗**、`代码`），层级不超过两层，不要输出一级标题。";

export const QA_AGENT_PROMPT_VERSION = "qa-agent-v1";

/**
 * 问答 Agent 动作协议（检索即推理）：模型每轮输出一个 JSON 动作，
 * 代码解析执行并把结果追加进轨迹，直到模型给出最终回答。
 */
export const QA_AGENT_SYSTEM_PROMPT =
  "你是个人知识库的问答助手，通过工具在知识库中检索资料来回答用户问题。" +
  "知识库包含两类资料：知识条目（用户采集的原文）与 Wiki 页面（AI 编译的结构化知识）。\n" +
  "你每轮只输出一个 JSON 对象（不要输出任何其他文字或代码块标记），从以下三种动作中选一种：\n" +
  '{"action":"search","query":"检索关键词"} —— 搜索知识库，返回带编号的资料列表\n' +
  '{"action":"read","target":3} —— 阅读指定编号的资料内容\n' +
  '{"action":"answer","text":"最终回答"} —— 给出最终回答并结束\n' +
  "纪律：\n" +
  "1. 给出回答前必须至少阅读过一份资料；回答中引用资料在句末标注编号（如 [1]），" +
  "只标注真正支撑该句结论、且你阅读过的资料编号，不要编造编号；\n" +
  "2. search 使用简短关键词（不要整句照抄），没有命中就换一组关键词；" +
  "资料不足以回答时在 answer 中明确说明缺少哪方面的信息，不得编造内容；\n" +
  "3. Wiki 页面适合获取总览与关联线索，知识条目适合细节与原文出处；阅读 Wiki 页面后可以继续阅读它关联的资料；\n" +
  "4. answer 的 text 用简体中文，可使用 Markdown 排版（要点列表、**加粗**），层级不超过两层，" +
  "不要输出一级标题，回答保持简洁、不要复述资料原文。";

export const WIKI_COMPILE_PROMPT_VERSION = "wiki-compile-v1";

/**
 * Wiki 编译（ADR 0023）：把一条知识条目编译进 LLM 维护的 Wiki 页面网络。
 * 输出严格 JSON 由代码解析；链接白名单在解析后由代码校验，
 * 未命中的 [[链接]] 降级为纯文本——提示词约束 + 确定性修复双保险。
 */
export const WIKI_COMPILE_SYSTEM_PROMPT =
  "你是个人知识库的 Wiki 管理员。用户会提供一条新的知识条目、现有 Wiki 页面清单以及部分相关页面的当前内容。" +
  "你的任务是把这条新知识编译进 Wiki：优先更新清单中已有的相关页面（补充新知识、修正表述），" +
  "没有合适页面时才新建页面。\n" +
  "输出要求：只输出一个 JSON 对象，不要输出任何其他文字或代码块标记。格式：\n" +
  '{"pages":[{"title":"页面标题","kind":"topic|entity|concept","summary":"一行摘要",' +
  '"aliases":["别名"],"body":"Markdown 正文"}]}\n' +
  "规则：\n" +
  "1. 本次输出 1~4 个页面；更新现有页面时 title 必须与清单中的标题完全一致，并输出该页合并新旧知识后的完整正文；\n" +
  "2. kind 取值：topic（主题）、entity（人物/产品/项目等实体）、concept（方法/理论等概念）；\n" +
  "3. body 用简体中文 Markdown，不超过 1200 字，条理清晰（要点列表、小标题不超过二级）；\n" +
  "4. 引用其他 Wiki 页面时使用 [[页面标题]] 语法，只允许引用现有页面清单中的页面或本次输出的页面，不要引用不存在的页面；\n" +
  "5. 所有事实必须来自条目内容或相关页面的既有内容，不得编造；条目中的个人观点要写明「作者认为」；\n" +
  "6. summary 不超过 60 字；aliases 是该页面主题的其他常见叫法（0~5 个，没有就给空数组）。";

/** 条目正文送入 Wiki 编译的截断上限（字符）。 */
export const WIKI_COMPILE_ITEM_CONTENT_LIMIT = 3000;

/** 随编译请求附带的现有页面目录上限（条）。 */
export const WIKI_COMPILE_CATALOG_LIMIT = 30;

/** 随编译请求附带完整正文的相关页面数上限。 */
export const WIKI_COMPILE_CONTEXT_PAGES_LIMIT = 3;

/** 相关页面正文节选上限（字符）。 */
export const WIKI_COMPILE_CONTEXT_BODY_LIMIT = 800;

/** 构造 Wiki 编译用户消息：条目素材 + 现有页面目录 + 相关页面正文节选。 */
export function buildWikiCompilePrompt(
  itemTitle: string,
  itemContent: string,
  catalogLines: string[],
  contextPages: { title: string; body: string }[],
): string {
  const parts: string[] = [
    "新知识条目：",
    `标题：${itemTitle}`,
    "内容：",
    truncatePlain(itemContent, WIKI_COMPILE_ITEM_CONTENT_LIMIT),
    "",
    "现有 Wiki 页面清单：",
  ];
  if (catalogLines.length === 0) {
    parts.push("（Wiki 目前为空）");
  } else {
    parts.push(...catalogLines);
  }
  if (contextPages.length > 0) {
    parts.push("", "相关页面当前内容（更新时以此为基础合并）：");
    for (const page of contextPages) {
      parts.push(`《${page.title}》`);
      parts.push(truncatePlain(page.body, WIKI_COMPILE_CONTEXT_BODY_LIMIT));
      parts.push("");
    }
  }
  return parts.join("\n");
}

/** 截断到上限（无标注后缀；编译素材哈希与提示词共用同一口径）。 */
export function truncatePlain(text: string, limit: number): string {
  if (text.length <= limit) {
    return text;
  }
  const lastChar = text.charCodeAt(limit - 1);
  const length = lastChar >= 0xd800 && lastChar <= 0xdbff ? limit - 1 : limit;
  return text.slice(0, length);
}

/** 正文送入模型前的截断上限（字符），控制 token 消耗。 */
export const MAX_CONTENT_LENGTH = 6000;

/** 超过此长度的正文走 map-reduce 分块摘要（否则单次即可）。 */
export const SUMMARY_CHUNK_SIZE = 6000;

/** 分块摘要的最大块数：约束长文的 LLM 调用次数与成本；超出部分截断。 */
export const SUMMARY_MAX_CHUNKS = 8;

export interface QaTurn {
  question: string;
  answer: string;
}

/** 截断到上限并附标注；不切断代理对（emoji / 扩展汉字）。 */
export function truncateText(text: string, limit: number): string {
  if (text.length <= limit) {
    return text;
  }
  const lastChar = text.charCodeAt(limit - 1);
  const length = lastChar >= 0xd800 && lastChar <= 0xdbff ? limit - 1 : limit;
  return `${text.slice(0, length)}…（已截断）`;
}

/** 构造用户消息：标题 + 截断后的正文。 */
export function buildUserPrompt(title: string, content: string): string {
  return `标题：${title}\n\n内容：\n${truncateText(content, MAX_CONTENT_LENGTH)}`;
}

/** 长文按块切分（用于 map-reduce 摘要），超出最大块数的部分截断。 */
export function chunkContent(content: string): string[] {
  const chunks: string[] = [];
  for (
    let offset = 0;
    offset < content.length && chunks.length < SUMMARY_MAX_CHUNKS;
    offset += SUMMARY_CHUNK_SIZE
  ) {
    chunks.push(content.slice(offset, offset + SUMMARY_CHUNK_SIZE));
  }
  return chunks;
}

/** map 阶段用户消息：标题 + 片段序号 + 片段正文。 */
export function buildSummaryMapPrompt(
  title: string,
  chunk: string,
  index: number,
  total: number,
): string {
  return `《${title}》片段 ${index}/${total}：\n${chunk}`;
}

/** reduce 阶段用户消息：把各片段要点综合去重。 */
export function buildSummaryReducePrompt(
  title: string,
  partialSummaries: string[],
): string {
  const lines = [
    `标题：${title}`,
    "",
    "以下是全文各片段的要点，请综合、去重后按要求的格式输出完整结果：",
    ...partialSummaries,
  ];
  return lines.join("\n");
}

/** 构造问答用户消息：资料块 + 近几轮对话 + 新问题。 */
export function buildQaUserPrompt(
  question: string,
  contextBlocks: string[],
  history?: QaTurn[],
): string {
  const parts: string[] = ["资料："];
  for (const block of contextBlocks) {
    parts.push(block, "");
  }
  if (history && history.length > 0) {
    parts.push("此前对话：");
    for (const turn of history) {
      parts.push(`问：${turn.question}`);
      parts.push(`答：${turn.answer}`);
    }
    parts.push("");
  }
  parts.push(`问题：${question}`);
  return parts.join("\n");
}

/** 构造问答 Agent 用户消息：历史对话 + 问题 + 动作轨迹 + 下一步指令。 */
export function buildQaAgentPrompt(
  question: string,
  history: QaTurn[] | undefined,
  transcript: string[],
): string {
  const parts: string[] = [];
  if (history && history.length > 0) {
    parts.push("此前对话：");
    for (const turn of history) {
      parts.push(`问：${turn.question}`);
      parts.push(`答：${turn.answer}`);
    }
    parts.push("");
  }
  parts.push(`用户问题：${question}`, "");
  parts.push("已执行的动作与结果：");
  if (transcript.length === 0) {
    parts.push("（尚未执行任何动作）");
  } else {
    parts.push(...transcript);
  }
  parts.push("", "请输出下一步动作（只输出一个 JSON 对象）：");
  return parts.join("\n");
}
