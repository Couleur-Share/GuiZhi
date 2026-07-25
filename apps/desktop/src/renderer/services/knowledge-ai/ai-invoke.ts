/**
 * 场景化 AI 调用：从设置解析场景对应的模型配置并发起对话。
 */
import { chatCompletion, type ChatMessage } from "../ai";
import { resolveScenarioAIConfig } from "../ai-defaults";
import { useSettingsStore } from "../../stores/settings.store";
import type { AIUsageScenario } from "../../stores/settings.store";

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
  options?: { temperature?: number; maxTokens?: number },
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
  });
  return { content: result.content, model: config.model };
}
