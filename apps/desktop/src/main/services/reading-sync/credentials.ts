import { safeStorage } from 'electron';
import { existsSync, readFileSync } from 'node:fs';
import { atomicReadingFile } from '@guizhi/core/reading-sync/outbox';
import { readingOrigin, type ReadingInvitation } from '@guizhi/core/reading-sync/pairing';
import { assertReadingId } from '@guizhi/shared/utils/reading-sync-format';

export interface ReadingConnection {
  origin: string; libraryId: string; generation: string; keyId: string; manifestId: string;
  writerCredential: string; contentKey: string; idKey: string; collectionIds: string[];
  provisioned: boolean; invitation?: ReadingInvitation; invitationExpiresAt?: number; approvedBundle?: string;
}
export class ReadingCredentials {
  constructor(private file: string) {}
  private requireSecure() {
    if (!safeStorage.isEncryptionAvailable() || (process.platform === 'linux' && safeStorage.getSelectedStorageBackend() === 'basic_text')) {
      throw new Error('系统安全存储不可用，无法开启阅读同步');
    }
  }
  read(): ReadingConnection | null {
    if (!existsSync(this.file)) return null;
    this.requireSecure();
    try {
      const stored = JSON.parse(readFileSync(this.file, 'utf8')) as { encrypted: string };
      const value = JSON.parse(safeStorage.decryptString(Buffer.from(stored.encrypted, 'base64'))) as ReadingConnection;
      readingOrigin(value.origin);
      for (const field of [value.libraryId, value.generation, value.keyId, value.manifestId]) assertReadingId(field);
      for (const key of [value.writerCredential, value.contentKey, value.idKey]) {
        if (typeof key !== 'string' || !/^[A-Za-z0-9_-]{43}$/.test(key) || Buffer.from(key, 'base64url').toString('base64url') !== key) throw new Error();
      }
      if (!Array.isArray(value.collectionIds) || value.collectionIds.length > 100 || typeof value.provisioned !== 'boolean') throw new Error();
      return value;
    } catch { throw new Error('阅读同步凭据损坏或无法解锁，已暂停同步'); }
  }
  write(value: ReadingConnection) {
    this.requireSecure();
    atomicReadingFile(this.file, { encrypted: safeStorage.encryptString(JSON.stringify(value)).toString('base64') });
  }
}
