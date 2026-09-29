// 分享入口 spike（仅 dev 包）：在当前进程里实测 Asset Store、配对凭证读取与网络是否可用。
// 扩展与主应用共用这一份探测，结果才能直接对照“扩展内 vs 主进程”。
// 探测结果只含状态码与耗时；不写地址、凭证或任何分享内容。
import { asset } from '@kit.AssetStoreKit';
import { http } from '@kit.NetworkKit';
import { util } from '@kit.ArkTS';
import { BusinessError } from '@kit.BasicServicesKit';
import { RelayRequestError } from '../core/ConnectionCheck';
import { Connection } from '../core/Protocol';
import { CredentialStore } from '../services/CredentialStore';
import { RelayClient } from '../services/RelayClient';
import { ProbeStep, ShareProbeEntry } from './ShareProbe';

const PROBE_ALIAS: string = 'guizhi.spike.probe.v1';
const PROBE_SECRET: string = 'probe-ok';
// 未配对时改测公网连通性；example.com 是 IANA 保留的示例域名。
const PUBLIC_URL: string = 'https://example.com/';

function codeOf(error: Object): string {
  const code = (error as BusinessError).code;
  return typeof code === 'number' ? `${code}` : 'unknown';
}

// 写入、读回、删除一条自有的探测条目：证明当前进程能正常使用 Asset Store，且不碰真实凭证。
async function probeAsset(): Promise<ProbeStep> {
  const encoder = new util.TextEncoder();
  const alias = encoder.encodeInto(PROBE_ALIAS);
  const add: asset.AssetMap = new Map();
  add.set(asset.Tag.ALIAS, alias);
  add.set(asset.Tag.SECRET, encoder.encodeInto(PROBE_SECRET));
  add.set(asset.Tag.ACCESSIBILITY, asset.Accessibility.DEVICE_UNLOCKED);
  add.set(asset.Tag.SYNC_TYPE, asset.SyncType.NEVER);
  add.set(asset.Tag.CONFLICT_RESOLUTION, asset.ConflictResolution.OVERWRITE);
  const query: asset.AssetMap = new Map();
  query.set(asset.Tag.ALIAS, alias);
  query.set(asset.Tag.RETURN_TYPE, asset.ReturnType.ALL);
  const remove: asset.AssetMap = new Map();
  remove.set(asset.Tag.ALIAS, alias);
  let stage = 'add';
  try {
    await asset.add(add);
    stage = 'query';
    const rows = await asset.query(query);
    if (rows.length === 0) return new ProbeStep('fail', 'query 未读回刚写入的条目');
    const back = new util.TextDecoder().decodeToString(rows[0].get(asset.Tag.SECRET) as Uint8Array);
    stage = 'remove';
    await asset.remove(remove);
    return back === PROBE_SECRET ? new ProbeStep('ok', '写入、读回、删除均成功') : new ProbeStep('fail', '读回内容与写入不一致');
  } catch (error) {
    // 失败时尽力清理探测条目，避免残留。
    try { await asset.remove(remove); } catch { /* 已不存在或同样失败，忽略 */ }
    return new ProbeStep('fail', `${stage} 错误码 ${codeOf(error as Object)}`);
  }
}

class CredentialProbe {
  step: ProbeStep = new ProbeStep();
  connection: Connection | undefined = undefined;
}

// 与主应用完全相同的读取路径：能读到，才说明扩展可以复用配对凭证。
async function probeCredential(): Promise<CredentialProbe> {
  const result = new CredentialProbe();
  try {
    const connection = await new CredentialStore().load();
    if (connection === undefined) {
      result.step = new ProbeStep('none', '未配对：Asset Store 可访问但没有配对凭证');
    } else {
      result.connection = connection;
      result.step = new ProbeStep('ok', '已读到配对凭证');
    }
  } catch (error) {
    // CredentialStore 抛出的是固定说明，不含机密。
    result.step = new ProbeStep('fail', (error as Error).message);
  }
  return result;
}

async function probeRelay(connection: Connection): Promise<ProbeStep> {
  const started = Date.now();
  try {
    const session = await new RelayClient(connection).session();
    return new ProbeStep('ok', `已访问中转，${session.paired ? '会话有效' : '会话未通过（可能尚未确认或已撤销）'}，${Date.now() - started} ms`);
  } catch (error) {
    if (error instanceof RelayRequestError && error.status > 0) {
      return new ProbeStep('fail', `中转返回 HTTP ${error.status} ${error.code}，${Date.now() - started} ms`);
    }
    return new ProbeStep('fail', `网络超时或中断，${Date.now() - started} ms`);
  }
}

async function probePublic(): Promise<ProbeStep> {
  const started = Date.now();
  const request = http.createHttp();
  try {
    const response = await request.request(PUBLIC_URL, {
      method: http.RequestMethod.GET, connectTimeout: 5000, readTimeout: 5000,
      expectDataType: http.HttpDataType.STRING, usingCache: false, maxRedirects: 0
    });
    const ok = response.responseCode >= 200 && response.responseCode < 400;
    return new ProbeStep(ok ? 'ok' : 'fail', `未配对，改测公网：HTTP ${response.responseCode}，${Date.now() - started} ms`);
  } catch (error) {
    return new ProbeStep('fail', `未配对，改测公网失败 错误码 ${codeOf(error as Object)}，${Date.now() - started} ms`);
  } finally {
    request.destroy();
  }
}

// 依次完成三项探测，每完成一项就通知调用方保存，这样即使进程随后被回收，已有结论也不丢。
export async function runProbes(entry: ShareProbeEntry, save: () => void): Promise<void> {
  const started = Date.now();
  entry.assetRw = await probeAsset();
  save();
  const credential = await probeCredential();
  entry.credential = credential.step;
  save();
  entry.network = credential.connection !== undefined ? await probeRelay(credential.connection) : await probePublic();
  entry.elapsedMs = Date.now() - started;
  save();
}
