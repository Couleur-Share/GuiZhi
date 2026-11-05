import type { ReadingPairStatus } from '@guizhi/shared/types/reading-sync';
import { ReadingDatabase } from './database';
import { check, credential, digest, hash, id, inspectToken } from './validation';

interface Pair { id: string; library: string; expires: number; device_id: string | null;
  credential_hash: string | null; proof: string | null; bundle: string | null; active: number; }
export class ReadingPairings {
  constructor(private db: ReadingDatabase) {}
  create(library: string, pairId: string, now = Date.now()): ReadingPairStatus {
    id(pairId);
    this.db.transaction(() => {
      const existing = this.db.get<Pair>('SELECT * FROM reading_pairs WHERE id=?', pairId);
      if (existing) { check(existing.library === library && existing.expires > now, 'pair_conflict', 409); return; }
      check(!this.db.get('SELECT id FROM reading_pairs WHERE library=? AND active=1', library), 'reader_already_paired', 409);
      // 一次只允许一个有效配对；旧未确认请求被取消。
      this.db.run('DELETE FROM reading_pairs WHERE library=? AND active=0', library);
      this.db.run('INSERT INTO reading_pairs(id,library,expires) VALUES(?,?,?)', pairId, library, now + 300000);
    });
    return this.status(library, pairId);
  }
  status(library: string, pairId: string): ReadingPairStatus {
    const pair = this.db.get<Pair>('SELECT * FROM reading_pairs WHERE id=? AND library=?', id(pairId), library);
    check(pair && pair.expires > Date.now(), 'pair_expired', 410);
    return { pairId: pair.id, expiresAt: pair.expires, bundle: pair.bundle,
      claim: pair.device_id ? { deviceId: pair.device_id, credentialHash: pair.credential_hash!, proof: pair.proof! } : null };
  }
  claim(library: string, pairId: string, deviceId: string, readerCredential: string, proof: string) {
    id(deviceId); digest(proof); const credentialHash = hash(credential(readerCredential));
    return this.db.transaction(() => {
      const pair = this.status(library, pairId);
      check(credentialHash !== this.db.library(library).writer_hash, 'credential_conflict', 409);
      if (pair.claim) {
        check(pair.claim.deviceId === deviceId && pair.claim.credentialHash === credentialHash && pair.claim.proof === proof, 'pair_conflict', 409);
        return { claimed: true };
      }
      check(!this.db.get('SELECT id FROM reading_pairs WHERE credential_hash=?', credentialHash), 'credential_conflict', 409);
      this.db.run('UPDATE reading_pairs SET device_id=?,credential_hash=?,proof=? WHERE id=?', deviceId, credentialHash, proof, pairId);
      return { claimed: true };
    });
  }
  approve(library: string, pairId: string, credentialHash: string, bundle: string) {
    return this.db.transaction(() => {
      const pair = this.status(library, pairId), lib = this.db.library(library);
      check(!lib.frozen, 'key_rotation_required', 409);
      check(pair.claim && pair.claim.credentialHash === digest(credentialHash), 'pair_conflict', 409);
      const header = inspectToken(bundle, library, lib.generation);
      check(header.kind === 'pairing' && header.objectId === pairId && header.keyId === lib.key_id, 'invalid_bundle');
      if (pair.bundle) { check(pair.bundle === bundle, 'pair_conflict', 409); return { approved: true }; }
      check(!this.db.get('SELECT id FROM reading_pairs WHERE library=? AND active=1', library), 'reader_already_paired', 409);
      this.db.run('UPDATE reading_pairs SET active=1,bundle=? WHERE id=?', bundle, pairId);
      return { approved: true };
    });
  }
  readerBundle(library: string, pairId: string, raw: string) {
    const pair = this.status(library, pairId);
    check(pair.claim?.credentialHash === hash(credential(raw)), 'unauthorized', 401);
    return { bundle: pair.bundle };
  }
  revoke(library: string) {
    this.db.transaction(() => {
      this.db.run('DELETE FROM reading_pairs WHERE library=?', library);
      // 撤销后冻结发布和再配对；必须显式轮换密钥，不能继续用旧密钥发布。
      this.db.run('UPDATE reading_libraries SET frozen=1 WHERE id=?', library);
    });
    return { revoked: true, rotationRequired: true };
  }
}
