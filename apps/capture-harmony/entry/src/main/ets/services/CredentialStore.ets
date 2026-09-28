import { asset } from '@kit.AssetStoreKit';
import { util } from '@kit.ArkTS';
import { BusinessError } from '@kit.BasicServicesKit';
import { Connection } from '../core/Protocol';

const ALIAS = new util.TextEncoder().encodeInto('guizhi.capture.connection.v1');

export class CredentialStore {
  async load(): Promise<Connection | undefined> {
    const query: asset.AssetMap = new Map();
    query.set(asset.Tag.ALIAS, ALIAS);
    query.set(asset.Tag.RETURN_TYPE, asset.ReturnType.ALL);
    let rows: asset.AssetMap[];
    try { rows = await asset.query(query); }
    catch (error) {
      if ((error as BusinessError).code === asset.ErrorCode.NOT_FOUND) return undefined;
      throw new Error('无法读取系统安全存储，请解锁手机后重试');
    }
    if (rows.length === 0) return undefined;
    const bytes = rows[0].get(asset.Tag.SECRET) as Uint8Array;
    const connection = JSON.parse(new util.TextDecoder().decodeToString(bytes)) as Connection;
    if (!/^https:\/\/[a-z0-9.-]+(?::[0-9]{1,5})?$/i.test(connection.origin) ||
      !/^[A-Za-z0-9_-]{43}$/.test(connection.credential) || !connection.pairingId) {
      throw new Error('连接数据损坏，请在系统设置清除归知收集的数据后重新配对');
    }
    return connection;
  }

  async save(connection: Connection): Promise<void> {
    const attributes: asset.AssetMap = new Map();
    attributes.set(asset.Tag.ALIAS, ALIAS);
    attributes.set(asset.Tag.SECRET, new util.TextEncoder().encodeInto(JSON.stringify(connection)));
    attributes.set(asset.Tag.ACCESSIBILITY, asset.Accessibility.DEVICE_UNLOCKED);
    attributes.set(asset.Tag.SYNC_TYPE, asset.SyncType.NEVER);
    attributes.set(asset.Tag.CONFLICT_RESOLUTION, asset.ConflictResolution.OVERWRITE);
    try { await asset.add(attributes); }
    catch { throw new Error('系统安全存储写入失败，未发起配对；请解锁手机后重试'); }
  }

  async clear(): Promise<void> {
    const query: asset.AssetMap = new Map(); query.set(asset.Tag.ALIAS, ALIAS);
    try { await asset.remove(query); }
    catch (error) {
      if ((error as BusinessError).code !== asset.ErrorCode.NOT_FOUND) throw new Error('未能清除本机连接凭证，请重试');
    }
  }
}
