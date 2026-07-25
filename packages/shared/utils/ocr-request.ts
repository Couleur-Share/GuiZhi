/**
 * 图片 OCR 的请求构造与响应解析。
 *
 * 两个调用方共用同一份：主进程在采集图文作品时自动逐图识别，
 * 渲染进程在图片条目详情页手动识别。
 */
import {
  extractTextFromChatResponse,
  toAnthropicContentParts,
  type MultimodalPart,
} from "./ai-protocol";
import type { AIProtocol } from "../types/ai";

/** 识别结果在正文中的小节标题。采集时自动写入、详情页重新识别时整节替换，两端必须一致 */
export const OCR_SECTION_HEADING = "## 图中文字";

export const OCR_PROMPT =
  "请识别图片中的所有文字，尽量保留原始排版结构，输出为 Markdown。" +
  "只输出识别出的文字内容，不要添加任何解释或前后缀。" +
  "如果图片中没有文字，输出：（图中未识别到文字）";

const OCR_MAX_TOKENS = 4000;

const IMAGE_MIME_BY_EXT: Record<string, string> = {
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  webp: "image/webp",
  gif: "image/gif",
  bmp: "image/bmp",
};

export function imageMimeFromFileName(fileName: string): string {
  const extension = fileName.split(".").pop()?.toLowerCase() ?? "";
  return IMAGE_MIME_BY_EXT[extension] ?? "image/png";
}

/**
 * 构造 OCR 请求体。
 *
 * Anthropic 的 /v1/messages 不认 image_url，图片必须拆成 base64 的
 * source 块，`max_tokens` 也是必填顶层字段；Gemini 走 OpenAI 兼容层，
 * 与 OpenAI 同构。
 */
export function buildOcrRequestBody(
  model: string,
  imageDataUrl: string,
  protocol: AIProtocol = "openai",
): string {
  const parts: MultimodalPart[] = [
    { type: "text", text: OCR_PROMPT },
    { type: "image_url", image_url: { url: imageDataUrl } },
  ];

  if (protocol === "anthropic") {
    return JSON.stringify({
      model,
      max_tokens: OCR_MAX_TOKENS,
      messages: [{ role: "user", content: toAnthropicContentParts(parts) }],
      stream: false,
    });
  }

  return JSON.stringify({
    model,
    messages: [{ role: "user", content: parts }],
    temperature: 0,
    max_tokens: OCR_MAX_TOKENS,
    stream: false,
  });
}

/** 解析对话响应正文（各协议结构不同，统一走共享提取器） */
export function parseOcrResponse(
  body: string,
  protocol: AIProtocol = "openai",
): string {
  const text = extractTextFromChatResponse(JSON.parse(body), protocol);
  if (text === null) {
    throw new Error("OCR 响应缺少文本内容");
  }
  return text.trim();
}
