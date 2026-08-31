import {
  createCipheriv,
  createDecipheriv,
  randomBytes,
  scryptSync,
} from "node:crypto";

const ENCRYPTED_MAGIC = Buffer.from("GZENC1", "ascii");
const NONCE_BYTES = 12;
const TAG_BYTES = 16;
const KEY_BYTES = 32;

export interface PasswordWrappedKey {
  salt: string;
  data: string;
}

export interface RepositoryKeyProtector {
  readonly backend: string;
  isAvailable(): boolean;
  isSecure(): boolean;
  wrap(key: Buffer): Buffer;
  unwrap(wrapped: Buffer): Buffer;
}

function derivePasswordKey(password: string, salt: Buffer): Buffer {
  return scryptSync(password.normalize("NFKC"), salt, KEY_BYTES, {
    N: 32_768,
    r: 8,
    p: 1,
    maxmem: 64 * 1024 * 1024,
  });
}

export function validateRecoveryPassword(password: string): void {
  if (Array.from(password.trim()).length < 12) {
    throw new Error("恢复口令至少需要 12 个字符");
  }
}

export function encryptBuffer(plain: Buffer, key: Buffer): Buffer {
  if (key.length !== KEY_BYTES) throw new Error("仓库密钥长度不合法");
  const nonce = randomBytes(NONCE_BYTES);
  const cipher = createCipheriv("aes-256-gcm", key, nonce);
  const encrypted = Buffer.concat([cipher.update(plain), cipher.final()]);
  return Buffer.concat([
    ENCRYPTED_MAGIC,
    nonce,
    cipher.getAuthTag(),
    encrypted,
  ]);
}

export function decryptBuffer(envelope: Buffer, key: Buffer): Buffer {
  const headerBytes = ENCRYPTED_MAGIC.length + NONCE_BYTES + TAG_BYTES;
  if (
    envelope.length < headerBytes ||
    !envelope.subarray(0, ENCRYPTED_MAGIC.length).equals(ENCRYPTED_MAGIC)
  ) {
    throw new Error("加密对象格式不合法");
  }
  const nonceStart = ENCRYPTED_MAGIC.length;
  const tagStart = nonceStart + NONCE_BYTES;
  const bodyStart = tagStart + TAG_BYTES;
  const decipher = createDecipheriv(
    "aes-256-gcm",
    key,
    envelope.subarray(nonceStart, tagStart),
  );
  decipher.setAuthTag(envelope.subarray(tagStart, bodyStart));
  return Buffer.concat([
    decipher.update(envelope.subarray(bodyStart)),
    decipher.final(),
  ]);
}

export function wrapKeyWithPassword(
  repositoryKey: Buffer,
  password: string,
): PasswordWrappedKey {
  validateRecoveryPassword(password);
  const salt = randomBytes(16);
  return {
    salt: salt.toString("base64"),
    data: encryptBuffer(
      repositoryKey,
      derivePasswordKey(password, salt),
    ).toString("base64"),
  };
}

export function unwrapKeyWithPassword(
  wrapped: PasswordWrappedKey,
  password: string,
): Buffer {
  validateRecoveryPassword(password);
  try {
    return decryptBuffer(
      Buffer.from(wrapped.data, "base64"),
      derivePasswordKey(password, Buffer.from(wrapped.salt, "base64")),
    );
  } catch {
    throw new Error("恢复口令不正确或仓库头已损坏");
  }
}

export function createRepositoryKey(): Buffer {
  return randomBytes(KEY_BYTES);
}
