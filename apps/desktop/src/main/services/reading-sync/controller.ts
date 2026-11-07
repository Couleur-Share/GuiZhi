import path from 'node:path';
import { existsSync } from 'node:fs';
import type Database from '@guizhi/db/adapter';
import { generateReadingId, generateReadingKey, createRecoveryKey, wrapReadingKey } from '@guizhi/core/reading-sync/crypto';
import { ReadingOutbox, atomicReadingFile, type ReadingUploadPort } from '@guizhi/core/reading-sync/outbox';
import { createReadingInvitation, readingInvitationUrl, readingOrigin, sealPairingKey } from '@guizhi/core/reading-sync/pairing';
import type { ReadingHead, ReadingPairStatus, ReadingDesktopStatus } from '@guizhi/shared/types/reading-sync';
import { getImagesDir, getUserDataPath } from '../../runtime-paths';
import { ReadingCredentials, type ReadingConnection } from './credentials';
import { readingRequest } from './transport';
import { readSelectedLibrary } from './source';

export class ReadingController {
  private root = path.join(getUserDataPath(), '.machine', 'reading-sync');
  private credentials = new ReadingCredentials(path.join(this.root, 'credentials.json'));
  private abort = new AbortController();
  private busy = false;
  private lastError: string | null = null;
  constructor(private db: Database.Database) {}
  private connection(): ReadingConnection {
    const config = this.credentials.read(); if (!config) throw new Error('请先开启手机阅读同步'); return config;
  }
  private identity(config: ReadingConnection) { return { libraryId: config.libraryId, generation: config.generation,
    keyId: config.keyId, objectId: config.manifestId, kind: 'manifest', revision: 1 }; }
  private outbox(config: ReadingConnection) { return new ReadingOutbox(path.join(this.root, config.libraryId + '.json'), this.identity(config)); }
  private request<T>(config: ReadingConnection, suffix: string, method = 'GET', body?: unknown): Promise<T> {
    return readingRequest(config.origin, `/v1/reading/${config.libraryId}${suffix}`, config.writerCredential, method, body, this.abort.signal);
  }
  status(): ReadingDesktopStatus {
    const config = this.credentials.read();
    const state = config ? this.outbox(config).status() : { sequence: 0, pending: false };
    return { available: true, configured: Boolean(config?.provisioned), origin: config?.origin ?? null, collectionIds: config?.collectionIds ?? [],
      sequence: state.sequence, pending: state.pending, busy: this.busy,
      error: existsSync(path.join(this.root, 'paused.json')) ? '阅读同步已冻结（数据恢复、目录切换或设备撤销）。恢复与密钥轮换将在后续阶段提供。' : this.lastError };
  }
  stop(pause: boolean) {
    this.abort.abort();
    if (pause && existsSync(path.join(this.root, 'credentials.json'))) {
      atomicReadingFile(path.join(this.root, 'paused.json'), { paused: true });
    }
  }
  async perform(action: () => Promise<unknown>) {
    if (this.busy) throw new Error('阅读同步正在进行，请稍后');
    if (existsSync(path.join(this.root, 'paused.json'))) throw new Error('阅读同步已暂停，恢复与密钥轮换将在后续阶段提供');
    this.busy = true; this.abort = new AbortController();
    try { const value = await action(); this.lastError = null; return value; }
    catch (error) { this.lastError = error instanceof Error ? error.message : '阅读同步失败'; throw error; }
    finally { this.busy = false; }
  }
  async activate(origin: string, bootstrapCredential: string) {
    origin = readingOrigin(origin);
    let config = this.credentials.read();
    if (config && config.origin !== origin) throw new Error('已有阅读同步连接，不能直接切换服务');
    if (!config) {
      config = { origin, libraryId: generateReadingId(), generation: generateReadingId(), keyId: generateReadingId(), manifestId: generateReadingId(),
        writerCredential: generateReadingKey().toString('base64url'), contentKey: generateReadingKey().toString('base64url'),
        idKey: generateReadingKey().toString('base64url'), collectionIds: [], provisioned: false };
      // 先安全落盘；创建请求失败后复用同一身份，避免孤立云库。
      this.credentials.write(config);
    }
    if (!config.provisioned) {
      await readingRequest(origin, '/v1/reading/libraries', bootstrapCredential, 'POST', {
        libraryId: config.libraryId, generation: config.generation, writerCredential: config.writerCredential,
      }, this.abort.signal);
      config.provisioned = true; this.credentials.write(config);
    }
    return this.status();
  }
  configure(collectionIds: string[]) {
    const config = this.connection();
    if (this.outbox(config).status().pending) throw new Error('请先完成或重试待发送快照，再修改同步范围');
    // 查询验证范围与资源，但不上传。即使为空也保留明确选择，后续手动同步才撤回云端内容。
    const key = Buffer.from(config.idKey, 'base64url');
    try { readSelectedLibrary(this.db, collectionIds, getImagesDir(), key); }
    finally { key.fill(0); }
    config.collectionIds = collectionIds; this.credentials.write(config); return this.status();
  }
  async sync() {
    const config = this.connection(); if (!config.provisioned) throw new Error('阅读服务尚未激活成功');
    const outbox = this.outbox(config), port: ReadingUploadPort = {
      head: () => this.request<ReadingHead>(config, '/head'),
      put: async (digest, token) => { await this.request(config, `/objects/${digest}`, 'PUT', { token }); },
      publish: value => this.request<ReadingHead>(config, '/head', 'PUT', value),
    };
    await outbox.flush(port); this.abort.signal.throwIfAborted();
    const key = Buffer.from(config.contentKey, 'base64url'), idKey = Buffer.from(config.idKey, 'base64url');
    try {
      const source = readSelectedLibrary(this.db, config.collectionIds, getImagesDir(), idKey);
      const revision = outbox.status().sequence + 1;
      outbox.enqueue(key, source.objects.map(object => ({ ...object, revision })), source.sourceDigest);
      await outbox.flush(port);
    } finally { key.fill(0); idKey.fill(0); }
    return this.status();
  }
  async pair() {
    const config = this.connection();
    if (this.outbox(config).status().sequence === 0) throw new Error('请先同步一次所选合集');
    if (!config.invitation || (config.invitationExpiresAt ?? 0) <= Date.now()) {
      config.invitation = createReadingInvitation(config.origin, config.libraryId, config.generation);
      config.invitationExpiresAt = Date.now() + 300000;
      delete config.approvedBundle; this.credentials.write(config);
    }
    const result = await this.request<ReadingPairStatus>(config, `/pairs/${config.invitation.pairId}`, 'POST');
    return { url: readingInvitationUrl(config.invitation), expiresAt: result.expiresAt };
  }
  async pairStatus() {
    const config = this.connection();
    if (!config.invitation) return null;
    const pair = await this.request<ReadingPairStatus>(config, `/pairs/${config.invitation.pairId}`);
    // 普通状态不返回密钥、设备凭证或配对密文。
    return { pairId: pair.pairId, deviceId: pair.claim?.deviceId ?? null, approved: pair.bundle !== null };
  }
  async approve(deviceId: string) {
    const config = this.connection(); if (!config.invitation) throw new Error('请先生成阅读配对邀请');
    const pair = await this.request<ReadingPairStatus>(config, `/pairs/${config.invitation.pairId}`);
    if (!pair.claim || pair.claim.deviceId !== deviceId) throw new Error('待确认设备已变化，请刷新');
    if (!config.approvedBundle) {
      const key = Buffer.from(config.contentKey, 'base64url');
      try { config.approvedBundle = sealPairingKey(config.invitation, pair.claim, config.keyId, key, this.outbox(config).checkpoint()); }
      finally { key.fill(0); }
      this.credentials.write(config);
    }
    await this.request(config, `/pairs/${config.invitation.pairId}/approve`, 'POST', { credentialHash: pair.claim.credentialHash, bundle: config.approvedBundle });
    return { approved: true };
  }
  recovery() {
    const config = this.connection(), recoveryKey = createRecoveryKey(), key = Buffer.from(config.contentKey, 'base64url');
    try { return { recoveryKey, keyId: config.keyId, libraryId: config.libraryId, generation: config.generation,
      bundle: wrapReadingKey(key, recoveryKey, { ...this.identity(config), objectId: config.keyId, kind: 'recovery' }) }; }
    finally { key.fill(0); }
  }
  async revoke() {
    const config = this.connection();
    await this.request(config, '/reader', 'DELETE');
    delete config.invitation; delete config.approvedBundle;
    this.credentials.write(config);
    // 密钥轮换在后续恢复/撤销阶段提供；此处明确冻结，不能带旧密钥继续上传。
    atomicReadingFile(path.join(this.root, 'paused.json'), { paused: true, reason: 'reader-revoked' });
    return { revoked: true };
  }
}
