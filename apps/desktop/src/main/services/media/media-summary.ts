/**
 * 视频/音频内容总结：以口播文字稿为素材，生成 dousnap 式的结构化 Markdown 总结
 * （总起段 + 加粗小节标题 + 嵌套要点），写入条目正文的「视频/音频总结」小节。
 *
 * 模型解析走 ai-config.json 的 mainText 路由（回退 fastText → 默认 chat 模型）：
 * 结构化长总结对模型要求高于排版/短摘要，质量优先。
 * 短文字稿单发；长文字稿分块提笔记再综合（map-reduce），与渲染进程摘要同一策略。
 *
 * 输出做确定性清洗（去代码围栏 / 分隔线，# 级标题降级为加粗行），
 * 保证写回正文后不破坏总结小节的边界锚点（下一个 `## ` 或 `---`）。
 */
import {
  chatCompletion,
  coreAIConfigService,
  type AIChatMessage,
  type AIClientConfig,
} from "@guizhi/core";
import { splitTranscriptChunks } from "./transcript-format";

/** 单发上限（字符）：超过则走 map-reduce 分块 */
const SINGLE_SHOT_MAX_CHARS = 10_000;
/** map 阶段每块字符数 */
const CHUNK_MAX_CHARS = 10_000;
/** 最大块数：约束超长文字稿的调用次数与成本，超出部分截断 */
const MAX_CHUNKS = 12;
/** 随提示词附带的简介上限（字符） */
const CONTEXT_MAX_CHARS = 500;
/** 结构化总结 + 思考类模型的推理消耗，与文字稿排版取同一口径 */
const SUMMARY_MAX_TOKENS = 6144;
/** 长输入 + 思考类模型，放宽单请求超时 */
const SUMMARY_TIMEOUT_MS = 180_000;
const SUMMARY_TEMPERATURE = 0.3;

const SUMMARY_SYSTEM_PROMPT =
  "你是视频内容总结助手。用户会提供视频（或音频）的标题、简介与完整口播文字稿，" +
  "请为这条内容拟一个准确的标题，并生成一份结构化的内容总结，让用户不看视频也能掌握核心内容。要求：\n" +
  "1. 第一行以「标题：」开头单独成行，输出你拟定的标题：15~30 字，准确概括核心内容，" +
  "书面语、不用夸张修辞与营销话术，不加书名号或引号；空一行后开始总结正文；\n" +
  "2. 总结开头用一段话概括主旨：讲了什么主题、解决什么问题、给出什么结论；\n" +
  "3. 正文按内容脉络分节：小节标题用「**加粗**」独立成行（可用 一、二、… 编号），" +
  "要点用列表呈现（最多两层嵌套），关键术语、数据与结论加粗；\n" +
  "4. 忠实于文字稿：保留具体的方法、步骤、参数与判断标准，不编造、不泛泛而谈；口语转为书面语；\n" +
  "5. 篇幅与信息量匹配：信息密集的长视频可写 300~800 字，内容简单的短视频精简即可；\n" +
  "6. 输出简体中文 Markdown；不要使用 #/## 级标题、--- 分隔线、表格或代码块；" +
  "不要输出「视频总结」这类总标题，也不要任何前言或结尾说明。";

const SUMMARY_MAP_SYSTEM_PROMPT =
  "你是视频内容总结助手。下面是视频口播文字稿的一个片段，请按内容脉络提取这一段的结构化笔记：" +
  "小节标题用「**加粗**」独立成行，要点用列表，保留具体的方法、步骤、数据与结论。" +
  "输出简体中文 Markdown，不要使用 #/## 级标题、--- 分隔线或代码块；" +
  "只输出笔记本身，不要前言或结尾说明。";

export interface MediaSummaryInput {
  title: string;
  /** 补充上下文（视频简介等），可选 */
  context?: string;
  transcript: string;
}

export interface MediaSummaryOptions {
  signal?: AbortSignal;
  /** 测试注入：底层 chat 调用 */
  chat?: typeof chatCompletion;
}

export interface MediaSummaryResult {
  /** 结构化总结正文（不含小节标题本身） */
  summary: string;
  /** AI 拟定的标题；模型未按协议输出时为 null，调用方保留原标题 */
  title: string | null;
}

/** 从共享 AI 配置解析总结用 chat 模型（mainText 优先）；未配置返回 null */
export function resolveMediaSummaryConfig(): AIClientConfig | null {
  try {
    const config = coreAIConfigService.read();
    const chatModels = config.models.filter(
      (model) => model.capabilities?.chat !== false,
    );
    const byId = (id: string | undefined) =>
      id ? chatModels.find((model) => model.id === id) : undefined;
    const model =
      byId(config.modelRouteDefaults.mainText) ??
      byId(config.modelRouteDefaults.fastText) ??
      chatModels.find((candidate) => candidate.isDefault) ??
      chatModels[0];
    if (
      !model ||
      !model.apiUrl?.trim() ||
      !model.apiKey?.trim() ||
      !model.model?.trim()
    ) {
      return null;
    }
    return {
      provider: model.provider,
      apiProtocol: model.apiProtocol,
      apiKey: model.apiKey.trim(),
      apiUrl: model.apiUrl.trim(),
      model: model.model.trim(),
    };
  } catch (error) {
    console.warn("[media] 读取内容总结模型配置失败:", error);
    return null;
  }
}

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

/** 首行若是「视频总结」这类冗余总标题则丢弃（正文小节标题由代码统一写入） */
const REDUNDANT_TITLE_PATTERN = /^\*{0,2}(?:视频|音频|内容)?总结\*{0,2}[:：]?$/;

/** 输出协议的标题行（允许模型加粗） */
const TITLE_LINE_PATTERN = /^\*{0,2}标题[:：]\s*(.+?)\*{0,2}$/;
/** AI 标题兜底上限（协议要求 15~30 字，防模型跑偏写成长句） */
const TITLE_MAX_CHARS = 60;

/** 归一化 AI 标题：压缩空白、去首尾书名号/引号、限长；无效返回 null */
function normalizeAiTitle(raw: string): string | null {
  const title = raw
    .replace(/\s+/g, " ")
    .trim()
    .replace(/^[《"“'‘]+|[》"”'’]+$/g, "")
    .trim();
  if (!title) {
    return null;
  }
  return title.length > TITLE_MAX_CHARS
    ? title.slice(0, TITLE_MAX_CHARS)
    : title;
}

/** 按输出协议拆出首行「标题：xxx」；未匹配则整段视为总结正文 */
function splitTitleFromSummary(raw: string): {
  title: string | null;
  body: string;
} {
  const lines = raw.split("\n");
  const match = TITLE_LINE_PATTERN.exec(lines[0]?.trim() ?? "");
  if (!match) {
    return { title: null, body: raw };
  }
  return { title: normalizeAiTitle(match[1]), body: lines.slice(1).join("\n") };
}

/**
 * 确定性清洗：# 级标题降级为加粗行、删除 --- 分隔线、去冗余总标题。
 * 保证总结体内不出现小节边界锚点（`## ` / `---`），再生成时不会误切。
 */
export function sanitizeMediaSummary(raw: string): string {
  const lines = stripWrappingCodeFence(raw.trim())
    .split("\n")
    .filter((line) => !/^\s*-{3,}\s*$/.test(line))
    .map((line) => {
      const heading = /^\s*#{1,6}\s+(.+)$/.exec(line);
      return heading ? `**${heading[1].trim()}**` : line;
    });
  while (lines.length > 0 && lines[0].trim() === "") {
    lines.shift();
  }
  if (lines.length > 0 && REDUNDANT_TITLE_PATTERN.test(lines[0].trim())) {
    lines.shift();
  }
  // 删行会留下相邻空行，折叠为单个空行
  return lines.join("\n").replace(/\n{3,}/g, "\n\n").trim();
}

function buildSummaryUserPrompt(input: MediaSummaryInput, body: string): string {
  const parts: string[] = [`标题：《${input.title.trim() || "无标题"}》`];
  const context = input.context?.trim();
  if (context) {
    parts.push(`简介：${context.slice(0, CONTEXT_MAX_CHARS)}`);
  }
  parts.push("", body);
  return parts.join("\n");
}

function buildMapUserPrompt(
  title: string,
  chunk: string,
  index: number,
  total: number,
): string {
  return `《${title.trim() || "无标题"}》文字稿片段 ${index}/${total}：\n${chunk}`;
}

async function runChat(
  config: AIClientConfig,
  messages: AIChatMessage[],
  options: MediaSummaryOptions | undefined,
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
    console.warn(`[media] 内容总结${label}输出被 max_tokens 截断`);
  }
  const content = result.content.trim();
  if (!content) {
    throw new Error(`模型未返回内容总结（${label}输出为空）`);
  }
  return content;
}

/**
 * 生成结构化内容总结与 AI 标题（输出协议：首行「标题：xxx」+ 总结正文）。
 * 文字稿为空或模型输出为空时抛错，由调用方决定是否阻断；
 * 标题未按协议输出时 title 为 null，调用方保留原标题。
 */
export async function generateMediaSummary(
  input: MediaSummaryInput,
  config: AIClientConfig,
  options?: MediaSummaryOptions,
): Promise<MediaSummaryResult> {
  const transcript = input.transcript.trim();
  if (!transcript) {
    throw new Error("文字稿为空，无法生成内容总结");
  }

  if (transcript.length <= SINGLE_SHOT_MAX_CHARS) {
    const content = await runChat(
      config,
      [
        { role: "system", content: SUMMARY_SYSTEM_PROMPT },
        {
          role: "user",
          content: buildSummaryUserPrompt(input, `文字稿：\n${transcript}`),
        },
      ],
      options,
      "",
    );
    return finalizeSummary(content);
  }

  // map：分块提取结构化笔记（保持时间顺序）
  const chunks = splitTranscriptChunks(transcript, CHUNK_MAX_CHARS);
  if (chunks.length > MAX_CHUNKS) {
    console.warn(
      `[media] 文字稿过长（${chunks.length} 块），仅总结前 ${MAX_CHUNKS} 块`,
    );
    chunks.length = MAX_CHUNKS;
  }
  const notes: string[] = [];
  for (const [index, chunk] of chunks.entries()) {
    options?.signal?.throwIfAborted();
    const note = await runChat(
      config,
      [
        { role: "system", content: SUMMARY_MAP_SYSTEM_PROMPT },
        {
          role: "user",
          content: buildMapUserPrompt(
            input.title,
            chunk,
            index + 1,
            chunks.length,
          ),
        },
      ],
      options,
      `（片段 ${index + 1}/${chunks.length}）`,
    );
    notes.push(note);
  }

  // reduce：综合各片段笔记输出完整总结
  options?.signal?.throwIfAborted();
  const noteBlocks = notes
    .map((note, index) => `【片段 ${index + 1} 笔记】\n${note}`)
    .join("\n\n");
  const reduced = await runChat(
    config,
    [
      { role: "system", content: SUMMARY_SYSTEM_PROMPT },
      {
        role: "user",
        content: buildSummaryUserPrompt(
          input,
          `以下是文字稿各片段的结构化笔记（按时间顺序），请综合去重，按要求输出完整的内容总结：\n\n${noteBlocks}`,
        ),
      },
    ],
    options,
    "（综合）",
  );
  return finalizeSummary(reduced);
}

const TITLE_SYSTEM_PROMPT =
  "你是内容标题助手。用户会提供一条图文内容的文案与图中文字，请为它拟一个准确的标题。要求：\n" +
  "1. 15~30 字，准确概括核心内容；\n" +
  "2. 书面语，不用夸张修辞与营销话术；不加书名号或引号；\n" +
  "3. 只输出标题本身，不要任何解释、前缀或后缀。";

/** 拟标题只需要很短的输出 */
const TITLE_MAX_TOKENS = 256;
/** 素材上限：标题只需抓主旨，喂全文既慢又贵 */
const TITLE_SOURCE_MAX_CHARS = 4000;

/**
 * 为图文内容拟标题（无总结）。
 *
 * 图文没有口播文字稿，套用视频总结那套提示词会答非所问；
 * 这里只做拟题，素材是文案 + 图中文字。模型输出无效时返回 null，调用方保留原标题。
 */
export async function generateContentTitle(
  source: string,
  config: AIClientConfig,
  options?: MediaSummaryOptions,
): Promise<string | null> {
  const material = source.trim().slice(0, TITLE_SOURCE_MAX_CHARS);
  if (!material) {
    return null;
  }
  const chat = options?.chat ?? chatCompletion;
  const result = await chat(
    config,
    [
      { role: "system", content: TITLE_SYSTEM_PROMPT },
      { role: "user", content: material },
    ],
    {
      temperature: SUMMARY_TEMPERATURE,
      maxTokens: TITLE_MAX_TOKENS,
      signal: options?.signal,
      timeoutMs: SUMMARY_TIMEOUT_MS,
    },
  );
  // 部分模型仍会按「标题：xxx」作答，一并归一
  const raw = stripWrappingCodeFence(result.content.trim());
  const line = raw.split("\n").find((entry) => entry.trim()) ?? "";
  const matched = TITLE_LINE_PATTERN.exec(line.trim());
  return normalizeAiTitle(matched ? matched[1] : line);
}

function finalizeSummary(content: string): MediaSummaryResult {
  const { title, body } = splitTitleFromSummary(
    stripWrappingCodeFence(content.trim()),
  );
  const summary = sanitizeMediaSummary(body);
  if (!summary) {
    throw new Error("模型未返回有效的内容总结");
  }
  return { title, summary };
}
