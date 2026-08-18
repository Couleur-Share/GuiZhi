import type {
  AIModelRoute,
  AIModelCapabilities,
  AIModelConfig,
  AIProviderConfig,
  AIUsageScenario,
  ModelRouteDefaults,
  ScenarioModelDefaults,
} from "../stores/settings.store";
import {
  AI_SCENARIO_MODEL_ROUTE,
  isModelActive,
} from "../stores/settings/settings-ai";
import type { AIConfig } from "./ai";

export function hasModelCapability(
  model: AIModelConfig,
  capability: keyof AIModelCapabilities,
): boolean {
  if (capability === "chat") {
    // chat 默认开启；embedding/转写等专用模型显式标记 chat: false
    return model.capabilities?.chat !== false;
  }

  if (
    capability === "embedding" ||
    capability === "rerank" ||
    capability === "audioTranscription" ||
    capability === "imageGeneration"
  ) {
    return model.capabilities?.[capability] === true;
  }

  // vision / reasoning / toolUse / webSearch 依附于 chat 能力
  return (
    hasModelCapability(model, "chat") &&
    model.capabilities?.[capability] === true
  );
}

export function getModelsByCapability(
  aiModels: AIModelConfig[],
  capability: keyof AIModelCapabilities,
  aiProviders?: AIProviderConfig[],
): AIModelConfig[] {
  return aiModels
    .filter((model) => isModelActive(model, aiProviders))
    .filter((model) => hasModelCapability(model, capability));
}

/**
 * 路由候选模型：
 * - embedding / visionText / imageGen 严格按能力过滤；
 * - audioText 转写模型优先，对话模型兜底（中转站模型名无法可靠判定）；
 * - 其余路由为对话模型。
 */
export function getRouteCandidateModels(
  aiModels: AIModelConfig[],
  route: AIModelRoute,
  aiProviders?: AIProviderConfig[],
): AIModelConfig[] {
  if (route === "embedding") {
    return getModelsByCapability(aiModels, "embedding", aiProviders);
  }
  if (route === "visionText") {
    return getModelsByCapability(aiModels, "vision", aiProviders);
  }
  if (route === "imageGen") {
    return getModelsByCapability(aiModels, "imageGeneration", aiProviders);
  }
  if (route === "audioText") {
    const transcriptionModels = getModelsByCapability(
      aiModels,
      "audioTranscription",
      aiProviders,
    );
    return [
      ...transcriptionModels,
      ...getModelsByCapability(aiModels, "chat", aiProviders),
    ];
  }
  return getModelsByCapability(aiModels, "chat", aiProviders);
}

function pickRouteModel(
  candidates: AIModelConfig[],
  route: AIModelRoute,
  explicitModelId: string | undefined,
): AIModelConfig | null {
  if (explicitModelId) {
    const explicitModel = candidates.find(
      (model) => model.id === explicitModelId,
    );
    if (explicitModel) {
      return explicitModel;
    }
  }

  // audioText / imageGen 主进程只认显式路由设置，这里同样不回退，
  // 否则 UI 会显示一个「已配置」而实际调用直接报未配置
  if (route === "audioText" || route === "imageGen") {
    return null;
  }

  return candidates.find((model) => model.isDefault) ?? candidates[0] ?? null;
}

export function resolveRouteModel(
  aiModels: AIModelConfig[],
  modelRouteDefaults: ModelRouteDefaults | undefined,
  route: AIModelRoute,
  aiProviders?: AIProviderConfig[],
): AIModelConfig | null {
  return pickRouteModel(
    getRouteCandidateModels(aiModels, route, aiProviders),
    route,
    modelRouteDefaults?.[route],
  );
}

export function resolveScenarioModel(
  aiModels: AIModelConfig[],
  scenarioModelDefaults: ScenarioModelDefaults | undefined,
  scenario: AIUsageScenario,
  modelRouteDefaults?: ModelRouteDefaults,
  aiProviders?: AIProviderConfig[],
): AIModelConfig | null {
  const route = AI_SCENARIO_MODEL_ROUTE[scenario];
  return pickRouteModel(
    getRouteCandidateModels(aiModels, route, aiProviders),
    route,
    modelRouteDefaults?.[route] ?? scenarioModelDefaults?.[scenario],
  );
}

export function toAIConfig(model: AIModelConfig): AIConfig {
  return {
    id: model.id,
    provider: model.provider,
    apiProtocol: model.apiProtocol,
    apiKey: model.apiKey,
    apiUrl: model.apiUrl,
    model: model.model,
  };
}

export function isConfiguredModel(
  model: AIModelConfig | null | undefined,
): model is AIModelConfig {
  return Boolean(
    model &&
    model.enabled !== false &&
    model.provider?.trim() &&
    model.apiKey?.trim() &&
    model.apiUrl?.trim() &&
    model.model?.trim(),
  );
}

interface ResolveScenarioAIConfigOptions {
  aiModels: AIModelConfig[];
  aiProviders?: AIProviderConfig[];
  scenarioModelDefaults: ScenarioModelDefaults | undefined;
  modelRouteDefaults?: ModelRouteDefaults;
  scenario: AIUsageScenario;
  allowLegacyFallback?: boolean;
  aiProvider: string;
  aiApiProtocol: AIConfig["apiProtocol"];
  aiApiKey: string;
  aiApiUrl: string;
  aiModel: string;
}

export function resolveScenarioAIConfig({
  aiModels,
  aiProviders,
  scenarioModelDefaults,
  modelRouteDefaults,
  scenario,
  allowLegacyFallback = true,
  aiProvider,
  aiApiProtocol,
  aiApiKey,
  aiApiUrl,
  aiModel,
}: ResolveScenarioAIConfigOptions): AIConfig | null {
  const selectedModel = resolveScenarioModel(
    aiModels,
    scenarioModelDefaults,
    scenario,
    modelRouteDefaults,
    aiProviders,
  );

  if (isConfiguredModel(selectedModel)) {
    return toAIConfig(selectedModel);
  }

  // legacy 单模型字段只兜底纯对话路由（问答/摘要类场景）
  const route = AI_SCENARIO_MODEL_ROUTE[scenario];
  if (
    allowLegacyFallback &&
    (route === "mainText" || route === "fastText") &&
    aiProvider.trim() &&
    aiApiKey.trim() &&
    aiApiUrl.trim() &&
    aiModel.trim()
  ) {
    return {
      provider: aiProvider,
      apiProtocol: aiApiProtocol,
      apiKey: aiApiKey,
      apiUrl: aiApiUrl,
      model: aiModel,
    };
  }

  return null;
}
