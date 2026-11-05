import { ReadingCrypto } from '../services/ReadingCrypto';
import { ReadingKeyStore } from '../services/ReadingKeyStore';
import { READING_FIXTURES } from './ReadingFixtures';
import { ReadingIdentity } from '../core/ReadingSyncFormat';

function fromHex(text: string): Uint8Array {
  const out = new Uint8Array(text.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(text.substring(i * 2, i * 2 + 2), 16);
  return out;
}

function hex(bytes: Uint8Array): string {
  let result = '';
  for (let i = 0; i < bytes.length; i++) result += bytes[i].toString(16).padStart(2, '0');
  return result;
}

// 无网络、无真实文章、无正式凭据；输出只含公开夹具密文及通过/失败状态。
export async function runReadingProbe(): Promise<string> {
  const crypto = new ReadingCrypto();
  let report = '阅读同步诊断 v1（仅虚构资料）\n';
  for (let i = 0; i < READING_FIXTURES.length; i++) {
    const fixture = READING_FIXTURES[i];
    const key = fromHex(fixture.keyHex);
    try {
      const plain = await crypto.decrypt(key, fixture.identity, fixture.token);
      if (hex(plain) !== fixture.plainHex) throw new Error('跨端解密结果不一致');
      const token = await crypto.encrypt(key, fixture.identity, plain);
      if (hex(await crypto.decrypt(key, fixture.identity, token)) !== fixture.plainHex) throw new Error('本机回读不一致');
      const wrongKey = key.slice(); wrongKey[0] ^= 1;
      let rejected = false;
      try { await crypto.decrypt(wrongKey, fixture.identity, token); } catch { rejected = true; }
      wrongKey.fill(0);
      if (!rejected) throw new Error('错误密钥未被拒绝');
      const parts = token.split('.');
      parts[3] = (parts[3].charAt(0) === 'A' ? 'B' : 'A') + parts[3].substring(1);
      rejected = false;
      try { await crypto.decrypt(key, fixture.identity, parts.join('.')); } catch { rejected = true; }
      if (!rejected) throw new Error('篡改密文未被拒绝');
      const wrongVersion: ReadingIdentity = {
        libraryId: fixture.identity.libraryId, generation: fixture.identity.generation,
        objectId: fixture.identity.objectId, keyId: fixture.identity.keyId,
        revision: fixture.identity.revision + 1, kind: fixture.identity.kind,
      };
      rejected = false;
      try { await crypto.decrypt(key, wrongVersion, token); } catch { rejected = true; }
      if (!rejected) throw new Error('版本错配未被拒绝');
      report += `PASS ${fixture.name}\nROUNDTRIP ${fixture.name} ${token}\n`;
    } finally { key.fill(0); }
  }
  const store = new ReadingKeyStore();
  const key = crypto.generateKey();
  const keyId = 'probe_' + hex(crypto.generateKey()).substring(0, 32);
  let stored = false;
  try {
    await store.save(keyId, key); stored = true;
    const loaded = await store.load(keyId);
    const equal = hex(loaded) === hex(key); loaded.fill(0);
    if (!equal) throw new Error('安全存储回读不一致');
    report += 'PASS asset-store-write-read\n';
  } finally {
    key.fill(0);
    // 仅清理由本轮成功创建的随机别名，避免任何既有用户密钥。
    if (stored) await store.remove(keyId);
  }
  report += 'PASS asset-store-cleanup\n';
  return report;
}
