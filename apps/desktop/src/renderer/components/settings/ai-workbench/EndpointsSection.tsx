import { useEffect, useMemo, useState, type ReactNode } from "react";

import {
  AudioLinesIcon,
  BrainIcon,
  CheckIcon,
  DatabaseIcon,
  EyeIcon,
  EyeOffIcon,
  ImageIcon,
  KeyRoundIcon,
  LinkIcon,
  ListPlusIcon,
  Loader2Icon,
  PackageIcon,
  PencilIcon,
  PlusIcon,
  RouteIcon,
  SearchIcon,
  SettingsIcon,
  StarIcon,
  TestTubeIcon,
  Trash2Icon,
  TypeIcon,
  XIcon,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { FUNASR_MODEL_ID, FUNASR_PROVIDER_ID } from "@guizhi/shared/constants";

import { ToggleSwitch } from "../shared";
import {
  hasModelCapability,
  isConfiguredModel,
} from "../../../services/ai-defaults";
import type { AIModelConfig } from "../../../stores/settings.store";
import { getCategoryIcon } from "../../ui/ModelIcons";
import {
  getEndpointDisplayName,
  getEndpointCategory,
  getEndpointHost,
  getModelCategory,
} from "./helpers";
import type { EndpointGroup, EndpointStatus, ModelFormState } from "./types";

function getStatusDotClass(tone: EndpointStatus["tone"]): string {
  if (tone === "ready") {
    return "bg-emerald-500 ring-emerald-500/15";
  }
  if (tone === "error") {
    return "bg-red-500 ring-red-500/15";
  }
  return "bg-amber-500 ring-amber-500/15";
}

function maskApiKey(apiKey: string): string {
  const trimmed = apiKey.trim();
  if (!trimmed) {
    return "******";
  }
  if (trimmed.length <= 8) {
    return "******";
  }
  return `${trimmed.slice(0, 3)}****${trimmed.slice(-4)}`;
}

type ModelBadge = {
  icon: LucideIcon;
  label: string;
  primary?: boolean;
};

function ModelIconBadge({ badge }: { badge: ModelBadge }) {
  const Icon = badge.icon;
  return (
    <span
      aria-label={badge.label}
      title={badge.label}
      className={`inline-flex h-7 w-7 items-center justify-center rounded-md ${
        badge.primary
          ? "bg-primary/10 text-primary"
          : "border border-border text-muted-foreground"
      }`}
    >
      <Icon aria-hidden="true" className="h-3.5 w-3.5" />
    </span>
  );
}

function ModelRouteBadge({ label }: { label: string }) {
  return (
    <span className="inline-flex h-7 items-center rounded-md bg-primary/10 px-2 text-[11px] font-medium text-primary">
      {label}
    </span>
  );
}

export function EndpointsSection({
  routingContent,
  endpointGroups,
  endpointStatuses,
  testingEndpointKey,
  testingModelId,
  modelScenarioBadges,
  onTestEndpoint,
  onEditEndpoint,
  onDeleteEndpoint,
  onToggleEndpointEnabled,
  onUpdateEndpointCredentials,
  onAddProvider,
  onAddModel,
  onFetchModels,
  onSetDefaultModel,
  onTestModel,
  onEditModel,
  onDeleteModel,
  onManageLocalEngine,
}: {
  routingContent: ReactNode;
  endpointGroups: EndpointGroup[];
  endpointStatuses: Record<string, EndpointStatus>;
  testingEndpointKey: string | null;
  testingModelId: string | null;
  modelScenarioBadges: Map<string, string[]>;
  onTestEndpoint: (group: EndpointGroup) => void;
  onEditEndpoint: (group: EndpointGroup) => void;
  onDeleteEndpoint: (group: EndpointGroup) => void;
  onToggleEndpointEnabled: (group: EndpointGroup, enabled: boolean) => void;
  onUpdateEndpointCredentials: (
    group: EndpointGroup,
    credentials: { apiKey: string; apiUrl: string },
  ) => void;
  onAddProvider: () => void;
  onAddModel: (
    preset?: Partial<ModelFormState>,
    options?: { lockEndpoint?: boolean; fetchModels?: boolean },
  ) => void;
  onFetchModels: (preset: Partial<ModelFormState>) => void;
  onSetDefaultModel: (modelId: string) => void;
  onTestModel: (model: AIModelConfig) => void;
  onEditModel: (model: AIModelConfig) => void;
  onDeleteModel: (model: AIModelConfig) => void;
  /** 跳到「设置 → 采集」：内置转写引擎的安装 / 卸载都在那边 */
  onManageLocalEngine: () => void;
}) {
  const { t } = useTranslation();
  const [searchText, setSearchText] = useState("");
  const [activePanel, setActivePanel] = useState<"provider" | "routing">(
    "routing",
  );
  const [selectedEndpointKey, setSelectedEndpointKey] = useState<string | null>(
    endpointGroups[0]?.key ?? null,
  );

  useEffect(() => {
    if (endpointGroups.length === 0) {
      setSelectedEndpointKey(null);
      return;
    }
    if (
      !selectedEndpointKey ||
      !endpointGroups.some((group) => group.key === selectedEndpointKey)
    ) {
      setSelectedEndpointKey(endpointGroups[0].key);
    }
  }, [endpointGroups, selectedEndpointKey]);

  const selectedGroup = useMemo(
    () =>
      endpointGroups.find((group) => group.key === selectedEndpointKey) ??
      endpointGroups[0] ??
      null,
    [endpointGroups, selectedEndpointKey],
  );
  const selectedApiKey =
    selectedGroup?.apiKey || selectedGroup?.models[0]?.apiKey || "";
  const selectedApiUrl = selectedGroup?.apiUrl || "";
  const [credentialDraft, setCredentialDraft] = useState({
    groupKey: selectedGroup?.key ?? "",
    apiKey: selectedApiKey,
    apiUrl: selectedApiUrl,
  });
  const [showApiKey, setShowApiKey] = useState(false);

  useEffect(() => {
    setCredentialDraft({
      groupKey: selectedGroup?.key ?? "",
      apiKey: selectedApiKey,
      apiUrl: selectedApiUrl,
    });
  }, [selectedApiKey, selectedApiUrl, selectedGroup?.key]);

  const credentialsDirty =
    credentialDraft.groupKey === selectedGroup?.key &&
    (credentialDraft.apiKey !== selectedApiKey ||
      credentialDraft.apiUrl !== selectedApiUrl);
  const canSaveCredentials =
    credentialsDirty && credentialDraft.apiUrl.trim().length > 0;

  const saveCredentials = () => {
    if (!selectedGroup || !canSaveCredentials) {
      return;
    }
    onUpdateEndpointCredentials(selectedGroup, {
      apiKey: credentialDraft.apiKey,
      apiUrl: credentialDraft.apiUrl,
    });
  };

  const resetCredentials = () => {
    setCredentialDraft({
      groupKey: selectedGroup?.key ?? "",
      apiKey: selectedApiKey,
      apiUrl: selectedApiUrl,
    });
  };

  const filteredEndpointGroups = useMemo(() => {
    const normalizedSearch = searchText.trim().toLowerCase();
    if (!normalizedSearch) {
      return endpointGroups;
    }
    return endpointGroups.filter((group) => {
      const providerLabel = getEndpointDisplayName(group).toLowerCase();
      const endpointHost = getEndpointHost(group.apiUrl, "").toLowerCase();
      const hasMatchingModel = group.models.some((model) =>
        `${model.name ?? ""} ${model.model}`
          .toLowerCase()
          .includes(normalizedSearch),
      );
      return (
        providerLabel.includes(normalizedSearch) ||
        endpointHost.includes(normalizedSearch) ||
        hasMatchingModel
      );
    });
  }, [endpointGroups, searchText]);

  const getProtocolLabel = (
    protocol: ModelFormState["apiProtocol"],
  ): string => {
    switch (protocol) {
      case "gemini":
        return t("settings.protocolGeminiCompatible");
      case "anthropic":
        return t("settings.protocolAnthropicCompatible");
      case "openai":
      default:
        return t("settings.protocolOpenAICompatible");
    }
  };

  const getEndpointStatus = (group: EndpointGroup): EndpointStatus => {
    if (group.enabled === false) {
      return {
        tone: "warning",
        label: t("settings.aiWorkbenchDisabled", "已停用"),
        detail: t("settings.aiWorkbenchProviderDisabledDetail", {
          count: group.models.length,
          defaultValue: `已停用 · ${group.models.length} 个模型`,
        }),
      };
    }
    const runtimeStatus = endpointStatuses[group.key];
    if (runtimeStatus) {
      return runtimeStatus;
    }
    if (
      group.models.some(
        (model) =>
          typeof model.lastVerifiedAt === "string" &&
          model.lastVerifiedAt.trim().length > 0,
      )
    ) {
      return {
        tone: "ready",
        label: t("settings.aiWorkbenchConnected"),
        detail: t("settings.aiWorkbenchModelCount", {
          count: group.models.length,
        }),
      };
    }
    if (group.models.some(isConfiguredModel)) {
      return {
        tone: "warning",
        label: t("settings.aiWorkbenchUnverified"),
        detail: t("settings.aiWorkbenchModelCount", {
          count: group.models.length,
        }),
      };
    }
    return {
      tone: "warning",
      label: t("settings.aiWorkbenchNotConfigured"),
      detail: t("settings.aiWorkbenchMissingModelConfig"),
    };
  };

  if (endpointGroups.length === 0 || !selectedGroup) {
    return (
      <section className="h-full min-h-0 overflow-hidden">
        <div className="grid h-full min-h-0 lg:grid-cols-[14rem_minmax(0,1fr)]">
          <aside className="app-wallpaper-panel flex min-h-0 flex-col border-r border-border">
            <div className="border-b border-border p-3">
              <div className="mb-3 flex items-center gap-2 text-[12px] text-muted-foreground">
                <span className="shrink-0">
                  {t("settings.aiWorkbenchSubmenuModelConfig")}
                </span>
                <div className="h-px flex-1 bg-border" />
              </div>
              <div className="space-y-1">
                <button
                  type="button"
                  onClick={() => setActivePanel("routing")}
                  className={`flex h-10 w-full items-center gap-3 rounded-lg px-3 text-sm transition-colors ${
                    activePanel === "routing"
                      ? "bg-primary text-primary-foreground"
                      : "hover:bg-background"
                  }`}
                >
                  <RouteIcon aria-hidden="true" className="h-4 w-4" />
                  {t("settings.aiWorkbenchModelRouting")}
                </button>
              </div>
              <div className="my-3 flex items-center gap-2 text-[12px] text-muted-foreground">
                <span className="shrink-0">{t("settings.providerName")}</span>
                <div className="h-px flex-1 bg-border" />
              </div>
              <div className="relative">
                <SearchIcon
                  aria-hidden="true"
                  className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
                />
                <input
                  type="search"
                  value={searchText}
                  onChange={(event) => setSearchText(event.target.value)}
                  placeholder={t("settings.searchProvidersAndModels")}
                  className="h-10 w-full rounded-lg border border-border bg-background pl-9 pr-3 text-sm outline-none focus:ring-2 focus:ring-primary/20"
                />
              </div>
            </div>
            <div className="flex min-h-0 flex-1 items-center justify-center p-6 text-center text-sm text-muted-foreground">
              {t("settings.aiWorkbenchNoModels")}
            </div>
            <div className="border-t border-border p-3">
              <button
                type="button"
                onClick={onAddProvider}
                className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg border border-border bg-background text-sm font-medium hover:bg-muted"
              >
                <PlusIcon aria-hidden="true" className="h-4 w-4" />
                {t("settings.addProvider")}
              </button>
            </div>
          </aside>
          <div className="min-h-0 overflow-y-auto px-6 py-5">
            <div className="w-full">
              <h1 className="mb-4 text-lg font-semibold">{t("settings.ai")}</h1>
              {activePanel === "routing" ? (
                routingContent
              ) : (
                <div className="flex min-h-[320px] items-center justify-center text-center text-sm text-muted-foreground">
                  {t("settings.aiWorkbenchNoModels")}
                </div>
              )}
            </div>
          </div>
        </div>
      </section>
    );
  }

  const endpointStatus = getEndpointStatus(selectedGroup);
  const endpointStatusDetail =
    selectedGroup.enabled === false
      ? t("settings.aiWorkbenchModelCount", {
          count: selectedGroup.models.length,
        })
      : endpointStatus.detail;
  const providerMetaLabel = getProtocolLabel(selectedGroup.apiProtocol);
  const selectedEndpointHost = getEndpointHost(
    selectedGroup.apiUrl,
    providerMetaLabel,
  );
  const firstModel = selectedGroup.models[0];
  // 内置本地转写引擎的条目只在安装时写入、卸载时移除，没有自愈路径：地址、
  // 密钥、模型全由安装程序填好，在这里改一个字或删一行，剩下的就是装着 3GB
  // 运行时却转写不了的状态，而界面上看不出发生过什么。管理入口在「设置 → 采集」。
  //
  // 只认安装写入的那个固定 id，不用 isLocalEngineProvider——那条判据还认
  // 127.0.0.1:8620 这个地址，是给配置导入剔除对端设备条目用的，宁可多剔一条。
  // 拿到这里会把「自己在同一端口上跑了服务、手工添加」的用户一并锁死，
  // 那是他自己的条目，改不了也删不掉纯属帮倒忙。
  const localEngineSelected =
    selectedGroup.providerConfigId === FUNASR_PROVIDER_ID;

  return (
    <section className="h-full min-h-0 overflow-hidden">
      <div className="grid h-full min-h-0 lg:grid-cols-[14rem_minmax(0,1fr)]">
        <aside className="app-wallpaper-panel flex min-h-0 flex-col border-b border-border lg:border-b-0 lg:border-r">
          <div className="border-b border-border p-3">
            <div className="mb-3 flex items-center gap-2 text-[12px] text-muted-foreground">
              <span className="shrink-0">
                {t("settings.aiWorkbenchSubmenuModelConfig")}
              </span>
              <div className="h-px flex-1 bg-border" />
            </div>
            <div className="space-y-1">
              <button
                type="button"
                onClick={() => setActivePanel("routing")}
                className={`flex h-10 w-full items-center gap-3 rounded-lg px-3 text-sm transition-colors ${
                  activePanel === "routing"
                    ? "bg-primary text-primary-foreground"
                    : "hover:bg-background"
                }`}
              >
                <RouteIcon aria-hidden="true" className="h-4 w-4" />
                {t("settings.aiWorkbenchModelRouting")}
              </button>
            </div>
            <div className="my-3 flex items-center gap-2 text-[12px] text-muted-foreground">
              <span className="shrink-0">{t("settings.providerName")}</span>
              <div className="h-px flex-1 bg-border" />
            </div>
            <div className="relative mt-3">
              <SearchIcon
                aria-hidden="true"
                className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground"
              />
              <input
                type="search"
                value={searchText}
                onChange={(event) => setSearchText(event.target.value)}
                placeholder={t("settings.searchProvidersAndModels")}
                className="h-10 w-full rounded-lg border border-border bg-background pl-9 pr-3 text-sm outline-none focus:ring-2 focus:ring-primary/20"
              />
            </div>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            <div className="space-y-1">
              {filteredEndpointGroups.map((group) => {
                const status = getEndpointStatus(group);
                const selected =
                  activePanel === "provider" && group.key === selectedGroup.key;
                const category = getEndpointCategory(
                  group.provider,
                  group.models,
                );
                const groupEnabled = group.enabled !== false;
                const groupHost = getEndpointHost(
                  group.apiUrl,
                  getProtocolLabel(group.apiProtocol),
                );
                // 内置引擎在这一行就交代清楚：不说的话它和用户自己配的服务商
                // 在列表里长得一模一样，得点进去才知道是两回事
                const groupDetail =
                  group.providerConfigId === FUNASR_PROVIDER_ID
                    ? `${t("settings.aiWorkbenchBuiltin", "内置")} · ${groupHost}`
                    : groupHost;
                const displayDetail = groupEnabled
                  ? groupDetail
                  : `${t("settings.aiWorkbenchDisabled", "已停用")} · ${groupDetail}`;

                return (
                  <button
                    key={group.key}
                    type="button"
                    onClick={() => {
                      setSelectedEndpointKey(group.key);
                      setActivePanel("provider");
                    }}
                    className={`flex h-12 w-full items-center gap-2 rounded-md border px-2 text-left transition-colors ${
                      selected
                        ? "border-border bg-background shadow-sm"
                        : "border-transparent hover:bg-background/70"
                    } ${groupEnabled ? "" : "opacity-60"}`}
                  >
                    <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-muted text-primary">
                      {getCategoryIcon(category, 17)}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm font-medium">
                        {getEndpointDisplayName(group)}
                      </span>
                      <span className="block truncate text-[11px] text-muted-foreground">
                        {displayDetail}
                      </span>
                    </span>
                    <span className="flex shrink-0 items-center gap-1.5">
                      <span
                        className={`h-1.5 w-1.5 rounded-full ${
                          !groupEnabled
                            ? "bg-muted-foreground/40"
                            : status.tone === "ready"
                              ? "bg-emerald-500"
                              : status.tone === "error"
                                ? "bg-red-500"
                                : "bg-amber-500"
                        }`}
                      />
                      <span className="min-w-5 rounded-full bg-muted px-1.5 py-0.5 text-center text-[10px] font-medium text-muted-foreground">
                        {group.models.length}
                      </span>
                    </span>
                  </button>
                );
              })}
            </div>
          </div>
          <div className="border-t border-border p-3">
            <button
              type="button"
              onClick={onAddProvider}
              className="inline-flex h-10 w-full items-center justify-center gap-2 rounded-lg border border-border bg-background text-sm font-medium hover:bg-muted"
            >
              <PlusIcon aria-hidden="true" className="h-4 w-4" />
              {t("settings.addProvider")}
            </button>
          </div>
        </aside>

        {activePanel === "routing" ? (
          <div className="min-w-0 overflow-y-auto px-6 py-5">
            <div className="w-full">
              <h1 className="mb-4 text-lg font-semibold">{t("settings.ai")}</h1>
              {routingContent}
            </div>
          </div>
        ) : (
          <div className="min-w-0 overflow-y-auto px-6 py-5">
            <div className="w-full">
              <h1 className="mb-4 text-lg font-semibold">{t("settings.ai")}</h1>
              <div className="overflow-hidden rounded-xl border border-border bg-card">
                <div
                  data-testid="ai-endpoint-header"
                  className="flex flex-col gap-3 border-b border-border px-4 py-3.5 md:flex-row md:items-center md:justify-between"
                >
                  <div className="flex min-w-0 flex-1 items-center gap-3">
                    <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary">
                      {getCategoryIcon(
                        getEndpointCategory(
                          selectedGroup.provider,
                          selectedGroup.models,
                        ),
                        20,
                      )}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex min-w-0 items-center gap-2.5">
                        <h4 className="truncate text-base font-semibold leading-tight">
                          {getEndpointDisplayName(selectedGroup)}
                        </h4>
                        {localEngineSelected ? (
                          <span className="inline-flex shrink-0 items-center gap-1 rounded-md bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">
                            <PackageIcon
                              aria-hidden="true"
                              className="h-3 w-3"
                            />
                            {t("settings.aiWorkbenchBuiltin", "内置")}
                          </span>
                        ) : (
                          <span className="shrink-0 rounded-md border border-border/70 bg-muted/30 px-2 py-0.5 text-[10px] font-medium text-muted-foreground">
                            {providerMetaLabel}
                          </span>
                        )}
                      </div>
                      <p className="mt-1.5 flex min-w-0 items-center gap-2 truncate text-xs text-muted-foreground">
                        <span
                          aria-hidden="true"
                          className={`h-1.5 w-1.5 shrink-0 rounded-full ring-[3px] ${getStatusDotClass(endpointStatus.tone)}`}
                        />
                        <span className="shrink-0">{endpointStatus.label}</span>
                        <span aria-hidden="true" className="text-border">
                          ·
                        </span>
                        <span className="shrink-0">{endpointStatusDetail}</span>
                        <span aria-hidden="true" className="text-border">
                          ·
                        </span>
                        <span className="truncate">
                          {selectedEndpointHost}
                        </span>
                      </p>
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1.5">
                    <div className="mr-0.5 flex h-8 items-center gap-2 rounded-lg border border-border/70 bg-muted/25 px-2.5">
                      <span className="select-none text-[11px] font-medium text-muted-foreground">
                        {t(
                          "settings.aiWorkbenchRoutingParticipation",
                          "参与路由",
                        )}
                      </span>
                      <ToggleSwitch
                        size="compact"
                        checked={selectedGroup.enabled !== false}
                        onChange={(checked) =>
                          onToggleEndpointEnabled(selectedGroup, checked)
                        }
                        ariaLabel={
                          selectedGroup.enabled !== false
                            ? t(
                                "settings.aiWorkbenchDisableProvider",
                                "停用此供应商",
                              )
                            : t(
                                "settings.aiWorkbenchEnableProvider",
                                "启用此供应商",
                              )
                        }
                      />
                    </div>
                    <button
                      type="button"
                      onClick={() => onTestEndpoint(selectedGroup)}
                      disabled={
                        testingEndpointKey === selectedGroup.key ||
                        selectedGroup.enabled === false
                      }
                      className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border/70 bg-background/40 px-2.5 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      {testingEndpointKey === selectedGroup.key ? (
                        <Loader2Icon
                          aria-hidden="true"
                          className="h-3.5 w-3.5 animate-spin"
                        />
                      ) : (
                        <TestTubeIcon
                          aria-hidden="true"
                          className="h-3.5 w-3.5"
                        />
                      )}
                      {t("settings.testConnection")}
                    </button>
                    {/* 置灰按钮不派发指针事件，title 挂在外层才提示得出来 */}
                    <span
                      className="inline-flex"
                      title={
                        localEngineSelected
                          ? t("settings.aiWorkbenchLocalEngineReadOnly")
                          : t("common.edit")
                      }
                    >
                      <button
                        type="button"
                        aria-label={t("common.edit")}
                        onClick={() => onEditEndpoint(selectedGroup)}
                        disabled={localEngineSelected}
                        data-testid="ai-endpoint-edit"
                        className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border/70 bg-background/40 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
                      >
                        <PencilIcon
                          aria-hidden="true"
                          className="h-3.5 w-3.5"
                        />
                      </button>
                    </span>
                    <span
                      className="inline-flex"
                      title={
                        localEngineSelected
                          ? t("settings.aiWorkbenchLocalEngineLocked")
                          : t("common.delete")
                      }
                    >
                      <button
                        type="button"
                        aria-label={t("common.delete")}
                        onClick={() => onDeleteEndpoint(selectedGroup)}
                        disabled={localEngineSelected}
                        data-testid="ai-endpoint-delete"
                        className="inline-flex h-8 w-8 items-center justify-center rounded-lg border border-border/70 bg-background/40 text-muted-foreground transition-colors hover:border-destructive/30 hover:bg-destructive/10 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-40"
                      >
                        <Trash2Icon
                          aria-hidden="true"
                          className="h-3.5 w-3.5"
                        />
                      </button>
                    </span>
                  </div>
                </div>

                <div className="px-4 py-3">
                  <div className="rounded-lg border border-border/70 bg-muted/20 p-3">
                    <div className="mb-3 flex items-start justify-between gap-3">
                      <div className="text-xs font-medium text-muted-foreground">
                        {localEngineSelected
                          ? t(
                              "settings.aiWorkbenchLocalEngineSection",
                              "内置引擎",
                            )
                          : t(
                              "settings.aiWorkbenchEndpointCredentials",
                              "Endpoint credentials",
                            )}
                      </div>
                      {credentialsDirty ? (
                        <div className="flex shrink-0 items-center gap-1">
                          <button
                            type="button"
                            onClick={saveCredentials}
                            disabled={!canSaveCredentials}
                            aria-label={t("common.save")}
                            title={t("common.save")}
                            className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-border bg-background text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40"
                          >
                            <CheckIcon aria-hidden="true" className="h-4 w-4" />
                          </button>
                          <button
                            type="button"
                            onClick={resetCredentials}
                            aria-label={t("common.cancel")}
                            title={t("common.cancel")}
                            className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-border bg-background text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                          >
                            <XIcon aria-hidden="true" className="h-4 w-4" />
                          </button>
                        </div>
                      ) : null}
                    </div>
                    <div className="space-y-2">
                      {localEngineSelected ? (
                        <p className="text-xs leading-relaxed text-muted-foreground">
                          {t(
                            "settings.aiWorkbenchLocalEngineManaged",
                            "地址、密钥与下方模型都由「设置 → 采集」的一键安装自动配置，无需填写。服务只监听本机，不需要密钥。",
                          )}
                        </p>
                      ) : null}
                      <div className="flex min-w-0 items-center gap-3">
                        <LinkIcon
                          aria-hidden="true"
                          className="h-4 w-4 shrink-0 text-muted-foreground"
                        />
                        <label className="min-w-0 flex-1">
                          <span className="mb-1 block text-xs text-muted-foreground">
                            {t("settings.apiUrl")}
                          </span>
                        {localEngineSelected ? (
                          // 不带边框：同样的高度加一圈边框就是一个 disabled 的
                          // 输入框，而这里要说的恰恰是「这不是让你填的东西」
                          <span className="flex h-9 w-full items-center truncate rounded-md bg-muted/50 px-3 font-mono text-sm text-muted-foreground">
                            {selectedApiUrl}
                          </span>
                        ) : (
                            <input
                              type="text"
                              value={credentialDraft.apiUrl}
                              onChange={(event) =>
                                setCredentialDraft((draft) => ({
                                  ...draft,
                                  apiUrl: event.target.value,
                                }))
                              }
                              onKeyDown={(event) => {
                                if (event.key === "Enter") {
                                  saveCredentials();
                                }
                                if (event.key === "Escape") {
                                  resetCredentials();
                                }
                              }}
                              aria-label={t(
                                "settings.aiWorkbenchEndpointApiUrl",
                                "Endpoint API URL",
                              )}
                              className="h-9 w-full rounded-md border border-border bg-background px-3 font-mono text-sm outline-none transition-colors focus:ring-2 focus:ring-primary/15"
                            />
                          )}
                        </label>
                      </div>
                      {/* 内置引擎的 Key 是安装程序填的占位串，本地服务根本不校验：
                          摆一个带「显示密码」的输入框只会让人以为这里缺一个秘密 */}
                      {localEngineSelected ? (
                        <button
                          type="button"
                          onClick={onManageLocalEngine}
                          className="inline-flex h-8 items-center gap-1.5 rounded-md border border-border bg-background px-2.5 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                        >
                          <SettingsIcon
                            aria-hidden="true"
                            className="h-3.5 w-3.5"
                          />
                          {t(
                            "settings.aiWorkbenchLocalEngineManage",
                            "管理引擎",
                          )}
                        </button>
                      ) : (
                        <div className="flex min-w-0 items-center gap-3">
                          <KeyRoundIcon
                            aria-hidden="true"
                            className="h-4 w-4 shrink-0 text-muted-foreground"
                          />
                          <label className="min-w-0 flex-1">
                            <span className="mb-1 block text-xs text-muted-foreground">
                              {t("settings.apiKey")}
                            </span>
                            <span className="relative block">
                              <input
                                type={showApiKey ? "text" : "password"}
                                value={credentialDraft.apiKey}
                                onChange={(event) =>
                                  setCredentialDraft((draft) => ({
                                    ...draft,
                                    apiKey: event.target.value,
                                  }))
                                }
                                onKeyDown={(event) => {
                                  if (event.key === "Enter") {
                                    saveCredentials();
                                  }
                                  if (event.key === "Escape") {
                                    resetCredentials();
                                  }
                                }}
                                aria-label={t(
                                  "settings.aiWorkbenchEndpointApiKey",
                                  "Endpoint API Key",
                                )}
                                placeholder={maskApiKey(selectedApiKey)}
                                className="h-9 w-full rounded-md border border-border bg-background px-3 pr-9 font-mono text-sm outline-none transition-colors placeholder:text-muted-foreground focus:ring-2 focus:ring-primary/15"
                              />
                              <button
                                type="button"
                                onClick={() =>
                                  setShowApiKey((visible) => !visible)
                                }
                                aria-label={
                                  showApiKey
                                    ? t("common.hide", "Hide")
                                    : t("common.show", "Show")
                                }
                                title={
                                  showApiKey
                                    ? t("common.hide", "Hide")
                                    : t("common.show", "Show")
                                }
                                className="absolute right-1 top-1/2 inline-flex h-7 w-7 -translate-y-1/2 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                              >
                                {showApiKey ? (
                                  <EyeOffIcon
                                    aria-hidden="true"
                                    className="h-4 w-4"
                                  />
                                ) : (
                                  <EyeIcon
                                    aria-hidden="true"
                                    className="h-4 w-4"
                                  />
                                )}
                              </button>
                            </span>
                          </label>
                        </div>
                      )}
                    </div>
                  </div>
                </div>

                <div className="flex items-center justify-between px-4 py-2.5">
                  <div className="text-sm font-semibold">
                    {t("settings.model")}
                  </div>
                  {/* 内置引擎只有 SenseVoiceSmall 一个模型，也是安装时写进去的：
                      「获取模型列表」拉回来的还是它，「添加模型」加出来的东西没有用处 */}
                  {localEngineSelected ? null : (
                    <div className="flex items-center gap-1.5">
                      <button
                        type="button"
                        onClick={() =>
                          onFetchModels({
                            providerId: selectedGroup.providerConfigId,
                            provider: selectedGroup.provider,
                            apiProtocol: selectedGroup.apiProtocol,
                            apiKey:
                              selectedGroup.apiKey || firstModel?.apiKey || "",
                            apiUrl: selectedGroup.apiUrl,
                          })
                        }
                        aria-label={t("settings.fetchModels")}
                        title={t("settings.fetchModels")}
                        className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-border text-muted-foreground hover:bg-muted"
                      >
                        <ListPlusIcon aria-hidden="true" className="h-4 w-4" />
                      </button>
                      <button
                        type="button"
                        onClick={() =>
                          onAddModel(
                            {
                              providerId: selectedGroup.providerConfigId,
                              provider: selectedGroup.provider,
                              apiProtocol: selectedGroup.apiProtocol,
                              apiKey:
                                selectedGroup.apiKey ||
                                firstModel?.apiKey ||
                                "",
                              apiUrl: selectedGroup.apiUrl,
                            },
                            { lockEndpoint: true },
                          )
                        }
                        aria-label={t("settings.addModel")}
                        title={t("settings.addModel")}
                        className="inline-flex h-8 w-8 items-center justify-center rounded-md border border-border text-muted-foreground hover:bg-muted"
                      >
                        <PlusIcon aria-hidden="true" className="h-4 w-4" />
                      </button>
                    </div>
                  )}
                </div>

                <div className="divide-y divide-border">
                  {selectedGroup.models.map((model) => {
                    // 安装时写入的那一条：删了或改了地址，磁盘上 3GB 运行时还在
                    // 却转写不了，而这套记录没有自愈路径，只能回「采集」重装
                    const builtinModel = model.id === FUNASR_MODEL_ID;
                    // 类型徽章：嵌入 / 转写 / 文生图为专用模型，其余按对话模型展示
                    const typeBadge: ModelBadge =
                      model.capabilities?.embedding === true
                        ? {
                            label: t("settings.embeddingModel"),
                            icon: DatabaseIcon,
                            primary: false,
                          }
                        : model.capabilities?.audioTranscription === true
                          ? {
                              label: t("settings.transcriptionModel"),
                              icon: AudioLinesIcon,
                              primary: false,
                            }
                          : model.capabilities?.imageGeneration === true
                            ? {
                                label: t("settings.imageGenModel"),
                                icon: ImageIcon,
                                primary: false,
                              }
                            : {
                                label: t("settings.chatModel"),
                                icon: TypeIcon,
                                primary: false,
                              };
                    const capabilityBadges: ModelBadge[] = [
                      typeBadge,
                      ...(model.isDefault
                        ? [
                            {
                              label: t("settings.aiWorkbenchTypeDefault"),
                              icon: StarIcon,
                              primary: true,
                            },
                          ]
                        : []),
                      ...(hasModelCapability(model, "vision")
                        ? [
                            {
                              label: t("settings.aiWorkbenchVisionCapability"),
                              icon: EyeIcon,
                              primary: false,
                            },
                          ]
                        : []),
                      ...(hasModelCapability(model, "reasoning")
                        ? [
                            {
                              label: t(
                                "settings.aiWorkbenchReasoningCapability",
                              ),
                              icon: BrainIcon,
                              primary: false,
                            },
                          ]
                        : []),
                    ];
                    const routeBadges = modelScenarioBadges.get(model.id) ?? [];

                    return (
                      <div
                        key={model.id}
                        data-testid={`ai-model-row-${model.id}`}
                        className="group flex flex-col gap-3 px-4 py-3 transition-colors hover:bg-muted/20 md:flex-row md:items-center md:justify-between"
                      >
                        <div className="flex min-w-0 items-center gap-3">
                          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-muted text-primary">
                            {getCategoryIcon(getModelCategory(model), 20)}
                          </div>
                          <div className="min-w-0">
                            <div className="truncate text-sm font-medium">
                              {model.name || model.model}
                            </div>
                            {model.name ? (
                              <div className="truncate text-xs text-muted-foreground">
                                {model.model}
                              </div>
                            ) : null}
                          </div>
                          <div className="flex min-w-0 flex-wrap gap-1.5">
                            {capabilityBadges.map((badge) => (
                              <ModelIconBadge
                                key={`${model.id}-${badge.label}`}
                                badge={badge}
                              />
                            ))}
                            {routeBadges.map((badge) => (
                              <ModelRouteBadge
                                key={`${model.id}-${badge}`}
                                label={badge}
                              />
                            ))}
                          </div>
                        </div>
                        <div className="flex items-center gap-1.5">
                          <button
                            type="button"
                            onClick={() => onTestModel(model)}
                            disabled={testingModelId === model.id}
                            aria-label={t("settings.aiWorkbenchTestAction")}
                            title={t("settings.aiWorkbenchTestAction")}
                            className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted disabled:opacity-50"
                          >
                            {testingModelId === model.id ? (
                              <Loader2Icon
                                aria-hidden="true"
                                className="h-3.5 w-3.5 animate-spin"
                              />
                            ) : (
                              <TestTubeIcon
                                aria-hidden="true"
                                className="h-3.5 w-3.5"
                              />
                            )}
                          </button>
                          {!model.isDefault ? (
                            <button
                              type="button"
                              onClick={() => onSetDefaultModel(model.id)}
                              aria-label={t("settings.setDefault")}
                              title={t("settings.setDefault")}
                              className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted"
                            >
                              <StarIcon
                                aria-hidden="true"
                                className="h-3.5 w-3.5"
                              />
                            </button>
                          ) : null}
                          {/* 置灰按钮不派发指针事件，title 挂在外层才提示得出来 */}
                          <span
                            className="inline-flex"
                            title={
                              builtinModel
                                ? t("settings.aiWorkbenchLocalEngineReadOnly")
                                : undefined
                            }
                          >
                            <button
                              type="button"
                              onClick={() => onEditModel(model)}
                              disabled={builtinModel}
                              aria-label={t("common.edit")}
                              title={
                                builtinModel ? undefined : t("common.edit")
                              }
                              className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted disabled:cursor-not-allowed disabled:opacity-50 disabled:hover:bg-transparent"
                            >
                              <PencilIcon
                                aria-hidden="true"
                                className="h-3.5 w-3.5"
                              />
                            </button>
                          </span>
                          <span
                            className="inline-flex"
                            title={
                              builtinModel
                                ? t("settings.aiWorkbenchLocalEngineLocked")
                                : undefined
                            }
                          >
                            <button
                              type="button"
                              onClick={() => onDeleteModel(model)}
                              disabled={builtinModel}
                              aria-label={t("common.delete")}
                              title={
                                builtinModel ? undefined : t("common.delete")
                              }
                              className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-red-500 hover:bg-red-500/5 disabled:cursor-not-allowed disabled:text-muted-foreground disabled:opacity-50 disabled:hover:bg-transparent"
                            >
                              <Trash2Icon
                                aria-hidden="true"
                                className="h-3.5 w-3.5"
                              />
                            </button>
                          </span>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </div>
            </div>
          </div>
        )}
      </div>
    </section>
  );
}
