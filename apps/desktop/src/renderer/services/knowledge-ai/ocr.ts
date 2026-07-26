/**
 * 图片 OCR：走 ocr 场景（visionText 路由）的多模态 chat 请求，
 * 图片经 image:readBase64 IPC 读取后以 data URL 内联。
 */
import {
  buildChatEndpointFromBase,
  buildHeadersForProtocol,
  resolveAIProtocol,
  resolveProtocolBase,
} from "@guizhi/shared/utils/ai-protocol";
import {
  buildOcrRequestBody,
  imageMimeFromFileName,
  parseOcrResponse,
} from "@guizhi/shared/utils/ocr-request";
import { useSettingsStore } from "../../stores/settings.store";
import { resolveScenarioAIConfig } from "../ai-defaults";
import type { AIConfig } from "../ai";
import { AiNotConfiguredError, recordAiUsage } from "./ai-invoke";

const OCR_TIMEOUT_MS = 120_000;

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
  const mime = imageMimeFromFileName(assetFileName);
  const protocol = resolveAIProtocol(config);
  const endpoint = buildChatEndpointFromBase(
    resolveProtocolBase(config.apiUrl, protocol),
  );
  let response;
  try {
    response = await window.api.ai.request({
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
  } catch (error) {
    recordAiUsage({ scenario: "ocr", model: config.model, failed: true });
    throw error;
  }
  if (!response.ok) {
    recordAiUsage({ scenario: "ocr", model: config.model, failed: true });
    const detail = (response.error || response.body || "").slice(0, 300);
    throw new Error(`OCR 请求失败 (HTTP ${response.status}): ${detail}`);
  }
  // 图文采集逐张调视觉模型，9 张图就是 9 次，不记等于漏掉一整类消耗
  recordAiUsage({ scenario: "ocr", model: config.model });
  const text = parseOcrResponse(response.body, protocol);
  if (!text) {
    throw new Error("OCR 未识别到内容");
  }
  return text;
}
