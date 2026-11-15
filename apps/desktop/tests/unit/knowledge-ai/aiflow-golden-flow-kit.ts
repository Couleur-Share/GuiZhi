/**
 * aiflow 流程层 golden 的共用件（非测试文件）：文本规格与指纹、请求压缩、流程跑一遍并收集结果。
 * 在 aiflow-golden-kit.ts（假传输、警告捕获、golden 读写）之上，给 knowledge / semantic / wiki / qa 等
 * 「编排流程」类的 golden 共用。Go 侧对应 apps/desktop-go/aiflow/golden_flow_test.go。
 *
 * 约定：
 *   - 长输入用「规格」描述（{cjk:n} / {ascii:n} / {lit:"…"} 的拼接），golden 里存规格与指纹，
 *     Go 侧按同一生成器重建后先核对指纹，再喂给被测函数；
 *   - 孤立代理项不入 golden：TS 能产生、Go 的 string 表示不了（见 native-ui-development.md §8.4），
 *     safeText 在导出时强制检查，用例不会让切口落在代理对中间；
 *   - 请求体超过 BODY_LIMIT 只记指纹，避免 golden 体积失控。
 */
import { createHash } from "node:crypto";
import { useSettingsStore } from "../../../src/renderer/stores/settings.store";
import { mergeSettingsState } from "../../../src/renderer/stores/settings/settings-persistence";
import {
  captureWarnings,
  describeError,
  installFakeTransport,
  type RecordedRequest,
  type ScriptStep,
} from "./aiflow-golden-kit";

export type Raw = Record<string, unknown>;

// ---- 文本规格与指纹 ----

const CJK = "知识库归纳整理摘要标签分类检索问答";
/** 纯 BMP 的确定性中文文本，恰好 n 个 UTF-16 码元。 */
export const cjk = (n: number): string => CJK.repeat(Math.ceil(n / CJK.length)).slice(0, n);
export const ascii = (n: number): string => "abcdefghij".repeat(Math.ceil(n / 10)).slice(0, n);

export type Part = { cjk: number } | { ascii: number } | { lit: string };
export type Spec = string | Part[];

export const C = (n: number): Part => ({ cjk: n });
export const A = (n: number): Part => ({ ascii: n });
export const L = (lit: string): Part => ({ lit });

export function render(spec: Spec): string {
  if (typeof spec === "string") return spec;
  return spec
    .map((part) => {
      if ("cjk" in part) return cjk(part.cjk);
      if ("ascii" in part) return ascii(part.ascii);
      return part.lit;
    })
    .join("");
}

export const sha256 = (text: string): string => createHash("sha256").update(text, "utf8").digest("hex");

/** 导出前强制检查：不含孤立代理项（Go 的 string 无法表示）。 */
export function safeText(text: string): string {
  for (let i = 0; i < text.length; i++) {
    const code = text.charCodeAt(i);
    if (code >= 0xd800 && code <= 0xdbff) {
      const next = text.charCodeAt(i + 1);
      if (!(next >= 0xdc00 && next <= 0xdfff)) throw new Error(`用例产生了孤立高代理项（位置 ${i}）`);
      i++;
    } else if (code >= 0xdc00 && code <= 0xdfff) {
      throw new Error(`用例产生了孤立低代理项（位置 ${i}）`);
    }
  }
  return text;
}

export interface Digest {
  length: number;
  sha256: string;
  head: string;
  tail: string;
}

/** 文本指纹：UTF-16 长度 + sha256 + 首尾各 16 个码元（便于失败时肉眼定位）。 */
export function digest(text: string): Digest {
  safeText(text);
  return { length: text.length, sha256: sha256(text), head: text.slice(0, 16), tail: text.slice(-16) };
}

/** 短文本原样、长文本只留指纹。 */
const SHORT_LIMIT = 300;
export const compactText = (text: string): string | Digest =>
  text.length <= SHORT_LIMIT ? safeText(text) : digest(text);

export const EMOJI = "😀"; // 2 个码元
export const HAN_EXT = "𠀀"; // U+20000，2 个码元

// ---- 设置夹具 ----

export const model = (id: string, over: Raw = {}): Raw => ({
  id,
  provider: "custom",
  apiProtocol: "openai",
  apiKey: `sk-${id}`,
  apiUrl: "https://api.example.com/v1",
  model: `model-${id}`,
  ...over,
});
export const chat = (id: string, over: Raw = {}): Raw => model(id, { capabilities: { chat: true }, ...over });
export const embeddingModel = (id: string, over: Raw = {}): Raw =>
  model(id, { capabilities: { chat: false, embedding: true }, apiUrl: "https://embed.example.com/v1", ...over });

export const TWO_TIER: Raw = {
  aiModels: [
    chat("main", { isDefault: true, provider: "openai", apiUrl: "https://api.openai.com/v1" }),
    chat("fast", { apiUrl: "https://fast.example.com" }),
  ],
  modelRouteDefaults: { mainText: "main", fastText: "fast" },
};

/** 两档对话模型 + 一个 embedding 模型。 */
export const WITH_EMBEDDING: Raw = {
  aiModels: [...(TWO_TIER.aiModels as Raw[]), embeddingModel("embed")],
  modelRouteDefaults: { mainText: "main", fastText: "fast", embedding: "embed" },
};

export const LEGACY_ONLY: Raw = {
  aiProvider: "custom",
  aiApiKey: "sk-legacy",
  aiApiUrl: "https://legacy.example.com",
  aiModel: "legacy-1",
};

export const body = (content: string, finish: string | null = "stop", usage?: unknown): string =>
  JSON.stringify({ choices: [{ message: { content }, finish_reason: finish }], ...(usage ? { usage } : {}) });

export const tokens = (prompt: number, completion: number): { prompt_tokens: number; completion_tokens: number } => ({
  prompt_tokens: prompt,
  completion_tokens: completion,
});

export const range = (n: number): number[] => Array.from({ length: n }, (_, i) => i + 1);

// ---- 请求压缩与流程跑一遍 ----

/** 请求体超过这个长度就只记指纹。 */
export const BODY_LIMIT = 2000;

export function compactRequest(request: RecordedRequest): Raw {
  const { body: requestBody, ...rest } = request;
  if (requestBody === undefined) return { ...rest };
  if (requestBody.length <= BODY_LIMIT) return { ...rest, body: safeText(requestBody) };
  return { ...rest, bodyDigest: digest(requestBody) };
}

export interface FlowCase {
  name: string;
  settings: Raw;
  script: ScriptStep[];
  abort?: "before" | "during";
}

export interface ErrorInfo {
  message: string;
  aborted: boolean;
  notConfigured?: boolean;
}

export interface FlowOutcome {
  requests: Raw[];
  unusedScript: number;
  usage: unknown[];
  warnings: string[];
  result: unknown;
  error: ErrorInfo | null;
}

/** 错误 → 对拍用的摘要；取消类错误的文案随运行时而异（Node 与 Chromium 的 AbortSignal.reason 不同），只比 aborted。 */
export function errorInfo(error: unknown, options: { abortMessage?: boolean } = {}): ErrorInfo {
  const info: ErrorInfo = { ...describeError(error) };
  if (error instanceof Error && error.name === "AiNotConfiguredError") info.notConfigured = true;
  if (info.aborted && options.abortMessage === false) info.message = "";
  return info;
}

/** 在设置 + 假传输下执行 call，收集请求、用量、警告、结果与错误。 */
export async function runFlow(
  c: FlowCase,
  baseline: Raw,
  call: (signal: AbortSignal, fake: ReturnType<typeof installFakeTransport>) => Promise<unknown>,
  options: { abortMessage?: boolean } = {},
): Promise<FlowOutcome> {
  useSettingsStore.setState(mergeSettingsState(c.settings, baseline as never));
  const fake = installFakeTransport(c.script);
  const warnings = captureWarnings();
  if (c.abort === "before") fake.controller.abort();
  let result: unknown = null;
  let error: ErrorInfo | null = null;
  try {
    result = await call(fake.controller.signal, fake);
  } catch (e) {
    error = errorInfo(e, options);
  } finally {
    warnings.restore();
  }
  // recordUsage 是 fire-and-forget 的 Promise，让它落地后再读
  await Promise.resolve();
  return {
    requests: fake.requests.map(compactRequest),
    unusedScript: c.script.length - fake.requests.length,
    usage: fake.usage,
    warnings: warnings.messages,
    result,
    error,
  };
}
