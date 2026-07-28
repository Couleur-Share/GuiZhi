/**
 * 文生图适配器：把一段提示词换成一张落进资产目录的图片。
 *
 * 这是本功能唯一的新轮子，麻烦全在协议分裂上：
 * - OpenAI 的 `gpt-image-*` **不接受** `response_format`（传了报
 *   invalid_request_error），它本来就只回 base64；而 `dall-e` 系与多数中转站
 *   模型默认返回 60 分钟过期的 URL，必须显式要 `b64_json`。按模型名分流。
 * - 真正的 16:9 只有 `gpt-image-2`（size 可任意，边长需为 16 的倍数、
 *   长短边比 ≤3:1）与 Gemini（imageConfig.aspectRatio）给得出；其余 OpenAI
 *   模型只有 1024x1024 / 1024x1536 / 1536x1024 三档，横版最宽是 3:2。
 * - Gemini 走原生 generateContent 而不是 OpenAI 兼容层：只有原生请求体里
 *   带得了 aspectRatio，兼容层出来的是方图。
 * - Anthropic 没有文生图 API，给可读提示而不是让用户撞 404。
 */
import fs from "fs/promises";
import path from "path";
import { coreAIConfigService } from "@guizhi/core";
import {
  buildGeminiImageEndpoint,
  buildHeadersForProtocol,
  buildImagesEndpointFromBase,
  resolveAIProtocol,
  resolveProtocolBase,
} from "@guizhi/shared/utils/ai-protocol";
import { ILLUSTRATION_ASSET_PREFIX } from "@guizhi/shared/utils/illustration-note";
import type {
  AIProtocol,
  IllustrationAspectRatio,
} from "@guizhi/shared/types";
import { fetchWithNetworkProxy } from "../network-proxy";
import { recordMainAiUsage } from "../ai-usage";
import { downloadToTempFile } from "../import/safe-fetch";
import {
  buildAssetFileName,
  getMediaAssetDir,
  MEDIA_SIZE_LIMITS,
} from "../import/media-files";

/** 生图动辄跑 30~60 秒，高质量档更久；`ai:httpRequest` 那 30 秒的默认值远远不够 */
const IMAGE_GEN_TIMEOUT_MS = 240_000;
/** 探测用的是最小尺寸最低质量，跑不了那么久；卡住时早点报错 */
const IMAGE_PROBE_TIMEOUT_MS = 90_000;
/**
 * 瞬时故障的退避重试间隔（毫秒），长度即额外尝试次数。
 *
 * 中转站按成功率在一池上游渠道之间轮询，而渠道之间的内容安全严格程度并不一致：
 * 同一份提示词落到 Azure 部署的渠道被判 safety、落到直连渠道就正常出图，
 * 状态码还常常不是官方那个 400（实测拿到过 429 + safety 文案）。
 * 这类抖动不该让一张已经策划好的图就此作废——生图按张计费又慢，
 * 一批五张里凭空少一张，用户只能再花一次钱重来。
 */
const RETRY_DELAYS_MS = [2_000, 6_000];
/**
 * 单张图的重试总预算。
 *
 * 正常一张 40~60 秒，而超时上限是 240 秒；不设总预算的话，一个卡死的上游
 * 会让单张图耗到 12 分钟、一批五张拖掉一小时。超预算就认输，
 * 剩下的交给面板里的「补生成」。
 */
const RETRY_TIME_BUDGET_MS = 300_000;
/** 连接测试的提示词：不含文字、不碰内容政策，画得越快越好 */
const IMAGE_PROBE_PROMPT =
  "A single small black circle centered on a plain white background.";
/** 响应体是 base64 的图，比对话响应大一到两个数量级 */
const RESPONSE_MAX_CHARS = 40 * 1024 * 1024;

export interface ImageGenModelConfig {
  apiUrl: string;
  apiKey: string;
  model: string;
  apiProtocol?: AIProtocol;
  provider?: string;
}

export interface GeneratedImage {
  data: Buffer;
  /** 含点的扩展名，如 `.png` */
  extension: string;
}

/** 从共享 AI 配置解析 imageGen 路由模型；未配置返回 null（不回退到对话模型） */
export function resolveImageGenConfig(): ImageGenModelConfig | null {
  try {
    const config = coreAIConfigService.read();
    const modelId = config.modelRouteDefaults.imageGen;
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
    console.warn("[illustration] 读取文生图模型配置失败:", error);
    return null;
  }
}

/**
 * OpenAI 的 size 参数。
 *
 * gpt-image-2 的 size 可任意（边长为 16 的倍数、长短边比 ≤3:1、总像素在
 * 655360~8294400 之间），1536x864 正好是 16:9；其余模型只有三档固定尺寸，
 * 横版最宽 3:2，只能靠提示词里的比例描述去逼近。
 */
export function openAiImageSize(
  model: string,
  aspectRatio: IllustrationAspectRatio,
): string {
  if (/gpt-image-2/i.test(model)) {
    if (aspectRatio === "16:9") return "1536x864";
    if (aspectRatio === "4:3") return "1280x960";
    return "1024x1024";
  }
  return aspectRatio === "1:1" ? "1024x1024" : "1536x1024";
}

export function buildOpenAIImageBody(
  model: string,
  prompt: string,
  aspectRatio: IllustrationAspectRatio,
  probe = false,
): string {
  const isGptImage = /gpt-image/i.test(model);
  const body: Record<string, unknown> = {
    model,
    prompt,
    n: 1,
    // 探测用最小的方图，别为一次连通性检查买一张宽幅大图
    size: probe ? "1024x1024" : openAiImageSize(model, aspectRatio),
  };
  // gpt-image-* 传 response_format 会被判为非法参数，它只回 b64_json；
  // 其余模型不显式要就会返回一个会过期的 URL
  if (!isGptImage) {
    body.response_format = "b64_json";
  } else if (probe) {
    // quality 的取值各家不同（dall-e-3 是 standard/hd），只对 gpt-image 系降档
    body.quality = "low";
  }
  return JSON.stringify(body);
}

export function buildGeminiImageBody(
  prompt: string,
  aspectRatio: IllustrationAspectRatio,
): string {
  return JSON.stringify({
    contents: [{ role: "user", parts: [{ text: prompt }] }],
    generationConfig: {
      responseModalities: ["IMAGE"],
      imageConfig: { aspectRatio },
    },
  });
}

const IMAGE_EXTENSION_BY_MIME: Record<string, string> = {
  "image/png": ".png",
  "image/jpeg": ".jpg",
  "image/jpg": ".jpg",
  "image/webp": ".webp",
};

function extensionFromMime(mime: string | undefined): string {
  return IMAGE_EXTENSION_BY_MIME[(mime ?? "").toLowerCase()] ?? ".png";
}

/** 解析结果：拿到 base64 直接用，拿到 URL 则需要再下载一次 */
export type ImageGenPayload =
  | { kind: "base64"; data: string; mime?: string }
  | { kind: "url"; url: string };

export function parseOpenAIImageResponse(raw: string): ImageGenPayload {
  const payload = JSON.parse(raw) as {
    data?: Array<{ b64_json?: unknown; url?: unknown }>;
    error?: { message?: unknown };
  };
  const first = payload.data?.[0];
  if (typeof first?.b64_json === "string" && first.b64_json) {
    return { kind: "base64", data: first.b64_json };
  }
  if (typeof first?.url === "string" && first.url) {
    return { kind: "url", url: first.url };
  }
  const message =
    typeof payload.error?.message === "string" ? payload.error.message : "";
  throw new Error(message || "文生图响应里没有图片数据");
}

export function parseGeminiImageResponse(raw: string): ImageGenPayload {
  const payload = JSON.parse(raw) as {
    candidates?: Array<{
      content?: {
        parts?: Array<{
          inlineData?: { data?: unknown; mimeType?: unknown };
          inline_data?: { data?: unknown; mime_type?: unknown };
        }>;
      };
    }>;
    error?: { message?: unknown };
  };
  for (const part of payload.candidates?.[0]?.content?.parts ?? []) {
    // REST 回 camelCase，部分中转站按 protobuf 原名回 snake_case
    const inline = part.inlineData ?? part.inline_data;
    const data = (inline as { data?: unknown } | undefined)?.data;
    if (typeof data === "string" && data) {
      const mime =
        (inline as { mimeType?: unknown })?.mimeType ??
        (inline as { mime_type?: unknown })?.mime_type;
      return {
        kind: "base64",
        data,
        mime: typeof mime === "string" ? mime : undefined,
      };
    }
  }
  const message =
    typeof payload.error?.message === "string" ? payload.error.message : "";
  throw new Error(message || "文生图响应里没有图片数据");
}

/** 少数中转站无视 response_format 仍回 URL；下载走采集链路那套 SSRF 防护 */
async function downloadGeneratedImage(url: string): Promise<GeneratedImage> {
  const matched = /\.(png|jpe?g|webp)(?:$|[?#])/i.exec(url);
  const extension = matched
    ? `.${matched[1].toLowerCase().replace("jpeg", "jpg")}`
    : ".png";
  const downloaded = await downloadToTempFile(url, {
    maxBytes: MEDIA_SIZE_LIMITS.image,
    fileName: `image${extension}`,
    accept: "image/*",
  });
  try {
    return { data: await fs.readFile(downloaded.filePath), extension };
  } finally {
    await fs.rm(downloaded.dir, { recursive: true, force: true });
  }
}

function decodeBase64Image(payload: {
  data: string;
  mime?: string;
}): GeneratedImage {
  // 部分实现回的是 data URL，把前缀剥掉再解
  const match = /^data:(image\/[\w.+-]+);base64,(.*)$/s.exec(payload.data);
  const base64 = (match ? match[2] : payload.data).replace(/\s/g, "");
  const data = Buffer.from(base64, "base64");
  if (data.length === 0) {
    throw new Error("文生图返回的图片数据为空");
  }
  if (data.length > MEDIA_SIZE_LIMITS.image) {
    throw new Error("文生图返回的图片超过大小上限");
  }
  return { data, extension: extensionFromMime(match?.[1] ?? payload.mime) };
}

interface ImageEndpoint {
  url: string;
  body: string;
  headers: Record<string, string>;
  parse: (raw: string) => ImageGenPayload;
}

function resolveEndpoint(
  config: ImageGenModelConfig,
  prompt: string,
  aspectRatio: IllustrationAspectRatio,
  probe: boolean,
): ImageEndpoint {
  const protocol = resolveAIProtocol(config);
  if (protocol === "anthropic") {
    throw new Error(
      "Anthropic 协议没有文生图接口——请把配图路由指向 OpenAI 或 Gemini 协议的模型",
    );
  }

  const base = resolveProtocolBase(config.apiUrl, protocol);
  if (protocol === "gemini") {
    const url = buildGeminiImageEndpoint(base, config.model);
    if (!url) {
      throw new Error("无法拼出 Gemini 生图端点，请检查该模型的 API 地址");
    }
    return {
      url,
      body: buildGeminiImageBody(prompt, aspectRatio),
      headers: buildHeadersForProtocol(protocol, config.apiKey, {
        useNativeGeminiAuth: true,
      }),
      parse: parseGeminiImageResponse,
    };
  }

  const url = buildImagesEndpointFromBase(base);
  if (!url) {
    throw new Error("无法拼出文生图端点，请检查该模型的 API 地址");
  }
  return {
    url,
    body: buildOpenAIImageBody(config.model, prompt, aspectRatio, probe),
    headers: buildHeadersForProtocol(protocol, config.apiKey),
    parse: parseOpenAIImageResponse,
  };
}

export interface GenerateImageOptions {
  signal?: AbortSignal;
  /** 连接测试：最小尺寸 + 最低质量档，只验证这条链路通不通 */
  probe?: boolean;
  /** 测试注入：退避间隔 */
  retryDelaysMs?: number[];
  /** 测试注入：底层 fetch */
  fetchImpl?: typeof fetchWithNetworkProxy;
}

/** 单次尝试的失败；retryable 决定要不要换一次上游重发 */
class ImageGenAttemptError extends Error {
  readonly retryable: boolean;

  constructor(message: string, retryable: boolean) {
    super(message);
    this.name = "ImageGenAttemptError";
    this.retryable = retryable;
  }
}

/**
 * 换一次上游重发有没有意义，只按状态码判。
 *
 * 5xx 是上游故障；429 在中转站那里既可能是限流、也可能是某个严格渠道把内容
 * 安全拦截塞进了这个状态码（官方那条是 400）——两种都值得换个渠道再来一次。
 * 其余 4xx（模型名不对、余额不足、400 moderation_blocked）重发一万次
 * 也是同一个结果，只会白白多等两轮。
 */
export function isRetryableImageStatus(status: number): boolean {
  return status === 429 || status >= 500;
}

/** 内容安全拦截。状态码经中转站转手后并不可靠，认这个 code 不认状态码 */
const MODERATION_CODE = "moderation_blocked";

/** 拆出错误体里的 message 与 code；非 JSON（被 WAF 拦下的整页 HTML）原文截断 */
function parseErrorBody(text: string): { message: string; code: string } {
  try {
    const parsed = JSON.parse(text) as {
      error?: { message?: unknown; code?: unknown };
    };
    const message =
      typeof parsed.error?.message === "string" ? parsed.error.message : "";
    const code =
      typeof parsed.error?.code === "string" ? parsed.error.code : "";
    if (message) {
      return { message: message.slice(0, 300), code };
    }
  } catch {
    // 不是 JSON，退回原文截断
  }
  return { message: text.replace(/\s+/g, " ").slice(0, 300), code: "" };
}

/**
 * HTTP 失败的说法。
 *
 * `error.code` 必须带上：它是「换个渠道重发就好」与「这句话改不了就是过不去」
 * 之间唯一的分界，只给一个状态码，用户只会一遍遍地点重试。
 */
export function describeImageHttpFailure(status: number, body: string): string {
  const { message, code } = parseErrorBody(body);
  if (code === MODERATION_CODE) {
    return `文生图被内容安全拦截，改写图题、物件或标注词后再试 (HTTP ${status} ${code}): ${message}`;
  }
  return `文生图请求失败 (HTTP ${status}${code ? ` ${code}` : ""}): ${message}`;
}

/**
 * 这次失败是不是内容安全拦截。
 *
 * 认的是消息文本而不是错误类型：重试用尽那一路会把原错误重新包一层
 * （`new Error(..., { cause })`），类型判断到那时已经失效，而 code 一直在消息里。
 */
export function isModerationBlockedError(error: unknown): boolean {
  return describe(error).includes(MODERATION_CODE);
}

function describeNetworkError(error: unknown, timeoutMs: number): string {
  if (error instanceof Error && error.name === "TimeoutError") {
    return `文生图请求超时（${Math.round(timeoutMs / 1000)} 秒无响应）`;
  }
  return `文生图请求失败：${describe(error)}`;
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/** 可被取消打断的等待：退避期间点了停止，不该还傻等几秒 */
function wait(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new Error("已取消"));
      return;
    }
    let timer: NodeJS.Timeout | undefined;
    const onAbort = () => {
      clearTimeout(timer);
      reject(new Error("已取消"));
    };
    timer = setTimeout(() => {
      signal?.removeEventListener("abort", onAbort);
      resolve();
    }, ms);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

/** 发一次请求并把图取回来；失败按「值不值得重发」分类后抛出 */
async function requestImage(
  endpoint: ImageEndpoint,
  probe: boolean,
  options?: GenerateImageOptions,
): Promise<GeneratedImage> {
  const timeoutMs = probe ? IMAGE_PROBE_TIMEOUT_MS : IMAGE_GEN_TIMEOUT_MS;
  const timeoutSignal = AbortSignal.timeout(timeoutMs);
  const send = options?.fetchImpl ?? fetchWithNetworkProxy;

  let response: Response;
  try {
    response = await send(endpoint.url, {
      method: "POST",
      headers: { ...endpoint.headers, "Content-Type": "application/json" },
      body: endpoint.body,
      signal: options?.signal
        ? AbortSignal.any([options.signal, timeoutSignal])
        : timeoutSignal,
    });
  } catch (error) {
    // 用户点的停止原样抛出，别被当成瞬时故障又重发两次
    if (options?.signal?.aborted) {
      throw error;
    }
    throw new ImageGenAttemptError(describeNetworkError(error, timeoutMs), true);
  }

  const text = await response.text();
  if (!response.ok) {
    throw new ImageGenAttemptError(
      describeImageHttpFailure(response.status, text),
      isRetryableImageStatus(response.status),
    );
  }
  if (text.length > RESPONSE_MAX_CHARS) {
    throw new Error("文生图响应过大，已中止");
  }

  const payload = endpoint.parse(text);
  return payload.kind === "url"
    ? await downloadGeneratedImage(payload.url)
    : decodeBase64Image(payload);
}

/** 按提示词生成一张图；返回图片字节，落盘交给 saveIllustrationAsset */
export async function generateImage(
  prompt: string,
  aspectRatio: IllustrationAspectRatio,
  config: ImageGenModelConfig,
  options?: GenerateImageOptions,
): Promise<GeneratedImage> {
  const probe = options?.probe === true;
  const endpoint = resolveEndpoint(config, prompt, aspectRatio, probe);
  // 探测不重试：它按张真实计费，一次连接测试悄悄变成三次不合适；
  // 何况这一步的职责就是如实报出第一手的失败原因
  const delays = probe ? [] : (options?.retryDelaysMs ?? RETRY_DELAYS_MS);
  const deadline = Date.now() + RETRY_TIME_BUDGET_MS;

  for (let attempt = 0; ; attempt++) {
    try {
      const image = await requestImage(endpoint, probe, options);
      // 按「一张图」记而不是按「一次 HTTP」记：重试只发生在 5xx / 429 上，
      // 那些请求没出图也不计费，按尝试次数记会凭空放大账单
      recordMainAiUsage({ scenario: "illustration", model: config.model });
      return image;
    } catch (error) {
      // 用户点的停止不是失败，也不该被写成「已自动重试 N 次」
      if (options?.signal?.aborted) {
        throw error;
      }
      const retryable =
        error instanceof ImageGenAttemptError && error.retryable;
      const exhausted = attempt >= delays.length || Date.now() >= deadline;
      if (!retryable || exhausted) {
        recordMainAiUsage({
          scenario: "illustration",
          model: config.model,
          failed: true,
        });
        // 重试过还是没成，得说出来：这时候用户手动再点一次多半也一样
        throw attempt > 0
          ? new Error(`${describe(error)}（已自动重试 ${attempt} 次）`, {
              cause: error,
            })
          : error;
      }
      console.warn(
        `[illustration] 第 ${attempt + 1} 次生图失败（${describe(error)}），${delays[attempt]} 毫秒后重试`,
      );
      await wait(delays[attempt], options?.signal);
    }
  }
}

/**
 * 连接测试。
 *
 * 真的画一张最小尺寸的图，不做「只探端点」的假测试：文生图最常见的失败是
 * 模型名不对或账号没开通这个模型，只打 /v1/models 一概查不出来。
 * 代价是这一次会真实计费——设置页的能力说明里写明了这点。
 */
export async function testImageGenConfig(
  config: ImageGenModelConfig,
): Promise<{ latency: number }> {
  const startedAt = Date.now();
  await generateImage(IMAGE_PROBE_PROMPT, "1:1", config, { probe: true });
  return { latency: Date.now() - startedAt };
}

/** 写进资产目录，返回资产文件名（正文里以 local-image:// 引用它） */
export async function saveIllustrationAsset(
  image: GeneratedImage,
): Promise<string> {
  const targetDir = getMediaAssetDir("image");
  await fs.mkdir(targetDir, { recursive: true });
  const assetFileName = buildAssetFileName(
    image.extension,
    ILLUSTRATION_ASSET_PREFIX,
  );
  await fs.writeFile(path.join(targetDir, assetFileName), image.data);
  return assetFileName;
}
