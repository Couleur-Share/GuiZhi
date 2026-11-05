import { createHash } from 'node:crypto';
import { assertReadingId, readingProtectedHeader, splitReadingEnvelope, type ReadingIdentity } from '@guizhi/shared/utils/reading-sync-format';

export class ReadingError extends Error {
  constructor(public code: string, public statusCode = 400) { super(code); }
}
export function check(value: unknown, code: string, status = 400): asserts value {
  if (!value) throw new ReadingError(code, status);
}
export const hash = (text: string) => createHash('sha256').update(text).digest('hex');
export function id(value: unknown): string {
  check(typeof value === 'string', 'invalid_id');
  try { assertReadingId(value); } catch { throw new ReadingError('invalid_id'); }
  return value;
}
export function digest(value: unknown): string {
  check(typeof value === 'string' && /^[a-f0-9]{64}$/.test(value), 'invalid_digest'); return value;
}
export function credential(value: unknown): string {
  check(typeof value === 'string' && /^[A-Za-z0-9_-]{43}$/.test(value) &&
    Buffer.from(value, 'base64url').toString('base64url') === value, 'invalid_credential'); return value;
}
export function objectBody(value: unknown, keys: string[]): Record<string, unknown> {
  check(value && typeof value === 'object' && !Array.isArray(value), 'invalid_body');
  check(Object.keys(value).sort().join(',') === [...keys].sort().join(','), 'invalid_body');
  return value as Record<string, unknown>;
}
// 服务端只能检查密文外壳，不能判断密文是否能解开；认证失败由客户端处理。
export function inspectToken(value: unknown, libraryId: string, generation: string): ReadingIdentity {
  check(typeof value === 'string', 'invalid_ciphertext');
  try {
    const parts = splitReadingEnvelope(value);
    const header = JSON.parse(Buffer.from(parts[0], 'base64url').toString('utf8')) as ReadingIdentity;
    check(Buffer.from(readingProtectedHeader(header)).toString('base64url') === parts[0], 'invalid_ciphertext');
    for (const index of [2, 3, 4]) check(Buffer.from(parts[index], 'base64url').toString('base64url') === parts[index], 'invalid_ciphertext');
    check(header.libraryId === libraryId && header.generation === generation, 'wrong_library');
    return header;
  } catch (error) { if (error instanceof ReadingError) throw error; throw new ReadingError('invalid_ciphertext'); }
}
