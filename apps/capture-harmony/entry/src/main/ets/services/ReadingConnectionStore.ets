import { asset } from '@kit.AssetStoreKit';
import { util } from '@kit.ArkTS';
import { BusinessError } from '@kit.BasicServicesKit';
import { ReadingConnection, validateConnection } from '../core/ReadingModel';
import { ReadingCrypto } from './ReadingCrypto';

const ALIAS = new util.TextEncoder().encodeInto('guizhi.reading.connection.v1');
// 配对秘密、手机凭证、内容/本机缓存密钥作为一个安全存储记录提交；不复用收集连接。
export class ReadingConnectionStore {
  async load(): Promise<ReadingConnection | null> {
    const query: asset.AssetMap = new Map();
    query.set(asset.Tag.ALIAS, ALIAS); query.set(asset.Tag.RETURN_TYPE, asset.ReturnType.ALL);
    let rows: asset.AssetMap[];
    try { rows = await asset.query(query); }
    catch (error) {
      if ((error as BusinessError).code === asset.ErrorCode.NOT_FOUND) return null;
      throw new Error('阅读安全存储不可用，请解锁手机后重试');
    }
    if (rows.length === 0) return null;
    if (rows.length !== 1) throw new Error('阅读安全存储存在冲突，已停止操作');
    const bytes = rows[0].get(asset.Tag.SECRET) as Uint8Array;
    try {
      const value = JSON.parse(new util.TextDecoder().decodeToString(bytes)) as ReadingConnection;
      validateConnection(value, new ReadingCrypto()); return value;
    } catch { throw new Error('阅读连接损坏或无法解锁，请保留数据并重试'); }
    finally { bytes.fill(0); }
  }
  async save(value: ReadingConnection): Promise<void> {
    validateConnection(value, new ReadingCrypto());
    const bytes = new util.TextEncoder().encodeInto(JSON.stringify(value)), attributes: asset.AssetMap = new Map();
    attributes.set(asset.Tag.ALIAS, ALIAS); attributes.set(asset.Tag.SECRET, bytes);
    attributes.set(asset.Tag.ACCESSIBILITY, asset.Accessibility.DEVICE_UNLOCKED);
    attributes.set(asset.Tag.SYNC_TYPE, asset.SyncType.NEVER);
    attributes.set(asset.Tag.CONFLICT_RESOLUTION, asset.ConflictResolution.OVERWRITE);
    try { await asset.add(attributes); }
    catch { throw new Error('阅读安全存储写入失败，未回落普通文件；请解锁后重试'); }
    finally { bytes.fill(0); }
  }
  async clear(): Promise<void> {
    const query: asset.AssetMap = new Map(); query.set(asset.Tag.ALIAS, ALIAS);
    try { await asset.remove(query); }
    catch (error) { if ((error as BusinessError).code !== asset.ErrorCode.NOT_FOUND) throw new Error('阅读连接清除失败，请重试'); }
  }
}
