import crypto from 'crypto';
import type Database from './database/sqlite';

/**
 * 主密码存储。
 *
 * 历史格式把 scrypt 派生出的密钥（`hash` 字段）原样写进 settings 表——
 * 那既是校验值也是 AES 密钥，等于密钥和密文躺在同一个文件里，scrypt 的
 * 口令加固失去意义。新格式只存密钥的 sha256（`verifier`），密钥本身
 * 只在解锁期间留在内存。老记录在下次成功解锁时就地升级。
 */
interface StoredMasterPassword {
  salt: string; // base64
  /** 新格式：sha256(derivedKey) 的 base64 */
  verifier?: string;
  /** 历史格式：派生密钥本身的 base64，解锁成功后会被替换为 verifier */
  hash?: string;
}

let inMemoryKey: Buffer | null = null;
let isUnlocked = false;

const SETTINGS_KEY = 'master_password';
const ALGO = 'aes-256-gcm';

function deriveKey(password: string, salt: Buffer): Buffer {
  // Use scrypt to derive 32-byte key
  // 使用 scrypt 派生 32 字节密钥
  return crypto.scryptSync(password, salt, 32);
}

function toVerifier(key: Buffer): Buffer {
  return crypto.createHash('sha256').update(key).digest();
}

function isStoredMasterPassword(value: unknown): value is StoredMasterPassword {
  if (!value || typeof value !== 'object') {
    return false;
  }

  const candidate = value as Partial<StoredMasterPassword>;
  return (
    typeof candidate.salt === 'string' &&
    (typeof candidate.verifier === 'string' || typeof candidate.hash === 'string')
  );
}

interface DecodedMasterPassword {
  salt: Buffer;
  expected: Buffer;
  /** true 表示存的是密钥本身（历史格式），需要在解锁后升级 */
  isLegacyKeyMaterial: boolean;
}

function decodeStored(
  stored: StoredMasterPassword,
): DecodedMasterPassword | null {
  try {
    const salt = Buffer.from(stored.salt, 'base64');
    const raw = stored.verifier ?? stored.hash;
    if (salt.length !== 16 || !raw) {
      return null;
    }
    const expected = Buffer.from(raw, 'base64');
    if (expected.length !== 32) {
      return null;
    }

    return {
      salt,
      expected,
      isLegacyKeyMaterial: stored.verifier === undefined,
    };
  } catch {
    return null;
  }
}

/** 口令是否匹配。历史格式比对派生密钥，新格式比对密钥的 sha256。 */
function matchesStored(key: Buffer, decoded: DecodedMasterPassword): boolean {
  const candidate = decoded.isLegacyKeyMaterial ? key : toVerifier(key);
  return crypto.timingSafeEqual(candidate, decoded.expected);
}

function getStored(db: Database.Database): StoredMasterPassword | null {
  try {
    const row = db
      .prepare('SELECT value FROM settings WHERE key = ?')
      .get(SETTINGS_KEY) as { value: string } | undefined;
    if (!row) return null;
    const parsed = JSON.parse(row.value) as unknown;
    if (!isStoredMasterPassword(parsed) || !decodeStored(parsed)) {
      console.error('Invalid stored master password payload');
      return null;
    }
    return parsed;
  } catch (e) {
    console.error('Failed to read master password from settings:', e);
    return null;
  }
}

function saveStored(db: Database.Database, stored: StoredMasterPassword) {
  db.prepare('INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)').run(
    SETTINGS_KEY,
    JSON.stringify(stored),
  );
}

function clearUnlockedState() {
  inMemoryKey = null;
  isUnlocked = false;
}

export function hasMasterPasswordConfigured(db: Database.Database): boolean {
  return !!getStored(db);
}

export function setMasterPassword(db: Database.Database, password: string) {
  const salt = crypto.randomBytes(16);
  const key = deriveKey(password, salt);
  saveStored(db, {
    salt: salt.toString('base64'),
    verifier: toVerifier(key).toString('base64'),
  });
  inMemoryKey = key;
  isUnlocked = true;
}

export function changeMasterPassword(
  db: Database.Database,
  oldPassword: string,
  newPassword: string,
): boolean {
  const stored = getStored(db);
  if (!stored) {
    clearUnlockedState();
    return false;
  }

  const decoded = decodeStored(stored);
  if (!decoded) {
    clearUnlockedState();
    return false;
  }

  if (!matchesStored(deriveKey(oldPassword, decoded.salt), decoded)) {
    clearUnlockedState();
    return false;
  }

  setMasterPassword(db, newPassword);
  return true;
}

export function unlock(db: Database.Database, password: string): boolean {
  const stored = getStored(db);
  if (!stored) {
    clearUnlockedState();
    return false;
  }

  const decoded = decodeStored(stored);
  if (!decoded) {
    clearUnlockedState();
    return false;
  }

  const derived = deriveKey(password, decoded.salt);
  const ok = matchesStored(derived, decoded);
  if (!ok) {
    clearUnlockedState();
    return false;
  }

  inMemoryKey = derived;
  isUnlocked = true;

  // 历史记录里存的是密钥本身，验证通过后就地换成校验值，密钥不再落盘
  if (decoded.isLegacyKeyMaterial) {
    saveStored(db, {
      salt: decoded.salt.toString('base64'),
      verifier: toVerifier(derived).toString('base64'),
    });
  }
  return true;
}

export function lock() {
  clearUnlockedState();
}

export function getKey(): Buffer | null {
  return inMemoryKey;
}

export function securityStatus(db: Database.Database) {
  return {
    configured: hasMasterPasswordConfigured(db),
    unlocked: isUnlocked,
  };
}

export function getUnlockedKey(): Buffer | null {
  return inMemoryKey;
}

export function encryptText(plain: string, key: Buffer): string {
  const iv = crypto.randomBytes(12);
  const cipher = crypto.createCipheriv(ALGO, key, iv);
  const enc = Buffer.concat([cipher.update(plain, 'utf8'), cipher.final()]);
  const tag = cipher.getAuthTag();
  const payload = Buffer.concat([iv, tag, enc]).toString('base64');
  return `ENC::${payload}`;
}

export function decryptText(data: string, key: Buffer): string | null {
  if (!data || !data.startsWith('ENC::')) return data;
  try {
    const buf = Buffer.from(data.slice(5), 'base64');
    const iv = buf.subarray(0, 12);
    const tag = buf.subarray(12, 28);
    const enc = buf.subarray(28);
    const decipher = crypto.createDecipheriv(ALGO, key, iv);
    decipher.setAuthTag(tag);
    const dec = Buffer.concat([decipher.update(enc), decipher.final()]);
    return dec.toString('utf8');
  } catch (e) {
    console.error('Decrypt failed', e);
    return null;
  }
}
