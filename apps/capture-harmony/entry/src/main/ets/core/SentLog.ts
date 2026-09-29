// 已发送记录：发出去的那一刻在本机留一份“这是什么内容”的副本。
// 中转在电脑取件后会清掉内容，回执里只剩条数与逐项状态，“近期”页想显示内容预览与来源，只能靠这份本机记录。
// 它不属于协议：只存本机、不上传、有条数上限，用户可以随时清除；缺失时近期页回退为只显示条数。
import { CaptureMode, Receipt } from './Protocol';
import { hostOfUrl, parseCapture } from './CaptureParser';
import { previewText } from './TextPreview';

// 本机最多保留的发送记录条数；超出时丢弃最早的。
export const SENT_LOG_LIMIT: number = 200;
// 记录里的内容预览长度（字符）；卡片再按行数截断显示。
export const SENT_PREVIEW_LENGTH: number = 60;
const STORAGE_VERSION: number = 1;
// 一次发送包含多个不同来源时的统一称呼。
export const MIXED_SOURCES: string = '多个来源';

export class SentEntry {
  requestId: string = '';
  // 内容首行预览，作为近期页卡片标题。
  preview: string = '';
  // 来源平台的展示名，如“抖音”；文字内容或无法判断时为空。
  platform: string = '';
  // 发送时中转确认的条数，用来判断 urls 能否与回执的逐项结果按序号对应。
  itemCount: number = 0;
  // 发送时识别到的链接（最多 20 个）；文字模式不保存。
  urls: string[] = [];
  sentAt: number = 0;
}

class PlatformRule {
  label: string;
  domains: string[];
  constructor(label: string, domains: string[]) { this.label = label; this.domains = domains; }
}

// 与桌面版 source-platforms 的平台清单及展示名保持一致（契约测试逐个平台核对）；识别不了的链接直接显示域名，比笼统的“网页”更有用。
// 这里只按域名归类：桌面端还会要求路径符合帖子/视频格式，所以对同一域名下的非内容页，手机端的标签可能比桌面端的归类更宽松，仅用于显示。
const PLATFORM_RULES: PlatformRule[] = [
  new PlatformRule('抖音', ['douyin.com', 'iesdouyin.com']),
  new PlatformRule('哔哩哔哩', ['bilibili.com', 'b23.tv']),
  new PlatformRule('小红书', ['xiaohongshu.com', 'xhslink.com', 'xhslink.cn']),
  new PlatformRule('YouTube', ['youtube.com', 'youtu.be']),
  new PlatformRule('V2EX', ['v2ex.com']),
  new PlatformRule('NGA', ['nga.cn', 'ngabbs.com', 'nga.178.com']),
  new PlatformRule('LINUX DO', ['linux.do']),
  new PlatformRule('NodeSeek', ['nodeseek.com']),
  new PlatformRule('小众软件', ['meta.appinn.net']),
  new PlatformRule('2Libra', ['2libra.com']),
  new PlatformRule('微信公众号', ['mp.weixin.qq.com'])
];

const HOST_LABEL_LIMIT: number = 24;

// 必须是该域本身或其子域；`evildouyin.com` 这类后缀碰撞不算。
function inDomain(host: string, domain: string): boolean {
  return host === domain || host.endsWith(`.${domain}`);
}

// 链接的来源展示名：已知平台用平台名，否则用去掉 www. 的域名。
export function sourceLabel(url: string): string {
  const host = hostOfUrl(url);
  for (let i = 0; i < PLATFORM_RULES.length; i++) {
    const rule = PLATFORM_RULES[i];
    for (let j = 0; j < rule.domains.length; j++) {
      if (inDomain(host, rule.domains[j])) return rule.label;
    }
  }
  const plain = host.startsWith('www.') ? host.slice(4) : host;
  return plain.length > HOST_LABEL_LIMIT ? `${plain.slice(0, HOST_LABEL_LIMIT)}…` : plain;
}

// 一组链接的统一来源：全部相同用该来源，混合来源用统一称呼，没有链接为空。
export function platformOf(urls: string[]): string {
  if (urls.length === 0) return '';
  const first = sourceLabel(urls[0]);
  for (let i = 1; i < urls.length; i++) {
    if (sourceLabel(urls[i]) !== first) return MIXED_SOURCES;
  }
  return first;
}

// 用发送时的草稿内容与中转返回的回执生成一条记录。
export function buildSentEntry(input: string, mode: CaptureMode, receipt: Receipt, now: number): SentEntry {
  const entry = new SentEntry();
  entry.requestId = receipt.requestId;
  entry.itemCount = receipt.itemCount;
  entry.sentAt = now;
  entry.preview = previewText(input, SENT_PREVIEW_LENGTH);
  // 文字模式的解析结果不含链接（笔记不是待抓取的来源），urls 与 platform 自然为空，无需另设分支；
  // 其余模式保留链接与混合内容里的候选链接，两者能否与回执逐项对应，由近期页按条数判断。
  entry.urls = parseCapture(input, mode).urls;
  entry.platform = platformOf(entry.urls);
  return entry;
}

// 追加一条并按上限修剪：同一请求编号（重试后成功）只保留最新一条，超出上限丢弃最早的。
export function appendSent(entries: SentEntry[], entry: SentEntry): SentEntry[] {
  const next: SentEntry[] = [];
  for (let i = 0; i < entries.length; i++) {
    if (entries[i].requestId !== entry.requestId) next.push(entries[i]);
  }
  next.push(entry);
  return next.length > SENT_LOG_LIMIT ? next.slice(next.length - SENT_LOG_LIMIT) : next;
}

export class SentLogLoad {
  entries: SentEntry[] = [];
  // 存储内容存在但整体无法识别（已被丢弃）。
  damaged: boolean = false;
}

// 持久化的整体结构；version 用于以后调整字段时识别旧数据。
class SentStorage {
  version: number = 0;
  entries: SentEntry[] = [];
}

export function serializeSent(entries: SentEntry[]): string {
  const stored = new SentStorage();
  stored.version = STORAGE_VERSION;
  stored.entries = entries;
  return JSON.stringify(stored);
}

function validEntry(value: Object | null | undefined): boolean {
  if (value === null || value === undefined || typeof value !== 'object') return false;
  const entry = value as SentEntry;
  if (typeof entry.requestId !== 'string' || entry.requestId === '') return false;
  if (typeof entry.preview !== 'string' || typeof entry.platform !== 'string') return false;
  if (typeof entry.itemCount !== 'number' || typeof entry.sentAt !== 'number') return false;
  if (!Array.isArray(entry.urls)) return false;
  for (let i = 0; i < entry.urls.length; i++) {
    if (typeof entry.urls[i] !== 'string') return false;
  }
  return true;
}

// 读取持久化内容：空字符串是首次使用；整体无法识别按“损坏”处理并从空记录开始；个别坏条目单独丢弃。
// 发送记录只是辅助信息，损坏不应阻断收集，也不需要像草稿那样保留原数据。
export function parseSent(raw: string): SentLogLoad {
  const result = new SentLogLoad();
  if (raw === '') return result;
  try {
    const stored = JSON.parse(raw) as SentStorage;
    if (stored.version !== STORAGE_VERSION || !Array.isArray(stored.entries)) {
      result.damaged = true;
      return result;
    }
    for (let i = 0; i < stored.entries.length; i++) {
      if (validEntry(stored.entries[i])) result.entries.push(stored.entries[i]);
    }
  } catch {
    // JSON 不合法，或根值是 null 等无法读取字段的类型。
    result.damaged = true;
    result.entries = [];
    return result;
  }
  // 存储里的条数不应超过上限；被外部改动时仍按上限修剪。
  if (result.entries.length > SENT_LOG_LIMIT) result.entries = result.entries.slice(result.entries.length - SENT_LOG_LIMIT);
  return result;
}

export interface SentLogPort {
  load(): Promise<SentLogLoad>;
  save(entries: SentEntry[]): Promise<void>;
}

// 内存优先的发送记录：读写串行，落盘由 port 完成。
// - 追加：先更新内存再落盘，落盘失败时本次运行内仍可用，同时把错误抛给调用者提示；
// - 清除：先落盘再清内存，失败时记录仍在，界面不会谎称已清除。
export class SentLog {
  private entries: SentEntry[] = [];
  private port: SentLogPort;
  private tail: Promise<void> = Promise.resolve();
  // 打开时读取失败或数据损坏的说明；空字符串表示正常。
  problem: string = '';

  constructor(port: SentLogPort) { this.port = port; }

  private serial<T>(action: () => Promise<T>): Promise<T> {
    const next: Promise<T> = this.tail.then(action);
    this.tail = next.then((): void => {}, (): void => {});
    return next;
  }

  // 读取已保存的记录；失败不抛出，退化为空记录并在 problem 里说明，保证收集与发送不受影响。
  async open(): Promise<void> {
    try {
      const loaded = await this.port.load();
      this.entries = loaded.entries;
      if (loaded.damaged) this.problem = '本机发送记录无法读取，已重新开始记录，之前的回执将只显示条数';
    } catch (error) {
      const message = (error as Error).message;
      this.problem = `本机发送记录无法打开${message ? `（${message}）` : ''}，近期页暂不显示内容预览`;
    }
  }

  snapshot(): SentEntry[] { return this.entries.slice(); }

  append(entry: SentEntry): Promise<void> {
    return this.serial<void>(async (): Promise<void> => {
      this.entries = appendSent(this.entries, entry);
      await this.port.save(this.entries);
    });
  }

  clear(): Promise<void> {
    return this.serial<void>(async (): Promise<void> => {
      await this.port.save([]);
      this.entries = [];
    });
  }
}
