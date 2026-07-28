/**
 * AI 调用的归属上下文：让「这次导入任务花了几次调用、多少 token」这个问题
 * 有个答案，而 `ai_usage_daily` 回答不了它——那张表的主键是
 * `(day, scenario, model)`，写入即聚合累加，任务维度在落库那一刻就被抹平了。
 *
 * 用 AsyncLocalStorage 而不是模块级的「当前任务」变量：导入队列并发是 2，
 * 单个变量必然让两条任务互相串账。也不走「把 taskId 穿进每层签名」——
 * 从队列到 `recordMainAiUsage` 隔着 extract → connectors → video-url →
 * transcript-format 四五层，还要乘上视频 / 图文 / 论坛三条子链，
 * 而这些层没有一层关心记账，穿过去的全是过路噪音。
 *
 * `recordMainAiUsage` 本来就是主进程全部 AI 调用的唯一收口，
 * 汇报点因此只有一处。
 */
import { AsyncLocalStorage } from "async_hooks";

export interface AiCallRecord {
  model: string;
  promptTokens?: number;
  completionTokens?: number;
  /** 调用失败（仍然计入次数：超时与限流同样可能产生费用） */
  failed?: boolean;
}

type AiCallSink = (record: AiCallRecord) => void;

const storage = new AsyncLocalStorage<AiCallSink>();

/**
 * 在 `fn` 的整个异步执行期内，把主进程发起的 AI 调用汇报给 `sink`。
 * 作用域随 await 链自动传递，嵌套调用以最内层为准。
 */
export function runWithAiCallSink<T>(
  sink: AiCallSink,
  fn: () => Promise<T>,
): Promise<T> {
  return storage.run(sink, fn);
}

/**
 * 汇报一次 AI 调用。不在任何作用域内（详情页手动触发的排版、转写等）时是空操作。
 *
 * sink 抛错不能反过来弄砸调用方：这里记的是观测数据，
 * 为了一条统计把用户一次几十秒的生成搞失败是本末倒置。
 */
export function reportAiCall(record: AiCallRecord): void {
  const sink = storage.getStore();
  if (!sink) {
    return;
  }
  try {
    sink(record);
  } catch (error) {
    console.warn("[usage] 归属 AI 调用到当前任务失败:", error);
  }
}
