// 协议纯逻辑的唯一来源；鸿蒙副本由 scripts/sync-reading-format.mjs 同步，并由测试逐字核对。
export const READING_MAX_BYTES = 8 * 1024 * 1024;
export const READING_MAX_TOKEN_CHARS = Math.ceil(READING_MAX_BYTES * 4 / 3) + 4096;

export interface ReadingIdentity {
  libraryId: string;
  generation: string;
  objectId: string;
  keyId: string;
  revision: number;
  kind: string;
}

interface ReadingHeader {
  alg: string;
  enc: string;
  typ: string;
  v: number;
  libraryId: string;
  generation: string;
  objectId: string;
  keyId: string;
  revision: number;
  kind: string;
}

export function assertReadingId(value: string): void {
  if (typeof value !== 'string' || !/^[A-Za-z0-9_-]{16,64}$/.test(value)) {
    throw new Error('阅读同步标识无效');
  }
}

export function assertReadingIdentity(value: ReadingIdentity): void {
  if (!value) throw new Error('缺少阅读同步身份');
  assertReadingId(value.libraryId); assertReadingId(value.generation);
  assertReadingId(value.objectId); assertReadingId(value.keyId);
  if (!Number.isSafeInteger(value.revision) || value.revision < 1) throw new Error('阅读同步版本无效');
  if (['article', 'image', 'manifest', 'recovery', 'pairing'].indexOf(value.kind) < 0) throw new Error('阅读同步对象类型无效');
}

// 固定、受限的 JWE profile：拒绝未知算法、重复字段、非规范字段顺序及额外字段。
// 期望身份必须来自可信配对或已认证清单，禁止从待解密对象自身推导。
export function readingProtectedHeader(value: ReadingIdentity): string {
  assertReadingIdentity(value);
  const header: ReadingHeader = {
    alg: 'dir', enc: 'A256GCM', typ: 'guizhi-reading+jwe', v: 1,
    libraryId: value.libraryId, generation: value.generation, objectId: value.objectId,
    keyId: value.keyId, revision: value.revision, kind: value.kind,
  };
  return JSON.stringify(header);
}

export function splitReadingEnvelope(token: string): string[] {
  if (typeof token !== 'string' || token.length > READING_MAX_TOKEN_CHARS) throw new Error('阅读同步密文超限');
  const parts = token.split('.');
  if (parts.length !== 5 || parts[1] !== '' || parts[0].length > 2048 ||
      parts[2].length !== 16 || parts[4].length !== 22) throw new Error('阅读同步密文格式无效');
  for (let i = 0; i < parts.length; i++) {
    if (i === 1) continue;
    if (!/^[A-Za-z0-9_-]+$/.test(parts[i]) || parts[i].length % 4 === 1) throw new Error('阅读同步编码无效');
  }
  return parts;
}

export interface ReadingCheckpoint {
  libraryId: string;
  generation: string;
  sequence: number;
  manifestDigest: string;
}

// 只验证，不推进状态；调用者须在本地数据事务成功后持久化新游标。
export function readingAdvance(current: ReadingCheckpoint, next: ReadingCheckpoint): boolean {
  assertReadingId(current.libraryId); assertReadingId(next.libraryId);
  assertReadingId(current.generation); assertReadingId(next.generation);
  if (!Number.isSafeInteger(current.sequence) || current.sequence < 1 ||
      !Number.isSafeInteger(next.sequence) || next.sequence < 1 ||
      !/^[a-f0-9]{64}$/.test(current.manifestDigest) || !/^[a-f0-9]{64}$/.test(next.manifestDigest)) {
    throw new Error('阅读同步检查点无效');
  }
  if (current.libraryId !== next.libraryId || current.generation !== next.generation) {
    throw new Error('知识库身份或世代已改变，需要重新确认完整同步');
  }
  if (next.sequence < current.sequence) throw new Error('拒绝阅读同步版本回退');
  if (next.sequence === current.sequence) {
    if (next.manifestDigest !== current.manifestDigest) throw new Error('同一同步版本内容冲突');
    return false;
  }
  return true;
}
