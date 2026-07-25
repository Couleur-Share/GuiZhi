/**
 * 主进程图片 OCR：采集图文作品时逐图识别，识别结果随条目一并入库。
 *
 * 与渲染进程详情页那条手动识别链路共用 `ocr-request` 的请求构造与解析，
 * 区别只在模型解析（这里直读 ai-config.json 的 visionText 路由，
 * 无需渲染进程转交凭据）和发请求的方式。
 */
import fs from "fs/promises";
import path from "path";
import { coreAIConfigService } from "@guizhi/core";
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
import type { AIProtocol } from "@guizhi/shared/types";
import { fetchWithNetworkProxy } from "../network-proxy";

const OCR_TIMEOUT_MS = 120_000;

export interface OcrModelConfig {
  apiUrl: string;
  apiKey: string;
  model: string;
  apiProtocol?: AIProtocol;
  provider?: string;
}

/** 从共享 AI 配置解析 visionText 路由模型；未配置返回 null */
export function resolveOcrConfig(): OcrModelConfig | null {
  try {
    const config = coreAIConfigService.read();
    const modelId = config.modelRouteDefaults.visionText;
    if (!modelId) {
      return null;
    }
    const model = config.models.find((candidate) => candidate.id === modelId);
    if (
      !model ||
      !model.apiUrl?.trim() ||
      !model.apiKey?.trim() ||
      !model.model?.trim()
    ) {
      return null;
    }
    return {
      apiUrl: model.apiUrl.trim(),
      apiKey: model.apiKey.trim(),
      model: model.model.trim(),
      apiProtocol: model.apiProtocol,
      provider: model.provider,
    };
  } catch (error) {
    console.warn("[media] 读取视觉模型配置失败:", error);
    return null;
  }
}

/** 识别本地图片文件中的文字 */
export async function recognizeImageFile(
  filePath: string,
  config: OcrModelConfig,
  signal?: AbortSignal,
): Promise<string> {
  const base64 = (await fs.readFile(filePath)).toString("base64");
  const mime = imageMimeFromFileName(path.basename(filePath));
  const protocol = resolveAIProtocol(config);
  const endpoint = buildChatEndpointFromBase(
    resolveProtocolBase(config.apiUrl, protocol),
  );

  const timeoutSignal = AbortSignal.timeout(OCR_TIMEOUT_MS);
  const response = await fetchWithNetworkProxy(endpoint, {
    method: "POST",
    headers: {
      ...buildHeadersForProtocol(protocol, config.apiKey),
      "Content-Type": "application/json",
    },
    body: buildOcrRequestBody(
      config.model,
      `data:${mime};base64,${base64}`,
      protocol,
    ),
    signal: signal ? AbortSignal.any([signal, timeoutSignal]) : timeoutSignal,
  });

  if (!response.ok) {
    const detail = (await response.text().catch(() => "")).slice(0, 300);
    throw new Error(`OCR 请求失败 (HTTP ${response.status}): ${detail}`);
  }

  const text = parseOcrResponse(await response.text(), protocol);
  if (!text) {
    throw new Error("OCR 未识别到内容");
  }
  return text;
}
