import { http } from '@kit.NetworkKit';
import { Connection, Meta, Session, Claim, ClaimResult, Receipt, Submission, ApiError, apiErrorMessage } from '../core/Protocol';
import { CaptureSender } from '../core/DraftQueue';
import { RelayRequestError } from '../core/ConnectionCheck';

export class RelayClient implements CaptureSender {
  private retryAfter: number = 0;
  private connection: Connection;
  constructor(connection: Connection) { this.connection = connection; }

  private async request<T>(path: string, method: http.RequestMethod, body: string = '', authenticated: boolean = true): Promise<T> {
    if (Date.now() < this.retryAfter) throw new RelayRequestError('服务请求受限，请等待一分钟后重试', 429);
    const request = http.createHttp();
    const headers: Record<string, string> = { 'Content-Type': 'application/json', 'X-Guizhi-Protocol': '1' };
    if (authenticated) headers['Authorization'] = `Bearer ${this.connection.credential}`;
    let response: http.HttpResponse;
    let deadline: number = -1;
    try {
      // 状态检查有独立的总时限，弱网时不让一次轮询长期占住界面。
      const timeout = path === 'session' ? 8000 : 20000;
      response = await new Promise<http.HttpResponse>((resolve, reject) => {
        deadline = setTimeout(() => { reject(new Error('timeout')); }, timeout);
        request.request(`${this.connection.origin}/v1/${path}`, {
          method: method, header: headers, extraData: body, expectDataType: http.HttpDataType.STRING,
          connectTimeout: Math.min(10000, timeout), readTimeout: timeout, usingCache: false, maxRedirects: 0
        }).then(resolve).catch(reject);
      });
    } catch {
      // 不拼接系统异常中的 URL、凭证和正文；用户仍可复制操作级错误。
      throw new RelayRequestError('网络超时或连接中断，结果尚未确认；请检查网络后重试，本机数据已保留');
    } finally { clearTimeout(deadline); request.destroy(); }
    if (response.responseCode === 429) this.retryAfter = Date.now() + 60000;
    if (response.responseCode < 200 || response.responseCode >= 300) {
      let code = '';
      try { code = (JSON.parse(response.result as string) as ApiError).error ?? ''; } catch {}
      const safeCode = /^[a-z_]{1,50}$/.test(code) ? code : 'request_failed';
      throw new RelayRequestError(`${apiErrorMessage(safeCode)}（HTTP ${response.responseCode} / ${safeCode}）`, response.responseCode);
    }
    try { return JSON.parse(response.result as string) as T; }
    catch { throw new Error('服务响应格式不正确，内容已保留'); }
  }

  async claim(): Promise<void> {
    const meta = await this.request<Meta>('meta', http.RequestMethod.GET, '', false);
    if (meta.protocol !== 1 || !meta.nativePairing) throw new Error('中转服务尚未支持原生配对，请先更新服务');
    const body: Claim = { pairingId: this.connection.pairingId, nonce: this.connection.nonce,
      credential: this.connection.credential, name: this.connection.name };
    await this.request<ClaimResult>('pairings/native-claim', http.RequestMethod.POST, JSON.stringify(body), false);
  }

  session(): Promise<Session> { return this.request<Session>('session', http.RequestMethod.GET); }
  history(): Promise<Receipt[]> { return this.request<Receipt[]>('history', http.RequestMethod.GET); }
  send(body: Submission): Promise<Receipt> { return this.request<Receipt>('captures', http.RequestMethod.POST, JSON.stringify(body)); }
  async disconnect(): Promise<void> { await this.request<Object>('session', http.RequestMethod.DELETE, '{}'); }
}
