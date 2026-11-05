import { existsSync, mkdirSync, openSync, writeFileSync, fsyncSync, closeSync, renameSync, readFileSync, rmSync } from 'node:fs';
import { dirname } from 'node:path';
import { randomUUID } from 'node:crypto';
import { sealReadingSnapshot, readingDigest, type ReadingObjectInput } from './snapshot';
import { assertReadingIdentity, type ReadingIdentity } from '../../../shared/utils/reading-sync-format';
import type { ReadingHead, ReadingPublish } from '../../../shared/types/reading-sync';

export function atomicReadingFile(file: string, value: unknown): void {
  mkdirSync(dirname(file), { recursive: true, mode: 0o700 });
  const temporary = `${file}.${randomUUID()}.tmp`;
  try {
    const fd = openSync(temporary, 'wx', 0o600);
    try { writeFileSync(fd, JSON.stringify(value)); fsyncSync(fd); }
    finally { closeSync(fd); }
    renameSync(temporary, file);
  } finally { rmSync(temporary, { force: true }); }
}
interface Pending { sourceDigest: string; publish: ReadingPublish; objects: [string, string][]; uploaded: number; }
interface State { schema: number; libraryId: string; generation: string; sequence: number; digest: string | null;
  sourceDigest: string | null; pending: Pending | null; }
export interface ReadingUploadPort {
  head(): Promise<ReadingHead>;
  put(digest: string, token: string): Promise<void>;
  publish(value: ReadingPublish): Promise<ReadingHead>;
}
export class ReadingOutbox {
  private state: State;
  private running = false;
  private uploadStartedAt = Date.now();
  constructor(private file: string, private identity: ReadingIdentity) {
    assertReadingIdentity(identity);
    const empty: State = { schema: 1, libraryId: identity.libraryId, generation: identity.generation,
      sequence: 0, digest: null, sourceDigest: null, pending: null };
    try { this.state = existsSync(file) ? JSON.parse(readFileSync(file, 'utf8')) as State : empty; }
    catch { throw new Error('阅读上传队列损坏，已暂停同步'); }
    const state = this.state;
    if (state.schema !== 1 || state.libraryId !== identity.libraryId || state.generation !== identity.generation ||
        !Number.isSafeInteger(state.sequence) || state.sequence < 0 ||
        (state.sequence === 0 ? state.digest !== null : !/^[a-f0-9]{64}$/.test(state.digest ?? ''))) throw new Error('阅读上传队列身份或版本无效');
    if (state.pending) {
      const pending = state.pending;
      if (pending.publish.sequence !== state.sequence + 1 || pending.publish.parentDigest !== state.digest ||
          !Array.isArray(pending.objects) || pending.objects.length > 10000 ||
          !Number.isSafeInteger(pending.uploaded) || pending.uploaded < 0 || pending.uploaded > pending.objects.length ||
          JSON.stringify(pending.objects.map(row => row[0]).sort()) !== JSON.stringify([...pending.publish.objectDigests].sort()) ||
          pending.objects.some(row => row.length !== 2 || readingDigest(row[1]) !== row[0])) throw new Error('待发送阅读快照损坏，已暂停同步');
      if (existsSync(this.file + '.progress')) {
        let progress: { digest: string; uploaded: number; startedAt: number };
        try { progress = JSON.parse(readFileSync(this.file + '.progress', 'utf8')); }
        catch { throw new Error('阅读上传进度损坏，已暂停同步'); }
        if (progress.digest === readingDigest(pending.publish.manifestToken)) {
          if (!Number.isSafeInteger(progress.uploaded) || progress.uploaded < 0 || progress.uploaded > pending.objects.length) {
            throw new Error('阅读上传进度无效，已暂停同步');
          }
          if (!Number.isSafeInteger(progress.startedAt) || progress.startedAt > Date.now()) throw new Error('阅读上传进度时间无效');
          // 云端暂存保留一天；中断超过半天必须重验整批，避免续传引用已被回收的对象。
          if (Date.now() - progress.startedAt < 12 * 3600000) {
            pending.uploaded = progress.uploaded;
            this.uploadStartedAt = progress.startedAt;
          } else pending.uploaded = 0;
        }
      }
    }
  }
  status() { return { sequence: this.state.sequence, pending: this.state.pending !== null, busy: this.running }; }
  checkpoint() {
    if (!this.state.digest || this.state.sequence < 1) throw new Error('请先发布阅读快照');
    return { manifestId: this.identity.objectId, sequence: this.state.sequence, digest: this.state.digest };
  }
  private save(state: State) { atomicReadingFile(this.file, state); this.state = state; }
  enqueue(key: Uint8Array, objects: ReadingObjectInput[], sourceDigest: string): boolean {
    if (this.running || this.state.pending) throw new Error('已有待发送快照，请先重试完成');
    if (!/^[a-f0-9]{64}$/.test(sourceDigest)) throw new Error('阅读数据指纹无效');
    if (this.state.sourceDigest === sourceDigest) return false;
    const identity = { ...this.identity, revision: this.state.sequence + 1 };
    const batch = sealReadingSnapshot(key, identity, objects);
    const encrypted = [...batch.objects.values()].map(token => [readingDigest(token), token] as [string, string]);
    const pending: Pending = { sourceDigest, objects: encrypted, uploaded: 0,
      publish: { sequence: identity.revision, parentDigest: this.state.digest, manifestToken: batch.manifestToken,
        objectDigests: encrypted.map(row => row[0]) } };
    this.uploadStartedAt = Date.now();
    this.save({ ...this.state, pending }); return true;
  }
  async flush(port: ReadingUploadPort): Promise<void> {
    if (this.running) throw new Error('阅读同步正在进行');
    this.running = true;
    try {
      const remote = await port.head(), pending = this.state.pending;
      if (remote.generation !== this.identity.generation) throw new Error('云端库世代已改变，请重新建立同步');
      // 提交已成功但响应丢失：只接受本地保留的同一批密文，不接受服务端任意更新。
      if (pending && remote.sequence === pending.publish.sequence && remote.digest === readingDigest(pending.publish.manifestToken)) {
        this.acknowledge(remote, pending); return;
      }
      if (remote.sequence !== this.state.sequence || remote.digest !== this.state.digest) throw new Error('本地与云端版本不一致，已暂停；可能恢复过旧备份');
      if (!pending) return;
      for (let index = pending.uploaded; index < pending.objects.length; index++) {
        const [digest, token] = pending.objects[index];
        await port.put(digest, token);
        // 逐件落盘进度，限流或断网后不再从头上传，避免大批次永远卡在首批限额。
        atomicReadingFile(this.file + '.progress', { digest: readingDigest(pending.publish.manifestToken), uploaded: index + 1, startedAt: this.uploadStartedAt });
        pending.uploaded = index + 1;
      }
      const committed = await port.publish(pending.publish);
      this.acknowledge(committed, pending);
    } finally { this.running = false; }
  }
  private acknowledge(head: ReadingHead, pending: Pending) {
    if (head.generation !== this.identity.generation || head.sequence !== pending.publish.sequence ||
        head.digest !== readingDigest(pending.publish.manifestToken)) throw new Error('云端提交回执无效，保留队列重试');
    this.save({ ...this.state, sequence: head.sequence, digest: head.digest, sourceDigest: pending.sourceDigest, pending: null });
  }
}
