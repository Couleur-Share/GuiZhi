/**
 * 文字稿 AI 排版：语音转写的原始输出通常无标点、无分段，
 * 用 chat 模型分块整理（补标点 / 分自然段 / 去语气词），不改写内容。
 *
 * 模型解析走 ai-config.json 的 fastText 路由（回退 mainText → 默认 chat 模型），
 * 与转写一样由主进程直读配置；未配置文本模型或排版失败时，调用方保留原始文字稿。
 *
 * 每块输出做验收（标点密度 / 长度比 / 截断），不合格自动重试一次，
 * 仍不合格则整体抛错——思考类模型偶发"复读式敷衍"（原样返回不加标点），
 * 宁可明确失败，不把未排版结果当成功写库。
 */
import {
  chatCompletion,
  coreAIConfigService,
  type AIChatMessage,
  type AIChatResult,
  type AIClientConfig,
} from "@guizhi/core";

/** 单块上限：兼顾输出 token 限制与单请求耗时 */
const CHUNK_MAX_CHARS = 1600;
/** 超过该长度跳过排版，避免超长转写带来意外的 token 开销 */
const FORMAT_MAX_TOTAL_CHARS = 50_000;
/** 排版块要完整复述原文，思考类模型还会先烧推理 token，放宽单请求超时 */
const CHUNK_TIMEOUT_MS = 120_000;
/** 完整复述 1600 字 ≈ 1100 token，思考类模型另耗 2000+ 推理 token，需留足余量 */
const CHUNK_MAX_TOKENS = 6144;
/** 每块最多尝试次数（1 次重试）：思考类模型的敷衍输出有随机性，重试通常可恢复 */
const MAX_ATTEMPTS_PER_CHUNK = 2;

const SYSTEM_PROMPT = "你是文字稿排版助手，负责为语音转写文本补标点、分段。";

/**
 * 排版指令并入 user 消息（部分中转站会丢弃或弱化 system 消息），
 * 原文用显式分隔符包裹，避免被误当成对话内容。
 */
const FORMAT_INSTRUCTIONS = [
  "下面是一段语音转写的原始文字，请整理排版，要求：",
  "1. 为全文添加或修正标点符号；",
  "2. 按语义划分自然段，段落之间用空行分隔；",
  "3. 删除明显的口头填充词（嗯、啊、呃等）与转写残留的多余空格；",
  "4. 仅在十分确定时修正明显的同音错别字；",
  "5. 严格保持原文的表述与信息：不概括、不扩写、不调整语序、不遗漏内容、不添加原文没有的信息，整理后字数应与原文相当；",
  "6. 输入可能是长文字稿的中间片段，开头结尾不完整属正常，按原样整理；",
  "7. 直接输出整理后的正文，不要输出任何说明、标题或 Markdown 标记。",
].join("\n");

function buildChunkMessages(chunk: string): AIChatMessage[] {
  return [
    { role: "system", content: SYSTEM_PROMPT },
    {
      role: "user",
      content: `${FORMAT_INSTRUCTIONS}\n\n【待整理文字稿开始】\n${chunk}\n【待整理文字稿结束】`,
    },
  ];
}

/** 从共享 AI 配置解析排版用 chat 模型；未配置返回 null */
export function resolveTranscriptFormatterConfig(): AIClientConfig | null {
  try {
    const config = coreAIConfigService.read();
    const chatModels = config.models.filter(
      (model) => model.capabilities?.chat !== false,
    );
    const byId = (id: string | undefined) =>
      id ? chatModels.find((model) => model.id === id) : undefined;
    const model =
      byId(config.modelRouteDefaults.fastText) ??
      byId(config.modelRouteDefaults.mainText) ??
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
    console.warn("[media] 读取文字稿排版模型配置失败:", error);
    return null;
  }
}

/** 按句读边界把长文字稿切成排版块；无标点时退化到空格/硬切 */
export function splitTranscriptChunks(
  raw: string,
  maxChars = CHUNK_MAX_CHARS,
): string[] {
  const text = raw.trim();
  if (!text) {
    return [];
  }
  if (text.length <= maxChars) {
    return [text];
  }

  const boundary = /[。！？!?；;.\n\s]/;
  const chunks: string[] = [];
  let start = 0;
  while (start < text.length) {
    let end = Math.min(start + maxChars, text.length);
    if (end < text.length) {
      // 从窗口末尾向前找边界，避免把句子拦腰截断；最多回退半个窗口
      for (let i = end - 1; i > start + Math.floor(maxChars / 2); i--) {
        if (boundary.test(text[i])) {
          end = i + 1;
          break;
        }
      }
    }
    const chunk = text.slice(start, end).trim();
    if (chunk) {
      chunks.push(chunk);
    }
    start = end;
  }
  return chunks;
}

const PUNCTUATION_PATTERN = /[。，、！？：；…,.!?:;\n]/g;

/**
 * 排版输出验收：空输出、被 max_tokens 截断、长度跑偏、
 * 仍无标点分段（模型偷懒复读）都判不合格。返回不合格原因，合格返回 null。
 */
export function rejectFormattedChunk(
  rawChunk: string,
  result: AIChatResult,
): string | null {
  const cleaned = result.content.trim();
  if (!cleaned) {
    return "输出为空";
  }
  if (result.finishReason === "length") {
    return "输出被 max_tokens 截断";
  }
  const ratio = cleaned.length / rawChunk.length;
  if (ratio < 0.65 || ratio > 2) {
    return `输出长度异常（原文的 ${Math.round(ratio * 100)}%）`;
  }
  const punctuation = (cleaned.match(PUNCTUATION_PATTERN) ?? []).length;
  const required = Math.max(1, Math.floor(cleaned.length / 200));
  if (punctuation < required) {
    return `输出仍缺少标点分段（仅 ${punctuation} 处）`;
  }
  return null;
}

export interface FormatTranscriptOptions {
  signal?: AbortSignal;
  /** 测试注入：底层 chat 调用 */
  chat?: typeof chatCompletion;
}

/**
 * 分块排版整个文字稿并按空行拼接。
 * 任一块验收不通过（重试后）或请求失败即抛错，由调用方决定保留原始文字稿。
 */
export async function formatTranscript(
  raw: string,
  config: AIClientConfig,
  options?: FormatTranscriptOptions,
): Promise<string> {
  const text = raw.trim();
  if (!text) {
    return text;
  }
  if (text.length > FORMAT_MAX_TOTAL_CHARS) {
    console.warn(`[media] 文字稿过长（${text.length} 字），跳过 AI 排版`);
    return text;
  }

  const chat = options?.chat ?? chatCompletion;
  const chunks = splitTranscriptChunks(text);
  console.log(
    `[media] 文字稿排版开始（model=${config.model}，共 ${chunks.length} 块）`,
  );

  const formatted: string[] = [];
  for (let index = 0; index < chunks.length; index++) {
    const chunk = chunks[index];
    let accepted: string | null = null;
    let lastReason = "";
    for (let attempt = 1; attempt <= MAX_ATTEMPTS_PER_CHUNK; attempt++) {
      options?.signal?.throwIfAborted();
      let result: AIChatResult;
      try {
        result = await chat(config, buildChunkMessages(chunk), {
          temperature: 0.1,
          maxTokens: CHUNK_MAX_TOKENS,
          signal: options?.signal,
          timeoutMs: CHUNK_TIMEOUT_MS,
        });
      } catch (error) {
        // 请求本身失败（超时/限流等瞬时故障）同样给重试机会；外部取消直接透传
        if (options?.signal?.aborted || attempt >= MAX_ATTEMPTS_PER_CHUNK) {
          throw error;
        }
        console.warn(
          `[media] 第 ${index + 1}/${chunks.length} 块排版请求失败（${
            error instanceof Error ? error.message : String(error)
          }），重试一次`,
        );
        continue;
      }
      const reason = rejectFormattedChunk(chunk, result);
      if (!reason) {
        accepted = result.content.trim();
        break;
      }
      lastReason = reason;
      console.warn(
        `[media] 第 ${index + 1}/${chunks.length} 块排版验收不合格：${reason}${
          attempt < MAX_ATTEMPTS_PER_CHUNK ? "，重试一次" : ""
        }`,
      );
    }
    if (accepted === null) {
      throw new Error(
        `模型未按排版要求输出（第 ${index + 1}/${chunks.length} 块：${lastReason}）`,
      );
    }
    formatted.push(accepted);
  }
  return formatted.join("\n\n");
}
