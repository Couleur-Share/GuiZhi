import type { EvidenceSnapshot } from '../types/evidence';

/** 访问凭证只用于请求，禁止进入历史证据。 */
export function evidenceUrl(value?: string): string | undefined {
  if (!value) return undefined;
  try {
    const url = new URL(value);
    if (!['https:', 'http:'].includes(url.protocol)) return undefined;
    url.username = ''; url.password = ''; url.hash = '';
    for (const name of [...url.searchParams.keys()]) {
      if (/token|key|auth|secret|password|credential|signature|^sig$|^x-amz-|^x-goog-/i.test(name)) url.searchParams.delete(name);
    }
    return url.href;
  } catch { return undefined; }
}
export function evidenceText(text: string): string {
  return text.replace(/https?:\/\/[^\s<>"）)\]]+/g, url => evidenceUrl(url) ?? '[链接不可用]');
}
export async function captureEvidence(input: Omit<EvidenceSnapshot, 'version' | 'fingerprint' | 'capturedAt' | 'reviewStatus' | 'reviewReasons'> & Partial<Pick<EvidenceSnapshot, 'fingerprint' | 'capturedAt' | 'reviewStatus' | 'reviewReasons'>>): Promise<EvidenceSnapshot> {
  const text = evidenceText(input.text);
  const fingerprint = input.fingerprint ?? Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(text))), b => b.toString(16).padStart(2, '0')).join('');
  return { ...input, text, url: evidenceUrl(input.url), version: 1, fingerprint, capturedAt: input.capturedAt ?? Date.now(), reviewStatus: input.reviewStatus ?? 'clear', reviewReasons: input.reviewReasons ?? [] };
}
export function qualityNotice(value: { reviewStatus?: string; reviewReasons?: string[] }): string {
  return value.reviewStatus === 'needs_review' ? `资料待复核，可能存在内容缺失：${value.reviewReasons?.join('；') || '尚未确认完整性'}。请明确标注限制，不据此推断未提供的内容。` : '';
}
