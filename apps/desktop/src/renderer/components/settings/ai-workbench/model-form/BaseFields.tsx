import {
  useMemo,
  type Dispatch,
  type ReactNode,
  type SetStateAction,
} from "react";

import {
  AudioLinesIcon,
  BrainIcon,
  CheckIcon,
  DatabaseIcon,
  EyeIcon,
  ImageIcon,
  Loader2Icon,
  SparklesIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";

import {
  getApiEndpointPreview,
  getBaseUrl,
  normalizeApiUrlInput,
} from "../../../../services/ai";
import { Select } from "../../../ui/Select";
import { getCategoryIcon } from "../../../ui/ModelIcons";
import { PasswordInput } from "../../shared";
import { PROVIDER_OPTIONS } from "../constants";
import { getProviderInfo } from "../helpers";
import type { ModelFormState } from "../types";

function CapabilityCheckbox({
  checked,
  ariaLabel,
  onChange,
}: {
  checked: boolean;
  ariaLabel: string;
  onChange: (checked: boolean) => void;
}) {
  return (
    <>
      <input
        type="checkbox"
        aria-label={ariaLabel}
        checked={checked}
        onChange={(event) => onChange(event.currentTarget.checked)}
        className="sr-only"
      />
      <span
        aria-hidden="true"
        className={`mt-1 flex h-4 w-4 shrink-0 items-center justify-center rounded border transition-colors ${
          checked
            ? "border-primary bg-primary text-primary-foreground"
            : "border-muted-foreground/70 bg-background"
        }`}
      >
        {checked ? <CheckIcon className="h-3 w-3" strokeWidth={3} /> : null}
      </span>
    </>
  );
}

function CapabilityCard({
  checked,
  ariaLabel,
  icon,
  title,
  description,
  onChange,
}: {
  checked: boolean;
  ariaLabel: string;
  icon: ReactNode;
  title: string;
  description: string;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="flex cursor-pointer items-start gap-3 rounded-md border border-border/60 bg-background px-3 py-2 text-sm transition-colors hover:border-primary/30 hover:bg-muted/20 focus-within:ring-2 focus-within:ring-primary/20">
      <CapabilityCheckbox
        checked={checked}
        ariaLabel={ariaLabel}
        onChange={onChange}
      />
      {icon}
      <span className="min-w-0">
        <span className="block font-medium">{title}</span>
        <span className="mt-0.5 block text-xs text-muted-foreground">
          {description}
        </span>
      </span>
    </label>
  );
}

export function BaseFields({
  modelForm,
  setModelForm,
  fetchingModels,
  onFetchModels,
  lockEndpointFields = false,
}: {
  modelForm: ModelFormState;
  setModelForm: Dispatch<SetStateAction<ModelFormState>>;
  fetchingModels: boolean;
  onFetchModels?: () => void;
  lockEndpointFields?: boolean;
}) {
  const { t } = useTranslation();
  const capabilities = modelForm.capabilities ?? {
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
  const trimmedApiUrl = modelForm.apiUrl.trim();
  const normalizedInput = useMemo(
    () => normalizeApiUrlInput(modelForm.apiUrl),
    [modelForm.apiUrl],
  );
  const baseUrlPreview = useMemo(
    () => getBaseUrl(modelForm.apiUrl),
    [modelForm.apiUrl],
  );
  const requestPreview = useMemo(
    () => getApiEndpointPreview(modelForm.apiUrl, modelForm.apiProtocol),
    [modelForm.apiProtocol, modelForm.apiUrl],
  );
  const fullEndpointDetected = Boolean(
    trimmedApiUrl &&
    !trimmedApiUrl.endsWith("#") &&
    baseUrlPreview &&
    baseUrlPreview !== trimmedApiUrl.replace(/\/$/, ""),
  );
  const providerExamples = useMemo(() => {
    if (modelForm.apiProtocol === "gemini") {
      return [
        "https://generativelanguage.googleapis.com",
        "https://generativelanguage.googleapis.com/v1beta",
      ];
    }

    if (modelForm.apiProtocol === "anthropic") {
      return ["https://api.anthropic.com", "https://api.anthropic.com/v1"];
    }

    const provider = getProviderInfo(modelForm.provider);
    return [
      provider?.defaultUrl || "https://api.openai.com",
      "https://api.example.com/v1",
    ].filter(Boolean);
  }, [modelForm.apiProtocol, modelForm.provider]);
  const providerInfo = useMemo(
    () => getProviderInfo(modelForm.provider),
    [modelForm.provider],
  );
  const showProtocolField = providerInfo?.allowsCustomProtocol === true;
  const providerOptions = PROVIDER_OPTIONS.map((item) => ({
    value: item.id,
    label: (
      <span className="flex min-w-0 items-center gap-2">
        <span aria-hidden="true" className="shrink-0">
          {getCategoryIcon(item.iconCategory, 18)}
        </span>
        <span className="truncate">{item.name}</span>
      </span>
    ),
    labelText: item.name,
  }));

  return (
    <>
      <div>
        <label className="mb-1 block text-xs text-muted-foreground">
          {t("settings.customNameOptional")}
        </label>
        <input
          type="text"
          value={modelForm.name}
          onChange={(event) =>
            setModelForm((prev) => ({ ...prev, name: event.target.value }))
          }
          aria-label={t("settings.customNameOptional")}
          placeholder={t("settings.customNamePlaceholder")}
          className="h-10 w-full rounded-lg bg-muted px-3 text-sm"
        />
      </div>

      {lockEndpointFields ? null : (
        <>
          <div className="grid gap-4 md:grid-cols-2">
            <div>
              <label className="mb-1 block text-xs text-muted-foreground">
                {t("settings.providerName")}
              </label>
              <Select
                value={modelForm.provider}
                ariaLabel={t("settings.providerName")}
                onChange={(value) => {
                  const provider = getProviderInfo(value);
                  setModelForm((prev) => ({
                    ...prev,
                    providerId: undefined,
                    provider: value,
                    apiProtocol:
                      provider?.recommendedProtocol || prev.apiProtocol,
                    apiUrl: provider?.defaultUrl || prev.apiUrl,
                  }));
                }}
                options={providerOptions}
              />
            </div>
            {showProtocolField ? (
              <div>
                <label className="mb-1 block text-xs text-muted-foreground">
                  {t("settings.protocol")}
                </label>
                <Select
                  value={modelForm.apiProtocol}
                  ariaLabel={t("settings.protocol")}
                  disabled={lockEndpointFields}
                  onChange={(value) =>
                    setModelForm((prev) => ({
                      ...prev,
                      apiProtocol: value as ModelFormState["apiProtocol"],
                    }))
                  }
                  options={[
                    {
                      value: "openai",
                      label: t("settings.protocolOpenAICompatible"),
                    },
                    {
                      value: "gemini",
                      label: t("settings.protocolGeminiCompatible"),
                    },
                    {
                      value: "anthropic",
                      label: t("settings.protocolAnthropicCompatible"),
                    },
                  ]}
                />
              </div>
            ) : null}
            <div>
              <label className="mb-1 block text-xs text-muted-foreground">
                {t("settings.apiKey")}
              </label>
              <PasswordInput
                value={modelForm.apiKey}
                disabled={lockEndpointFields}
                ariaLabel={t("settings.apiKey")}
                placeholder={t("settings.apiKeyPlaceholder")}
                onChange={(value) =>
                  setModelForm((prev) => ({ ...prev, apiKey: value }))
                }
              />
            </div>
          </div>

          <div>
            <label className="mb-1 block text-xs text-muted-foreground">
              {t("settings.apiUrl")}
            </label>
            <input
              type="text"
              value={modelForm.apiUrl}
              disabled={lockEndpointFields}
              onChange={(event) =>
                setModelForm((prev) => ({
                  ...prev,
                  apiUrl: event.target.value,
                }))
              }
              onBlur={() =>
                setModelForm((prev) => {
                  const nextApiUrl = normalizeApiUrlInput(prev.apiUrl);
                  return nextApiUrl === prev.apiUrl
                    ? prev
                    : { ...prev, apiUrl: nextApiUrl };
                })
              }
              aria-label={t("settings.apiUrl")}
              placeholder={t("settings.apiUrlPlaceholder")}
              className="h-10 w-full rounded-lg bg-muted px-3 text-sm disabled:cursor-not-allowed disabled:opacity-60"
            />
            <div className="mt-2 space-y-2 rounded-lg border border-border/60 bg-muted/20 p-3 text-xs">
              <div className="text-muted-foreground">
                {t("settings.aiWorkbenchApiUrlGuide")}
              </div>
              <div className="text-muted-foreground">
                <span className="font-medium text-foreground">
                  {t("settings.aiWorkbenchApiUrlExamplesLabel")}:
                </span>{" "}
                <span className="font-mono">
                  {providerExamples.join("  ·  ")}
                </span>
              </div>
              {baseUrlPreview ? (
                <div className="flex flex-col gap-1 text-muted-foreground">
                  <span className="font-medium text-foreground">
                    {t("settings.aiWorkbenchApiUrlBaseLabel")}:
                  </span>
                  <span className="break-all font-mono text-primary">
                    {baseUrlPreview}
                  </span>
                </div>
              ) : null}
              {requestPreview ? (
                <div className="flex flex-col gap-1 text-muted-foreground">
                  <span className="font-medium text-foreground">
                    {t("settings.aiWorkbenchApiUrlRequestLabel")}:
                  </span>
                  <span className="break-all font-mono text-primary">
                    {requestPreview}
                  </span>
                </div>
              ) : null}
              {trimmedApiUrl.endsWith("#") ? (
                <div className="inline-flex w-fit rounded-md border border-amber-500/20 bg-amber-500/10 px-2 py-1 text-[11px] text-amber-600 dark:text-amber-400">
                  {t("settings.autoFillDisabled")}
                </div>
              ) : null}
              {fullEndpointDetected || normalizedInput !== trimmedApiUrl ? (
                <div className="text-[11px] text-amber-600 dark:text-amber-400">
                  {t("settings.aiWorkbenchApiUrlDetectedFullEndpoint")}
                </div>
              ) : null}
            </div>
          </div>
        </>
      )}

      <div>
        <div className="mb-1 flex items-center justify-between text-xs text-muted-foreground">
          <span>{t("settings.modelName")}</span>
          {onFetchModels ? (
            <button
              type="button"
              onClick={onFetchModels}
              disabled={fetchingModels}
              className="inline-flex items-center gap-1 rounded-md px-2 py-1 text-primary hover:bg-primary/10 disabled:opacity-50"
            >
              {fetchingModels ? (
                <Loader2Icon
                  aria-hidden="true"
                  className="h-3.5 w-3.5 animate-spin"
                />
              ) : (
                <SparklesIcon aria-hidden="true" className="h-3.5 w-3.5" />
              )}
              {t("settings.fetchModels")}
            </button>
          ) : null}
        </div>
        <input
          type="text"
          value={modelForm.model}
          onChange={(event) =>
            setModelForm((prev) => ({ ...prev, model: event.target.value }))
          }
          aria-label={t("settings.modelName")}
          placeholder={t("settings.modelNamePlaceholder")}
          className="h-10 w-full rounded-lg bg-muted px-3 text-sm"
        />
      </div>

      <div className="rounded-lg border border-border/60 bg-muted/20 p-3">
        <div className="mb-1 text-xs font-medium text-muted-foreground">
          {t("settings.aiWorkbenchModelCapabilities")}
        </div>
        <p className="mb-3 text-[11px] leading-relaxed text-muted-foreground/70">
          {t(
            "settings.aiWorkbenchModelCapabilitiesHint",
            "普通对话模型无需勾选任何项，即可用于问答、摘要、排版等文本任务。",
          )}
        </p>
        <div className="grid gap-2 lg:grid-cols-2">
          <CapabilityCard
            checked={capabilities.vision === true}
            ariaLabel={t("settings.aiWorkbenchVisionCapability")}
            icon={
              <EyeIcon className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
            }
            title={t("settings.aiWorkbenchVisionCapability")}
            description={t("settings.aiWorkbenchVisionCapabilityDesc")}
            onChange={(checked) => {
                setModelForm((prev) => ({
                  ...prev,
                  capabilities: {
                    ...prev.capabilities,
                    chat: true,
                    vision: checked,
                    embedding: false,
                    audioTranscription: false,
                    imageGeneration: false,
                  },
                }));
            }}
          />

          <CapabilityCard
            checked={capabilities.reasoning === true}
            ariaLabel={t("settings.aiWorkbenchReasoningCapability")}
            icon={
              <BrainIcon className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
            }
            title={t("settings.aiWorkbenchReasoningCapability")}
            description={t("settings.aiWorkbenchReasoningCapabilityDesc")}
            onChange={(checked) => {
                setModelForm((prev) => ({
                  ...prev,
                  capabilities: {
                    ...prev.capabilities,
                    chat: true,
                    reasoning: checked,
                    embedding: false,
                    audioTranscription: false,
                    imageGeneration: false,
                  },
                }));
            }}
          />

          <CapabilityCard
            checked={capabilities.embedding === true}
            ariaLabel={t("settings.aiWorkbenchEmbeddingCapability")}
            icon={
              <DatabaseIcon className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
            }
            title={t("settings.aiWorkbenchEmbeddingCapability")}
            description={t("settings.aiWorkbenchEmbeddingCapabilityDesc")}
            onChange={(checked) => {
                // 嵌入是专用模型：勾选后退出对话能力
                setModelForm((prev) => ({
                  ...prev,
                  capabilities: {
                    ...prev.capabilities,
                    chat: !checked,
                    vision: false,
                    reasoning: checked ? false : prev.capabilities.reasoning,
                    embedding: checked,
                    audioTranscription: false,
                    imageGeneration: false,
                  },
                }));
            }}
          />

          <CapabilityCard
            checked={capabilities.audioTranscription === true}
            ariaLabel={t("settings.aiWorkbenchTranscriptionCapability")}
            icon={
              <AudioLinesIcon className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
            }
            title={t("settings.aiWorkbenchTranscriptionCapability")}
            description={t("settings.aiWorkbenchTranscriptionCapabilityDesc")}
            onChange={(checked) => {
                // 转写是专用模型：勾选后退出对话能力
                setModelForm((prev) => ({
                  ...prev,
                  capabilities: {
                    ...prev.capabilities,
                    chat: !checked,
                    vision: false,
                    reasoning: checked ? false : prev.capabilities.reasoning,
                    embedding: false,
                    audioTranscription: checked,
                    imageGeneration: false,
                  },
                }));
            }}
          />

          <CapabilityCard
            checked={capabilities.imageGeneration === true}
            ariaLabel={t("settings.aiWorkbenchImageGenCapability")}
            icon={
              <ImageIcon className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
            }
            title={t("settings.aiWorkbenchImageGenCapability")}
            description={t("settings.aiWorkbenchImageGenCapabilityDesc")}
            onChange={(checked) => {
                // 文生图是专用模型：勾选后退出对话能力
                setModelForm((prev) => ({
                  ...prev,
                  capabilities: {
                    ...prev.capabilities,
                    chat: !checked,
                    vision: false,
                    reasoning: checked ? false : prev.capabilities.reasoning,
                    embedding: false,
                    audioTranscription: false,
                    imageGeneration: checked,
                  },
                }));
            }}
          />
        </div>
      </div>
    </>
  );
}
