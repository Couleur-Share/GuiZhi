import { asset } from '@kit.AssetStoreKit';
import { util } from '@kit.ArkTS';
import { BusinessError } from '@kit.BasicServicesKit';
import { assertReadingId } from '../core/ReadingSyncFormat';

// 与收集凭据隔离；仅安全存储，不使用 Preferences/普通文件回落。
export class ReadingKeyStore {
  private alias(keyId: string): Uint8Array {
    assertReadingId(keyId);
    return new util.TextEncoder().encodeInto('guizhi.reading.key.v1.' + keyId);
  }

  async save(keyId: string, key: Uint8Array): Promise<void> {
    if (key.length !== 32) throw new Error('阅读密钥长度无效');
    const attributes: asset.AssetMap = new Map();
    attributes.set(asset.Tag.ALIAS, this.alias(keyId));
    attributes.set(asset.Tag.SECRET, key);
    attributes.set(asset.Tag.ACCESSIBILITY, asset.Accessibility.DEVICE_UNLOCKED);
    attributes.set(asset.Tag.SYNC_TYPE, asset.SyncType.NEVER);
    // 默认冲突报错：已有密钥不能以相同 keyId 静默覆盖。
    try { await asset.add(attributes); }
    catch { throw new Error('阅读密钥保存失败，请解锁设备并重试；未回落到普通文件'); }
  }

  async load(keyId: string): Promise<Uint8Array> {
    const query: asset.AssetMap = new Map();
    query.set(asset.Tag.ALIAS, this.alias(keyId));
    query.set(asset.Tag.RETURN_TYPE, asset.ReturnType.ALL);
    let rows: asset.AssetMap[];
    try { rows = await asset.query(query); }
    catch { throw new Error('无法读取阅读密钥，请解锁设备或执行恢复'); }
    if (rows.length !== 1) throw new Error('未找到唯一阅读密钥');
    const key = rows[0].get(asset.Tag.SECRET) as Uint8Array;
    if (!key || key.length !== 32) throw new Error('安全存储中的阅读密钥无效');
    return key;
  }

  async remove(keyId: string): Promise<void> {
    const query: asset.AssetMap = new Map(); query.set(asset.Tag.ALIAS, this.alias(keyId));
    try { await asset.remove(query); }
    catch (error) {
      if ((error as BusinessError).code !== asset.ErrorCode.NOT_FOUND) throw new Error('阅读密钥移除失败');
    }
  }
}
