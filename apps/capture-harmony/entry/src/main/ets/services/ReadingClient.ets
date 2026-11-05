import { http } from '@kit.NetworkKit';
import { ReadingConnection, ReadingHead, readingOrigin } from '../core/ReadingModel';
import { ReadingRemote } from '../core/ReadingLibrary';
import { ReadingClaim } from '../core/ReadingPairing';

interface ObjectResponse { token: string; }
interface BundleResponse { bundle: string | null; }
interface ErrorResponse { error: string; }
export class ReadingClient implements ReadingRemote {
  private active: http.HttpRequest[] = [];
  private retryAt: number = 0;
  private connection: ReadingConnection;
  constructor(connection: ReadingConnection) { readingOrigin(connection.origin); this.connection = connection; }
  cancel(): void { this.active.forEach((request: http.HttpRequest) => request.destroy()); this.active = []; }
  private async request<T>(suffix: string, method: http.RequestMethod = http.RequestMethod.GET, body: string = ''): Promise<T> {
    if (Date.now() < this.retryAt) throw new Error('阅读请求过于频繁，请等待 60 秒后重试');
    if (!/^\/(?:head|objects\/[a-f0-9]{64}|pairs\/[A-Za-z0-9_-]{16,64}\/(?:claim|bundle))$/.test(suffix)) throw new Error('阅读请求路径无效');
    const request = http.createHttp(); this.active.push(request); let timer: number = -1;
    let response: http.HttpResponse;
    try {
      response = await new Promise<http.HttpResponse>((resolve, reject) => {
        timer = setTimeout(() => reject(new Error('timeout')), 20000);
        request.request(`${this.connection.origin}/v1/reading/${this.connection.libraryId}${suffix}`, {
          method: method, header: { 'Content-Type': 'application/json', 'X-Guizhi-Reading': '1', Authorization: `Bearer ${this.connection.credential}` },
          extraData: body, expectDataType: http.HttpDataType.STRING, usingCache: false, maxRedirects: 0,
          connectTimeout: 8000, readTimeout: 20000, maxLimit: 12 * 1024 * 1024
        }).then(resolve).catch(reject);
      });
    } catch { throw new Error('阅读网络不可用或已取消，已有离线内容保留；请联网重试'); }
    finally { clearTimeout(timer); request.destroy(); this.active = this.active.filter((item: http.HttpRequest): boolean => item !== request); }
    const status = response.responseCode;
    if (status === 429) { this.retryAt = Date.now() + 60000; throw new Error('阅读请求过于频繁，请等待 60 秒后重试'); }
    if (status === 401 || status === 403) throw new Error('阅读权限已失效或被撤销，请在电脑检查连接；已下载的离线内容尚未删除');
    if (status === 410) throw new Error('阅读配对码已过期，请在电脑重新生成并扫码');
    if (status === 404) throw new Error('云端对象已变更或撤回，请重新同步正文');
    if (status < 200 || status >= 300) throw new Error(`阅读服务未完成请求（HTTP ${status}），请重试`);
    if (typeof response.result !== 'string' || response.result.length > 12 * 1024 * 1024) throw new Error('阅读响应类型或长度无效');
    try { return JSON.parse(response.result as string) as T; } catch { throw new Error('阅读服务响应格式无效'); }
  }
  head(): Promise<ReadingHead> { return this.request<ReadingHead>('/head'); }
  async object(digest: string): Promise<string> { return (await this.request<ObjectResponse>('/objects/' + digest)).token; }
  async claim(value: ReadingClaim): Promise<void> { await this.request<ErrorResponse>(`/pairs/${this.connection.pairId}/claim`, http.RequestMethod.POST, JSON.stringify(value)); }
  async bundle(): Promise<string | null> { return (await this.request<BundleResponse>(`/pairs/${this.connection.pairId}/bundle`)).bundle; }
}
