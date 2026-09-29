// 电脑在线状态：中转按服务端时间给出“电脑最近一次取件距今多少毫秒”（session.desktopSeenAgoMs），手机只负责按阈值翻译成人话。
// 不依赖手机时钟；字段缺失（旧中转）或格式异常时不显示任何状态，避免把“不知道”说成“不在线”。
import { Receipt, ReceiptTone } from './Protocol';

// 桌面约每 30 秒取件一次：连续三次没取到（90 秒）就不再算“在线”。
export const DESKTOP_ONLINE_LIMIT_MS: number = 90000;
// 桌面取件出错时最长退避 5 分钟；6 分钟内仍属于“刚刚还在”。
export const DESKTOP_RECENT_LIMIT_MS: number = 360000;
// 手机在前台且已配对时查询会话的间隔。
export const DESKTOP_POLL_INTERVAL_MS: number = 30000;

const MINUTE: number = 60000;
const HOUR: number = 3600000;
const DAY: number = 86400000;

// unknown：不知道（旧中转、未配对、查询失败），界面不显示；never：电脑还没有取过件。
export type DesktopPresence = 'unknown' | 'online' | 'recent' | 'offline' | 'never';

export class DesktopStatus {
  presence: DesktopPresence = 'unknown';
  headline: string = '';
  // 补充说明（连接页显示）；在线、最近取件和未知时为空。
  detail: string = '';
  tone: ReceiptTone = 'neutral';
  // 中转已接收、电脑尚未接收的条数。
  waiting: number = 0;

  get visible(): boolean { return this.presence !== 'unknown'; }

  // 状态行文案：状态 ·（电脑没在取件时）内容暂存说明 ·（有待取件时）等待条数。
  get summary(): string {
    if (!this.visible) return '';
    const parts: string[] = [this.headline];
    if (this.presence === 'offline' || this.presence === 'never') parts.push('内容暂存在中转');
    if (this.waiting > 0) parts.push(`等待电脑取件 ${this.waiting} 条`);
    return parts.join(' · ');
  }
}

// 时间跨度的口语标签，如“5 分钟”“2 小时”“3 天”；不足一分钟按一分钟算。
export function agoLabel(ms: number): string {
  if (ms >= DAY) return `${Math.floor(ms / DAY)} 天`;
  if (ms >= HOUR) return `${Math.floor(ms / HOUR)} 小时`;
  return `${Math.max(1, Math.floor(ms / MINUTE))} 分钟`;
}

// seenAgoMs：undefined 表示中转没有提供该字段，null 表示电脑从未取过件；非有限数（格式异常）按未知处理。
export function desktopStatusOf(seenAgoMs: number | null | undefined, waiting: number): DesktopStatus {
  const status = new DesktopStatus();
  status.waiting = waiting > 0 ? Math.floor(waiting) : 0;
  if (seenAgoMs === undefined) return status;
  if (seenAgoMs === null) {
    status.presence = 'never';
    status.headline = '电脑尚未取件';
    status.detail = '电脑打开归知并开启手机收集后会自动取件；发送的内容先暂存在中转，最长保留 30 天。';
    return status;
  }
  // 格式异常（非有限数）不能当作“很久没取件”，按不知道处理。
  if (!Number.isFinite(seenAgoMs)) return status;
  // 负数（服务端时钟回拨）自然落在“在线”区间，无需单独处理。
  if (seenAgoMs <= DESKTOP_ONLINE_LIMIT_MS) {
    status.presence = 'online';
    status.headline = '电脑在线';
    status.tone = 'success';
  } else if (seenAgoMs <= DESKTOP_RECENT_LIMIT_MS) {
    status.presence = 'recent';
    status.headline = `最近取件 ${agoLabel(seenAgoMs)}前`;
  } else {
    status.presence = 'offline';
    status.headline = '电脑未在取件';
    status.tone = 'warning';
    status.detail = `上次取件在 ${agoLabel(seenAgoMs)}前。电脑可能关机、断网或暂停了手机收集；发送的内容先暂存在中转，最长保留 30 天，电脑恢复后会自动取件。`;
  }
  return status;
}

// 等待电脑取件的条数：中转已接收（accepted）、电脑尚未确认接收的回执。
export function waitingCount(receipts: Receipt[]): number {
  let count = 0;
  for (let i = 0; i < receipts.length; i++) {
    if (receipts[i].state === 'accepted') count++;
  }
  return count;
}

// 前台查询会话的节奏：距上次查询满一个间隔才到期；时钟回拨时立即到期，不会停在“未来”。
export class DesktopPoll {
  private lastAt: number = -1;

  due(now: number): boolean {
    return this.lastAt < 0 || now < this.lastAt || now - this.lastAt >= DESKTOP_POLL_INTERVAL_MS;
  }

  started(now: number): void { this.lastAt = now; }

  reset(): void { this.lastAt = -1; }
}
