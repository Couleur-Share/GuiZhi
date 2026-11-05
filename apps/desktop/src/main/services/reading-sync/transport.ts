import https from 'node:https';
import { readingOrigin } from '@guizhi/core/reading-sync/pairing';
import { resolvePublicAddress } from '../net-safety';
import { getHttpRequestAgent, hasAnyProxyConfigured } from '../network-proxy';

const readingErrors: Record<string, string> = {
  rate_limited: '阅读服务请求过于频繁，请等待 60 秒后重试；已完成的上传会保留',
  storage_quota: '云端阅读容量已满。未使用的暂存对象保留 24 小时，回收后可重试；本地主库不受影响',
  head_conflict: '本地与云端版本不一致，已保留队列；请勿用旧备份覆盖云端',
  key_rotation_required: '设备已撤销，阅读同步已冻结；密钥轮换将在后续阶段提供',
  reader_already_paired: '当前试用仅支持一台阅读手机，已有设备完成配对',
  pair_expired: '配对请求已过期，请重新生成配对码',
  pair_conflict: '配对信息已改变，请等待当前配对码过期后重新生成',
  unauthorized: '阅读服务凭据无效或设备已撤销，请检查连接',
};

export async function readingRequest<T>(origin: string, route: string, credential: string, method: string,
  body: unknown, signal: AbortSignal): Promise<T> {
  if (!/^\/v1\/reading(?:\/[A-Za-z0-9_-]+)*$/.test(route)) throw new Error('阅读服务请求路径无效');
  const url = new URL(route, readingOrigin(origin));
  const address = await resolvePublicAddress(url.hostname, { allowProxyCompatibilityAddress: hasAnyProxyConfigured() });
  signal.throwIfAborted();
  const payload = body === undefined ? undefined : JSON.stringify(body);
  if (payload && Buffer.byteLength(payload) > 12 * 1024 * 1024) throw new Error('阅读同步请求超限');
  return new Promise<T>((resolve, reject) => {
    const request = https.request(url, { method, signal, timeout: 30000, family: address.family, agent: getHttpRequestAgent(url),
      lookup: (_host, _options, callback) => callback(null, address.address, address.family),
      headers: { 'X-Guizhi-Reading': '1', Authorization: `Bearer ${credential}`,
        ...(payload === undefined ? {} : { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(payload) }) } }, response => {
      const chunks: Buffer[] = []; let bytes = 0;
      response.on('data', (chunk: Buffer) => { bytes += chunk.length; if (bytes > 12 * 1024 * 1024) response.destroy(new Error('响应超限')); else chunks.push(chunk); });
      response.on('error', () => reject(new Error('阅读同步连接中断，请重试')));
      response.on('end', () => {
        const status = response.statusCode ?? 500;
        // 不跟随重定向，不把服务端自由文本或凭证放入错误信息。
        if (status < 200 || status >= 300) {
          let code = '';
          try { code = JSON.parse(Buffer.concat(chunks).toString('utf8')).error; } catch { /* 仅识别固定错误码。 */ }
          const message = Object.hasOwn(readingErrors, code) ? readingErrors[code] : `阅读同步请求失败（HTTP ${status}），上传队列已保留`;
          reject(new Error(message)); return;
        }
        try { resolve(JSON.parse(Buffer.concat(chunks).toString('utf8')) as T); }
        catch { reject(new Error('阅读同步回执无效，请重试')); }
      });
    });
    request.on('timeout', () => request.destroy());
    request.on('error', () => reject(new Error(signal.aborted ? '阅读同步已取消，队列保留' : '阅读同步网络不可用，请重试')));
    request.end(payload);
  });
}
