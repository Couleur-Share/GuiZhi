/**
 * 页面加载后的“沉降”等待：事件驱动，替代固定的 1.2 s + 3×(滚动 + 0.2 s)。
 *
 * 旧做法不管页面是否早已静止都睡满约 1.84 s，占一次渲染采集耗时的三成；
 * 而且“页面在这段时间里处于什么状态”取决于加载快慢，正文随之波动。
 * 这里观察三个信号，全部静止后才提前结束：
 *   1. 网络：没有未完成的子请求，且最近 NETWORK_QUIET_MS 内没有请求开始或结束；
 *      样式表、图片以及文档类请求（链接预取、子框架）不改动主文档，不计入，见 web-network-activity.ts；
 *   2. DOM：最近 DOM_QUIET_MS 内没有节点增删或文字变化（MutationObserver）；
 *   3. 正文：可见文字长度最近 TEXT_STABLE_MS 内不变，且不少于 MIN_TEXT_CHARS（空壳页保持完整等待）。
 *      它补充 MutationObserver 看不到的情形：仅靠类名/样式显隐正文，DOM 节点并未增删。
 *
 * 每一段的上限都等于旧做法，所以最坏情况与旧行为相同，不会更慢。
 *
 * 已知限制：既不发请求、也不改 DOM 的纯定时器延迟渲染（例如 setTimeout 700 ms 才写入正文）
 * 无法被事件观察到，可能比固定等待更早被截取。open shadow root 内部的变化同样不可见，
 * 由“网络静止”兜底（组件渲染几乎总伴随请求）。遇到问题可设 GUIZHI_WEB_SETTLE=fixed 回退。
 */

import type { WebActivityBlocker } from "./web-network-activity";

export const WEB_SETTLE_ENV = "GUIZHI_WEB_SETTLE";

/** 首段等待上限，等于旧版固定等待。 */
export const INITIAL_CAP_MS = 1200;
/** 每次滚动后的等待上限，等于旧版固定等待。 */
export const STEP_CAP_MS = 200;
export const SCROLL_STEPS = 3;
/** 旧做法的总等待；新做法的各段上限之和与它相同。 */
export const LEGACY_SETTLE_MS = INITIAL_CAP_MS + SCROLL_STEPS * STEP_CAP_MS;

const POLL_MS = 100;
const NETWORK_QUIET_MS = 300;
const DOM_QUIET_MS = 300;
const TEXT_STABLE_MS = 300;
const MIN_TEXT_CHARS = 200;
/** 滚动后至少等这么久：让 IntersectionObserver 等回调有机会发起请求或修改 DOM。 */
const STEP_FLOOR_MS = 100;
const STEP_POLL_MS = 50;
const STEP_NETWORK_QUIET_MS = 50;
// 兜底轮数：时钟停滞（或被测试冻结）时也保证循环终止。
const MAX_INITIAL_POLLS = Math.ceil(INITIAL_CAP_MS / POLL_MS) + 2;
const MAX_STEP_POLLS = Math.ceil(STEP_CAP_MS / STEP_POLL_MS) + 2;

export type WebSettleReason =
  /** 网络、DOM、正文都静止，提前结束。 */
  | "quiet"
  /** 到上限仍有活动（持续轮询、动画等），等满首段。 */
  | "cap"
  /** 到上限且可见文字极少（疑似加载中的空壳页），等满首段。 */
  | "shell"
  /** 被 GUIZHI_WEB_SETTLE=fixed 关闭，使用固定等待。 */
  | "fixed"
  /** 无法观察页面（脚本注入或读取失败），退回固定等待。 */
  | "blind";

/** 首段的观察统计：解释“为什么没有提前结束”，用于调参与追踪，不影响判定。 */
export interface WebSettleStats {
  /** 读到页面状态的轮询次数。 */
  polls: number;
  /** 各信号处于“未静止”的轮询次数（同一轮可计入多项）：
   *  pending 有未完成的子请求；network 刚有请求开始/结束（未满静止窗口）；
   *  dom 刚有 DOM 变化；text 可见文字刚变过；short 可见文字不足 MIN_TEXT_CHARS。 */
  blocked: {
    pending: number;
    network: number;
    dom: number;
    text: number;
    short: number;
  };
  /** 一次读取的最长往返（毫秒，含在页面主线程排队的时间）：判断首段为什么超出上限。 */
  maxPollMs: number;
  /** 页面侧单次读取自身的最长计算耗时（毫秒，主要是 innerText 触发的布局）：
   *  与 maxPollMs 对照，可区分“页面主线程忙”和“轮询本身太重”。 */
  maxCostMs: number;
  /** 最后一次轮询看到的原始值，用来判断离“静止”还差多少；
   *  inflight 为在途请求按类别的计数，blockers 为正在阻止网络静止的请求（解释为什么等满上限）。 */
  last?: {
    networkQuietMs: number;
    domQuietMs: number;
    textLength: number;
    inflight?: Record<string, number>;
    blockers?: WebActivityBlocker[];
  };
}

export interface WebSettleResult extends Partial<WebSettleStats> {
  reason: WebSettleReason;
  /** 首段（旧版固定 1.2 s）实际用时，毫秒。 */
  initialMs: number;
}

export interface WebPageSample {
  /** 距最近一次 DOM 变化的毫秒数。 */
  domQuietMs: number;
  /** 可见文字（innerText）长度。 */
  textLength: number;
  /** 页面侧读取可见文字的计算耗时（毫秒），仅用于诊断。 */
  costMs?: number;
}

export interface WebSettleEnv {
  now(): number;
  pause(ms: number): Promise<void>;
  scroll(): Promise<void>;
  /** 有会改动 DOM 的请求在途时为 0，否则为距最近一次此类请求开始/结束的毫秒数。 */
  networkQuietMs(): number;
  /** 在途请求按类别的计数，仅用于诊断统计。 */
  inflight?(): Record<string, number>;
  /** 正在阻止网络静止的请求，仅用于诊断统计。 */
  blockers?(): WebActivityBlocker[];
  /** 在页面里安装 DOM 观察；返回是否可用。 */
  install(): Promise<boolean>;
  /** 读取页面状态；null 表示观察已失效（文档被替换），需要重新安装。 */
  sample(): Promise<WebPageSample | null>;
}

export function webSettleMode(): "quiet" | "fixed" {
  return process.env[WEB_SETTLE_ENV] === "fixed" ? "fixed" : "quiet";
}

/** 取消/超时必须继续向上传播；只有页面侧失败才降级为默认值。 */
async function tolerate<T>(
  signal: AbortSignal,
  work: () => Promise<T>,
  fallback: T,
): Promise<T> {
  try {
    return await work();
  } catch (error) {
    if (signal.aborted) throw error;
    return fallback;
  }
}

/**
 * 首段上限的剩余毫秒数。起点是整个沉降的开始（安装探针之前）：页面刚加载完常在做耗时的水合，
 * 安装探针要排在它后面，这段排队也必须算进上限，否则首段会比旧版的固定等待更长。
 */
const initialRest = (env: WebSettleEnv, startedAt: number) =>
  Math.max(0, INITIAL_CAP_MS - (env.now() - startedAt));

/** 首段：等到三个信号都静止，或到达上限。统计写入 stats。 */
async function waitInitial(
  env: WebSettleEnv,
  signal: AbortSignal,
  stats: WebSettleStats,
  startedAt: number,
): Promise<{ reason: WebSettleReason; observable: boolean }> {
  const rest = () => initialRest(env, startedAt);
  const blind = async () => {
    await env.pause(rest());
    return { reason: "blind" as const, observable: false };
  };
  let previousText = -1;
  let textChangedAt = startedAt;
  let text = 0;
  for (let polls = 0; polls < MAX_INITIAL_POLLS; polls++) {
    const polledAt = env.now();
    const sample = await tolerate<WebPageSample | null | undefined>(
      signal,
      () => env.sample(),
      undefined,
    );
    stats.maxPollMs = Math.max(
      stats.maxPollMs,
      Math.round(env.now() - polledAt),
    );
    if (sample === undefined) return blind();
    if (sample === null) {
      // 文档被替换（页面脚本跳转）后观察器随之消失：重新安装，文字长度重新计时。
      previousText = -1;
      if (!(await tolerate(signal, () => env.install(), false))) return blind();
    } else {
      text = sample.textLength;
      stats.maxCostMs = Math.max(
        stats.maxCostMs,
        Math.round(sample.costMs ?? 0),
      );
      if (text !== previousText) {
        previousText = text;
        textChangedAt = env.now();
      }
      // 可见文字与 DOM 一样，需要连续 TEXT_STABLE_MS 不变，才不会被间隔较长的变化漏掉。
      const networkQuiet = env.networkQuietMs();
      const textQuiet = env.now() - textChangedAt;
      stats.polls++;
      if (networkQuiet === 0) stats.blocked.pending++;
      else if (networkQuiet < NETWORK_QUIET_MS) stats.blocked.network++;
      if (sample.domQuietMs < DOM_QUIET_MS) stats.blocked.dom++;
      if (textQuiet < TEXT_STABLE_MS) stats.blocked.text++;
      if (text < MIN_TEXT_CHARS) stats.blocked.short++;
      const blockers = env.blockers?.() ?? [];
      stats.last = {
        networkQuietMs: Math.round(networkQuiet),
        domQuietMs: Math.round(sample.domQuietMs),
        textLength: text,
        ...(env.inflight ? { inflight: env.inflight() } : {}),
        ...(blockers.length ? { blockers } : {}),
      };
      if (
        text >= MIN_TEXT_CHARS &&
        textQuiet >= TEXT_STABLE_MS &&
        sample.domQuietMs >= DOM_QUIET_MS &&
        networkQuiet >= NETWORK_QUIET_MS
      )
        return { reason: "quiet", observable: true };
    }
    const left = rest();
    if (left <= 0) break;
    await env.pause(Math.min(POLL_MS, left));
  }
  return { reason: text < MIN_TEXT_CHARS ? "shell" : "cap", observable: true };
}

/** 一次滚动之后：等到没有请求、DOM 也不再变化，最多 STEP_CAP_MS。返回是否仍可观察。 */
async function waitStep(
  env: WebSettleEnv,
  signal: AbortSignal,
): Promise<boolean> {
  const startedAt = env.now();
  const rest = () => Math.max(0, STEP_CAP_MS - (env.now() - startedAt));
  await env.pause(STEP_FLOOR_MS);
  for (let polls = 0; polls < MAX_STEP_POLLS; polls++) {
    if (rest() <= 0) return true;
    if (env.networkQuietMs() >= STEP_NETWORK_QUIET_MS) {
      const sample = await tolerate<WebPageSample | null | undefined>(
        signal,
        () => env.sample(),
        undefined,
      );
      if (sample === undefined) {
        await env.pause(rest());
        return false;
      }
      if (sample && sample.domQuietMs >= STEP_FLOOR_MS) return true;
    }
    await env.pause(Math.min(STEP_POLL_MS, rest()));
  }
  return true;
}

export async function settleRenderedPage(
  env: WebSettleEnv,
  signal: AbortSignal,
  mode: "quiet" | "fixed" = webSettleMode(),
): Promise<WebSettleResult> {
  const startedAt = env.now();
  const stats: WebSettleStats = {
    polls: 0,
    blocked: { pending: 0, network: 0, dom: 0, text: 0, short: 0 },
    maxPollMs: 0,
    maxCostMs: 0,
  };
  let reason: WebSettleReason = "fixed";
  let observable = false;
  if (mode === "quiet") {
    observable = await tolerate(signal, () => env.install(), false);
    reason = "blind";
  }
  if (observable)
    ({ reason, observable } = await waitInitial(env, signal, stats, startedAt));
  else await env.pause(initialRest(env, startedAt));
  const initialMs = Math.round(env.now() - startedAt);
  for (let step = 0; step < SCROLL_STEPS; step++) {
    await env.scroll();
    if (observable) observable = await waitStep(env, signal);
    else await env.pause(STEP_CAP_MS);
  }
  return { reason, initialMs, ...(stats.polls ? stats : {}) };
}

// ---- 页面侧脚本（在网页主世界运行，必须自包含，供 executeJavaScript 序列化） ----

/** 安装 DOM 变化观察，状态挂在 Symbol.for 键下，不污染页面的常规属性枚举。 */
export function installSettleProbe(): boolean {
  const key = Symbol.for("guizhi.settle");
  const host = window as unknown as Record<symbol, unknown>;
  if (host[key]) return true;
  try {
    const state = { last: performance.now() };
    // 不观察 attributes：样式/类名动画噪声很大，且与正文无关；
    // 由“可见文字长度”覆盖靠类名显隐正文的情形。
    new MutationObserver(() => {
      state.last = performance.now();
    }).observe(document, {
      childList: true,
      subtree: true,
      characterData: true,
    });
    Object.defineProperty(window, key, {
      value: state,
      enumerable: false,
      configurable: true,
    });
    return true;
  } catch {
    return false;
  }
}

export function readSettleProbe(): WebPageSample | null {
  const state = (
    window as unknown as Record<symbol, { last: number } | undefined>
  )[Symbol.for("guizhi.settle")];
  if (!state) return null;
  const domQuietMs = performance.now() - state.last;
  // innerText 会强制布局：单独计时，用来判断轮询本身对页面的开销。
  const startedAt = performance.now();
  const textLength = document.body ? document.body.innerText.length : 0;
  return { domQuietMs, textLength, costMs: performance.now() - startedAt };
}

const SCROLL_SCRIPT = "window.scrollBy(0, Math.min(window.innerHeight, 1000))";

function isPageSample(value: unknown): value is WebPageSample {
  const sample = value as Partial<WebPageSample> | null;
  return (
    typeof sample?.domQuietMs === "number" &&
    Number.isFinite(sample.domQuietMs) &&
    typeof sample.textLength === "number" &&
    Number.isFinite(sample.textLength) &&
    (sample.costMs === undefined ||
      (typeof sample.costMs === "number" && Number.isFinite(sample.costMs)))
  );
}

/** 把“在页面执行脚本”的能力装配成 WebSettleEnv；脚本返回值在此校验，格式不符按读取失败处理。 */
export function pageSettleEnv(host: {
  evaluate(script: string): Promise<unknown>;
  pause(ms: number): Promise<void>;
  networkQuietMs(): number;
  inflight?(): Record<string, number>;
  blockers?(): WebActivityBlocker[];
  now?: () => number;
}): WebSettleEnv {
  return {
    now: host.now ?? (() => performance.now()),
    pause: host.pause,
    networkQuietMs: host.networkQuietMs,
    inflight: host.inflight,
    blockers: host.blockers,
    scroll: async () => {
      await host.evaluate(SCROLL_SCRIPT);
    },
    install: async () =>
      (await host.evaluate(`(${installSettleProbe.toString()})()`)) === true,
    sample: async () => {
      const value = await host.evaluate(`(${readSettleProbe.toString()})()`);
      if (value === null) return null;
      if (!isPageSample(value)) throw new Error("页面探针返回了无法识别的数据");
      return value;
    },
  };
}
