import type { Settings } from "@guizhi/shared/types";
import type { AIProtocol } from "@guizhi/shared/types";
import type {
  AIModelCapabilities,
  AIModelConfig,
  AIModelRoute,
  AIProviderConfig,
  AIUsageScenario,
  ModelRouteDefaults,
  ScenarioModelDefaults,
  SettingsState,
} from "./settings-types";

export const AI_SCENARIO_MODEL_ROUTE: Record<AIUsageScenario, AIModelRoute> = {
  summary: "fastText",
  tagging: "fastText",
  qa: "mainText",
  wiki: "mainText",
  transcription: "audioText",
  ocr: "visionText",
};

export function normalizeAIProtocol(
  value: unknown,
  provider?: string,
  apiUrl?: string,
): AIProtocol {
  if (value === "openai" || value === "gemini" || value === "anthropic") {
    return value;
  }

  const providerLower = (provider || "").trim().toLowerCase();
  const normalizedUrl = (apiUrl || "").trim().toLowerCase();
  if (
    providerLower === "anthropic" ||
    normalizedUrl.includes("api.anthropic.com")
  ) {
    return "anthropic";
  }
  if (
    providerLower === "google" ||
    providerLower === "gemini" ||
    normalizedUrl.includes("generativelanguage.googleapis.com")
  ) {
    return "gemini";
  }
  return "openai";
}

export function normalizeAIModelCapabilities(
  value: unknown,
): AIModelCapabilities {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return {
      chat: true,
      vision: false,
      reasoning: false,
      toolUse: false,
      webSearch: false,
      embedding: false,
      rerank: false,
      audioTranscription: false,
      imageGeneration: false,
    };
  }

  const capabilities = value as Partial<
    Record<keyof AIModelCapabilities, unknown>
  >;
  // chat 默认开启；embedding/转写等专用模型显式标记 chat: false
  return {
    chat: capabilities.chat !== false,
    vision: capabilities.chat !== false && capabilities.vision === true,
    reasoning: capabilities.reasoning === true,
    toolUse: capabilities.toolUse === true,
    webSearch: capabilities.webSearch === true,
    embedding: capabilities.embedding === true,
    rerank: capabilities.rerank === true,
    audioTranscription: capabilities.audioTranscription === true,
    imageGeneration: capabilities.imageGeneration === true,
  };
}

export function normalizePersistedAIModels(value: unknown): AIModelConfig[] {
  if (!Array.isArray(value)) {
    return [];
  }

  return value
    .filter((model): model is Partial<AIModelConfig> =>
      Boolean(
        model &&
        typeof model === "object" &&
        !Array.isArray(model) &&
        typeof model.id === "string" &&
        model.id.trim() &&
        typeof model.provider === "string" &&
        model.provider.trim() &&
        typeof model.apiUrl === "string" &&
        model.apiUrl.trim() &&
        typeof model.model === "string" &&
        model.model.trim(),
      ),
    )
    .map((model) => {
      const provider = model.provider!.trim();
      const apiUrl = model.apiUrl!.trim();
      return {
        ...model,
        id: model.id!.trim(),
        providerId:
          typeof model.providerId === "string" && model.providerId.trim()
            ? model.providerId.trim()
            : undefined,
        provider,
        apiProtocol: normalizeAIProtocol(model.apiProtocol, provider, apiUrl),
        apiKey: typeof model.apiKey === "string" ? model.apiKey : "",
        apiUrl,
        model: model.model!.trim(),
        capabilities: normalizeAIModelCapabilities(model.capabilities),
        enabled: model.enabled !== false,
      };
    });
}

export function normalizePersistedAIProviders(
  value: unknown,
): AIProviderConfig[] {
  if (!Array.isArray(value)) {
    return [];
  }

  return value
    .filter((provider): provider is Partial<AIProviderConfig> =>
      Boolean(
        provider &&
        typeof provider === "object" &&
        !Array.isArray(provider) &&
        typeof provider.id === "string" &&
        provider.id.trim() &&
        typeof provider.provider === "string" &&
        provider.provider.trim() &&
        typeof provider.apiUrl === "string" &&
        provider.apiUrl.trim(),
      ),
    )
    .map((provider) => {
      const providerId = provider.provider!.trim();
      const apiUrl = provider.apiUrl!.trim();
      return {
        ...provider,
        id: provider.id!.trim(),
        name:
          typeof provider.name === "string"
            ? provider.name.trim() || undefined
            : undefined,
        provider: providerId,
        apiProtocol: normalizeAIProtocol(
          provider.apiProtocol,
          providerId,
          apiUrl,
        ),
        apiKey: typeof provider.apiKey === "string" ? provider.apiKey : "",
        apiUrl,
        enabled: provider.enabled !== false,
      };
    });
}

function normalizeModelRoute(value: unknown): AIModelRoute | null {
  return value === "mainText" ||
    value === "fastText" ||
    value === "visionText" ||
    value === "embedding" ||
    value === "audioText" ||
    value === "imageGen"
    ? value
    : null;
}

function normalizeAIUsageScenario(value: unknown): AIUsageScenario | null {
  return value === "summary" ||
    value === "tagging" ||
    value === "qa" ||
    value === "wiki" ||
    value === "transcription" ||
    value === "ocr"
    ? value
    : null;
}

export function normalizeScenarioModelDefaults(
  value: unknown,
): ScenarioModelDefaults {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return {};
  }
  return Object.entries(value).reduce<ScenarioModelDefaults>(
    (acc, [scenario, modelId]) => {
      const normalizedScenario = normalizeAIUsageScenario(scenario);
      const normalizedModelId =
        typeof modelId === "string" ? modelId.trim() : "";
      if (normalizedScenario && normalizedModelId) {
        acc[normalizedScenario] = normalizedModelId;
      }
      return acc;
    },
    {},
  );
}

export function normalizeModelRouteDefaults(
  value: unknown,
): ModelRouteDefaults {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    return {};
  }
  return Object.entries(value).reduce<ModelRouteDefaults>(
    (acc, [route, modelId]) => {
      const normalizedRoute = normalizeModelRoute(route);
      const normalizedModelId =
        typeof modelId === "string" ? modelId.trim() : "";
      if (normalizedRoute && normalizedModelId) {
        acc[normalizedRoute] = normalizedModelId;
      }
      return acc;
    },
    {},
  );
}

function deriveModelRouteDefaultsFromScenarios(
  scenarioDefaults: ScenarioModelDefaults,
): ModelRouteDefaults {
  const next: ModelRouteDefaults = {};
  if (scenarioDefaults.qa) next.mainText = scenarioDefaults.qa;
  else if (scenarioDefaults.wiki) next.mainText = scenarioDefaults.wiki;
  if (scenarioDefaults.ocr) next.visionText = scenarioDefaults.ocr;
  if (scenarioDefaults.summary) next.fastText = scenarioDefaults.summary;
  else if (scenarioDefaults.tagging) next.fastText = scenarioDefaults.tagging;
  return next;
}

export function normalizeAIModelDefaults(
  next: Pick<SettingsState, "scenarioModelDefaults" | "modelRouteDefaults">,
): void {
  next.scenarioModelDefaults = normalizeScenarioModelDefaults(
    next.scenarioModelDefaults,
  );
  next.modelRouteDefaults = normalizeModelRouteDefaults(
    next.modelRouteDefaults,
  );
  if (Object.keys(next.modelRouteDefaults).length === 0) {
    next.modelRouteDefaults = deriveModelRouteDefaultsFromScenarios(
      next.scenarioModelDefaults,
    );
  }
}

export function isModelActive(
  model: AIModelConfig,
  providers: AIProviderConfig[] = [],
): boolean {
  if (model.enabled === false) {
    return false;
  }
  if (providers.length > 0) {
    const provider = findMatchingAIProvider(providers, model);
    if (provider && provider.enabled === false) {
      return false;
    }
  }
  return true;
}

export function findMatchingAIProvider(
  providers: AIProviderConfig[],
  config: Pick<
    AIModelConfig,
    "provider" | "apiProtocol" | "apiKey" | "apiUrl"
  > & { providerId?: string },
): AIProviderConfig | undefined {
  if (config.providerId?.trim()) {
    return providers.find((provider) => provider.id === config.providerId);
  }
  return providers.find(
    (provider) =>
      provider.id === config.provider ||
      (provider.provider === config.provider &&
        provider.apiProtocol === config.apiProtocol &&
        provider.apiUrl === config.apiUrl &&
        provider.apiKey === config.apiKey),
  );
}

/**
 * 清掉与所属供应商同名的模型别名。
 *
 * 端点编辑曾把整份供应商配置灌进旗下每个模型（含供应商的 name），于是模型
 * 路由的下拉里每一项都叫「云雾API」，八个模型一个都分不出。写入侧已经修好，
 * 但落进 localStorage 与 ai-models.json 的值没有任何自愈路径，只能在读的
 * 时候清：这样的别名与它上面那一行供应商名逐字相同、不携带任何信息，清掉后
 * 回落到模型 id 严格优于现状；真想要这个别名，重新填一次即可。
 */
export function dropProviderNameAliases(
  providers: AIProviderConfig[],
  models: AIModelConfig[],
): AIModelConfig[] {
  return models.map((model) => {
    const alias = model.name?.trim();
    if (!alias) {
      return model;
    }
    const provider = findMatchingAIProvider(providers, model);
    return provider?.name?.trim() === alias
      ? { ...model, name: undefined }
      : model;
  });
}

export function attachProviderIdsToAIModels(
  providers: AIProviderConfig[],
  models: AIModelConfig[],
): AIModelConfig[] {
  return models.map((model) => {
    const provider = findMatchingAIProvider(providers, model);
    return provider && model.providerId !== provider.id
      ? { ...model, providerId: provider.id }
      : model;
  });
}

export function buildAISettingsSyncPayload(
  state: SettingsState,
): Partial<Settings> {
  return {
    aiProvider: state.aiProvider,
    aiApiProtocol: state.aiApiProtocol,
    aiApiKey: state.aiApiKey,
    aiApiUrl: state.aiApiUrl,
    aiModel: state.aiModel,
    aiProviders: state.aiProviders,
    aiModels: state.aiModels,
    modelRouteDefaults: state.modelRouteDefaults,
  } as Partial<Settings>;
}
