/**
 * 抖音采集：不经 yt-dlp。
 *
 * yt-dlp 的 Douyin 提取器打的是 `douyin.com/aweme/v1/web/aweme/detail/`，
 * 该接口对没有签名 cookie（`__ac_signature` / `ttwid` 等，由页面 JS 挑战
 * 生成）的请求一律返回空 body，于是报「Fresh cookies are needed」。
 *
 * 而 `iesdouyin.com` 的分享页在移动端 UA 下是服务端渲染的：作品信息全在
 * `window._ROUTER_DATA` 里，播放地址把 `playwm` 换成 `play` 就是无水印源，
 * 全程不需要任何 cookie。这里走的就是这条路。
 *
 * 代价是依赖未公开的页面结构（历史上叫过 `RENDER_DATA` 且是 URL 编码的），
 * 抖音改版就要跟着修——解析失败会降级成可读原因，不会静默产出空条目。
 */
import path from "path";
import { PlatformParseError } from "@guizhi/shared/utils/platform-parse-error";
import type { ImageNoteSource } from "./image-note-entry";
import { IMAGE_EXTENSIONS, MEDIA_SIZE_LIMITS } from "./media-files";
import { logPlatformStructureMissing } from "./platform-parse-log";
import { downloadToTempFile, fetchHtml } from "./safe-fetch";

/** 分享页只在移动端 UA 下服务端渲染，桌面 UA 会被 302 到 douyin.com */
const DOUYIN_MOBILE_UA =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1";
const ROUTER_DATA_MARKER = "window._ROUTER_DATA";
const AWEME_PATH_RE = /\/(?:video|note|slides)\/(\d{6,})/;
const AWEME_ID_RE = /^\d{6,}$/;
/** 与 yt-dlp 链路的 --max-filesize 300m 对齐 */
const MEDIA_MAX_BYTES = 300 * 1024 * 1024;
const TITLE_MAX_LENGTH = 120;
export const DOUYIN_LABEL = "抖音";

export interface DouyinAweme {
  awemeId: string;
  /**
   * 图文作品同样带 play_addr（抖音会拿图片合成一个幻灯片视频），
   * 所以只能按 images 判定，不能按「有没有播放地址」判。
   */
  kind: "video" | "note";
  /** 抖音没有独立标题字段，标题取文案首行 */
  title: string;
  /** 完整文案；与标题相同（单行且未截断）时为空，避免正文里重复一遍 */
  description: string;
  author: string;
  durationSeconds: number | null;
  /** 无水印播放地址；图文作品为 null（那个幻灯片视频不值得转写） */
  playUrl: string | null;
  /** 图文作品逐张图片的 CDN 镜像地址（视频作品为空数组） */
  imageMirrors: string[][];
  /** 规范化来源链接，参与去重：短链 / 分享链 / 站内链都收敛到同一个 */
  webpageUrl: string;
}

export interface DouyinFetchDeps {
  /** 测试注入：抓取分享页 */
  fetchPage?: (
    url: string,
    signal?: AbortSignal,
  ) => Promise<{ html: string; finalUrl: string }>;
}

/** 从各种抖音链接形态里取作品 ID；短链等取不到的返回 null */
export function extractAwemeId(url: string): string | null {
  let parsed: URL;
  try {
    parsed = new URL(url.trim());
  } catch {
    return null;
  }
  const fromPath = AWEME_PATH_RE.exec(parsed.pathname)?.[1];
  if (fromPath) {
    return fromPath;
  }
  // douyin.com/discover?modal_id=xxx、用户主页上点开的作品
  const modalId = parsed.searchParams.get("modal_id");
  return modalId && AWEME_ID_RE.test(modalId) ? modalId : null;
}

/**
 * 分享页地址。`/share/video/` 与 `/share/note/` 互通（视频和图文都能取到），
 * `/share/slides/` 两种内容都不返回 `_ROUTER_DATA`，所以一律走 video。
 */
export function douyinShareUrl(awemeId: string): string {
  return `https://www.iesdouyin.com/share/video/${awemeId}/`;
}

interface RawUrlList {
  url_list?: unknown;
}

interface RawAweme {
  aweme_id?: unknown;
  desc?: unknown;
  author?: { nickname?: unknown } | null;
  video?: {
    duration?: unknown;
    play_addr?: RawUrlList | null;
  } | null;
  images?: RawUrlList[] | null;
}

interface RawVideoInfoRes {
  item_list?: RawAweme[];
  filter_list?: { detail_msg?: unknown; notice?: unknown }[];
}

/** 同一份资源抖音会给多个 CDN 地址，全部保留用于下载时逐个降级 */
function urlList(source: RawUrlList | null | undefined): string[] {
  const list = source?.url_list;
  if (!Array.isArray(list)) {
    return [];
  }
  return list.filter(
    (entry): entry is string => typeof entry === "string" && entry.length > 0,
  );
}

function firstUrl(source: RawUrlList | null | undefined): string | null {
  return urlList(source)[0] ?? null;
}

function readString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

/** 播放地址去水印：`/aweme/v1/playwm/` 换成 `/aweme/v1/play/` */
function stripWatermark(playUrl: string): string {
  return playUrl.replace("/playwm/", "/play/");
}

/** 标题取文案首行：图文的文案常是整篇长文，直接截前 N 字会切出带换行的标题 */
function buildTitle(desc: string, awemeId: string): string {
  const firstLine = desc.split(/\r?\n/).find((line) => line.trim())?.trim();
  if (!firstLine) {
    return `抖音作品 ${awemeId}`;
  }
  return firstLine.length > TITLE_MAX_LENGTH
    ? `${firstLine.slice(0, TITLE_MAX_LENGTH)}…`
    : firstLine;
}

/**
 * 从页面脚本里切出 `window._ROUTER_DATA = {…}` 的那个对象。
 * 按花括号配平扫描（跳过字符串字面量），不受赋值语句后面还有多少代码影响。
 */
function sliceRouterDataJson(html: string): string | null {
  const marker = html.indexOf(ROUTER_DATA_MARKER);
  if (marker < 0) {
    return null;
  }
  const start = html.indexOf("{", marker + ROUTER_DATA_MARKER.length);
  if (start < 0) {
    return null;
  }

  let depth = 0;
  let inString = false;
  let escaped = false;
  for (let index = start; index < html.length; index++) {
    const char = html[index];
    if (inString) {
      if (escaped) {
        escaped = false;
      } else if (char === "\\") {
        escaped = true;
      } else if (char === '"') {
        inString = false;
      }
      continue;
    }
    if (char === '"') {
      inString = true;
    } else if (char === "{") {
      depth++;
    } else if (char === "}" && --depth === 0) {
      return html.slice(start, index + 1);
    }
  }
  return null;
}

function throwDouyinStructureMissing(html: string, message: string): never {
  logPlatformStructureMissing({
    platform: DOUYIN_LABEL,
    marker: ROUTER_DATA_MARKER,
    html,
    action: "解析抖音分享页",
  });
  throw new PlatformParseError("structure_missing", message);
}

/** 解析分享页里的 `window._ROUTER_DATA`；作品不可用时抛出平台给的原因 */
export function parseDouyinRouterData(
  html: string,
  awemeId: string,
): DouyinAweme {
  const json = sliceRouterDataJson(html);
  if (!json) {
    throwDouyinStructureMissing(
      html,
      "分享页未返回作品数据（抖音可能已改版，或该链接需要在 App 内打开）",
    );
  }

  let payload: { loaderData?: Record<string, unknown> };
  try {
    payload = JSON.parse(json) as { loaderData?: Record<string, unknown> };
  } catch {
    throwDouyinStructureMissing(
      html,
      "分享页数据解析失败（页面结构可能已变化）",
    );
  }

  // 承载数据的 key 形如 `video_(id)/page`，按内容找比按名字找更耐改版
  const info = Object.values(payload.loaderData ?? {})
    .map((page) => (page as { videoInfoRes?: RawVideoInfoRes })?.videoInfoRes)
    .find((candidate): candidate is RawVideoInfoRes => Boolean(candidate));

  const item = info?.item_list?.[0];
  if (!item) {
    const filtered = info?.filter_list?.[0];
    const reason =
      readString(filtered?.detail_msg) || readString(filtered?.notice);
    throw new PlatformParseError(
      "note_unavailable",
      reason || "作品不存在或已被删除",
    );
  }

  const desc = readString(item.desc);
  const resolvedId = readString(item.aweme_id) || awemeId;
  const title = buildTitle(desc, resolvedId);
  const durationMs = item.video?.duration;
  const playUrl = firstUrl(item.video?.play_addr);
  const imageMirrors = (item.images ?? [])
    .map((image) => urlList(image))
    .filter((mirrors) => mirrors.length > 0);
  const kind = imageMirrors.length > 0 ? "note" : "video";

  return {
    awemeId: resolvedId,
    kind,
    title,
    description: desc === title ? "" : desc,
    author: readString(item.author?.nickname),
    durationSeconds:
      typeof durationMs === "number" && Number.isFinite(durationMs)
        ? Math.round(durationMs / 1000)
        : null,
    playUrl: kind === "note" || !playUrl ? null : stripWatermark(playUrl),
    imageMirrors,
    // 规范链接按解析到的实际内容定，比按原链接的路由猜更准
    webpageUrl: `https://www.douyin.com/${kind}/${resolvedId}`,
  };
}

/**
 * 抓取并解析抖音作品。链接里带作品 ID 时一次请求即可；
 * 短链（v.douyin.com/xxx）先跟随重定向拿到 ID，再取分享页。
 */
export async function fetchDouyinAweme(
  url: string,
  signal?: AbortSignal,
  deps: DouyinFetchDeps = {},
): Promise<DouyinAweme> {
  const fetchPage =
    deps.fetchPage ??
    ((target: string, targetSignal?: AbortSignal) =>
      fetchHtml(target, targetSignal, { userAgent: DOUYIN_MOBILE_UA }));

  let awemeId = extractAwemeId(url);
  if (!awemeId) {
    const landed = await fetchPage(url, signal);
    awemeId = extractAwemeId(landed.finalUrl);
    if (!awemeId) {
      throw new PlatformParseError(
        "note_unavailable",
        "无法从该链接解析出抖音作品 ID",
      );
    }
  }

  const page = await fetchPage(douyinShareUrl(awemeId), signal);
  return parseDouyinRouterData(page.html, awemeId);
}

/** 下载无水印视频到临时目录，供转码转写使用 */
export async function downloadDouyinMedia(
  playUrl: string,
  signal?: AbortSignal,
): Promise<{ dir: string; filePath: string }> {
  return downloadToTempFile(playUrl, {
    signal,
    userAgent: DOUYIN_MOBILE_UA,
    referer: "https://www.douyin.com/",
    maxBytes: MEDIA_MAX_BYTES,
    fileName: "douyin.mp4",
  });
}

/** 从图片地址取扩展名；资产入库要靠它决定文件后缀 */
export function imageExtensionFromUrl(url: string): string {
  let extension: string;
  try {
    extension = path.extname(new URL(url).pathname).toLowerCase();
  } catch {
    extension = "";
  }
  return (IMAGE_EXTENSIONS as readonly string[]).includes(extension)
    ? extension
    : ".webp";
}

/** 下载一张配图；同一张图抖音给了多个 CDN 镜像，逐个降级 */
export async function downloadDouyinImage(
  mirrors: string[],
  signal?: AbortSignal,
): Promise<{ dir: string; filePath: string }> {
  const failures: string[] = [];
  for (const url of mirrors) {
    try {
      return await downloadToTempFile(url, {
        signal,
        userAgent: DOUYIN_MOBILE_UA,
        referer: "https://www.douyin.com/",
        maxBytes: MEDIA_SIZE_LIMITS.image,
        fileName: `image${imageExtensionFromUrl(url)}`,
      });
    } catch (error) {
      if (error instanceof Error && error.message === "已取消") {
        throw error;
      }
      failures.push(error instanceof Error ? error.message : String(error));
    }
  }
  throw new Error(failures.join("；") || "没有可用的图片地址");
}

/** 图文作品 → 通用图文条目素材 */
export function douyinImageNoteSource(aweme: DouyinAweme): ImageNoteSource {
  return {
    platformLabel: DOUYIN_LABEL,
    title: aweme.title,
    // 抖音没有标题字段，这个「标题」只是文案首行，配了文本模型就重拟
    authoredTitle: false,
    description: aweme.description,
    author: aweme.author,
    imageMirrors: aweme.imageMirrors,
    webpageUrl: aweme.webpageUrl,
    downloadImage: downloadDouyinImage,
  };
}
