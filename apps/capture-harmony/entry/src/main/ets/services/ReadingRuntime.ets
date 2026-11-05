import { common, bundleManager } from '@kit.AbilityKit';
import { ReadingConnection, ReadingInvite } from '../core/ReadingModel';
import { acceptReadingBundle, newReadingConnection, readingClaim } from '../core/ReadingPairing';
import { ReadingLibrary } from '../core/ReadingLibrary';
import { ReadingCrypto } from './ReadingCrypto';
import { ReadingConnectionStore } from './ReadingConnectionStore';
import { ReadingCacheStore } from './ReadingCacheStore';
import { ReadingClient } from './ReadingClient';

export function readingTrialEnabled(): boolean {
  try { return bundleManager.getBundleInfoForSelfSync(bundleManager.BundleFlag.GET_BUNDLE_INFO_DEFAULT).name.endsWith('.dev'); }
  catch { return false; }
}
export class ReadingRuntime {
  connection: ReadingConnection | null = null;
  library: ReadingLibrary | null = null;
  private store: ReadingConnectionStore = new ReadingConnectionStore();
  private crypto: ReadingCrypto = new ReadingCrypto();
  private client: ReadingClient | null = null;
  private cache: ReadingCacheStore | null = null;
  private epoch: number = 0;
  private context: common.UIAbilityContext;
  constructor(context: common.UIAbilityContext) {
    this.context = context;
    if (!readingTrialEnabled()) throw new Error('手机阅读当前仅在独立开发包中开放');
  }
  async open(): Promise<void> {
    const epoch = this.epoch;
    const connection = await this.store.load();
    if (epoch !== this.epoch) throw new Error('阅读加载已取消');
    this.connection = connection;
    if (!connection) return;
    this.client = new ReadingClient(connection);
    if (connection.anchor) {
      this.cache = new ReadingCacheStore(this.context, connection);
      this.library = new ReadingLibrary(connection, this.crypto, this.cache, this.client);
      await this.library.open();
    }
  }
  async pair(invitation: ReadingInvite): Promise<void> {
    if (this.connection?.anchor) throw new Error('已有阅读连接，请先清除本机连接；云端设备需由电脑撤销');
    const epoch = this.epoch;
    const next = newReadingConnection(invitation, this.crypto);
    // 保存后再联网；响应丢失可复用同一凭证，重试不会生成第二台设备。
    await this.store.save(next);
    if (epoch !== this.epoch) throw new Error('阅读配对已取消，已保存待确认连接');
    this.connection = next; this.client = new ReadingClient(next);
    await this.retryPair();
  }
  async retryPair(): Promise<boolean> {
    if (!this.connection || !this.client) throw new Error('请先扫描阅读配对码');
    if (this.connection.anchor) return true;
    const epoch = this.epoch;
    const claim = await readingClaim(this.connection, this.crypto);
    if (epoch !== this.epoch) throw new Error('阅读配对已取消');
    await this.client.claim(claim);
    const token = await this.client.bundle();
    if (epoch !== this.epoch) throw new Error('阅读配对已取消');
    if (token === null) return false;
    const next = await acceptReadingBundle(this.connection, token, this.crypto);
    if (epoch !== this.epoch) throw new Error('阅读配对已取消');
    await this.store.save(next);
    if (epoch !== this.epoch) throw new Error('阅读配对已取消，确认结果已安全保存');
    this.connection = next; this.client = new ReadingClient(next);
    this.cache = new ReadingCacheStore(this.context, next);
    this.library = new ReadingLibrary(next, this.crypto, this.cache, this.client);
    return true;
  }
  lock(): void {
    this.epoch++; this.library?.lock(); this.client?.cancel();
    this.library = null; this.connection = null; this.client = null; this.cache = null;
  }
  async forget(): Promise<void> {
    if (this.library?.busy) throw new Error('请先等待阅读下载结束');
    // 先清密文缓存，再删除安全凭据；清理失败时保留密钥以便重试。
    if (this.connection?.anchor) new ReadingCacheStore(this.context, this.connection).clear();
    await this.store.clear(); this.lock();
  }
}
