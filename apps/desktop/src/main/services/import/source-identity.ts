import { normalizeUrl } from './url-normalize';
import { extractUrlsFromText } from '@guizhi/shared/utils/url-text';
export function sourceIdentity(raw: string): string | null {
  const url = extractUrlsFromText(raw)[0] ?? raw;
  try {
    const parsed = new URL(url), host = parsed.hostname.toLowerCase();
    if (/(^|\.)xiaohongshu\.com$/.test(host)) {
      const id = parsed.pathname.match(/\/(?:explore|discovery\/item)\/([a-f\d]{24})/i)?.[1];
      if (id) return `https://www.xiaohongshu.com/explore/${id.toLowerCase()}`;
    }
    if (/(^|\.)douyin\.com$/.test(host)) {
      const id = parsed.pathname.match(/\/(?:video|note|slides)\/(\d{6,})/)?.[1] ?? parsed.searchParams.get('modal_id');
      if (id && /^\d{6,}$/.test(id)) return `https://www.douyin.com/video/${id}`;
    }
    if (/(^|\.)bilibili\.com$/.test(host)) { const id = parsed.pathname.match(/\/video\/(BV[\da-z]+)/i)?.[1]; if (id) return `https://www.bilibili.com/video/${id}`; }
    if (host === 'youtu.be' || /(^|\.)youtube\.com$/.test(host)) { const id = host === 'youtu.be' ? parsed.pathname.slice(1) : parsed.searchParams.get('v') ?? parsed.pathname.match(/\/(?:shorts|live)\/([^/]+)/)?.[1]; if (id && /^[\w-]{11}$/.test(id)) return `https://www.youtube.com/watch?v=${id}`; }
    return normalizeUrl(url);
  } catch { return null; }
}
export async function resolveSourceIdentity(raw: string, signal: AbortSignal): Promise<string | null> {
  const url = extractUrlsFromText(raw)[0] ?? raw;
  try {
    const host = new URL(url).hostname;
    if (['xhslink.com','xhslink.cn','v.douyin.com','b23.tv'].includes(host)) {
      const { resolveRedirectUrl } = await import("./safe-fetch");
      const final = await resolveRedirectUrl(url, AbortSignal.any([signal, AbortSignal.timeout(5000)]));
      return sourceIdentity(final);
    }
  } catch { signal.throwIfAborted(); /* 无法轻量解析时仍走连接器，并保留最终哈希判重。 */ }
  return sourceIdentity(raw);
}
