import { createHash } from 'node:crypto';
import { encryptReadingObject, decryptReadingObject } from './crypto';
import { assertReadingIdentity, READING_MAX_BYTES, type ReadingIdentity } from '../../../shared/utils/reading-sync-format';
import { parseReadingManifest, type ReadingManifest, type ReadingManifestEntry } from '../../../shared/utils/reading-sync-manifest';

const MAX_SNAPSHOT_BYTES = 64 * 1024 * 1024;
export const readingDigest = (token: string): string => createHash('sha256').update(token, 'utf8').digest('hex');

export interface ReadingObjectInput {
  objectId: string;
  revision: number;
  kind: 'article' | 'image';
  bytes: Uint8Array;
}

export interface SealedReadingSnapshot {
  manifestToken: string;
  manifestDigest: string;
  objects: Map<string, string>;
}

// 阶段 1 的有界快照组装；outbox 应持久化返回的整批密文，重试不能重新加密。
export function sealReadingSnapshot(key: Uint8Array, identity: ReadingIdentity, input: ReadingObjectInput[]): SealedReadingSnapshot {
  assertReadingIdentity(identity);
  if (identity.kind !== 'manifest' || input.length > 10000) throw new Error('阅读快照用途或条数无效');
  let total = 0;
  const ids = new Set<string>();
  for (const object of input) {
    if (!object || !(object.bytes instanceof Uint8Array) || object.bytes.length < 1 || object.bytes.length > READING_MAX_BYTES ||
        !['article', 'image'].includes(object.kind) || ids.has(object.objectId) || object.objectId === identity.objectId) {
      throw new Error('阅读快照对象无效或重复');
    }
    assertReadingIdentity({ ...identity, objectId: object.objectId, revision: object.revision, kind: object.kind });
    ids.add(object.objectId); total += Math.ceil(object.bytes.length * 4 / 3) + 4096;
    if (total > MAX_SNAPSHOT_BYTES) throw new Error('阅读快照总容量超限');
  }
  const objects = new Map<string, string>();
  const entries: ReadingManifestEntry[] = [];
  for (const object of input) {
    const token = encryptReadingObject(key, { ...identity, objectId: object.objectId,
      revision: object.revision, kind: object.kind }, object.bytes);
    objects.set(object.objectId, token);
    entries.push({ objectId: object.objectId, keyId: identity.keyId, revision: object.revision, kind: object.kind,
      ciphertextBytes: Buffer.byteLength(token), ciphertextSha256: readingDigest(token) });
  }
  const manifest: ReadingManifest = { schema: 1, mode: 'snapshot', libraryId: identity.libraryId,
    generation: identity.generation, sequence: identity.revision, entries };
  const manifestToken = encryptReadingObject(key, identity, Buffer.from(JSON.stringify(manifest)));
  return { objects, manifestToken, manifestDigest: readingDigest(manifestToken) };
}

// 合成验证/后续下载器的基础：整批通过后才返回；调用者仍须事务提交数据与游标。
// 大库后续分批落入不可见暂存区，不能把这个内存实现直接当成无限容量生产下载器。
export async function openReadingSnapshot(key: Uint8Array, expected: ReadingIdentity, token: string,
  loadObject: (entry: ReadingManifestEntry) => Promise<string>): Promise<ReadingObjectInput[]> {
  const decodedManifest = decryptReadingObject(key, expected, token);
  let manifest: ReadingManifest;
  try { manifest = parseReadingManifest(decodedManifest.toString('utf8'), expected); }
  finally { decodedManifest.fill(0); }
  if (manifest.entries.reduce((sum, entry) => sum + entry.ciphertextBytes, 0) > MAX_SNAPSHOT_BYTES) {
    throw new Error('阅读快照总容量超限');
  }
  const pending: ReadingObjectInput[] = [];
  try {
    for (const entry of manifest.entries) {
      if (entry.keyId !== expected.keyId || entry.objectId === expected.objectId) throw new Error('快照对象密钥或身份不匹配');
      const object = await loadObject(entry);
      if (typeof object !== 'string' || Buffer.byteLength(object) !== entry.ciphertextBytes ||
          readingDigest(object) !== entry.ciphertextSha256) throw new Error('快照对象缺失或完整性校验失败');
      const bytes = decryptReadingObject(key, { ...expected, objectId: entry.objectId,
        revision: entry.revision, keyId: entry.keyId, kind: entry.kind }, object);
      pending.push({ objectId: entry.objectId, revision: entry.revision, kind: entry.kind, bytes });
    }
    return pending;
  } catch (error) {
    for (const object of pending) object.bytes.fill(0);
    throw error;
  }
}
