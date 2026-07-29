import { useMemo, useState } from "react";

import { TestTubeIcon } from "lucide-react";
import type { TFunction } from "i18next";
import { useTranslation } from "react-i18next";

import {
  fetchAvailableModels,
  normalizeApiUrlInput,
  testAIConnection,
  type AIConfig,
  type FetchModelsResult,
  type ModelInfo,
} from "../../services/ai";
import {
  isConfiguredModel,
  resolveRouteModel,
  toAIConfig,
} from "../../services/ai-defaults";
import { embedTexts } from "../../services/knowledge-ai/embeddings";
import {
  useSettingsStore,
  type AIModelCapabilities,
  type AIModelConfig,
  type AIModelRoute,
} from "../../stores/settings.store";
import { useUIStore } from "../../stores/ui.store";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { useToast } from "../ui/Toast";
import { Spinner } from "../ui/Spinner";
import { EMPTY_FORM, MODEL_ROUTE_DEFINITIONS } from "./ai-workbench/constants";
import { EndpointFormModal } from "./ai-workbench/EndpointFormModal";
import { EndpointsSection } from "./ai-workbench/EndpointsSection";
import {
  buildEndpointKey,
  buildEndpointGroupKey,
  cloneDefaultCapabilities,
  createFormFromModel,
  getModelDisplayName,
  getEndpointDisplayName,
  getProviderInfo,
  inferModelAttributes,
} from "./ai-workbench/helpers";
import { ModelFetchModal } from "./ai-workbench/ModelFetchModal";
import { ModelFormModal } from "./ai-workbench/ModelFormModal";
import { ScenarioDefaultsSection } from "./ai-workbench/ScenarioDefaultsSection";
import { StatusCard } from "./ai-workbench/shared";
import { UsageSection } from "./ai-workbench/UsageSection";
import type {
  EndpointDraft,
  EndpointGroup,
  EndpointStatus,
  ModelFormState,
  StatusCardData,
} from "./ai-workbench/types";

function buildVerifiedEndpointStatus(
  group: EndpointGroup,
  t: TFunction,
): EndpointStatus | null {
  const verifiedModels = group.models.filter(
    (model) =>
      typeof model.lastVerifiedAt === "string" &&
      model.lastVerifiedAt.trim().length > 0,
  );

  if (verifiedModels.length === 0) {
    return null;
  }

  const latestVerifiedAt = verifiedModels.reduce(
    (latest, model) => {
      const current = Date.parse(model.lastVerifiedAt || "");
      if (!Number.isFinite(current)) {
        return latest;
      }
      return latest === null || current > latest ? current : latest;
    },
    null as number | null,
  );

  const verifiedModel = verifiedModels[0];
  const detailPrefix = verifiedModel?.model?.trim()
    ? `${verifiedModel.model} · `
    : "";
  const detailSuffix =
    latestVerifiedAt !== null
      ? new Date(latestVerifiedAt).toLocaleString()
      : t("settings.aiWorkbenchModelCount", {
          count: group.models.length,
        });

  return {
    tone: "ready",
    label: t("settings.aiWorkbenchConnected"),
    detail: `${detailPrefix}${detailSuffix}`,
  };
}

function getFetchModelsFeedback(
  result: FetchModelsResult,
  t: TFunction,
  apiUrl?: string,
): { message: string; type: "error" | "warning" | "info" } {
  if (result.success && result.models.length === 0) {
    return {
      message: t("settings.aiWorkbenchFetchModelsEmpty"),
      type: "warning",
    };
  }

  switch (result.reason) {
    case "auth":
      return {
        message: t("settings.aiWorkbenchFetchModelsAuthError"),
        type: "error",
      };
    case "unsupported":
    case "parse":
      return {
        message: t("settings.aiWorkbenchFetchModelsUnsupported"),
        type: "info",
      };
    case "network":
      return {
        message: getConnectionErrorMessage(
          result.error || t("settings.aiWorkbenchFetchModelsNetworkError"),
          t,
          result.endpoint || apiUrl,
        ),
        type: "warning",
      };
    default:
      return {
        message: result.error || t("settings.aiWorkbenchFetchModelsFailed"),
        type: "error",
      };
  }
}

function findProviderForModel(
  providers: EndpointGroup[],
  model: AIModelConfig,
): EndpointGroup | undefined {
  if (model.providerId?.trim()) {
    return providers.find(
      (provider) => provider.providerConfigId === model.providerId,
    );
  }

  return providers.find(
    (provider) =>
      provider.provider === model.provider &&
      provider.apiProtocol === model.apiProtocol &&
      provider.apiUrl === model.apiUrl &&
      provider.apiKey === model.apiKey,
  );
}

function getConnectionErrorMessage(
  message: string,
  t: TFunction,
  apiUrl?: string,
): string {
  const normalized = message.toLowerCase();
  if (
    normalized.includes("failed to fetch") ||
    normalized.includes("networkerror")
  ) {
    try {
      const currentOrigin =
        typeof window !== "undefined" ? window.location.origin : "";
      const targetOrigin = apiUrl ? new URL(apiUrl).origin : "";
      if (
        currentOrigin &&
        /^https?:\/\/(localhost|127\.0\.0\.1)(:\d+)?$/i.test(currentOrigin) &&
        targetOrigin
      ) {
        return t("settings.aiWorkbenchCorsBlockedDev", {
          origin: currentOrigin,
          target: targetOrigin,
        });
      }
      if (targetOrigin) {
        return t("settings.aiWorkbenchCorsBlocked", {
          target: targetOrigin,
        });
      }
    } catch {
      // fall through to generic network copy
    }
    return t("settings.aiWorkbenchConnectionNetworkError");
  }
  if (
    normalized.includes("401") ||
    normalized.includes("403") ||
    normalized.includes("unauthorized") ||
    normalized.includes("invalid api key")
  ) {
    return t("settings.aiWorkbenchConnectionAuthError");
  }
  return message;
}

function formatModelTestSuccessToast(
  modelName: string,
  latency: number,
  t: TFunction,
  extra?: string,
): string {
  return `${modelName} ${t("settings.aiWorkbenchModelTestSuccess", "测试成功")} (${latency}ms)${extra ?? ""}`;
}

type ModelTestOutcome =
  | { status: "success"; latency: number }
  | { status: "failed"; message: string };

/**
 * 按模型能力分流连接测试：转写模型经主进程用静音样本发真实转写请求，
 * 嵌入模型走 /embeddings，其余走对话补全。
 */
async function runModelConnectionTest(
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
      return {
        status: "failed",
        message: error instanceof Error ? error.message : String(error),
      };
    }
  }

  // 文生图模型走 /images/generations，拿 chat completions 去测只会撞
  // model_not_supported
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

function formatModelTestFailureToast(
  modelName: string,
  message: string,
  t: TFunction,
  apiUrl?: string,
): string {
  return `${modelName} ${t("settings.aiWorkbenchModelTestFailed", "测试失败")}: ${getConnectionErrorMessage(message, t, apiUrl)}`;
}

export function AISettingsPrototype() {
  const settings = useSettingsStore();
  const { showToast } = useToast();
  const { t } = useTranslation();
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );

  const [modelForm, setModelForm] = useState<ModelFormState>(EMPTY_FORM);
  const [editingModelId, setEditingModelId] = useState<string | null>(null);
  const [showModelForm, setShowModelForm] = useState(false);
  const [showModelFetch, setShowModelFetch] = useState(false);
  const [showEndpointForm, setShowEndpointForm] = useState(false);
  const [endpointDraft, setEndpointDraft] = useState<EndpointDraft | null>(
    null,
  );
  const [testingDefault, setTestingDefault] = useState(false);
  const [testingModelId, setTestingModelId] = useState<string | null>(null);
  const [testingEndpointKey, setTestingEndpointKey] = useState<string | null>(
    null,
  );
  const [savingModel, setSavingModel] = useState(false);
  const [pendingDeleteModel, setPendingDeleteModel] =
    useState<AIModelConfig | null>(null);
  const [pendingDeleteEndpoint, setPendingDeleteEndpoint] =
    useState<EndpointGroup | null>(null);
  const [modelEndpointLocked, setModelEndpointLocked] = useState(false);
  const [availableModels, setAvailableModels] = useState<ModelInfo[]>([]);
  const [fetchingModels, setFetchingModels] = useState(false);
  const [endpointStatuses, setEndpointStatuses] = useState<
    Record<string, EndpointStatus>
  >({});

  const aiModels = settings.aiModels;

  const resolvedRouteModels = useMemo(
    () =>
      Object.fromEntries(
        MODEL_ROUTE_DEFINITIONS.map((item) => [
          item.key,
          resolveRouteModel(aiModels, settings.modelRouteDefaults, item.key),
        ]),
      ) as Record<AIModelRoute, AIModelConfig | null>,
    [aiModels, settings.modelRouteDefaults],
  );

  const endpointGroups = useMemo(() => {
    const grouped = (settings.aiProviders ?? []).reduce<
      Record<string, EndpointGroup>
    >((acc, providerConfig) => {
      const key = buildEndpointKey(providerConfig);
      acc[key] = {
        key,
        providerConfigId: providerConfig.id,
        name: providerConfig.name,
        provider: providerConfig.provider,
        apiProtocol: providerConfig.apiProtocol,
        apiKey: providerConfig.apiKey,
        apiUrl: providerConfig.apiUrl,
        models: [],
      };
      return acc;
    }, {});

    for (const model of aiModels) {
      const providerGroup = findProviderForModel(Object.values(grouped), model);
      const key = providerGroup?.key ?? buildEndpointGroupKey(model);
      if (!grouped[key]) {
        grouped[key] = {
          key,
          providerConfigId: model.providerId,
          provider: model.provider,
          apiProtocol: model.apiProtocol,
          apiKey: model.apiKey,
          apiUrl: model.apiUrl,
          models: [],
        };
      }
      grouped[key].models.push(model);
    }

    return Object.values(grouped).sort((left, right) =>
      left.provider.localeCompare(right.provider),
    );
  }, [aiModels, settings.aiProviders]);

  const hasLegacyOnlyConfig = useMemo(
    () =>
      aiModels.length === 0 &&
      Boolean(
        settings.aiProvider.trim() &&
        settings.aiApiKey.trim() &&
        settings.aiApiUrl.trim() &&
        settings.aiModel.trim(),
      ),
    [
      aiModels.length,
      settings.aiApiKey,
      settings.aiApiUrl,
      settings.aiModel,
      settings.aiProvider,
    ],
  );

  const modelScenarioBadges = useMemo(() => {
    const entries = Object.entries(resolvedRouteModels) as Array<
      [AIModelRoute, AIModelConfig | null]
    >;
    const mapping = new Map<string, string[]>();

    for (const [route, model] of entries) {
      if (!model) {
        continue;
      }

      const badgeKey = MODEL_ROUTE_DEFINITIONS.find(
        (item) => item.key === route,
      )?.badgeKey;
      const badge = badgeKey ? t(badgeKey) : null;
      if (!badge) {
        continue;
      }

      const existing = mapping.get(model.id) ?? [];
      existing.push(badge);
      mapping.set(model.id, existing);
    }

    return mapping;
  }, [resolvedRouteModels, t]);

  // 状态总览与模型路由共用同一份路由定义，保证五条路由一一对应
  const statusCards = useMemo<StatusCardData[]>(
    () =>
      MODEL_ROUTE_DEFINITIONS.map((item) => ({
        title: t(item.labelKey),
        value: getModelDisplayName(
          resolvedRouteModels[item.key],
          t("settings.aiWorkbenchNotConfigured"),
        ),
        detail: t(item.descKey),
        tone: resolvedRouteModels[item.key] ? "ready" : "warning",
        icon: item.icon,
      })),
    [resolvedRouteModels, t],
  );

  const fetchModelsForForm = async (form: ModelFormState) => {
    if (!form.apiKey.trim() || !form.apiUrl.trim()) {
      showToast(t("settings.fillApiFirst"), "error");
      return false;
    }

    setFetchingModels(true);
    const result = await fetchAvailableModels(
      form.apiUrl,
      form.apiKey,
      form.apiProtocol,
    );
    setFetchingModels(false);

    if (!result.success || result.models.length === 0) {
      const feedback = getFetchModelsFeedback(result, t, form.apiUrl);
      showToast(feedback.message, feedback.type);
      return false;
    }

    setAvailableModels(result.models);
    showToast(
      t("settings.modelsLoaded", { count: result.models.length }),
      "success",
    );
    return true;
  };

  const createModelFormState = (preset?: Partial<ModelFormState>) => {
    const provider = preset?.provider || EMPTY_FORM.provider;
    const providerInfo = getProviderInfo(provider);
    const apiProtocol =
      preset?.apiProtocol ??
      providerInfo?.recommendedProtocol ??
      EMPTY_FORM.apiProtocol;
    const nextForm = {
      ...EMPTY_FORM,
      ...preset,
      provider,
      apiProtocol,
      apiUrl: preset?.apiUrl ?? providerInfo?.defaultUrl ?? EMPTY_FORM.apiUrl,
      capabilities: preset?.capabilities
        ? { ...cloneDefaultCapabilities(), ...preset.capabilities }
        : cloneDefaultCapabilities(),
    };

    return nextForm;
  };

  const openAddModel = (
    preset?: Partial<ModelFormState>,
    options?: { lockEndpoint?: boolean },
  ) => {
    const nextForm = createModelFormState(preset);

    setEditingModelId(null);
    setAvailableModels([]);
    setModelEndpointLocked(options?.lockEndpoint === true);
    setModelForm(nextForm);
    setShowModelForm(true);
  };

  const openFetchModels = async (preset?: Partial<ModelFormState>) => {
    const nextForm = createModelFormState(preset);

    setEditingModelId(null);
    setAvailableModels([]);
    setModelEndpointLocked(true);
    setModelForm(nextForm);
    setShowModelFetch(true);

    const loaded = await fetchModelsForForm(nextForm);
    if (!loaded) {
      setShowModelFetch(false);
    }
  };

  const openEditModel = (model: AIModelConfig) => {
    setEditingModelId(model.id);
    setAvailableModels([]);
    setModelEndpointLocked(true);
    setModelForm(createFormFromModel(model));
    setShowModelForm(true);
  };

  const closeModelForm = () => {
    setEditingModelId(null);
    setAvailableModels([]);
    setModelEndpointLocked(false);
    setShowModelForm(false);
    setModelForm({
      ...EMPTY_FORM,
      capabilities: cloneDefaultCapabilities(),
    });
  };

  const closeModelFetch = () => {
    setShowModelFetch(false);
    setAvailableModels([]);
    setModelForm({
      ...EMPTY_FORM,
      capabilities: cloneDefaultCapabilities(),
    });
  };

  const notifyModelTestOutcome = (
    outcome: ModelTestOutcome,
    modelName: string,
    apiUrl: string,
  ) => {
    if (outcome.status === "success") {
      showToast(
        formatModelTestSuccessToast(modelName, outcome.latency, t),
        "success",
      );
      return;
    }
    showToast(
      formatModelTestFailureToast(
        modelName,
        outcome.message || t("toast.connectionFailed"),
        t,
        apiUrl,
      ),
      "error",
    );
  };

  const handleTestDraft = async () => {
    if (
      !modelForm.apiKey.trim() ||
      !modelForm.apiUrl.trim() ||
      !modelForm.model.trim()
    ) {
      showToast(t("settings.fillComplete"), "error");
      return;
    }

    setTestingModelId(editingModelId || "__draft__");
    const modelName = modelForm.name.trim() || modelForm.model.trim() || "AI";
    const outcome = await runModelConnectionTest(
      {
        provider: modelForm.provider,
        apiProtocol: modelForm.apiProtocol,
        apiKey: modelForm.apiKey,
        apiUrl: modelForm.apiUrl,
        model: modelForm.model,
      },
      modelForm.capabilities,
    );
    setTestingModelId(null);
    notifyModelTestOutcome(outcome, modelName, modelForm.apiUrl);
  };

  const handleSaveModel = () => {
    if (
      !modelForm.provider.trim() ||
      !modelForm.apiKey.trim() ||
      !modelForm.apiUrl.trim() ||
      !modelForm.model.trim()
    ) {
      showToast(t("settings.fillComplete"), "error");
      return;
    }

    setSavingModel(true);
    const payload = {
      name: modelForm.name.trim(),
      providerId: modelForm.providerId?.trim() || undefined,
      provider: modelForm.provider.trim(),
      apiProtocol: modelForm.apiProtocol,
      apiKey: modelForm.apiKey.trim(),
      apiUrl: normalizeApiUrlInput(modelForm.apiUrl),
      model: modelForm.model.trim(),
      capabilities: {
        ...cloneDefaultCapabilities(),
        ...modelForm.capabilities,
      },
    };

    if (editingModelId) {
      settings.updateAiModel(editingModelId, payload);
      showToast(t("settings.modelUpdated"), "success");
    } else {
      settings.addAiModel(payload);
      showToast(t("settings.modelAdded"), "success");
    }

    setSavingModel(false);
    closeModelForm();
  };

  const handleBatchAddModels = (selectedIds: string[]) => {
    if (
      !modelForm.provider.trim() ||
      !modelForm.apiKey.trim() ||
      !modelForm.apiUrl.trim()
    ) {
      showToast(t("settings.fillApiFirst"), "error");
      return;
    }

    const inferredModels = selectedIds.map((modelId) => ({
      modelId,
      attributes: inferModelAttributes(modelId),
    }));

    setSavingModel(true);
    for (const { modelId, attributes } of inferredModels) {
      settings.addAiModel({
        name: "",
        providerId: modelForm.providerId?.trim() || undefined,
        provider: modelForm.provider.trim(),
        apiProtocol: modelForm.apiProtocol,
        apiKey: modelForm.apiKey.trim(),
        apiUrl: normalizeApiUrlInput(modelForm.apiUrl),
        model: modelId,
        capabilities: {
          ...cloneDefaultCapabilities(),
          ...attributes.capabilities,
        },
      });
    }
    setSavingModel(false);
    showToast(t("settings.modelAdded") + ` (${selectedIds.length})`, "success");
    closeModelForm();
    closeModelFetch();
  };

  const confirmDeleteModel = () => {
    const model = pendingDeleteModel;
    if (!model) {
      return;
    }
    setPendingDeleteModel(null);

    const group = endpointGroups.find((item) =>
      item.models.some((groupModel) => groupModel.id === model.id),
    );
    if (group && !group.providerConfigId && group.models.length === 1) {
      settings.addAiProvider({
        name: group.name || getEndpointDisplayName(group),
        provider: group.provider,
        apiProtocol: group.apiProtocol,
        apiKey: group.apiKey,
        apiUrl: group.apiUrl,
        lastVerifiedAt: undefined,
      });
    }

    settings.deleteAiModel(model.id);
    showToast(t("settings.aiWorkbenchModelDeleted"), "success");
  };

  const confirmDeleteEndpoint = () => {
    const group = pendingDeleteEndpoint;
    if (!group) {
      return;
    }
    setPendingDeleteEndpoint(null);
    settings.deleteAiProvider({
      providerId: group.providerConfigId,
      modelIds: group.models.map((model) => model.id),
    });
    showToast(t("settings.aiWorkbenchProviderDeleted"), "success");
  };

  const handleTestModel = async (model: AIModelConfig) => {
    if (!isConfiguredModel(model)) {
      showToast(t("settings.aiWorkbenchIncompleteModel"), "error");
      return;
    }

    setTestingModelId(model.id);
    const modelName = getModelDisplayName(model, "AI");
    const outcome = await runModelConnectionTest(
      toAIConfig(model),
      model.capabilities,
    );
    setTestingModelId(null);
    notifyModelTestOutcome(outcome, modelName, model.apiUrl);
  };

  const handleTestEndpoint = async (group: EndpointGroup) => {
    // 优先用对话模型测端点连通性；只有专用模型时按其能力测试
    const targetModel =
      group.models.find(
        (model) =>
          isConfiguredModel(model) && model.capabilities?.chat !== false,
      ) ?? group.models.find(isConfiguredModel);
    if (!targetModel) {
      showToast(t("settings.aiWorkbenchEndpointNotTestable"), "error");
      return;
    }

    setTestingEndpointKey(group.key);
    const outcome = await runModelConnectionTest(
      toAIConfig(targetModel),
      targetModel.capabilities,
    );
    setTestingEndpointKey(null);

    if (outcome.status === "success") {
      const verifiedAt = new Date().toISOString();
      setEndpointStatuses((prev) => ({
        ...prev,
        [group.key]: {
          tone: "ready",
          label: t("settings.aiWorkbenchConnected"),
          detail: `${targetModel.model} · ${outcome.latency}ms`,
        },
      }));
      for (const model of group.models) {
        settings.updateAiModel(model.id, { lastVerifiedAt: verifiedAt });
      }
      showToast(
        t("settings.aiWorkbenchEndpointConnected", {
          latency: outcome.latency,
        }),
        "success",
      );
      return;
    }

    const message = getConnectionErrorMessage(
      outcome.message || t("toast.connectionFailed"),
      t,
      targetModel.apiUrl,
    );
    setEndpointStatuses((prev) => ({
      ...prev,
      [group.key]: {
        tone: "error",
        label: t("toast.connectionFailed"),
        detail: message,
      },
    }));
    showToast(message, "error");
  };

  const openEditEndpoint = (group: EndpointGroup) => {
    const firstModel = group.models[0];
    setEndpointDraft({
      key: group.key,
      providerConfigId: group.providerConfigId,
      name: group.name || getEndpointDisplayName(group),
      provider: group.provider,
      apiProtocol: group.apiProtocol,
      apiKey: group.apiKey || firstModel?.apiKey || "",
      apiUrl: group.apiUrl,
    });
    setShowEndpointForm(true);
  };

  const openAddEndpoint = () => {
    const providerInfo = getProviderInfo(EMPTY_FORM.provider);
    setEndpointDraft({
      key: "",
      providerConfigId: undefined,
      name: providerInfo?.name || EMPTY_FORM.provider,
      provider: EMPTY_FORM.provider,
      apiProtocol: providerInfo?.recommendedProtocol || EMPTY_FORM.apiProtocol,
      apiKey: "",
      apiUrl: providerInfo?.defaultUrl || EMPTY_FORM.apiUrl,
    });
    setShowEndpointForm(true);
  };

  const closeEndpointForm = () => {
    setShowEndpointForm(false);
    setEndpointDraft(null);
  };

  const updateEndpointConfig = (
    targetGroup: EndpointGroup,
    providerConfig: {
      name: string;
      provider: string;
      apiProtocol: EndpointGroup["apiProtocol"];
      apiKey: string;
      apiUrl: string;
      lastVerifiedAt?: string;
    },
  ) => {
    if (targetGroup.providerConfigId) {
      settings.updateAiProvider(targetGroup.providerConfigId, providerConfig);
    }

    // 只把端点字段落到旗下模型上。整份 spread 会把供应商的 name 一路灌进
    // model.name，模型路由的下拉里每一项都变成供应商名，一个模型都分不出来。
    for (const model of targetGroup.models) {
      settings.updateAiModel(model.id, {
        providerId: targetGroup.providerConfigId,
        provider: providerConfig.provider,
        apiProtocol: providerConfig.apiProtocol,
        apiKey: providerConfig.apiKey,
        apiUrl: providerConfig.apiUrl,
        // 凭据变了，上一次的验证结果不再作数，这个 undefined 要写进去
        lastVerifiedAt: providerConfig.lastVerifiedAt,
      });
    }

    setEndpointStatuses((prev) => {
      const next = { ...prev };
      delete next[targetGroup.key];
      return next;
    });
  };

  const handleSaveEndpoint = () => {
    if (!endpointDraft) {
      return;
    }

    const providerConfig = {
      name:
        endpointDraft.name.trim() ||
        getEndpointDisplayName({ provider: endpointDraft.provider }),
      provider: endpointDraft.provider.trim(),
      apiProtocol: endpointDraft.apiProtocol,
      apiKey: endpointDraft.apiKey.trim(),
      apiUrl: normalizeApiUrlInput(endpointDraft.apiUrl),
      lastVerifiedAt: undefined,
    };

    const targetGroup = endpointGroups.find(
      (group) => group.key === endpointDraft.key,
    );
    if (!targetGroup) {
      settings.addAiProvider(providerConfig);
      closeEndpointForm();
      showToast(t("settings.aiWorkbenchProviderAdded"), "success");
      return;
    }

    updateEndpointConfig(targetGroup, providerConfig);
    closeEndpointForm();
    showToast(t("settings.aiWorkbenchEndpointUpdated"), "success");
  };

  const handleUpdateEndpointCredentials = (
    group: EndpointGroup,
    credentials: { apiKey: string; apiUrl: string },
  ) => {
    updateEndpointConfig(group, {
      name: group.name || getEndpointDisplayName(group),
      provider: group.provider,
      apiProtocol: group.apiProtocol,
      apiKey: credentials.apiKey.trim(),
      apiUrl: normalizeApiUrlInput(credentials.apiUrl),
      lastVerifiedAt: undefined,
    });
    showToast(t("settings.aiWorkbenchEndpointUpdated"), "success");
  };

  const handleTestDefaultModel = async () => {
    const model = resolvedRouteModels.mainText || resolvedRouteModels.fastText;

    if (!model || !isConfiguredModel(model)) {
      showToast(t("settings.aiWorkbenchNoDefaultModel"), "error");
      return;
    }

    setTestingDefault(true);
    await handleTestModel(model);
    setTestingDefault(false);
  };

  const importLegacyConfig = () => {
    settings.addAiModel({
      name: settings.aiModel,
      provider: settings.aiProvider,
      apiProtocol: settings.aiApiProtocol,
      apiKey: settings.aiApiKey,
      apiUrl: settings.aiApiUrl,
      model: settings.aiModel,
      capabilities: cloneDefaultCapabilities(),
    });
    showToast(t("settings.aiWorkbenchLegacyImported"), "success");
  };

  const resolvedEndpointStatuses = Object.fromEntries(
    endpointGroups
      .map((group) => {
        const runtimeStatus = endpointStatuses[group.key];
        if (runtimeStatus) {
          return [group.key, runtimeStatus] as const;
        }

        const persistedStatus = buildVerifiedEndpointStatus(group, t);
        return persistedStatus ? ([group.key, persistedStatus] as const) : null;
      })
      .filter(
        (entry): entry is readonly [string, EndpointStatus] => entry !== null,
      ),
  );

  const routingContent = (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center justify-end gap-3">
        <button
          type="button"
          onClick={() => void handleTestDefaultModel()}
          disabled={testingDefault}
          className="inline-flex h-9 shrink-0 items-center gap-2 whitespace-nowrap rounded-lg border border-border bg-background px-4 text-sm font-medium leading-none shadow-sm transition-colors hover:bg-accent hover:text-accent-foreground disabled:opacity-50"
        >
          {testingDefault ? (
            <Spinner aria-hidden="true" size="sm" tone="muted" />
          ) : (
            <TestTubeIcon
              aria-hidden="true"
              className="h-4 w-4 text-muted-foreground"
            />
          )}
          {t("settings.aiWorkbenchTestDefault")}
        </button>
        <button
          type="button"
          onClick={() => {
            const group = endpointGroups[0];
            if (!group) {
              openAddEndpoint();
              return;
            }
            openAddModel(
              {
                provider: group.provider,
                apiProtocol: group.apiProtocol,
                apiKey: group.apiKey,
                apiUrl: group.apiUrl,
              },
              { lockEndpoint: true },
            );
          }}
          className="inline-flex h-9 shrink-0 items-center gap-2 whitespace-nowrap rounded-lg bg-primary px-4 text-sm font-medium leading-none text-primary-foreground shadow-sm transition-colors hover:bg-primary/90"
        >
          {t("settings.addModel")}
        </button>
      </div>

      <div>
        <h3 className="mb-3 text-sm font-medium text-muted-foreground">
          {t("settings.aiWorkbenchStatusOverview")}
        </h3>
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {statusCards.map((card) => (
            <StatusCard key={card.title} {...card} />
          ))}
        </div>
      </div>

      <ScenarioDefaultsSection
        aiModels={aiModels}
        modelRouteDefaults={settings.modelRouteDefaults}
        onRouteChange={(route, value) =>
          settings.setModelRouteDefault(route, value)
        }
      />

      <UsageSection />
    </div>
  );

  // 旗下模型会一并删除，确认框必须把这个数字说出来
  const deleteEndpointMessage = pendingDeleteEndpoint
    ? t(
        pendingDeleteEndpoint.models.length > 0
          ? "settings.aiWorkbenchConfirmDeleteProviderModels"
          : "settings.aiWorkbenchConfirmDeleteProvider",
        {
          name: getEndpointDisplayName(pendingDeleteEndpoint),
          count: pendingDeleteEndpoint.models.length,
        },
      )
    : "";

  return (
    <div className="h-full min-h-0 min-w-0">
      {hasLegacyOnlyConfig ? (
        <div className="mb-3 rounded-lg border border-amber-500/30 bg-amber-500/5 p-3">
          <div className="flex items-start justify-between gap-4">
            <div>
              <div className="text-sm font-medium">
                {t("settings.aiWorkbenchLegacyBannerTitle")}
              </div>
              <div className="mt-1 text-xs text-muted-foreground">
                {t("settings.aiWorkbenchLegacyBannerDesc")}
              </div>
            </div>
            <button
              type="button"
              onClick={importLegacyConfig}
              className="inline-flex h-8 shrink-0 items-center gap-2 whitespace-nowrap rounded-md bg-primary px-3 text-xs font-medium leading-none text-primary-foreground"
            >
              {t("settings.aiWorkbenchImportLegacy")}
            </button>
          </div>
        </div>
      ) : null}

      <EndpointsSection
        routingContent={routingContent}
        endpointGroups={endpointGroups}
        endpointStatuses={resolvedEndpointStatuses}
        testingEndpointKey={testingEndpointKey}
        testingModelId={testingModelId}
        modelScenarioBadges={modelScenarioBadges}
        onTestEndpoint={(group) => void handleTestEndpoint(group)}
        onEditEndpoint={openEditEndpoint}
        onDeleteEndpoint={(group) => setPendingDeleteEndpoint(group)}
        onUpdateEndpointCredentials={handleUpdateEndpointCredentials}
        onAddProvider={openAddEndpoint}
        onAddModel={openAddModel}
        onFetchModels={(preset) => void openFetchModels(preset)}
        onSetDefaultModel={(modelId) => settings.setDefaultAiModel(modelId)}
        onTestModel={(model) => void handleTestModel(model)}
        onEditModel={openEditModel}
        onDeleteModel={(model) => setPendingDeleteModel(model)}
        onManageLocalEngine={() => requestSettingsSection("general")}
      />

      <ConfirmDialog
        isOpen={pendingDeleteModel !== null}
        onClose={() => setPendingDeleteModel(null)}
        onConfirm={confirmDeleteModel}
        title={t("common.delete")}
        message={t("settings.confirmDelete")}
        confirmText={t("common.delete")}
        cancelText={t("common.cancel")}
        variant="destructive"
      />

      <ConfirmDialog
        isOpen={pendingDeleteEndpoint !== null}
        onClose={() => setPendingDeleteEndpoint(null)}
        onConfirm={confirmDeleteEndpoint}
        title={t("settings.aiWorkbenchDeleteProvider")}
        message={deleteEndpointMessage}
        confirmText={t("common.delete")}
        cancelText={t("common.cancel")}
        variant="destructive"
      />

      {showModelForm ? (
        <ModelFormModal
          editingModelId={editingModelId}
          modelForm={modelForm}
          setModelForm={setModelForm}
          testingModelId={testingModelId}
          savingModel={savingModel}
          lockEndpointFields={modelEndpointLocked}
          onClose={closeModelForm}
          onTestDraft={() => void handleTestDraft()}
          onSave={handleSaveModel}
        />
      ) : null}

      {showModelFetch ? (
        <ModelFetchModal
          setModelForm={setModelForm}
          availableModels={availableModels}
          fetchingModels={fetchingModels}
          savingModel={savingModel}
          onClose={closeModelFetch}
          onBatchAdd={handleBatchAddModels}
        />
      ) : null}

      {showEndpointForm && endpointDraft ? (
        <EndpointFormModal
          endpointDraft={endpointDraft}
          setEndpointDraft={setEndpointDraft}
          onClose={closeEndpointForm}
          onSave={handleSaveEndpoint}
        />
      ) : null}
    </div>
  );
}
