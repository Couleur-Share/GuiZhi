import { common } from '@kit.AbilityKit';
import { util } from '@kit.ArkTS';
import { cryptoFramework } from '@kit.CryptoArchitectureKit';
import { DraftQueue } from '../core/DraftQueue';
import { Connection, PairingLink } from '../core/Protocol';
import { CredentialStore } from './CredentialStore';
import { LocalStore } from './LocalStore';
import { RelayClient } from './RelayClient';

export class CaptureRuntime {
  private static opening: Promise<CaptureRuntime> | undefined;
  connection: Connection | undefined;
  client: RelayClient | undefined;
  readonly vault: CredentialStore = new CredentialStore();
  queue: DraftQueue;
  constructor(queue: DraftQueue) { this.queue = queue; }

  static open(context: common.Context): Promise<CaptureRuntime> {
    if (!CaptureRuntime.opening) {
      CaptureRuntime.opening = CaptureRuntime.create(context).catch((error: Error) => {
        CaptureRuntime.opening = undefined; throw error;
      });
    }
    return CaptureRuntime.opening;
  }

  private static async create(context: common.Context): Promise<CaptureRuntime> {
    const store = await LocalStore.open(context);
    const runtime = new CaptureRuntime(new DraftQueue(store, await store.load()));
    runtime.connection = await runtime.vault.load();
    if (runtime.connection) runtime.client = new RelayClient(runtime.connection);
    return runtime;
  }

  async pair(link: PairingLink, name: string, progress: (message: string) => void = () => {}): Promise<void> {
    if (!name.trim() || name.length > 60) throw new Error('请输入 1–60 个字符的设备名称');
    if (this.connection && this.connection.pairingId !== link.pairingId) throw new Error('请先解除当前连接，再配对另一台电脑');
    if (!this.connection) {
      const connection = new Connection();
      connection.origin = link.origin; connection.pairingId = link.pairingId;
      connection.nonce = link.nonce; connection.name = name.trim();
      const bytes = cryptoFramework.createRandom().generateRandomSync(32).data;
      connection.credential = new util.Base64Helper().encodeToStringSync(bytes).replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '');
      progress('正在安全保存连接…');
      await this.vault.save(connection); // 先存系统安全存储，响应丢失后沿用同一凭证。
      this.connection = connection; this.client = new RelayClient(connection);
    }
    if (!this.client) throw new Error('连接未准备好');
    progress('正在向电脑提交配对请求…');
    await this.client.claim();
  }

  async disconnect(): Promise<void> {
    if (this.client) await this.client.disconnect(); // 服务端撤销成功之前保留凭证以便重试。
    await this.vault.clear(); this.connection = undefined; this.client = undefined;
  }

  async forget(): Promise<void> {
    // 用户明确确认后才可本地移除；界面告知仍需在电脑撤销。
    await this.vault.clear(); this.connection = undefined; this.client = undefined;
  }
}
