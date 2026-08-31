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
  | "setModelRouteDefault"
  | "applyAiQuickSetup";

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
            enabled: config.enabled !== false,
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
    enabled:
      updates.enabled === undefined
        ? current.enabled !== false
        : updates.enabled !== false,
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

/**
 * 供应商与旗下模型一起删。
 *
 * 只删 provider 记录、把模型的 providerId 解绑是不行的：设置页对没有
 * providerId 的模型会按 model.id 各自合成一个分组，那一行非但不消失，
 * 还会炸成 N 行。合成分组本来就没有 provider 记录，此时 providerId 为空、
 * 只删模型。一次提交而不是逐条删，否则要写 N 次 localStorage、同步 N 次主进程。
 */
function createAiProviderDeleteAction(context: SettingsActionContext) {
  const { get, commitAISettings } = context;
  return {
    deleteAiProvider: ({ providerId, modelIds }) => {
      const state = get();
      const removedIds = new Set(modelIds);
      const aiModels = state.aiModels.filter(
        (model) => !removedIds.has(model.id),
      );
      const droppedDefault = state.aiModels.some(
        (model) => removedIds.has(model.id) && model.isDefault,
      );
      if (droppedDefault && aiModels.length > 0) {
        aiModels[0] = { ...aiModels[0], isDefault: true };
      }
      const partial: Partial<SettingsState> = {
        aiProviders: state.aiProviders.filter(
          (provider) => provider.id !== providerId,
        ),
        aiModels,
        ...removeModelDefaults(state, removedIds),
      };
      if (droppedDefault && aiModels.length > 0) {
        applyChatModelToLegacyDefaults(partial, aiModels[0]);
      }
      commitAISettings(partial);
    },
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
        enabled: config.enabled !== false,
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
    enabled:
      updates.enabled === undefined
        ? current.enabled !== false
        : updates.enabled !== false,
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
  removedIds: Set<string>,
): Pick<SettingsState, "scenarioModelDefaults" | "modelRouteDefaults"> {
  const scenarioModelDefaults = { ...state.scenarioModelDefaults };
  const modelRouteDefaults = { ...state.modelRouteDefaults };
  for (const [scenario, modelId] of Object.entries(scenarioModelDefaults)) {
    if (removedIds.has(modelId))
      delete scenarioModelDefaults[scenario as AIUsageScenario];
  }
  for (const [route, modelId] of Object.entries(modelRouteDefaults)) {
    if (removedIds.has(modelId))
      delete modelRouteDefaults[route as AIModelRoute];
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
        ...removeModelDefaults(state, new Set([id])),
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

function createAiQuickSetupAction(context: SettingsActionContext) {
  const { get, setTouched } = context;
  return {
    applyAiQuickSetup: async (input) => {
      const providerId = createAiProviderId();
      const provider: AIProviderConfig = {
        ...input.provider,
        id: providerId,
        name: input.provider.name?.trim() || undefined,
        provider: input.provider.provider.trim(),
        apiProtocol: normalizeAIProtocol(
          input.provider.apiProtocol,
          input.provider.provider,
          input.provider.apiUrl,
        ),
        apiKey: input.provider.apiKey.trim(),
        apiUrl: input.provider.apiUrl.trim(),
        enabled: true,
      };
      const createdAt = Date.now();
      const models = input.models.map(
        (model, index): SettingsState["aiModels"][number] => ({
          id: `model_${createdAt}_${index}_${Math.random().toString(36).slice(2, 7)}`,
          name: model.name?.trim() || undefined,
          providerId,
          provider: provider.provider,
          apiProtocol: provider.apiProtocol,
          apiKey: provider.apiKey,
          apiUrl: provider.apiUrl,
          model: model.model.trim(),
          capabilities: normalizeAIModelCapabilities(model.capabilities),
          isDefault: index === 0,
          enabled: true,
          lastVerifiedAt: model.verified ? new Date().toISOString() : undefined,
        }),
      );
      const modelRouteDefaults = { ...get().modelRouteDefaults };
      for (const [route, index] of Object.entries(input.routes)) {
        const target = models[index];
        if (target) modelRouteDefaults[route as AIModelRoute] = target.id;
      }
      const scenarioModelDefaults = { ...get().scenarioModelDefaults };
      for (const [scenario, route] of Object.entries(AI_SCENARIO_MODEL_ROUTE)) {
        const modelId = modelRouteDefaults[route];
        if (modelId) scenarioModelDefaults[scenario as AIUsageScenario] = modelId;
      }
      const partial: Partial<SettingsState> = {
        aiProviders: [...get().aiProviders, provider],
        aiModels: [
          ...get().aiModels.map((model) => ({ ...model, isDefault: false })),
          ...models,
        ],
        modelRouteDefaults,
        scenarioModelDefaults,
      };
      if (models[0]) applyChatModelToLegacyDefaults(partial, models[0]);
      // 先让主进程以一次配置文件写入接受整套 Provider/模型/路由；失败时
      // Renderer 不变，避免出现只保存了一半的向导结果。
      await window.api.settings.set(partial as never);
      setTouched(partial);
    },
  } satisfies SettingsActionGroup<"applyAiQuickSetup">;
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
    createAiQuickSetupAction(context),
  );
}
