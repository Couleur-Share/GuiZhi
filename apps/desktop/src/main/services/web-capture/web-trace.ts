import { createHash } from "node:crypto";
import { appendFile } from "node:fs/promises";
import path from "node:path";
import { logAppError } from "../../diagnostic-log";
import type { WebSettleResult } from "./web-settle";

/**
 * 网页采集诊断追踪（默认关闭）。
 * Web capture diagnostic trace (off by default).
 *
 * 设置环境变量 `GUIZHI_WEB_TRACE_FILE=<绝对路径>` 后，每次 captureWebPage 结束时向该文件追加一行 JSON：
 * 各阶段耗时、网络出口统计（请求数、槽位排队、新建/复用连接）与静态路由判定，
 * 用来回答「一次采集的时间花在哪里」，并为连接复用、路由放宽等优化提供前后对比。
 *
 * 隐私：只记录 origin 与网址哈希（SHA-256 前 12 位），不含路径、查询串、凭据、正文与代理地址；
 * 子请求仅在失败明细里记录其 origin，用来判断是目标站点还是第三方域名出的问题。
 * 未设置环境变量时 createWebTrace 返回共享的空实现，采集路径不分配对象、不写盘。
 * 写入失败不影响采集；同一文件的同一错误只向 error.log 记一次，避免刷屏。
 */
export const WEB_TRACE_ENV = "GUIZHI_WEB_TRACE_FILE";

/** 一组耗时的汇总（毫秒，取整）。p50/p95 采用最近秩法。 */
export interface WebTraceDist {
  sum: number;
  p50: number;
  p95: number;
  max: number;
}

export interface WebTraceRoute {
  decision: string;
  reasons: string[];
  textChars: number;
  semanticRegions: number;
  hasScripts: boolean;
}

/**
 * 由请求头 Accept 粗分资源类型。
 * 自定义协议处理器收不到 Sec-Fetch-Dest，也拿不到 webRequest 的 resourceType；
 * Chromium 按目标类型设置的 Accept 可以可靠区分样式表、图片与“浏览器发起的文档类请求”
 * （主框架/子框架导航，以及 <link rel=prefetch>——Docusaurus、Next 等借它预取脚本分块），其余归 other。
 * 文档类只认 Chromium 导航请求的固定前缀 text/html,application/xhtml+xml（逗号后无空格）；
 * jQuery 等库的 XHR 常以 "text/html, " 开头，那是取 HTML 片段的数据请求，必须归 other。
 */
export function classifyAccept(accept: string | null | undefined): string {
  const value = (accept ?? "").trimStart().toLowerCase();
  if (value.startsWith("text/html,application/xhtml+xml")) return "document";
  if (value.startsWith("text/css")) return "style";
  if (value.startsWith("image/")) return "image";
  return "other";
}

/** 经网络出口的一次请求；耗时均为毫秒。 */
export interface WebTraceEgressSample {
  /** 资源类型粗分（见 classifyAccept）；静态抓取为 document。 */
  type: string;
  method: string;
  /** 请求目标的 origin，仅用于失败明细。 */
  origin: string;
  /** 失败前是否已收到响应头。 */
  responded: boolean;
  /** 进入出口后到发出请求前：安全校验、代理解析、地址解析。 */
  prepMs: number;
  /** 等待网络槽位的排队时间。 */
  gateWaitMs: number;
  /** 从发出请求到拿到 socket（代理场景含 CONNECT 往返）；未观测到则为 0。 */
  socketMs: number;
  /** 从发出请求到响应读完（不含排队），重试时包含失败的那一次。 */
  requestMs: number;
  /** 最终这次请求是否复用了已有连接。 */
  reused: boolean;
  /** 是否因复用连接被重置而重试过。 */
  retried: boolean;
  failed: boolean;
  /** 失败时的错误码或名称；不含错误消息，避免带出网址等信息。 */
  error?: string;
  bytes: number;
}

export interface WebTraceFields {
  engine: string;
  proxy: string;
  outcome: string;
  /** 静态被接受但抽取为空，回退到渲染。 */
  fallback: "empty";
  route: WebTraceRoute;
  /** 快照时被展开的 open shadow root 宿主数；没有展开则不记录。 */
  shadowRoots: number;
  /** 页面沉降等待的结果：结束原因与首段用时，用于评估事件驱动等待的收益与风险。 */
  settle: WebSettleResult;
  /** 渲染中被取消的跨源子框架数（见 web-subframe-policy）；没有则不记录。 */
  blockedFrames: number;
  /** 主框架 DOMContentLoaded 距窗口创建的毫秒数；对照 ms.load 可看出子资源把 load 拖后了多少。 */
  domReadyMs: number;
}

export interface WebTrace {
  /** 是否真正记录；调用方可据此跳过为追踪而额外做的计算。 */
  readonly enabled: boolean;
  /** 一个阶段结束：把距上一个标记的耗时累加到 ms[name]（同名多次累加，如回退渲染）。 */
  mark(name: string): void;
  set(fields: Partial<WebTraceFields>): void;
  egress(sample: WebTraceEgressSample): void;
  /** 写入一行并结束；幂等，永不抛出。 */
  flush(): Promise<void>;
}

export const NOOP_WEB_TRACE: WebTrace = Object.freeze({
  enabled: false,
  mark: () => undefined,
  set: () => undefined,
  egress: () => undefined,
  flush: () => Promise.resolve(),
});

export interface WebTraceOptions {
  /** 覆盖环境变量（测试用）。 */
  file?: string;
  /** 单调时钟（测试用），默认 performance.now。 */
  now?: () => number;
}

const reported = new Set<string>();
function reportOnce(key: string, message: string): void {
  if (reported.has(key)) return;
  reported.add(key);
  logAppError({ scope: "main", action: "网页采集追踪", message });
}

export function createWebTrace(
  url: string,
  purpose: string,
  options: WebTraceOptions = {},
): WebTrace {
  const file = options.file ?? process.env[WEB_TRACE_ENV];
  if (!file) return NOOP_WEB_TRACE;
  if (!path.isAbsolute(file)) {
    reportOnce(
      `relative|${file}`,
      `${WEB_TRACE_ENV} 必须是绝对路径，已忽略：${file}`,
    );
    return NOOP_WEB_TRACE;
  }
  return new RecordingWebTrace(
    file,
    url,
    purpose,
    options.now ?? (() => performance.now()),
  );
}

function dist(values: number[]): WebTraceDist {
  if (!values.length) return { sum: 0, p50: 0, p95: 0, max: 0 };
  const sorted = [...values].sort((a, b) => a - b);
  const rank = (q: number) =>
    sorted[Math.max(0, Math.ceil(q * sorted.length) - 1)];
  return {
    sum: Math.round(sorted.reduce((total, value) => total + value, 0)),
    p50: Math.round(rank(0.5)),
    p95: Math.round(rank(0.95)),
    max: Math.round(sorted[sorted.length - 1]),
  };
}

/** 失败明细最多记录的条数；统计数字（errors）覆盖全部失败。 */
const FAILURE_DETAIL_LIMIT = 8;

function summarizeEgress(samples: WebTraceEgressSample[]) {
  const pick = (
    read: (sample: WebTraceEgressSample) => number,
    keep: (sample: WebTraceEgressSample) => boolean = () => true,
  ) => dist(samples.filter(keep).map(read));
  const byType: Record<string, { n: number; bytes: number; ms: number }> = {};
  const errors: Record<string, number> = {};
  for (const sample of samples) {
    const entry = (byType[sample.type.slice(0, 32)] ??= {
      n: 0,
      bytes: 0,
      ms: 0,
    });
    entry.n++;
    entry.bytes += sample.bytes;
    entry.ms += Math.round(sample.requestMs);
    if (sample.failed) {
      const label = (sample.error ?? "unknown").slice(0, 32);
      errors[label] = (errors[label] ?? 0) + 1;
    }
  }
  return {
    requests: samples.length,
    bytes: samples.reduce((total, sample) => total + sample.bytes, 0),
    failed: samples.filter((sample) => sample.failed).length,
    errors,
    failures: samples
      .filter((sample) => sample.failed)
      .slice(0, FAILURE_DETAIL_LIMIT)
      .map((sample) => ({
        error: (sample.error ?? "unknown").slice(0, 32),
        type: sample.type.slice(0, 32),
        method: sample.method,
        origin: sample.origin,
        reused: sample.reused,
        responded: sample.responded,
        ms: Math.round(sample.requestMs),
      })),
    reusedSocket: samples.filter((sample) => sample.reused).length,
    retried: samples.filter((sample) => sample.retried).length,
    prepMs: pick((sample) => sample.prepMs),
    gateWaitMs: pick((sample) => sample.gateWaitMs),
    socketMs: pick((sample) => sample.socketMs),
    requestMs: pick((sample) => sample.requestMs),
    freshMs: pick(
      (sample) => sample.requestMs,
      (sample) => !sample.reused,
    ),
    reusedMs: pick(
      (sample) => sample.requestMs,
      (sample) => sample.reused,
    ),
    byType,
  };
}

class RecordingWebTrace implements WebTrace {
  readonly enabled = true;
  private readonly startedAt: number;
  private last: number;
  private readonly stages = new Map<string, number>();
  private readonly fields: Partial<WebTraceFields> = {};
  private readonly samples: WebTraceEgressSample[] = [];
  private flushed = false;

  constructor(
    private readonly file: string,
    private readonly url: string,
    private readonly purpose: string,
    private readonly now: () => number,
  ) {
    this.startedAt = this.last = now();
  }

  mark(name: string): void {
    const at = this.now();
    this.stages.set(name, (this.stages.get(name) ?? 0) + (at - this.last));
    this.last = at;
  }

  set(fields: Partial<WebTraceFields>): void {
    Object.assign(this.fields, fields);
  }

  egress(sample: WebTraceEgressSample): void {
    this.samples.push(sample);
  }

  async flush(): Promise<void> {
    if (this.flushed) return;
    this.flushed = true;
    try {
      await appendFile(this.file, JSON.stringify(this.build()) + "\n", "utf8");
    } catch (error) {
      const code = (error as NodeJS.ErrnoException)?.code ?? "UNKNOWN";
      reportOnce(
        `write|${this.file}|${code}`,
        `追踪文件写入失败（${code}）：${this.file}`,
      );
    }
  }

  private build(): Record<string, unknown> {
    const finishedAt = this.now();
    let origin = "invalid";
    try {
      origin = new URL(this.url).origin;
    } catch {
      // 网址无法解析时仍写入一行，origin 标记为 invalid。
    }
    const {
      engine,
      proxy,
      outcome,
      fallback,
      route,
      shadowRoots,
      settle,
      blockedFrames,
      domReadyMs,
    } = this.fields;
    return {
      v: 1,
      at: new Date().toISOString(),
      origin,
      urlHash: createHash("sha256").update(this.url).digest("hex").slice(0, 12),
      purpose: this.purpose,
      engine: engine ?? null,
      proxy: proxy ?? null,
      outcome: outcome ?? "unknown",
      ...(fallback ? { fallback } : {}),
      ...(route ? { route } : {}),
      ...(shadowRoots ? { shadowRoots } : {}),
      ...(settle ? { settle } : {}),
      ...(blockedFrames ? { blockedFrames } : {}),
      ...(domReadyMs !== undefined ? { domReadyMs } : {}),
      ms: {
        total: Math.round(finishedAt - this.startedAt),
        ...Object.fromEntries(
          [...this.stages, ["tail", finishedAt - this.last] as const].map(
            ([name, value]) => [name, Math.round(value)],
          ),
        ),
      },
      ...(this.samples.length ? { egress: summarizeEgress(this.samples) } : {}),
    };
  }
}
