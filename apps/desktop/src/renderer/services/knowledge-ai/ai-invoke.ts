/**
 * 场景化 AI 调用：从设置解析场景对应的模型配置并发起对话。
 */
import { chatCompletion, type ChatMessage } from "../ai";
import { resolveScenarioAIConfig } from "../ai-defaults";
import { useSettingsStore } from "../../stores/settings.store";
import type { AIUsageScenario } from "../../stores/settings.store";

/**
 * 知识域的模型调用都是「长提示词 + 整段生成」，主进程 30s 的兜底超时
 * （`ai.ipc.ts`）是按连通性探测定的，套到这里会在生成中途被掐断。
 */
const SCENARIO_CHAT_TIMEOUT_MS = 120_000;

/** AI 未配置（区别于调用失败，UI 引导去设置页）。 */
export class AiNotConfiguredError extends Error {
  constructor() {
    super("AI_NOT_CONFIGURED");
    this.name = "AiNotConfiguredError";
  }
}

export function isAiConfiguredForScenario(scenario: AIUsageScenario): boolean {
  return resolveConfig(scenario) !== null;
}

function resolveConfig(scenario: AIUsageScenario) {
  const state = useSettingsStore.getState();
  return resolveScenarioAIConfig({
    aiModels: state.aiModels,
    scenarioModelDefaults: state.scenarioModelDefaults,
    modelRouteDefaults: state.modelRouteDefaults,
    scenario,
    aiProvider: state.aiProvider,
    aiApiProtocol: state.aiApiProtocol,
    aiApiKey: state.aiApiKey,
    aiApiUrl: state.aiApiUrl,
    aiModel: state.aiModel,
  });
}

/**
 * 计量点。
 *
 * 对话走 runScenarioChat，embedding 与 OCR 各自直连 `ai:httpRequest`，
 * 三条路都要经过这里，否则一次全库语义索引的消耗在面板上是完全不可见的 0。
 * 统计失败不能影响业务结果。
 */
export function recordAiUsage(entry: {
  scenario: string;
  model: string;
  promptTokens?: number;
  completionTokens?: number;
  failed?: boolean;
}): void {
  void window.api?.ai
    ?.recordUsage({
      scenario: entry.scenario,
      model: entry.model,
      promptTokens: entry.promptTokens ?? 0,
      completionTokens: entry.completionTokens ?? 0,
      failed: entry.failed,
    })
    .catch((error: unknown) => {
      console.warn("记录 AI 用量失败:", error);
    });
}

export interface ScenarioChatResult {
  content: string;
  model: string;
  /** openai 系 finish_reason；"length" 表示输出撞上 max_tokens 被截断 */
  finishReason?: string;
}

export async function runScenarioChat(
  scenario: AIUsageScenario,
  messages: ChatMessage[],
  options?: {
    temperature?: number;
    maxTokens?: number;
    /** 传下去才能真正中断在途请求，否则「停止」只是标记状态 */
    signal?: AbortSignal;
    /** 素材特别长的场景（如 Wiki 编译）可再放宽 */
    timeoutMs?: number;
    /**
     * 传了就走流式，逐块回调原始输出。
     *
     * 问答是唯一有人盯着屏幕等的场景；摘要、标签、Wiki 编译都是后台批处理，
     * 整段返回反而省掉一路 SSE 解析。
     */
    onDelta?: (chunk: string) => void;
  },
): Promise<ScenarioChatResult> {
  const config = resolveConfig(scenario);
  if (!config) {
    throw new AiNotConfiguredError();
  }

  let result;
  try {
    result = await chatCompletion(config, messages, {
      temperature: options?.temperature,
      maxTokens: options?.maxTokens,
      stream: Boolean(options?.onDelta),
      onStream: options?.onDelta,
      enableThinking: false,
      signal: options?.signal,
      timeoutMs: options?.timeoutMs ?? SCENARIO_CHAT_TIMEOUT_MS,
    });
  } catch (error) {
    // 失败的调用同样要进统计：超时、限流重试、思考 token 烧光后返回空正文
    // 这些都已经产生了费用，只记成功会让面板显著低估实际消耗
    recordAiUsage({ scenario, model: config.model, failed: true });
    throw error;
  }

  // 截断是「HTTP 200 但没写完」，调用方不看就永远发现不了。这里是知识域
  // 唯一的收口点，先记一笔，让静默截断变成日志里数得出来的事件。
  if (result.finishReason === "length") {
    console.warn(
      `[ai] ${scenario} 输出被 max_tokens 截断（上限 ${options?.maxTokens ?? "默认"}），结果可能不完整`,
    );
  }

  recordAiUsage({
    scenario,
    model: config.model,
    promptTokens: result.usage?.promptTokens ?? 0,
    completionTokens: result.usage?.completionTokens ?? 0,
  });

  return {
    content: result.content,
    model: config.model,
    finishReason: result.finishReason,
  };
}
