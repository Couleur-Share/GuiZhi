import { createHash, createHmac, hkdfSync, timingSafeEqual } from 'node:crypto';
import { decryptReadingObject, encryptReadingObject, generateReadingId, generateReadingKey } from './crypto';
import { assertReadingId, type ReadingIdentity } from '../../../shared/utils/reading-sync-format';
import type { ReadingPairClaim } from '../../../shared/types/reading-sync';

export interface ReadingInvitation { origin: string; libraryId: string; generation: string; pairId: string; secret: string; }
export interface ReadingPairCheckpoint { manifestId: string; sequence: number; digest: string; }
function validateCheckpoint(value: ReadingPairCheckpoint): void {
  if (!value) throw new Error('配对阅读检查点缺失');
  assertReadingId(value.manifestId);
  if (!Number.isSafeInteger(value.sequence) || value.sequence < 1 || !/^[a-f0-9]{64}$/.test(value.digest)) throw new Error('配对阅读检查点无效');
}
const hash = (value: string) => createHash('sha256').update(value).digest('hex');
export function readingOrigin(value: string): string {
  const url = new URL(value);
  if (url.protocol !== 'https:' || url.username || url.password || url.pathname !== '/' || url.search || url.hash) {
    throw new Error('请输入 HTTPS 阅读服务根地址');
  }
  return url.origin;
}
function invitationKey(invitation: ReadingInvitation, purpose: string): Buffer {
  readingOrigin(invitation.origin); assertReadingId(invitation.libraryId); assertReadingId(invitation.generation); assertReadingId(invitation.pairId);
  const secret = Buffer.from(invitation.secret, 'base64url');
  if (secret.length !== 32 || secret.toString('base64url') !== invitation.secret) throw new Error('阅读邀请密钥无效');
  // HMAC 证明与 JWE 封装使用不同子密钥；绑定库、世代和一次性配对编号。
  try { return Buffer.from(hkdfSync('sha256', secret, Buffer.from(invitation.pairId),
    Buffer.from(JSON.stringify(['guizhi-reading-pair-v1', purpose, invitation.origin, invitation.libraryId, invitation.generation])), 32)); }
  finally { secret.fill(0); }
}
export function createReadingInvitation(origin: string, libraryId: string, generation: string): ReadingInvitation {
  assertReadingId(libraryId); assertReadingId(generation);
  const secret = generateReadingKey();
  try { return { origin: readingOrigin(origin), libraryId, generation, pairId: generateReadingId(), secret: secret.toString('base64url') }; }
  finally { secret.fill(0); }
}
export function readingInvitationUrl(invitation: ReadingInvitation): string {
  const key = invitationKey(invitation, 'proof'); key.fill(0);
  return `${invitation.origin}/#reading=${Buffer.from(JSON.stringify(invitation)).toString('base64url')}`;
}
export function readingClaim(invitation: ReadingInvitation, deviceId: string, credential: string): ReadingPairClaim {
  assertReadingId(deviceId);
  if (!/^[A-Za-z0-9_-]{43}$/.test(credential) || Buffer.from(credential, 'base64url').toString('base64url') !== credential) throw new Error('阅读设备凭证无效');
  const credentialHash = hash(credential), key = invitationKey(invitation, 'proof');
  try { return { deviceId, credentialHash, proof: createHmac('sha256', key).update(JSON.stringify([deviceId, credentialHash])).digest('hex') }; }
  finally { key.fill(0); }
}
export function verifyReadingClaim(invitation: ReadingInvitation, claim: ReadingPairClaim): void {
  assertReadingId(claim.deviceId);
  if (!/^[a-f0-9]{64}$/.test(claim.credentialHash) || !/^[a-f0-9]{64}$/.test(claim.proof)) throw new Error('阅读配对证明无效');
  const key = invitationKey(invitation, 'proof');
  try {
    const proof = createHmac('sha256', key).update(JSON.stringify([claim.deviceId, claim.credentialHash])).digest();
    if (!timingSafeEqual(proof, Buffer.from(claim.proof, 'hex'))) throw new Error('阅读配对身份校验失败，请重新扫码');
  } finally { key.fill(0); }
}
export function pairingIdentity(invitation: ReadingInvitation, keyId: string): ReadingIdentity {
  return { libraryId: invitation.libraryId, generation: invitation.generation,
    objectId: invitation.pairId, keyId, revision: 1, kind: 'pairing' };
}
export function sealPairingKey(invitation: ReadingInvitation, claim: ReadingPairClaim, keyId: string, contentKey: Uint8Array, checkpoint: ReadingPairCheckpoint): string {
  verifyReadingClaim(invitation, claim);
  validateCheckpoint(checkpoint);
  if (contentKey.length !== 32) throw new Error('阅读内容密钥无效');
  const key = invitationKey(invitation, 'wrap');
  try { return encryptReadingObject(key, pairingIdentity(invitation, keyId),
    Buffer.from(JSON.stringify({ deviceId: claim.deviceId, credentialHash: claim.credentialHash,
      contentKey: Buffer.from(contentKey).toString('base64url'), checkpoint }))); }
  finally { key.fill(0); }
}
export function openPairingBundle(invitation: ReadingInvitation, claim: ReadingPairClaim, keyId: string, token: string): { contentKey: Buffer; checkpoint: ReadingPairCheckpoint } {
  verifyReadingClaim(invitation, claim);
  const key = invitationKey(invitation, 'wrap');
  let plain: Buffer | undefined;
  try {
    plain = decryptReadingObject(key, pairingIdentity(invitation, keyId), token);
    const payload = JSON.parse(plain.toString('utf8')) as { deviceId: string; credentialHash: string; contentKey: string; checkpoint: ReadingPairCheckpoint };
    if (payload.deviceId !== claim.deviceId || payload.credentialHash !== claim.credentialHash || typeof payload.contentKey !== 'string') throw new Error('配对密钥接收者不匹配');
    validateCheckpoint(payload.checkpoint);
    const result = Buffer.from(payload.contentKey, 'base64url');
    if (result.length !== 32 || result.toString('base64url') !== payload.contentKey) { result.fill(0); throw new Error('配对内容密钥无效'); }
    return { contentKey: result, checkpoint: payload.checkpoint };
  } finally { key.fill(0); plain?.fill(0); }
}
