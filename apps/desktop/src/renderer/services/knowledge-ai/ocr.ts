/**
 * 图片 OCR：走 ocr 场景（visionText 路由）的多模态 chat 请求，
 * 图片经 image:readBase64 IPC 读取后以 data URL 内联。
 */
import {
  buildChatEndpointFromBase,
  buildHeadersForProtocol,
  extractTextFromChatResponse,
  resolveAIProtocol,
  resolveProtocolBase,
  toAnthropicContentParts,
  type MultimodalPart,
} from "@guizhi/shared/utils/ai-protocol";
import type { AIProtocol } from "@guizhi/shared/types";
import { useSettingsStore } from "../../stores/settings.store";
import { resolveScenarioAIConfig } from "../ai-defaults";
import type { AIConfig } from "../ai";
import { AiNotConfiguredError } from "./ai-invoke";

const OCR_TIMEOUT_MS = 120_000;
const OCR_PROMPT =
  "请识别图片中的所有文字，尽量保留原始排版结构，输出为 Markdown。" +
  "只输出识别出的文字内容，不要添加任何解释或前后缀。" +
  "如果图片中没有文字，输出：（图中未识别到文字）";

const IMAGE_MIME_BY_EXT: Record<string, string> = {
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  webp: "image/webp",
  gif: "image/gif",
  bmp: "image/bmp",
};

function resolveOcrConfig(): AIConfig | null {
  const state = useSettingsStore.getState();
  // ocr 场景固定走 visionText 路由，候选已按 vision 能力过滤
  return resolveScenarioAIConfig({
    aiModels: state.aiModels,
    scenarioModelDefaults: state.scenarioModelDefaults,
    modelRouteDefaults: state.modelRouteDefaults,
    scenario: "ocr",
    allowLegacyFallback: false,
    aiProvider: state.aiProvider,
    aiApiProtocol: state.aiApiProtocol,
    aiApiKey: state.aiApiKey,
    aiApiUrl: state.aiApiUrl,
    aiModel: state.aiModel,
  });
}

export function isOcrConfigured(): boolean {
  return resolveOcrConfig() !== null;
}

const OCR_MAX_TOKENS = 4000;

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

export async function recognizeImageText(
  assetFileName: string,
): Promise<string> {
  const config = resolveOcrConfig();
  if (!config) {
    throw new AiNotConfiguredError();
  }

  const base64 = await window.electron?.readImageBase64?.(assetFileName);
  if (!base64) {
    throw new Error("无法读取图片资产文件");
  }
  const extension = assetFileName.split(".").pop()?.toLowerCase() ?? "png";
  const mime = IMAGE_MIME_BY_EXT[extension] ?? "image/png";

  const protocol = resolveAIProtocol(config);
  const endpoint = buildChatEndpointFromBase(
    resolveProtocolBase(config.apiUrl, protocol),
  );
  const response = await window.api.ai.request({
    method: "POST",
    url: endpoint,
    headers: buildHeadersForProtocol(protocol, config.apiKey),
    body: buildOcrRequestBody(
      config.model,
      `data:${mime};base64,${base64}`,
      protocol,
    ),
    timeoutMs: OCR_TIMEOUT_MS,
  });
  if (!response.ok) {
    const detail = (response.error || response.body || "").slice(0, 300);
    throw new Error(`OCR 请求失败 (HTTP ${response.status}): ${detail}`);
  }
  const text = parseOcrResponse(response.body, protocol);
  if (!text) {
    throw new Error("OCR 未识别到内容");
  }
  return text;
}
