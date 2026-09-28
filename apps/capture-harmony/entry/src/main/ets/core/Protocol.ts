// 与 packages/shared/types/mobile-capture.ts 的 v1 HTTP 契约保持一致；不依赖 Node。
export type CaptureMode = 'auto' | 'urls' | 'text';
export class Submission {
  requestId: string = '';
  input: string = '';
  mode: CaptureMode = 'auto';
}
export class Draft extends Submission {
  attempted: boolean = false;
  binding: string = '';
  createdAt: number = 0;
}
export class LocalState {
  version: number = 1;
  editor: string = '';
  mode: CaptureMode = 'auto';
  drafts: Draft[] = [];
}
export class Connection {
  origin: string = '';
  pairingId: string = '';
  nonce: string = '';
  credential: string = '';
  name: string = '';
}
export class PairingLink {
  origin: string = '';
  pairingId: string = '';
  nonce: string = '';
}
export interface ProgressItem { index: number; status: string; error?: string }
export interface Progress { version: number; items: ProgressItem[] }
export interface Receipt {
  id: string; requestId: string; createdAt: number; itemCount: number;
  state: string; progress: Progress | null;
}
export interface Session { paired: boolean; deviceId: string }
export interface Meta { protocol: number; nativePairing?: boolean }
export interface Claim { pairingId: string; nonce: string; credential: string; name: string }
export interface ClaimResult { id: string }
export interface ApiError { error?: string }

export function parsePairingLink(raw: string): PairingLink {
  const match = /^https:\/\/([a-z0-9.-]+(?::[0-9]{1,5})?)\/?#([^\s]+)$/i.exec(raw.trim());
  if (!match) throw new Error('请扫描归知桌面「手机收集」中的 HTTPS 配对二维码');
  const result = new PairingLink();
  result.origin = `https://${match[1].toLowerCase()}`;
  const parts = match[2].split('&');
  for (let i = 0; i < parts.length; i++) {
    const pair = parts[i].split('=');
    if (pair.length !== 2) throw new Error('配对二维码格式不正确');
    if (pair[0] === 'pair' && !result.pairingId) result.pairingId = decodeURIComponent(pair[1]);
    else if (pair[0] === 'nonce' && !result.nonce) result.nonce = decodeURIComponent(pair[1]);
    else throw new Error('配对二维码包含重复或未知参数');
  }
  if (!/^[0-9a-f-]{36}$/i.test(result.pairingId) || !/^[A-Za-z0-9_-]{43}$/.test(result.nonce)) {
    throw new Error('配对二维码不完整，请在电脑重新生成');
  }
  return result;
}

export function validateInput(input: string): void {
  if (!input.trim()) throw new Error('请输入链接或文字');
  // 逐码点计算 UTF-8 大小，保持与服务端 32 KiB 限制一致。
  let bytes = 0;
  for (let i = 0; i < input.length; i++) {
    const code = input.charCodeAt(i);
    if (code < 0x80) bytes++;
    else if (code < 0x800) bytes += 2;
    else if (code >= 0xD800 && code <= 0xDBFF && i + 1 < input.length &&
      input.charCodeAt(i + 1) >= 0xDC00 && input.charCodeAt(i + 1) <= 0xDFFF) { bytes += 4; i++; }
    else bytes += 3;
  }
  if (bytes > 32768) throw new Error('内容超过 32 KiB，请拆分后保存');
}

export function receiptLabel(receipt: Receipt): string {
  if (receipt.state === 'expired') return '已过期';
  if (!receipt.progress || receipt.progress.items.length === 0) {
    return receipt.state === 'received' ? '电脑已接收，等待处理' : '中转已接收，等待电脑';
  }
  const items = receipt.progress.items;
  let completed = 0, failed = 0, duplicate = 0, canceled = 0;
  for (let i = 0; i < items.length; i++) {
    if (items[i].status === 'completed') completed++;
    if (items[i].status === 'failed') failed++;
    if (items[i].status === 'duplicate') duplicate++;
    if (items[i].status === 'canceled') canceled++;
  }
  const finished = completed + failed + duplicate + canceled;
  const summary = `成功 ${completed} · 失败 ${failed} · 重复 ${duplicate} · 取消 ${canceled}`;
  return finished < receipt.itemCount ? `电脑处理中（${finished}/${receipt.itemCount}） · ${summary}` : summary;
}

export function apiErrorMessage(code: string): string {
  switch (code) {
    case 'unauthorized': return '连接已失效或尚未确认，请检查电脑上的设备状态';
    case 'pairing_expired': return '二维码已过期，请取消本次配对并在电脑重新生成';
    case 'pairing_claimed': return '二维码已被其他设备使用，请在电脑重新生成';
    case 'inbox_full': return '电脑待收内容已满，请先启动电脑收件';
    case 'daily_limit': return '今日收集已达上限，草稿已保留';
    case 'rate_limited': return '请求过于频繁，请稍后重试';
    case 'request_conflict': return '请求内容冲突，原草稿已保留，请先核对近期记录';
    case 'protocol_mismatch': return '客户端与中转服务版本不兼容';
    case 'invalid_name': return '设备名称应为 1–60 个字符';
    default: return '服务暂时无法完成请求，草稿已保留';
  }
}
