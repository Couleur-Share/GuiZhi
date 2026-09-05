import type { PlatformCapturePlatform } from "@guizhi/shared/types";
import {
  extractAwemeId,
  parseDouyinRouterData,
  type DouyinAweme,
} from "../import/douyin";
import {
  extractXhsNoteId,
  parseXiaohongshuNote,
  type XiaohongshuNote,
} from "../import/xiaohongshu";
import {
  BrowserCaptureService,
  PlatformCaptureError,
} from "./browser-capture";

function findRecord(
  value: unknown,
  predicate: (record: Record<string, unknown>) => boolean,
  depth = 0,
): Record<string, unknown> | null {
  if (depth > 10 || value == null) return null;
  if (Array.isArray(value)) {
    for (const entry of value) {
      const match = findRecord(entry, predicate, depth + 1);
      if (match) return match;
    }
    return null;
  }
  if (typeof value !== "object") return null;
  const record = value as Record<string, unknown>;
  if (predicate(record)) return record;
  for (const entry of Object.values(record)) {
    const match = findRecord(entry, predicate, depth + 1);
    if (match) return match;
  }
  return null;
}

/**
 * 认证模式优先消费页面自身发出的 JSON 响应；只把已拿到的对象包成现有解析器
 * 所需的数据外壳，不构造请求，也不接触任何签名参数。
 */
function douyinNetworkResponseHtml(payloads: unknown[]): string | null {
  for (const payload of payloads) {
    const loader = findRecord(payload, (record) => Boolean(record.loaderData));
    if (loader) return `<script>window._ROUTER_DATA = ${JSON.stringify(loader)}</script>`;
    const info = findRecord(payload, (record) => Array.isArray(record.item_list));
    if (info) {
      return `<script>window._ROUTER_DATA = ${JSON.stringify({ loaderData: { capture: { videoInfoRes: info } } })}</script>`;
    }
    const detail = findRecord(payload, (record) => Boolean(record.aweme_detail));
    if (detail?.aweme_detail) {
      return `<script>window._ROUTER_DATA = ${JSON.stringify({ loaderData: { capture: { videoInfoRes: { item_list: [detail.aweme_detail] } } } })}</script>`;
    }
  }
  return null;
}

function xhsNetworkResponseHtml(payloads: unknown[]): string | null {
  for (const payload of payloads) {
    const state = findRecord(payload, (record) => {
      const note = record.note as Record<string, unknown> | undefined;
      return Boolean(note && typeof note === "object" && note.noteDetailMap);
    });
    if (state) return `<script>window.__INITIAL_STATE__ = ${JSON.stringify(state)}</script>`;
    const map = findRecord(payload, (record) => Boolean(record.noteDetailMap));
    if (map) {
      return `<script>window.__INITIAL_STATE__ = ${JSON.stringify({ note: map })}</script>`;
    }
  }
  return null;
}

function platformChanged(message: string, cause?: unknown): PlatformCaptureError {
  return new PlatformCaptureError("platform_changed", message, { cause });
}

export async function fetchAuthenticatedDouyin(
  service: BrowserCaptureService,
  url: string,
  signal?: AbortSignal,
): Promise<DouyinAweme> {
  const page = await service.capturePage("douyin", url, signal);
  const awemeId = extractAwemeId(page.finalUrl) ?? extractAwemeId(url);
  if (!awemeId) throw platformChanged("无法从登录态页面识别抖音作品 ID");
  try {
    return parseDouyinRouterData(
      douyinNetworkResponseHtml(page.jsonPayloads) ?? page.html,
      awemeId,
    );
  } catch (error) {
    throw platformChanged("抖音登录态页面结构可能已变更", error);
  }
}

export async function fetchAuthenticatedXiaohongshu(
  service: BrowserCaptureService,
  url: string,
  signal?: AbortSignal,
): Promise<XiaohongshuNote> {
  const page = await service.capturePage("xiaohongshu", url, signal);
  const noteId = extractXhsNoteId(page.finalUrl) ?? extractXhsNoteId(url);
  try {
    return parseXiaohongshuNote(
      xhsNetworkResponseHtml(page.jsonPayloads) ?? page.html,
      noteId,
    );
  } catch (error) {
    throw platformChanged("小红书登录态页面结构可能已变更", error);
  }
}

/** 经 Electron 会话请求 Discourse JSON（携带 Cloudflare / 登录 Cookie） */
export async function fetchAuthenticatedLinuxdoJson<T>(
  service: BrowserCaptureService,
  url: string,
  signal?: AbortSignal,
): Promise<T> {
  return service.fetchJsonViaSession<T>("linuxdo", url, signal);
}

export function platformFromAuthenticatedUrl(
  value: string,
): PlatformCapturePlatform | null {
  try {
    const host = new URL(value).hostname.toLowerCase();
    if (host === "xhslink.com" || host === "xhslink.cn" || host.endsWith(".xiaohongshu.com")) return "xiaohongshu";
    if (host === "iesdouyin.com" || host.endsWith(".douyin.com")) return "douyin";
    if (host === "linux.do" || host.endsWith(".linux.do")) return "linuxdo";
  } catch {
    return null;
  }
  return null;
}
