/**
 * 渲染页子请求的活动跟踪，供沉降等待判断“网络是否已静止”。
 *
 * 快照是渲染后主框架 DOM 的 HTML，所以下列请求即使仍在途中甚至迟迟不结束，也不会改变它，
 * 把它们算进来只会让沉降等待白白等满上限：
 *   - style、image：样式表与图片；
 *   - document：子框架（内容不在主文档快照里）与 <link rel=prefetch>（Docusaurus、Next 等
 *     会一口气预取几十个脚本分块，但预取的资源不会执行）。
 *     主框架自己的导航（脚本跳转）同样是文档类请求，但不需要靠它判断：导航期间对页面的
 *     executeJavaScript 会等到新文档就绪才返回，沉降轮询随之顺延（夹具 jsRedirect 实测，
 *     并作为回归哨兵）。
 * 其余请求（脚本、XHR/fetch、字体、信标……）在协议处理器里无法可靠区分，
 * 一律按“可能改动 DOM”处理——保守，宁可多等。
 *
 * 请求类别取自 Accept（见 classifyAccept）：protocol.handle 接管 http/https 后，
 * webRequest 不再触发，处理器里也收不到 Sec-Fetch-Dest（Electron 33 实测）。
 */
const INERT_KINDS = new Set(["style", "image", "document"]);

export function affectsDocument(kind: string): boolean {
  return !INERT_KINDS.has(kind);
}

/** 一个仍在途、会改动 DOM 的请求；origin 仅在追踪开启时登记。 */
export interface WebActivityBlocker {
  kind: string;
  origin?: string;
  /** 已在途的毫秒数。 */
  ageMs: number;
}

interface ActiveRequest {
  kind: string;
  origin?: string;
  startedAt: number;
}

export class WebNetworkActivity {
  private readonly inflightByKind = new Map<string, number>();
  /** 在途且会改动 DOM 的请求，按开始先后排列。 */
  private readonly active = new Set<ActiveRequest>();
  private lastAt: number;

  constructor(private readonly now: () => number = () => performance.now()) {
    this.lastAt = now();
  }

  /** 一个请求开始；返回的函数在它结束（成功或失败）时调用，重复调用无副作用。 */
  begin(kind: string, origin?: string): () => void {
    const counts = this.inflightByKind;
    counts.set(kind, (counts.get(kind) ?? 0) + 1);
    const entry = affectsDocument(kind)
      ? { kind, origin, startedAt: this.now() }
      : undefined;
    if (entry) {
      this.active.add(entry);
      this.lastAt = entry.startedAt;
    }
    let ended = false;
    return () => {
      if (ended) return;
      ended = true;
      const left = (counts.get(kind) ?? 1) - 1;
      if (left > 0) counts.set(kind, left);
      else counts.delete(kind);
      if (entry) {
        this.active.delete(entry);
        this.lastAt = this.now();
      }
    };
  }

  /** 有会改动 DOM 的请求在途时为 0，否则为距最近一次此类请求开始/结束的毫秒数。 */
  quietMs(): number {
    return this.active.size > 0 ? 0 : this.now() - this.lastAt;
  }

  /** 当前在途请求按类别计数（含不影响判定的类别），仅供诊断。 */
  inflight(): Record<string, number> {
    return Object.fromEntries(this.inflightByKind);
  }

  /** 正在阻止“网络静止”的请求（最久的在前，最多 limit 个），仅供诊断。 */
  blockers(limit = 5): WebActivityBlocker[] {
    const now = this.now();
    const result: WebActivityBlocker[] = [];
    for (const { kind, origin, startedAt } of this.active) {
      if (result.length >= limit) break;
      result.push({
        kind,
        ...(origin ? { origin } : {}),
        ageMs: Math.round(now - startedAt),
      });
    }
    return result;
  }
}
