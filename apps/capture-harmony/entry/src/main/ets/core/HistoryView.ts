// “近期”页的展示模型：把中转回执与本机发送记录合并，并给出卡片所需的逐项失败说明和分页规则。
// 回执是事实来源（状态、条数、逐项结果），本机发送记录只补充“这是什么内容”；两者按请求编号关联，
// 关联不上（其他设备发送、更新前的旧回执、记录被清除）时如实退回“N 项收集”，不猜测内容。
import { ProgressItem, Receipt } from './Protocol';
import { hostOfUrl } from './CaptureParser';
import { SentEntry } from './SentLog';

// 与中转 GET /v1/history 单页上限一致；返回不足一页说明已经取完。
export const HISTORY_PAGE_SIZE: number = 100;

export class HistoryItem {
  receipt: Receipt;
  // 卡片标题：有本机记录时是内容预览，否则是“N 项收集”。
  title: string = '';
  // 来源展示名；没有本机记录或内容为文字时为空。
  source: string = '';
  // 发送时识别到的链接，没有本机记录时为空。
  urls: string[] = [];
  constructor(receipt: Receipt) { this.receipt = receipt; }
}

export function mergeHistory(receipts: Receipt[], sent: SentEntry[]): HistoryItem[] {
  const byRequest = new Map<string, SentEntry>();
  // 记录按发送先后排列，同一编号后者覆盖前者。
  for (let i = 0; i < sent.length; i++) byRequest.set(sent[i].requestId, sent[i]);
  const items: HistoryItem[] = [];
  for (let i = 0; i < receipts.length; i++) {
    const item = new HistoryItem(receipts[i]);
    const entry = byRequest.get(receipts[i].requestId);
    if (entry !== undefined) {
      item.title = entry.preview;
      item.source = entry.platform;
      item.urls = entry.urls;
    }
    if (item.title === '') item.title = `${receipts[i].itemCount} 项收集`;
    items.push(item);
  }
  return items;
}

// 列表项的键：凡是会影响卡片显示的内容都要参与。ForEach 对键不变的项不会刷新内容，
// 只用回执编号做键时，状态推进、清除发送记录后标题变化都会停留在旧画面上。
export function historyKey(item: HistoryItem): string {
  const parts: string[] = [item.receipt.id, item.receipt.state, `${item.receipt.itemCount}`, item.title, item.source, `${item.urls.length}`];
  const progress = item.receipt.progress;
  if (progress !== null) {
    const rows: string[] = [];
    for (let i = 0; i < progress.items.length; i++) {
      const row: ProgressItem = progress.items[i];
      rows.push(row.error === undefined ? row.status : `${row.status}:${row.error}`);
    }
    parts.push(rows.join(','));
  }
  return parts.join('|');
}

// 中转只回传错误类别；文案与桌面端的说明保持同一含义。
export function itemErrorLabel(error: string | undefined): string {
  const code: string = error === undefined ? '' : error;
  switch (code) {
    case 'login_required': return '需在电脑登录来源网站';
    case 'incomplete': return '内容不完整';
    case 'rate_limited': return '来源限流';
    case 'network': return '来源网络失败';
    default: return '采集失败';
  }
}

export class FailedItem {
  // 与回执 progress 一致的 0 起序号。
  index: number = 0;
  // 无法确认对应链接时为空。
  url: string = '';
  host: string = '';
  reason: string = '';
  // 展示与“复制原因”共用的一行：第 N 项 · 域名 · 原因。
  summary: string = '';
}

// 失败或带错误类别的逐项结果。链接只在条数与回执一致时才按序号对应：
// 混合内容按文字整体计 1 项、发送后又被去重等情况下，序号对不上就不显示链接，而不是显示错的那一条。
export function failedItems(item: HistoryItem): FailedItem[] {
  const result: FailedItem[] = [];
  const progress = item.receipt.progress;
  if (progress === null) return result;
  const mapped = item.urls.length === item.receipt.itemCount;
  for (let i = 0; i < progress.items.length; i++) {
    const row: ProgressItem = progress.items[i];
    if (row.error === undefined && row.status !== 'failed') continue;
    const failed = new FailedItem();
    failed.index = row.index;
    failed.reason = itemErrorLabel(row.error);
    if (mapped && row.index >= 0 && row.index < item.urls.length) {
      failed.url = item.urls[row.index];
      failed.host = hostOfUrl(failed.url);
    }
    const parts: string[] = [`第 ${row.index + 1} 项`];
    if (failed.host !== '') parts.push(failed.host);
    parts.push(failed.reason);
    failed.summary = parts.join(' · ');
    result.push(failed);
  }
  return result;
}

export class HistoryPage {
  receipts: Receipt[] = [];
  // 服务端可能还有更早的记录。
  more: boolean = false;
}

// “加载更多”的游标：已加载列表（按创建时间倒序）里最早一条的创建时间；空列表为 0，表示取最新一页。
export function historyCursor(receipts: Receipt[]): number {
  return receipts.length === 0 ? 0 : receipts[receipts.length - 1].createdAt;
}

// 刷新最新一页：用新数据替换最新的部分，同时保留此前已经“加载更多”得到、且比新数据更早的记录，
// 避免下拉刷新或发送后的自动刷新把用户翻到的旧记录丢掉。
export function firstPage(current: Receipt[], currentMore: boolean, fresh: Receipt[]): HistoryPage {
  const page = new HistoryPage();
  page.receipts = fresh.slice();
  // 不足一页说明记录已全部在这里，此前多翻出的旧记录已不存在或已过期。
  if (fresh.length < HISTORY_PAGE_SIZE) return page;
  const oldest = fresh[fresh.length - 1].createdAt;
  const kept: Receipt[] = [];
  for (let i = 0; i < current.length; i++) {
    if (current[i].createdAt < oldest) kept.push(current[i]);
  }
  page.receipts = fresh.concat(kept);
  page.more = kept.length > 0 ? currentMore : true;
  return page;
}

// 追加更早的一页：按编号去重；这一页取满才认为可能还有更多。
export function nextPage(current: Receipt[], older: Receipt[]): HistoryPage {
  const page = new HistoryPage();
  const seen = new Set<string>();
  for (let i = 0; i < current.length; i++) seen.add(current[i].id);
  page.receipts = current.slice();
  for (let i = 0; i < older.length; i++) {
    if (!seen.has(older[i].id)) page.receipts.push(older[i]);
  }
  page.more = older.length >= HISTORY_PAGE_SIZE;
  return page;
}
