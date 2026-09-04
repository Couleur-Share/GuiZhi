import type {
  ResearchCandidateInput,
  ResearchPage,
  ResearchSearchInput,
  ResearchSource,
} from "@guizhi/shared/types";
import { fetchJson } from "../import/safe-fetch";
import type { BrowserCaptureService } from "../platform-capture/browser-capture";

export interface ResearchCollector {
  readonly source: ResearchSource;
  search(input: ResearchSearchInput): Promise<ResearchPage>;
}

function inRange(
  item: ResearchCandidateInput,
  input: Pick<ResearchSearchInput, "rangeFrom" | "rangeTo">,
): boolean {
  return item.publishedAt == null || (
    item.publishedAt >= input.rangeFrom && item.publishedAt <= input.rangeTo
  );
}

export class BrowserResearchCollector implements ResearchCollector {
  constructor(
    readonly source: "xiaohongshu" | "douyin",
    private readonly browser: BrowserCaptureService,
  ) {}

  async search(input: ResearchSearchInput): Promise<ResearchPage> {
    if (input.signal.aborted) throw new DOMException("已取消", "AbortError");
    const page = await this.browser.search(
      {
        platform: this.source,
        keyword: input.topic,
        cursor: input.cursor,
        limit: input.limit,
      },
      input.signal,
    );
    const items = page.items.map((item): ResearchCandidateInput => ({
      source: this.source,
      externalId: item.externalId,
      url: item.url,
      title: item.title,
      author: item.author,
      snippet: item.snippet,
      publishedAt: item.publishedAt,
      dateConfidence: item.dateConfidence ?? (item.publishedAt ? "medium" : "low"),
      mediaType: item.mediaType,
      engagement: item.engagement,
      discoveryMethod: item.discoveryMethod ?? "authenticated-browser",
    })).filter((item) => inRange(item, input));
    return { items, cursor: page.cursor, hasMore: page.hasMore };
  }
}

export interface BilibiliSearchResponse {
  code?: number;
  message?: string;
  data?: {
    page?: number;
    numPages?: number;
    result?: Array<Record<string, unknown>>;
  };
}

interface BilibiliVisitorResponse {
  code?: number;
  message?: string;
  data?: {
    b_3?: string;
    b_4?: string;
  };
}

const BILIBILI_BROWSER_USER_AGENT =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
  "(KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36";

function isHttp412(error: unknown): boolean {
  return error instanceof Error && /\bHTTP 412\b/i.test(error.message);
}

function isBilibiliRiskControl(response: BilibiliSearchResponse): boolean {
  return response.code === -352 || response.code === -412;
}

function cleanText(value: unknown, max = 1000): string {
  return typeof value === "string"
    ? value.replace(/<[^>]+>/g, "").replace(/&(?:amp|nbsp|lt|gt);/g, " ").replace(/\s+/g, " ").trim().slice(0, max)
    : "";
}

function numberValue(value: unknown): number | undefined {
  const parsed = typeof value === "number" ? value : Number(String(value ?? "").replace(/,/g, ""));
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : undefined;
}

function bvidFrom(row: Record<string, unknown>): string {
  const direct = cleanText(row.bvid, 30);
  if (direct) return direct;
  return /\/video\/(BV[\w]+)/i.exec(cleanText(row.arcurl, 500))?.[1] ?? "";
}

export class BilibiliResearchCollector implements ResearchCollector {
  readonly source = "bilibili" as const;
  private visitorCookie: string | undefined;

  constructor(private readonly requestJson: typeof fetchJson = fetchJson) {}

  async search(input: ResearchSearchInput): Promise<ResearchPage> {
    const page = Math.max(1, Number.parseInt(input.cursor ?? "1", 10) || 1);
    const url = new URL("https://api.bilibili.com/x/web-interface/wbi/search/type");
    url.searchParams.set("search_type", "video");
    url.searchParams.set("keyword", input.topic);
    url.searchParams.set("page", String(page));
    url.searchParams.set("page_size", String(Math.min(input.limit, 20)));
    url.searchParams.set("order", "totalrank");
    let response: BilibiliSearchResponse;
    try {
      response = await this.fetchSearch(url, input, this.visitorCookie);
    } catch (error) {
      if (!isHttp412(error)) throw error;
      this.visitorCookie = await this.fetchVisitorCookie(input.signal);
      try {
        response = await this.fetchSearch(url, input, this.visitorCookie);
      } catch (retryError) {
        if (isHttp412(retryError)) {
          throw new Error("B 站搜索触发访问保护，请稍后重试", {
            cause: retryError,
          });
        }
        throw retryError;
      }
    }
    if (isBilibiliRiskControl(response)) {
      this.visitorCookie = await this.fetchVisitorCookie(input.signal);
      response = await this.fetchSearch(url, input, this.visitorCookie);
    }
    if (response.code !== 0) {
      throw new Error(`B 站搜索接口失败：${response.message || response.code || "unknown"}`);
    }
    return parseBilibiliSearchResponse(response, input, page);
  }

  private fetchSearch(
    url: URL,
    input: ResearchSearchInput,
    cookie?: string,
  ): Promise<BilibiliSearchResponse> {
    return this.requestJson<BilibiliSearchResponse>(url.toString(), input.signal, {
      userAgent: BILIBILI_BROWSER_USER_AGENT,
      referer: `https://search.bilibili.com/all?keyword=${encodeURIComponent(input.topic)}`,
      cookie,
    });
  }

  private async fetchVisitorCookie(signal: AbortSignal): Promise<string> {
    const response = await this.requestJson<BilibiliVisitorResponse>(
      "https://api.bilibili.com/x/frontend/finger/spi",
      signal,
      {
        userAgent: BILIBILI_BROWSER_USER_AGENT,
        referer: "https://www.bilibili.com/",
      },
    );
    const buvid3 = cleanText(response.data?.b_3, 200);
    const buvid4 = cleanText(response.data?.b_4, 300);
    if (response.code !== 0 || !buvid3) {
      throw new Error("B 站匿名访客信息获取失败，请稍后重试");
    }
    return [`buvid3=${buvid3}`, buvid4 ? `buvid4=${buvid4}` : ""]
      .filter(Boolean)
      .join("; ");
  }
}

export function parseBilibiliSearchResponse(
  response: BilibiliSearchResponse,
  input: Pick<ResearchSearchInput, "rangeFrom" | "rangeTo">,
  page = 1,
): ResearchPage {
    const rows = response.data?.result ?? [];
    const items = rows.map((row): ResearchCandidateInput | null => {
      const externalId = bvidFrom(row);
      if (!externalId) return null;
      const publishedSeconds = numberValue(row.pubdate ?? row.senddate);
      return {
        source: "bilibili",
        externalId,
        url: `https://www.bilibili.com/video/${externalId}`,
        title: cleanText(row.title, 300) || `B 站视频 ${externalId}`,
        author: cleanText(row.author, 200),
        snippet: cleanText(row.description, 1200),
        publishedAt: publishedSeconds ? publishedSeconds * 1000 : undefined,
        dateConfidence: publishedSeconds ? "high" : "low",
        mediaType: "video",
        engagement: {
          views: numberValue(row.play),
          danmaku: numberValue(row.video_review),
          favorites: numberValue(row.favorites),
          likes: numberValue(row.like),
          comments: numberValue(row.review),
        },
        discoveryMethod: "public-api",
      };
    }).filter((item): item is ResearchCandidateInput => item !== null && inRange(item, input));
    const numPages = Math.max(page, numberValue(response.data?.numPages) ?? page);
    return {
      items,
      cursor: page < numPages ? String(page + 1) : null,
      hasMore: page < numPages,
    };
}
