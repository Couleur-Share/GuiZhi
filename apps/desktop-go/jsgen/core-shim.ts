// 替代 @guizhi/core：只提供管线用到的 chatCompletion，实际请求在 Go 侧（与媒体总结等共用同一实现）。
import { hostAsync } from "./pipeline-host";

export type AIClientConfig = { provider?: string; apiProtocol?: string; apiKey: string; apiUrl: string; model: string };
export type AIChatResult = { content: string; finishReason?: string; usage?: { promptTokens?: number; completionTokens?: number } };

export async function chatCompletion(
  config: AIClientConfig,
  messages: { role: string; content: string }[],
  options?: { temperature?: number; maxTokens?: number; responseFormat?: { type: string }; signal?: AbortSignal; timeoutMs?: number; stream?: boolean; onProgress?: (n: number) => void; onDelta?: (s: string) => void },
): Promise<AIChatResult> {
  const o = options ?? {};
  const raw = await hostAsync("chat", {
    config, messages,
    options: { temperature: o.temperature, maxTokens: o.maxTokens, responseFormat: o.responseFormat?.type, timeoutMs: o.timeoutMs, stream: o.stream, progress: !!o.onProgress, delta: !!o.onDelta },
  }, o.signal, { progress: o.onProgress, delta: o.onDelta });
  return JSON.parse(raw);
}
