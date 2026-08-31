/**
 * 首次使用设置清单的就绪判定（纯函数，无 IPC）。
 * 引擎安装状态由调用方探测后传入。
 */
import type {
  AIModelConfig,
  ModelRouteDefaults,
} from "../stores/settings.store";
import { isConfiguredModel, resolveRouteModel } from "./ai-defaults";

export const SETUP_DISMISSED_KEY = "guizhi-setup-dismissed";

export type SetupChecklistItemId =
  | "textModel"
  | "transcription"
  | "ytdlp"
  | "embedding";

export type SetupSettingsSection = "ai" | "capture";

export interface SetupChecklistItem {
  id: SetupChecklistItemId;
  ready: boolean;
  /** 未完成则视为「还不能完整用起来」的阻塞项 */
  required: boolean;
}

export interface LegacyAiFields {
  aiProvider: string;
  aiApiKey: string;
  aiApiUrl: string;
  aiModel: string;
}

export interface BuildSetupChecklistInput {
  aiModels: AIModelConfig[];
  modelRouteDefaults: ModelRouteDefaults | undefined;
  legacy?: LegacyAiFields;
  funasrInstalled: boolean;
  ytdlpInstalled: boolean;
}

export function isLegacyTextModelReady(legacy: LegacyAiFields): boolean {
  return Boolean(
    legacy.aiProvider.trim() &&
      legacy.aiApiKey.trim() &&
      legacy.aiApiUrl.trim() &&
      legacy.aiModel.trim(),
  );
}

/** 主文本或快速模型任一可用（含 legacy 扁平字段）即算核心就绪 */
export function isCoreTextModelReady(
  aiModels: AIModelConfig[],
  modelRouteDefaults: ModelRouteDefaults | undefined,
  legacy?: LegacyAiFields,
): boolean {
  const main = resolveRouteModel(aiModels, modelRouteDefaults, "mainText");
  const fast = resolveRouteModel(aiModels, modelRouteDefaults, "fastText");
  if (isConfiguredModel(main) || isConfiguredModel(fast)) {
    return true;
  }
  return legacy ? isLegacyTextModelReady(legacy) : false;
}

/** 本地 FunASR 已装，或 audioText 路由已显式配置 */
export function isTranscriptionReady(
  aiModels: AIModelConfig[],
  modelRouteDefaults: ModelRouteDefaults | undefined,
  funasrInstalled: boolean,
): boolean {
  if (funasrInstalled) {
    return true;
  }
  return isConfiguredModel(
    resolveRouteModel(aiModels, modelRouteDefaults, "audioText"),
  );
}

export function isEmbeddingReady(
  aiModels: AIModelConfig[],
  modelRouteDefaults: ModelRouteDefaults | undefined,
): boolean {
  return isConfiguredModel(
    resolveRouteModel(aiModels, modelRouteDefaults, "embedding"),
  );
}

export function buildSetupChecklist(
  input: BuildSetupChecklistInput,
): SetupChecklistItem[] {
  const { aiModels, modelRouteDefaults, legacy } = input;
  return [
    {
      id: "textModel",
      ready: isCoreTextModelReady(aiModels, modelRouteDefaults, legacy),
      required: true,
    },
    {
      id: "transcription",
      ready: isTranscriptionReady(
        aiModels,
        modelRouteDefaults,
        input.funasrInstalled,
      ),
      required: false,
    },
    {
      id: "ytdlp",
      ready: input.ytdlpInstalled,
      required: false,
    },
    {
      id: "embedding",
      ready: isEmbeddingReady(aiModels, modelRouteDefaults),
      required: false,
    },
  ];
}

export function setupItemSettingsSection(
  id: SetupChecklistItemId,
): SetupSettingsSection {
  if (id === "textModel" || id === "embedding") {
    return "ai";
  }
  return "capture";
}
