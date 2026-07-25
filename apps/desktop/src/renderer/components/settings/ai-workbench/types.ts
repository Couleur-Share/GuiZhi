import type { LucideIcon } from "lucide-react";
import type { AIProtocol } from "@guizhi/shared/types";

import type {
  AIModelCapabilities,
  AIModelConfig,
  AIModelRoute,
} from "../../../stores/settings.store";

export type ProviderOption = {
  id: string;
  name: string;
  defaultUrl: string;
  recommendedProtocol: AIProtocol;
  allowsCustomProtocol: boolean;
  iconCategory: string;
};

export type ModelFormState = {
  name: string;
  providerId?: string;
  provider: string;
  apiProtocol: AIProtocol;
  apiKey: string;
  apiUrl: string;
  model: string;
  capabilities: Required<AIModelCapabilities>;
  chatParams: {
    temperature: number;
    maxTokens: number;
    topP: number;
    topK: string;
    frequencyPenalty: number;
    presencePenalty: number;
    stream: boolean;
    enableThinking: boolean;
    customParamsText: string;
  };
};

export type EndpointStatus = {
  tone: "ready" | "warning" | "error";
  label: string;
  detail: string;
};

export type EndpointGroup = {
  key: string;
  providerConfigId?: string;
  name?: string;
  provider: string;
  apiProtocol: AIProtocol;
  apiKey: string;
  apiUrl: string;
  models: AIModelConfig[];
};

export type EndpointDraft = {
  key: string;
  providerConfigId?: string;
  name: string;
  provider: string;
  apiProtocol: AIProtocol;
  apiKey: string;
  apiUrl: string;
};

export type ScenarioDefinition = {
  key: AIModelRoute;
  labelKey: string;
  descKey: string;
  badgeKey: string;
  /** 状态总览卡片使用的图标 */
  icon: LucideIcon;
};

export type ModelOption = {
  value: string;
  label: string;
};

export type StatusCardData = {
  title: string;
  value: string;
  detail: string;
  tone: "ready" | "warning";
  icon: LucideIcon;
};
