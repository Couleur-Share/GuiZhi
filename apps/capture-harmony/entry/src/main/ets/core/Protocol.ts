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
  // 最近一次发送失败的原因与时间；重新发送成功（草稿被移除）或改为可编辑时不再保留。
  lastError: string = '';
  lastErrorAt: number = 0;
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

// 逐码点计算 UTF-8 字节数，与服务端 32 KiB 限制的计算口径一致（孤立代理项按 3 字节，与 TextEncoder 的替换字符一致）。
export function utf8Bytes(input: string): number {
  let bytes = 0;
  for (let i = 0; i < input.length; i++) {
    const code = input.charCodeAt(i);
    if (code < 0x80) bytes++;
    else if (code < 0x800) bytes += 2;
    else if (code >= 0xD800 && code <= 0xDBFF && i + 1 < input.length &&
      input.charCodeAt(i + 1) >= 0xDC00 && input.charCodeAt(i + 1) <= 0xDFFF) { bytes += 4; i++; }
    else bytes += 3;
  }
  return bytes;
}

export function validateInput(input: string): void {
  if (!input.trim()) throw new Error('请输入链接或文字');
  if (utf8Bytes(input) > 32768) throw new Error('内容超过 32 KiB，请拆分后保存');
}

// 电脑端逐项处理结果的统计。
export class ProgressTally {
  completed: number = 0;
  failed: number = 0;
  duplicate: number = 0;
  canceled: number = 0;
  get finished(): number { return this.completed + this.failed + this.duplicate + this.canceled; }
}

export function tallyProgress(receipt: Receipt): ProgressTally {
  const tally = new ProgressTally();
  if (!receipt.progress) return tally;
  const items = receipt.progress.items;
  for (let i = 0; i < items.length; i++) {
    if (items[i].status === 'completed') tally.completed++;
    else if (items[i].status === 'failed') tally.failed++;
    else if (items[i].status === 'duplicate') tally.duplicate++;
    else if (items[i].status === 'canceled') tally.canceled++;
  }
  return tally;
}

// 记录状态的语义色调：界面据此选择 success / warning / error 色，颜色本身不承载唯一信息（文案仍完整）。
export type ReceiptTone = 'success' | 'warning' | 'error' | 'neutral';

// 回执的一枚计数徽标，如“成功 2”。
export class ReceiptBadge {
  text: string;
  tone: ReceiptTone;
  constructor(text: string, tone: ReceiptTone) { this.text = text; this.tone = tone; }
}

function hasProgress(receipt: Receipt): boolean {
  return receipt.state !== 'expired' && receipt.progress !== null && receipt.progress.items.length > 0;
}

// 逐项结果的计数，只列出非零的，避免“重复 0 · 取消 0”这类噪声。
export function receiptBadges(receipt: Receipt): ReceiptBadge[] {
  const badges: ReceiptBadge[] = [];
  if (!hasProgress(receipt)) return badges;
  const tally = tallyProgress(receipt);
  if (tally.completed > 0) badges.push(new ReceiptBadge(`成功 ${tally.completed}`, 'success'));
  if (tally.failed > 0) badges.push(new ReceiptBadge(`失败 ${tally.failed}`, 'error'));
  if (tally.duplicate > 0) badges.push(new ReceiptBadge(`重复 ${tally.duplicate}`, 'neutral'));
  if (tally.canceled > 0) badges.push(new ReceiptBadge(`取消 ${tally.canceled}`, 'neutral'));
  return badges;
}

// 处理进度说明；电脑已处理完且有逐项计数时为空，由计数徽标表达结果。
export function receiptStatus(receipt: Receipt): string {
  if (receipt.state === 'expired') return '已过期';
  if (!hasProgress(receipt)) return receipt.state === 'received' ? '电脑已接收，等待处理' : '中转已接收，等待电脑';
  const tally = tallyProgress(receipt);
  if (tally.finished < receipt.itemCount) return `电脑处理中（${tally.finished}/${receipt.itemCount}）`;
  return receiptBadges(receipt).length > 0 ? '' : '电脑已处理';
}

// 一行完整文案：进度说明加各项计数，供不分栏显示与无障碍朗读使用。
export function receiptLabel(receipt: Receipt): string {
  const parts: string[] = [];
  const status = receiptStatus(receipt);
  if (status !== '') parts.push(status);
  const badges = receiptBadges(receipt);
  for (let i = 0; i < badges.length; i++) parts.push(badges[i].text);
  return parts.join(' · ');
}

export function receiptTone(receipt: Receipt): ReceiptTone {
  if (receipt.state === 'expired') return 'warning';
  if (!receipt.progress || receipt.progress.items.length === 0) return 'neutral';
  const tally = tallyProgress(receipt);
  if (tally.finished < receipt.itemCount) return 'neutral';
  if (tally.failed > 0) return tally.completed > 0 || tally.duplicate > 0 ? 'warning' : 'error';
  return tally.completed > 0 || tally.duplicate > 0 ? 'success' : 'neutral';
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
    case 'empty_input': return '内容为空，请输入链接或文字';
    case 'invalid_input': return '内容格式不正确，请检查后重试';
    case 'input_too_large': return '内容超过 32 KiB，请拆分后保存';
    case 'no_links': return '没有识别到链接。请切换到「文字」模式保存，或粘贴以 http:// 或 https:// 开头的链接';
    case 'too_many_links': return '一次最多收集 20 个链接，请拆分后保存';
    case 'forbidden': return '此设备没有权限执行该操作，请在电脑上检查设备状态';
    case 'json_required': return '请求格式异常，请更新应用后重试';
    case 'invalid_cursor': return '记录游标无效，请下拉刷新后重试';
    case 'invalid_credential': return '连接凭证无效，请解除配对后重新配对';
    case 'invalid_id': return '请求编号格式无效，请重新保存后再试';
    case 'device_limit': return '已绑定设备达到 50 台上限，请在电脑撤销不用的设备后再配对';
    default: return '服务暂时无法完成请求，草稿已保留';
  }
}
