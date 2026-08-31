import type { AIConfig } from "./ai-types";
import { testAIConnection } from "./ai";
import { embedTexts } from "./knowledge-ai/embeddings";
import type { AIModelCapabilities } from "../stores/settings.store";

export type ModelTestOutcome =
  | { status: "success"; latency: number }
  | { status: "failed"; message: string };

/** 按能力走真实端点；文生图测试会真实生成一张最低质量图片并产生费用。 */
export async function runModelConnectionTest(
  config: AIConfig,
  capabilities: AIModelCapabilities | undefined,
): Promise<ModelTestOutcome> {
  if (capabilities?.audioTranscription === true) {
    const result = await window.api.media.testTranscription({
      apiUrl: config.apiUrl,
      apiKey: config.apiKey,
      model: config.model,
    });
    return result.success
      ? { status: "success", latency: result.latency ?? 0 }
      : { status: "failed", message: result.error || "" };
  }
  if (capabilities?.embedding === true) {
    const startTime = Date.now();
    try {
      await embedTexts(config, ["ping"]);
      return { status: "success", latency: Date.now() - startTime };
    } catch (error) {
      return { status: "failed", message: error instanceof Error ? error.message : String(error) };
    }
  }
  if (capabilities?.imageGeneration === true) {
    const result = await window.api.illustration.testModel({
      apiUrl: config.apiUrl,
      apiKey: config.apiKey,
      model: config.model,
      apiProtocol: config.apiProtocol,
      provider: config.provider,
    });
    return result.success
      ? { status: "success", latency: result.latency ?? 0 }
      : { status: "failed", message: result.error || "" };
  }
  const result = await testAIConnection(config);
  return result.success
    ? { status: "success", latency: result.latency ?? 0 }
    : { status: "failed", message: result.error || "" };
}
