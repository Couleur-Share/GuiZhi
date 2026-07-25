/**
 * 场景化 AI 调用：从设置解析场景对应的模型配置并发起对话。
 */
import { chatCompletion, type ChatMessage } from "../ai";
import { resolveScenarioAIConfig } from "../ai-defaults";
import { useSettingsStore } from "../../stores/settings.store";
import type { AIUsageScenario } from "../../stores/settings.store";

/**
 * 知识域的模型调用都是「长提示词 + 整段生成」，主进程 30s 的兜底超时
 * （`ai.ipc.ts`）是按连通性探测定的，套到这里会在生成中途被掐断。
 */
const SCENARIO_CHAT_TIMEOUT_MS = 120_000;

/** AI 未配置（区别于调用失败，UI 引导去设置页）。 */
export class AiNotConfiguredError extends Error {
  constructor() {
    super("AI_NOT_CONFIGURED");
    this.name = "AiNotConfiguredError";
  }
}

export function isAiConfiguredForScenario(scenario: AIUsageScenario): boolean {
  return resolveConfig(scenario) !== null;
}

function resolveConfig(scenario: AIUsageScenario) {
  const state = useSettingsStore.getState();
  return resolveScenarioAIConfig({
    aiModels: state.aiModels,
    scenarioModelDefaults: state.scenarioModelDefaults,
    modelRouteDefaults: state.modelRouteDefaults,
    scenario,
    aiProvider: state.aiProvider,
    aiApiProtocol: state.aiApiProtocol,
    aiApiKey: state.aiApiKey,
    aiApiUrl: state.aiApiUrl,
    aiModel: state.aiModel,
  });
}

export interface ScenarioChatResult {
  content: string;
  model: string;
}

export async function runScenarioChat(
  scenario: AIUsageScenario,
  messages: ChatMessage[],
  options?: {
    temperature?: number;
    maxTokens?: number;
    /** 传下去才能真正中断在途请求，否则「停止」只是标记状态 */
    signal?: AbortSignal;
    /** 素材特别长的场景（如 Wiki 编译）可再放宽 */
    timeoutMs?: number;
  },
): Promise<ScenarioChatResult> {
  const config = resolveConfig(scenario);
  if (!config) {
    throw new AiNotConfiguredError();
  }
  const result = await chatCompletion(config, messages, {
    temperature: options?.temperature,
    maxTokens: options?.maxTokens,
    stream: false,
    enableThinking: false,
    signal: options?.signal,
    timeoutMs: options?.timeoutMs ?? SCENARIO_CHAT_TIMEOUT_MS,
  });

  // 知识域的每一次模型调用都经过这里，是唯一的计量点。
  // 统计失败不能影响业务结果。
  void window.api?.ai
    ?.recordUsage({
      scenario,
      model: config.model,
      promptTokens: result.usage?.promptTokens ?? 0,
      completionTokens: result.usage?.completionTokens ?? 0,
    })
    .catch((error: unknown) => {
      console.warn("记录 AI 用量失败:", error);
    });

  return { content: result.content, model: config.model };
}
