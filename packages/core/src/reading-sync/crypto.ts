import { createCipheriv, createDecipheriv, randomBytes } from 'node:crypto';
import {
  READING_MAX_BYTES, readingProtectedHeader, splitReadingEnvelope,
  type ReadingIdentity,
} from '../../../shared/utils/reading-sync-format';

export const generateReadingKey = (): Buffer => randomBytes(32);
export const generateReadingId = (): string => randomBytes(24).toString('base64url');

function assertKey(key: Uint8Array): void {
  if (!(key instanceof Uint8Array) || key.byteLength !== 32) throw new Error('阅读密钥必须为 256 位');
}

function decode(value: string): Buffer {
  const result = Buffer.from(value, 'base64url');
  if (result.toString('base64url') !== value) throw new Error('阅读同步编码非规范');
  return result;
}

// 不向业务调用方开放 IV 注入，所有新密文使用系统随机数；重试应复用完整密文。
export function encryptReadingObject(key: Uint8Array, identity: ReadingIdentity, plain: Uint8Array): string {
  assertKey(key);
  if (!(plain instanceof Uint8Array) || plain.byteLength < 1 || plain.byteLength > READING_MAX_BYTES) {
    throw new Error('阅读同步对象为空或超限');
  }
  const header = Buffer.from(readingProtectedHeader(identity)).toString('base64url');
  const iv = randomBytes(12);
  const cipher = createCipheriv('aes-256-gcm', key, iv, { authTagLength: 16 });
  cipher.setAAD(Buffer.from(header, 'ascii'));
  const ciphertext = Buffer.concat([cipher.update(plain), cipher.final()]);
  return [header, '', iv.toString('base64url'), ciphertext.toString('base64url'), cipher.getAuthTag().toString('base64url')].join('.');
}

export function decryptReadingObject(key: Uint8Array, expected: ReadingIdentity, token: string): Buffer {
  assertKey(key);
  const parts = splitReadingEnvelope(token);
  const header = Buffer.from(readingProtectedHeader(expected)).toString('base64url');
  if (parts[0] !== header) throw new Error('阅读同步身份或协议不匹配');
  const iv = decode(parts[2]), ciphertext = decode(parts[3]), tag = decode(parts[4]);
  if (ciphertext.byteLength > READING_MAX_BYTES) throw new Error('阅读同步对象超限');
  const decipher = createDecipheriv('aes-256-gcm', key, iv, { authTagLength: 16 });
  decipher.setAAD(Buffer.from(header, 'ascii')); decipher.setAuthTag(tag);
  const pending = decipher.update(ciphertext);
  try {
    // 认证完成前不返回 update 的明文。
    const final = decipher.final();
    return Buffer.concat([pending, final]);
  } catch {
    throw new Error('阅读同步认证失败，内容或密钥不匹配');
  } finally { pending.fill(0); }
}

const RECOVERY_PREFIX = 'guizhi-rk1.';

export function createRecoveryKey(): string {
  const key = generateReadingKey();
  try { return RECOVERY_PREFIX + key.toString('base64url'); }
  finally { key.fill(0); }
}

function recoveryKey(text: string): Buffer {
  if (typeof text !== 'string' || !text.startsWith(RECOVERY_PREFIX) || text.length !== RECOVERY_PREFIX.length + 43) {
    throw new Error('恢复密钥格式无效');
  }
  const key = decode(text.slice(RECOVERY_PREFIX.length)); assertKey(key); return key;
}

export function wrapReadingKey(contentKey: Uint8Array, recovery: string, identity: ReadingIdentity): string {
  assertKey(contentKey);
  if (identity.kind !== 'recovery') throw new Error('恢复封装用途无效');
  const key = recoveryKey(recovery);
  try { return encryptReadingObject(key, identity, contentKey); }
  finally { key.fill(0); }
}

export function unwrapReadingKey(token: string, recovery: string, expected: ReadingIdentity): Buffer {
  if (expected.kind !== 'recovery') throw new Error('恢复封装用途无效');
  const key = recoveryKey(recovery);
  try {
    const result = decryptReadingObject(key, expected, token);
    if (result.length !== 32) { result.fill(0); throw new Error('恢复内容密钥长度无效'); }
    return result;
  } finally { key.fill(0); }
}
