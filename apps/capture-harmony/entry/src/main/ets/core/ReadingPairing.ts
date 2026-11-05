import { ReadingIdentity, assertReadingId, splitReadingEnvelope } from './ReadingSyncFormat';
import { ReadingAnchor, ReadingConnection, ReadingCryptoPort, ReadingInvite, assertAnchor, readingOrigin, validateConnection } from './ReadingModel';

interface PairPayload { deviceId: string; credentialHash: string; contentKey: string; checkpoint: ReadingAnchor; }
export interface ReadingClaim { deviceId: string; proof: string; }

export function parseReadingInvite(raw: string, crypto: ReadingCryptoPort): ReadingInvite {
  if (typeof raw !== 'string' || raw.length > 8192) throw new Error('阅读配对链接过长');
  const match = raw.trim().match(/^(https:\/\/[^\/#?]+)\/#reading=([A-Za-z0-9_-]+)$/);
  if (!match) throw new Error('请扫描电脑“手机阅读同步”的完整二维码，收集配对码不能用于阅读');
  readingOrigin(match[1]);
  const plain = crypto.decode(match[2]);
  try {
    const value = JSON.parse(crypto.text(plain)) as ReadingInvite;
    if (!value || value.origin !== match[1]) throw new Error('阅读服务地址与邀请不匹配');
    [value.libraryId, value.generation, value.pairId].forEach((id: string) => assertReadingId(id));
    const secret = crypto.decode(value.secret); const valid = secret.length === 32; secret.fill(0);
    if (!valid) throw new Error('阅读邀请密钥无效');
    return value;
  } finally { plain.fill(0); }
}
export function newReadingConnection(invitation: ReadingInvite, crypto: ReadingCryptoPort): ReadingConnection {
  const connection = new ReadingConnection();
  connection.origin = invitation.origin; connection.libraryId = invitation.libraryId;
  connection.generation = invitation.generation; connection.pairId = invitation.pairId; connection.secret = invitation.secret;
  connection.deviceId = randomText(crypto); connection.credential = randomText(crypto);
  connection.cacheKey = randomText(crypto); connection.cacheId = randomText(crypto);
  validateConnection(connection, crypto); return connection;
}
function randomText(crypto: ReadingCryptoPort): string {
  const key = crypto.generateKey(); try { return crypto.encode(key); } finally { key.fill(0); }
}
async function pairKey(connection: ReadingConnection, purpose: string, crypto: ReadingCryptoPort): Promise<Uint8Array> {
  const secret = crypto.decode(connection.secret);
  try { return await crypto.derive(secret, connection.pairId,
    JSON.stringify(['guizhi-reading-pair-v1', purpose, connection.origin, connection.libraryId, connection.generation])); }
  finally { secret.fill(0); }
}
export async function readingClaim(connection: ReadingConnection, crypto: ReadingCryptoPort): Promise<ReadingClaim> {
  const key = await pairKey(connection, 'proof', crypto);
  try { return { deviceId: connection.deviceId,
    proof: await crypto.hmac(key, JSON.stringify([connection.deviceId, await crypto.hash(connection.credential)])) }; }
  finally { key.fill(0); }
}
export async function acceptReadingBundle(connection: ReadingConnection, token: string, crypto: ReadingCryptoPort): Promise<ReadingConnection> {
  const parts = splitReadingEnvelope(token);
  const header = JSON.parse(crypto.text(crypto.decode(parts[0]))) as ReadingIdentity;
  // keyId 首次由一次性扫码秘密认证；其他身份全部来自已确认邀请，不能由服务端覆盖。
  assertReadingId(header.keyId);
  const identity: ReadingIdentity = { libraryId: connection.libraryId, generation: connection.generation,
    objectId: connection.pairId, keyId: header.keyId, revision: 1, kind: 'pairing' };
  const key = await pairKey(connection, 'wrap', crypto);
  let plain: Uint8Array | undefined;
  try {
    plain = await crypto.decrypt(key, identity, token);
    const payload = JSON.parse(crypto.text(plain)) as PairPayload;
    if (!payload || payload.deviceId !== connection.deviceId || payload.credentialHash !== await crypto.hash(connection.credential)) throw new Error('配对接收者不匹配');
    assertAnchor(payload.checkpoint);
    const next = JSON.parse(JSON.stringify(connection)) as ReadingConnection;
    next.keyId = header.keyId; next.contentKey = payload.contentKey; next.anchor = payload.checkpoint; next.secret = '';
    validateConnection(next, crypto); return next;
  } finally { key.fill(0); if (plain) plain.fill(0); }
}
