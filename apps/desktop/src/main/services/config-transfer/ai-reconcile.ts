/**
 * 导入的 AI 配置与本机现状对账。
 *
 * 两件事必须在写盘前处理干净：
 *
 * 其一是本地转写引擎。funasr 的服务商与模型是安装时写进 ai-models.json 的、
 * 卸载才移除，没有任何自愈路径；而导入走的是整份替换。不把本机那两条捞回来，
 * 装了本地引擎的机器导一次配置就把它弄丢，只能重装。反方向同理——导出方那两条
 * 指向 127.0.0.1:8620，在没装引擎的机器上是一条点了必然失败的路由。
 *
 * 其二是脏数据。`normalizeModelConfig` 遇到空的 id / provider / apiUrl / model
 * 会直接抛 AIConfigError，而 `replace()` 是一次性写整份——一个坏条目会让整次
 * 导入失败。逐条筛掉并记进 warnings，比整份作废对用户有用得多。
 */
import type {
  CoreAIConfigFile,
  CoreAIModelConfig,
  CoreAIModelRoute,
  CoreAIProviderConfig,
} from "@guizhi/core";
import {
  FUNASR_MODEL_ID,
  FUNASR_PROVIDER_ID,
  isLocalEngineProvider,
  isManagedFunasrUrl,
} from "@guizhi/shared/constants";

/** 文件里的模型条目：结构同 CoreAIModelConfig，另带渲染进程独有的 chatParams */
export type TransferModelConfig = CoreAIModelConfig & {
  chatParams?: unknown;
};

export interface AiReconcileInput {
  providers: unknown;
  models: unknown;
  routes: unknown;
}

export interface AiReconcileResult {
  providers: CoreAIProviderConfig[];
  models: TransferModelConfig[];
  routes: Partial<Record<CoreAIModelRoute, string>>;
  warnings: string[];
}

function isPlainRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

function nonEmpty(value: unknown): boolean {
  return typeof value === "string" && value.trim().length > 0;
}

/** 与 normalizeProviderConfig 的抛错条件一致 */
function isUsableProvider(value: unknown): value is CoreAIProviderConfig {
  return (
    isPlainRecord(value) &&
    nonEmpty(value.id) &&
    nonEmpty(value.provider) &&
    nonEmpty(value.apiUrl)
  );
}

/** 与 normalizeModelConfig 的抛错条件一致 */
function isUsableModel(value: unknown): value is TransferModelConfig {
  return (
    isPlainRecord(value) &&
    nonEmpty(value.id) &&
    nonEmpty(value.provider) &&
    nonEmpty(value.apiUrl) &&
    nonEmpty(value.model)
  );
}

function isLocalEngineModel(model: TransferModelConfig): boolean {
  return (
    model.id === FUNASR_MODEL_ID ||
    model.providerId === FUNASR_PROVIDER_ID ||
    isManagedFunasrUrl(model.apiUrl)
  );
}

function readRoutes(value: unknown): Partial<Record<CoreAIModelRoute, string>> {
  if (!isPlainRecord(value)) {
    return {};
  }
  const next: Partial<Record<CoreAIModelRoute, string>> = {};
  for (const [route, modelId] of Object.entries(value)) {
    if (typeof modelId === "string" && modelId.trim()) {
      next[route as CoreAIModelRoute] = modelId.trim();
    }
  }
  return next;
}

export function reconcileImportedAiConfig(
  imported: AiReconcileInput,
  local: CoreAIConfigFile,
): AiReconcileResult {
  const warnings: string[] = [];

  const rawProviders = Array.isArray(imported.providers)
    ? imported.providers
    : [];
  const rawModels = Array.isArray(imported.models) ? imported.models : [];

  const usableProviders = rawProviders.filter(isUsableProvider);
  const usableModels = rawModels.filter(isUsableModel);
  const droppedCount =
    rawProviders.length -
    usableProviders.length +
    (rawModels.length - usableModels.length);
  if (droppedCount > 0) {
    warnings.push(`已跳过 ${droppedCount} 条字段不完整的服务商 / 模型记录`);
  }

  const providers = usableProviders.filter((p) => !isLocalEngineProvider(p));
  const models = usableModels.filter((m) => !isLocalEngineModel(m));
  if (
    providers.length !== usableProviders.length ||
    models.length !== usableModels.length
  ) {
    warnings.push(
      "配置里的本地转写引擎条目已跳过：它指向导出设备上的本地服务，换台机器点了必然失败",
    );
  }

  // 本机自己的内置引擎条目原样保留
  const localProviders = local.providers.filter(isUsableProvider).filter(isLocalEngineProvider);
  const localModels = (local.models as TransferModelConfig[])
    .filter(isUsableModel)
    .filter(isLocalEngineModel);
  if (localModels.length > 0) {
    warnings.push("已保留本机安装的本地转写引擎条目");
  }

  const providerIds = new Set(providers.map((p) => p.id));
  for (const provider of localProviders) {
    if (!providerIds.has(provider.id)) {
      providers.push(provider);
      providerIds.add(provider.id);
    }
  }
  const modelIds = new Set(models.map((m) => m.id));
  for (const model of localModels) {
    if (!modelIds.has(model.id)) {
      models.push(model);
      modelIds.add(model.id);
    }
  }

  // 指向不存在模型的路由要清掉：normalizeRoutes 不做这件事，留着就是一条
  // 界面上看着配好了、用起来找不到模型的死路由
  const importedRoutes = readRoutes(imported.routes);
  const routes: Partial<Record<CoreAIModelRoute, string>> = {};
  let danglingCount = 0;
  for (const [route, modelId] of Object.entries(importedRoutes)) {
    if (modelIds.has(modelId)) {
      routes[route as CoreAIModelRoute] = modelId;
    } else {
      danglingCount += 1;
    }
  }
  if (danglingCount > 0) {
    warnings.push(`已清空 ${danglingCount} 条指向不存在模型的路由`);
  }

  // 导入方没指定语音转写路由时，把本机原来指向内置引擎的那条接回去
  if (!routes.audioText) {
    const localAudioText = local.modelRouteDefaults?.audioText;
    if (localAudioText && localModels.some((m) => m.id === localAudioText)) {
      routes.audioText = localAudioText;
    }
  }

  return { providers, models, routes, warnings };
}
