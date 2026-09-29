import { common } from '@kit.AbilityKit';
import { util } from '@kit.ArkTS';
import { cryptoFramework } from '@kit.CryptoArchitectureKit';
import { DraftQueue } from '../core/DraftQueue';
import { Connection, PairingLink } from '../core/Protocol';
import { SentLog } from '../core/SentLog';
import { CredentialStore } from './CredentialStore';
import { LocalStore } from './LocalStore';
import { RelayClient } from './RelayClient';
import { SentLogStore } from './SentLogStore';

export class CaptureRuntime {
  private static opening: Promise<CaptureRuntime> | undefined;
  connection: Connection | undefined;
  client: RelayClient | undefined;
  readonly vault: CredentialStore = new CredentialStore();
  queue: DraftQueue;
  // 已发送记录：近期页的内容预览来源，读写失败不影响收集与发送。
  sentLog: SentLog;
  constructor(queue: DraftQueue, sentLog: SentLog) { this.queue = queue; this.sentLog = sentLog; }

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
    const queue = new DraftQueue(store, await store.load());
    // 发送记录打不开时不抛出，只在 problem 中说明；草稿读取失败仍会中止启动，以免覆盖原数据。
    const sentLog = new SentLog(new SentLogStore(context));
    await sentLog.open();
    const runtime = new CaptureRuntime(queue, sentLog);
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
