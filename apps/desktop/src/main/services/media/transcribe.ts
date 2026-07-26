/**
 * 音视频远程转写：读取本地媒体资产，multipart 上传到
 * OpenAI 兼容的 /audio/transcriptions 接口，返回文字稿。
 *
 * 模型解析走 ai-config.json 的 audioText 路由（主进程直读，
 * 无需渲染进程转交凭据）。
 */
import fs, { openAsBlob } from "fs";
import os from "os";
import path from "path";
import { randomUUID } from "crypto";
import { coreAIConfigService } from "@guizhi/core";
import {
  buildTranscriptionsEndpointFromBase,
  resolveAIProtocol,
  resolveProtocolBase,
} from "@guizhi/shared/utils/ai-protocol";
import type { AIProtocol } from "@guizhi/shared/types";
import { fetchWithNetworkProxy } from "../network-proxy";

const TRANSCRIBE_TIMEOUT_MS = 10 * 60 * 1000;
const TEST_TIMEOUT_MS = 60 * 1000;

export interface TranscriptionModelConfig {
  apiUrl: string;
  apiKey: string;
  model: string;
  apiProtocol?: AIProtocol;
  provider?: string;
}

/** 从共享 AI 配置解析 audioText 路由模型；未配置返回 null */
export function resolveTranscriptionConfig(): TranscriptionModelConfig | null {
  try {
    const config = coreAIConfigService.read();
    const modelId = config.modelRouteDefaults.audioText;
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
    console.warn("[media] 读取转写模型配置失败:", error);
    return null;
  }
}

/**
 * 转写端点。
 *
 * 只有 OpenAI 兼容协议提供 multipart 的 /audio/transcriptions：Anthropic
 * 根本没有转写 API，Gemini 的音频要走 generateContent 内联。此前一律拼
 * /v1/audio/transcriptions，配错协议的用户只会撞上一个莫名的 404。
 */
export function buildTranscriptionsEndpoint(
  config: Pick<
    TranscriptionModelConfig,
    "apiUrl" | "apiProtocol" | "provider"
  >,
): string {
  const protocol = resolveAIProtocol(config);
  return buildTranscriptionsEndpointFromBase(
    resolveProtocolBase(config.apiUrl, protocol),
  );
}

/** 发起转写请求并返回原始文本（可能为空串——测试静音样本时属正常） */
async function requestTranscription(
  filePath: string,
  config: TranscriptionModelConfig,
  signal: AbortSignal,
): Promise<string> {
  const endpoint = buildTranscriptionsEndpoint(config);
  if (!endpoint) {
    throw new Error(
      "当前模型的接口协议不支持语音转写：请改用 OpenAI 兼容的转写服务，或在设置里启用内置转写引擎",
    );
  }

  const form = new FormData();
  form.append("file", await openAsBlob(filePath), path.basename(filePath));
  form.append("model", config.model);
  form.append("response_format", "json");

  const response = await fetchWithNetworkProxy(endpoint, {
    method: "POST",
    headers: { Authorization: `Bearer ${config.apiKey}` },
    body: form,
    signal,
  });

  if (!response.ok) {
    const detail = (await response.text().catch(() => "")).slice(0, 300);
    throw new Error(`转写请求失败 (HTTP ${response.status}): ${detail}`);
  }

  const payload = (await response.json()) as { text?: unknown };
  if (typeof payload.text !== "string") {
    throw new Error("转写接口返回格式异常（缺少 text 字段）");
  }
  return payload.text;
}

/**
 * CJK 文字与全角标点。两者相邻处出现的空格是转写按 VAD 切段再拼接留下的
 * 噪音（实测「所谓的高手 都是」「持っ ていけない」），不是分词。
 */
const CJK_RANGES =
  "\\u3000-\\u303f\\u3040-\\u30ff\\u3400-\\u4dbf\\u4e00-\\u9fff\\uac00-\\ud7af\\uff00-\\uffef";
const CJK_ADJACENT_SPACE = new RegExp(
  `([${CJK_RANGES}])[ \\t]+(?=[${CJK_RANGES}])`,
  "g",
);
const PUNCTUATION_RUN = /[，、；：。！？]{2,}/g;
const SENTENCE_END = "。！？";

/**
 * 转写文本清洗。
 *
 * 开了 use_itn 的 SenseVoice 会在 VAD 段边界处叠出「高手，。」这类
 * 双标点——前一段收尾的逗号撞上后一段起头的句号。折叠时句末标点优先，
 * 因为叠加位置本身就是一次停顿。
 *
 * 对已经排版规整的云端转写结果是幂等的，不会改动 ASCII 文本的空格。
 */
export function cleanTranscriptText(raw: string): string {
  return raw
    .replace(CJK_ADJACENT_SPACE, "$1")
    .replace(PUNCTUATION_RUN, (run) => {
      for (const char of run) {
        if (SENTENCE_END.includes(char)) {
          return char;
        }
      }
      return run[0];
    })
    .trim();
}

export async function transcribeMediaFile(
  filePath: string,
  config: TranscriptionModelConfig,
  signal?: AbortSignal,
): Promise<string> {
  const timeoutSignal = AbortSignal.timeout(TRANSCRIBE_TIMEOUT_MS);
  const text = cleanTranscriptText(
    await requestTranscription(
      filePath,
      config,
      signal ? AbortSignal.any([signal, timeoutSignal]) : timeoutSignal,
    ),
  );
  if (!text) {
    throw new Error("转写接口未返回有效文本");
  }
  return text;
}

/** 生成指定时长的 16kHz 单声道 16bit 静音 WAV（连通性测试样本） */
export function writeSilentWav(filePath: string, seconds: number): void {
  const sampleRate = 16000;
  const sampleCount = Math.max(1, Math.round(sampleRate * seconds));
  const dataSize = sampleCount * 2;
  const buffer = Buffer.alloc(44 + dataSize);
  buffer.write("RIFF", 0, "ascii");
  buffer.writeUInt32LE(36 + dataSize, 4);
  buffer.write("WAVE", 8, "ascii");
  buffer.write("fmt ", 12, "ascii");
  buffer.writeUInt32LE(16, 16); // fmt 块大小
  buffer.writeUInt16LE(1, 20); // PCM
  buffer.writeUInt16LE(1, 22); // 单声道
  buffer.writeUInt32LE(sampleRate, 24);
  buffer.writeUInt32LE(sampleRate * 2, 28); // 字节率
  buffer.writeUInt16LE(2, 32); // 块对齐
  buffer.writeUInt16LE(16, 34); // 位深
  buffer.write("data", 36, "ascii");
  buffer.writeUInt32LE(dataSize, 40);
  fs.writeFileSync(filePath, buffer);
}

/**
 * 转写模型连通性测试：用静音样本发起一次真实转写请求，
 * 走与生产完全相同的链路（端点拼接 / 鉴权 / 代理）。
 * 接口正常应答即视为成功（静音返回空文本属正常）。
 */
export async function testTranscriptionConfig(
  config: TranscriptionModelConfig,
): Promise<{ latency: number }> {
  const dir = path.join(
    os.tmpdir(),
    `guizhi-transcribe-test-${randomUUID().slice(0, 8)}`,
  );
  const samplePath = path.join(dir, "sample.wav");
  fs.mkdirSync(dir, { recursive: true });
  try {
    writeSilentWav(samplePath, 0.5);
    const start = Date.now();
    await requestTranscription(
      samplePath,
      config,
      AbortSignal.timeout(TEST_TIMEOUT_MS),
    );
    return { latency: Date.now() - start };
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}
