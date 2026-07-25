import type {
  AIModelRoute,
  AIProviderConfig,
  AIUsageScenario,
  SettingsState,
} from "./settings-types";
import type {
  SettingsActionContext,
  SettingsActionGroup,
} from "./settings-action-context";
import {
  AI_SCENARIO_MODEL_ROUTE,
  findMatchingAIProvider,
  normalizeAIModelCapabilities,
  normalizeAIProtocol,
} from "./settings-ai";

type AIActionKey =
  | "setAiProvider"
  | "setAiApiProtocol"
  | "setAiApiKey"
  | "setAiApiUrl"
  | "setAiModel"
  | "addAiProvider"
  | "updateAiProvider"
  | "deleteAiProvider"
  | "addAiModel"
  | "updateAiModel"
  | "deleteAiModel"
  | "setDefaultAiModel"
  | "setScenarioModelDefault"
  | "setModelRouteDefault";

function createAiConnectionActions(context: SettingsActionContext) {
  const { commitAISettings } = context;
  return {
    setAiProvider: (aiProvider) => commitAISettings({ aiProvider }),
    setAiApiProtocol: (aiApiProtocol) => commitAISettings({ aiApiProtocol }),
    setAiApiKey: (aiApiKey) => commitAISettings({ aiApiKey }),
    setAiApiUrl: (aiApiUrl) => commitAISettings({ aiApiUrl }),
    setAiModel: (aiModel) => commitAISettings({ aiModel }),
  } satisfies SettingsActionGroup<
    | "setAiProvider"
    | "setAiApiProtocol"
    | "setAiApiKey"
    | "setAiApiUrl"
    | "setAiModel"
  >;
}

function createAiProviderId(): string {
  return `provider_${Date.now()}_${Math.random().toString(36).substring(2, 11)}`;
}

function createAiModelId(): string {
  return `model_${Date.now()}_${Math.random().toString(36).substring(2, 11)}`;
}

function createAiProviderAddAction(context: SettingsActionContext) {
  const { get, commitAISettings } = context;
  return {
    addAiProvider: (config) => {
      const provider = config.provider.trim();
      commitAISettings({
        aiProviders: [
          ...get().aiProviders,
          {
            ...config,
            id: createAiProviderId(),
            name: config.name?.trim() || undefined,
            provider,
            apiProtocol: normalizeAIProtocol(
              config.apiProtocol,
              provider,
              config.apiUrl,
            ),
            apiKey: config.apiKey.trim(),
            apiUrl: config.apiUrl.trim(),
          },
        ],
      });
    },
  } satisfies SettingsActionGroup<"addAiProvider">;
}

function buildUpdatedAiProvider(
  current: AIProviderConfig,
  updates: Parameters<
    SettingsActionGroup<"updateAiProvider">["updateAiProvider"]
  >[1],
): AIProviderConfig {
  const provider = updates.provider ?? current.provider;
  const apiUrl = updates.apiUrl ?? current.apiUrl;
  return {
    ...current,
    ...updates,
    name:
      updates.name === undefined
        ? current.name
        : updates.name.trim() || undefined,
    provider: provider.trim(),
    apiProtocol: normalizeAIProtocol(
      updates.apiProtocol ?? current.apiProtocol,
      provider,
      apiUrl,
    ),
    apiKey: (updates.apiKey ?? current.apiKey).trim(),
    apiUrl: apiUrl.trim(),
  };
}

function syncModelsWithProvider(
  models: SettingsState["aiModels"],
  providerId: string,
  provider: AIProviderConfig,
): SettingsState["aiModels"] {
  return models.map((model) =>
    model.providerId === providerId ||
    (!model.providerId && findMatchingAIProvider([provider], model))
      ? {
          ...model,
          providerId: provider.id,
          provider: provider.provider,
          apiProtocol: provider.apiProtocol,
          apiKey: provider.apiKey,
          apiUrl: provider.apiUrl,
        }
      : model,
  );
}

function createAiProviderUpdateAction(context: SettingsActionContext) {
  const { get, commitAISettings } = context;
  return {
    updateAiProvider: (id, config) => {
      let updatedProvider: AIProviderConfig | null = null;
      const aiProviders = get().aiProviders.map((provider) => {
        if (provider.id !== id) return provider;
        updatedProvider = buildUpdatedAiProvider(provider, config);
        return updatedProvider;
      });
      const aiModels = updatedProvider
        ? syncModelsWithProvider(get().aiModels, id, updatedProvider)
        : get().aiModels;
      commitAISettings({ aiProviders, aiModels });
    },
  } satisfies SettingsActionGroup<"updateAiProvider">;
}

function createAiProviderDeleteAction(context: SettingsActionContext) {
  const { get, commitAISettings } = context;
  return {
    deleteAiProvider: (id) =>
      commitAISettings({
        aiProviders: get().aiProviders.filter((provider) => provider.id !== id),
        aiModels: get().aiModels.map((model) =>
          model.providerId === id ? { ...model, providerId: undefined } : model,
        ),
      }),
  } satisfies SettingsActionGroup<"deleteAiProvider">;
}

function createAiProviderActions(context: SettingsActionContext) {
  return {
    ...createAiProviderAddAction(context),
    ...createAiProviderUpdateAction(context),
    ...createAiProviderDeleteAction(context),
  };
}

function applyChatModelToLegacyDefaults(
  partial: Partial<SettingsState>,
  model: SettingsState["aiModels"][number],
): void {
  // legacy 单模型字段仅作对话兜底，专用模型（embedding/转写）不写入
  if (model.capabilities?.chat === false) {
    return;
  }
  Object.assign(partial, {
    aiProvider: model.provider,
    aiApiProtocol: model.apiProtocol,
    aiApiKey: model.apiKey,
    aiApiUrl: model.apiUrl,
    aiModel: model.model,
  });
}

function createAiModelAddAction(context: SettingsActionContext) {
  const { get, commitAISettings } = context;
  return {
    addAiModel: (config) => {
      const currentModels = get().aiModels;
      const provider = findMatchingAIProvider(get().aiProviders, config);
      const model = {
        ...config,
        id: createAiModelId(),
        providerId: provider?.id ?? config.providerId,
        provider: provider?.provider ?? config.provider,
        apiProtocol: provider?.apiProtocol ?? config.apiProtocol,
        apiKey: provider?.apiKey ?? config.apiKey,
        apiUrl: provider?.apiUrl ?? config.apiUrl,
        capabilities: normalizeAIModelCapabilities(config.capabilities),
        isDefault: currentModels.length === 0,
      };
      const partial: Partial<SettingsState> = {
        aiModels: [...currentModels, model],
      };
      if (model.isDefault) applyChatModelToLegacyDefaults(partial, model);
      commitAISettings(partial);
    },
  } satisfies SettingsActionGroup<"addAiModel">;
}

function mergeAiModel(
  context: SettingsActionContext,
  current: SettingsState["aiModels"][number],
  updates: Parameters<SettingsActionGroup<"updateAiModel">["updateAiModel"]>[1],
) {
  const merged = { ...current, ...updates };
  const provider = findMatchingAIProvider(context.get().aiProviders, merged);
  return {
    ...merged,
    providerId: provider?.id ?? merged.providerId,
    provider: provider?.provider ?? merged.provider,
    apiProtocol: provider?.apiProtocol ?? merged.apiProtocol,
    apiKey: provider?.apiKey ?? merged.apiKey,
    apiUrl: provider?.apiUrl ?? merged.apiUrl,
    capabilities: normalizeAIModelCapabilities(
      updates.capabilities ?? current.capabilities,
    ),
  };
}

function createAiModelUpdateAction(context: SettingsActionContext) {
  const { get, commitAISettings } = context;
  return {
    updateAiModel: (id, config) => {
      const aiModels = get().aiModels.map((model) =>
        model.id === id ? mergeAiModel(context, model, config) : model,
      );
      const updated = aiModels.find((model) => model.id === id);
      const partial: Partial<SettingsState> = { aiModels };
      if (updated?.isDefault) applyChatModelToLegacyDefaults(partial, updated);
      commitAISettings(partial);
    },
  } satisfies SettingsActionGroup<"updateAiModel">;
}

function removeModelDefaults(
  state: SettingsState,
  id: string,
): Pick<SettingsState, "scenarioModelDefaults" | "modelRouteDefaults"> {
  const scenarioModelDefaults = { ...state.scenarioModelDefaults };
  const modelRouteDefaults = { ...state.modelRouteDefaults };
  for (const [scenario, modelId] of Object.entries(scenarioModelDefaults)) {
    if (modelId === id)
      delete scenarioModelDefaults[scenario as AIUsageScenario];
  }
  for (const [route, modelId] of Object.entries(modelRouteDefaults)) {
    if (modelId === id) delete modelRouteDefaults[route as AIModelRoute];
  }
  return { scenarioModelDefaults, modelRouteDefaults };
}

function createAiModelDeleteAction(context: SettingsActionContext) {
  const { get, commitAISettings } = context;
  return {
    deleteAiModel: (id) => {
      const state = get();
      const deleted = state.aiModels.find((model) => model.id === id);
      const aiModels = state.aiModels.filter((model) => model.id !== id);
      if (deleted?.isDefault && aiModels.length > 0) {
        aiModels[0] = { ...aiModels[0], isDefault: true };
      }
      const partial: Partial<SettingsState> = {
        aiModels,
        ...removeModelDefaults(state, id),
      };
      if (deleted?.isDefault && aiModels.length > 0) {
        applyChatModelToLegacyDefaults(partial, aiModels[0]);
      }
      commitAISettings(partial);
    },
  } satisfies SettingsActionGroup<"deleteAiModel">;
}

function createAiModelActions(context: SettingsActionContext) {
  return {
    ...createAiModelAddAction(context),
    ...createAiModelUpdateAction(context),
    ...createAiModelDeleteAction(context),
  };
}

function createAiDefaultModelAction(context: SettingsActionContext) {
  const { get, commitAISettings } = context;
  return {
    setDefaultAiModel: (id) => {
      const target = get().aiModels.find((model) => model.id === id);
      if (!target) return;
      const aiModels = get().aiModels.map((model) => ({
        ...model,
        isDefault: model.id === id,
      }));
      const partial: Partial<SettingsState> = { aiModels };
      applyChatModelToLegacyDefaults(partial, target);
      commitAISettings(partial);
    },
  } satisfies SettingsActionGroup<"setDefaultAiModel">;
}

function createAiScenarioDefaultAction(context: SettingsActionContext) {
  const { get, commitAISettings } = context;
  return {
    setScenarioModelDefault: (scenario, modelId) => {
      const scenarioModelDefaults = { ...get().scenarioModelDefaults };
      const modelRouteDefaults = { ...get().modelRouteDefaults };
      const route = AI_SCENARIO_MODEL_ROUTE[scenario];
      if (modelId) {
        scenarioModelDefaults[scenario] = modelId;
        modelRouteDefaults[route] = modelId;
      } else {
        delete scenarioModelDefaults[scenario];
        delete modelRouteDefaults[route];
      }
      commitAISettings({ scenarioModelDefaults, modelRouteDefaults });
    },
  } satisfies SettingsActionGroup<"setScenarioModelDefault">;
}

function createAiRouteDefaultAction(context: SettingsActionContext) {
  const { get, commitAISettings } = context;
  return {
    setModelRouteDefault: (route, modelId) => {
      const modelRouteDefaults = { ...get().modelRouteDefaults };
      if (modelId) modelRouteDefaults[route] = modelId;
      else delete modelRouteDefaults[route];
      commitAISettings({ modelRouteDefaults });
    },
  } satisfies SettingsActionGroup<"setModelRouteDefault">;
}

function createAiDefaultActions(context: SettingsActionContext) {
  return {
    ...createAiDefaultModelAction(context),
    ...createAiScenarioDefaultAction(context),
    ...createAiRouteDefaultAction(context),
  };
}

export function createAISettingsActions(
  context: SettingsActionContext,
): SettingsActionGroup<AIActionKey> {
  return Object.assign(
    {},
    createAiConnectionActions(context),
    createAiProviderActions(context),
    createAiModelActions(context),
    createAiDefaultActions(context),
  );
}
