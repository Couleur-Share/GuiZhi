import { assertReadingId, assertReadingIdentity, READING_MAX_BYTES, type ReadingIdentity } from './reading-sync-format';

export interface ReadingManifestEntry {
  objectId: string;
  keyId: string;
  revision: number;
  kind: 'article' | 'image';
  ciphertextBytes: number;
  ciphertextSha256: string;
}

export interface ReadingManifest {
  schema: 1;
  mode: 'snapshot';
  libraryId: string;
  generation: string;
  sequence: number;
  entries: ReadingManifestEntry[];
}

function exactKeys(value: unknown, keys: string[]): asserts value is Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value) ||
      Object.keys(value).sort().join(',') !== [...keys].sort().join(',')) throw new Error('阅读清单结构无效');
}

// 只在 JWE 认证后调用。完整清单省略的旧条目代表撤回；必须整批落盘再切换可见版本。
// 上线前由写入端身份认证保证清单来源；仅持有内容密钥不等于拥有上传权限。
export function parseReadingManifest(json: string, expected: ReadingIdentity): ReadingManifest {
  assertReadingIdentity(expected);
  if (expected.kind !== 'manifest' || typeof json !== 'string' || json.length > 4 * 1024 * 1024) {
    throw new Error('阅读清单用途或长度无效');
  }
  const value: unknown = JSON.parse(json);
  exactKeys(value, ['schema', 'mode', 'libraryId', 'generation', 'sequence', 'entries']);
  if (value.schema !== 1 || value.mode !== 'snapshot' || value.libraryId !== expected.libraryId ||
      value.generation !== expected.generation || value.sequence !== expected.revision ||
      !Array.isArray(value.entries) || value.entries.length > 10000) throw new Error('阅读清单身份或容量无效');
  const ids = new Set<string>();
  const entries: ReadingManifestEntry[] = value.entries.map((item: unknown) => {
    exactKeys(item, ['objectId', 'keyId', 'revision', 'kind', 'ciphertextBytes', 'ciphertextSha256']);
    assertReadingId(item.objectId as string); assertReadingId(item.keyId as string);
    if (ids.has(item.objectId as string) || !Number.isSafeInteger(item.revision) || (item.revision as number) < 1 ||
        (item.kind !== 'article' && item.kind !== 'image') || !Number.isSafeInteger(item.ciphertextBytes) ||
        (item.ciphertextBytes as number) < 1 || (item.ciphertextBytes as number) > Math.ceil(READING_MAX_BYTES * 4 / 3) + 4096 ||
        typeof item.ciphertextSha256 !== 'string' || !/^[a-f0-9]{64}$/.test(item.ciphertextSha256)) {
      throw new Error('阅读清单对象无效或重复');
    }
    ids.add(item.objectId as string);
    return { objectId: item.objectId as string, keyId: item.keyId as string, revision: item.revision as number,
      kind: item.kind, ciphertextBytes: item.ciphertextBytes as number, ciphertextSha256: item.ciphertextSha256 };
  });
  return { schema: 1, mode: 'snapshot', libraryId: expected.libraryId, generation: expected.generation,
    sequence: expected.revision, entries };
}
