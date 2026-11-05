// 真实客户端代码 + Node 平台边界替身；不模拟 ArkUI 或宣称真机密码能力通过。
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const { load } = require('./support.cjs');
const format = load('core/ReadingSyncFormat.ts');
const model = load('core/ReadingModel.ts', { './ReadingSyncFormat': format });
const pairing = load('core/ReadingPairing.ts', { './ReadingModel': model, './ReadingSyncFormat': format });
const reader = load('core/ReadingReader.ts');
const library = load('core/ReadingLibrary.ts', { './ReadingModel': model, './ReadingReader': reader });
const util = {
  TextEncoder: class { encodeInto(text) { return new Uint8Array(Buffer.from(text)); } },
  TextDecoder: class { decodeToString(bytes) { return new TextDecoder('utf-8', { fatal: true }).decode(bytes); } },
  Base64Helper: class { encodeToStringSync(bytes) { return Buffer.from(bytes).toString('base64'); } decodeSync(value) { return new Uint8Array(Buffer.from(value, 'base64')); } },
};
const system = {
  CryptoMode: { ENCRYPT_MODE: 1, DECRYPT_MODE: 2 },
  createRandom: () => ({ generateRandomSync: n => ({ data: new Uint8Array(crypto.randomBytes(n)) }) }),
  createSymKeyGenerator: algorithm => {
    assert.ok(['AES256', 'HMAC'].includes(algorithm));
    return { convertKey: async blob => ({ raw: Buffer.from(blob.data), clearMem() { this.raw.fill(0); } }) };
  },
  createCipher: algorithm => {
    assert.equal(algorithm, 'AES256|GCM|NoPadding'); let cipher, encrypt;
    return { init: async (mode, key, params) => {
      encrypt = mode === 1; cipher = (encrypt ? crypto.createCipheriv : crypto.createDecipheriv)('aes-256-gcm', key.raw, params.iv.data);
      cipher.setAAD(params.aad.data); if (!encrypt) cipher.setAuthTag(params.authTag.data);
    }, doFinal: async blob => {
      const result = Buffer.concat([cipher.update(blob.data), cipher.final()]);
      return { data: new Uint8Array(encrypt ? Buffer.concat([result, cipher.getAuthTag()]) : result) };
    } };
  },
  createMd: algorithm => { assert.equal(algorithm, 'SHA256'); const md = crypto.createHash('sha256');
    return { update: async blob => { md.update(blob.data); }, digest: async () => ({ data: new Uint8Array(md.digest()) }) }; },
  createMac: algorithm => { assert.equal(algorithm, 'SHA256'); let mac;
    return { init: async key => { mac = crypto.createHmac('sha256', key.raw); }, update: async blob => { mac.update(blob.data); }, doFinal: async () => ({ data: new Uint8Array(mac.digest()) }) }; },
  createKdf: algorithm => { assert.equal(algorithm, 'HKDF|SHA256');
    return { generateSecret: async spec => { assert.equal(spec.algName, 'HKDF'); assert.equal(spec.keySize, 32);
      return { data: new Uint8Array(crypto.hkdfSync('sha256', spec.key, spec.salt, spec.info, spec.keySize)) }; } }; },
};
const cryptoModule = load('services/ReadingCrypto.ets', { '@kit.CryptoArchitectureKit': { cryptoFramework: system },
  '@kit.ArkTS': { util }, '../core/ReadingSyncFormat': format }, { Uint8Array });
const cipher = new cryptoModule.ReadingCrypto();
const id = () => crypto.randomBytes(18).toString('base64url');
function connection() {
  const invite = { origin: 'https://reading.example.com', libraryId: id(), generation: id(), pairId: id(), secret: id() };
  invite.secret = crypto.randomBytes(32).toString('base64url');
  const value = pairing.newReadingConnection(invite, cipher);
  value.keyId = id(); value.contentKey = crypto.randomBytes(32).toString('base64url');
  value.anchor = { manifestId: id(), sequence: 1, digest: 'a'.repeat(64) };
  return value;
}
function article(articleId = id(), text = '归知本地知识库正文') {
  return { schema: 1, id: articleId, title: '阅读测试', content: text, summary: '合成摘要', itemType: 'note', status: 'active', collectionName: '合成知识库', tags: ['离线'], sourceUrl: null };
}
async function snapshot(connection, articles, sequence = 1, images = []) {
  const key = cipher.decode(connection.contentKey), objects = new Map(), entries = [];
  for (const object of [...articles.map(article => ({ objectId: article.id, kind: 'article', bytes: cipher.bytes(JSON.stringify(article)) })), ...images]) {
    const token = await cipher.encrypt(key, { libraryId: connection.libraryId, generation: connection.generation, keyId: connection.keyId,
      objectId: object.objectId, kind: object.kind, revision: sequence }, object.bytes);
    const digest = await cipher.hash(token); objects.set(digest, token);
    entries.push({ objectId: object.objectId, keyId: connection.keyId, kind: object.kind, revision: sequence, ciphertextBytes: token.length, ciphertextSha256: digest });
  }
  const manifest = { schema: 1, mode: 'snapshot', libraryId: connection.libraryId, generation: connection.generation, sequence, entries };
  const token = await cipher.encrypt(key, model.manifestIdentity(connection, sequence), cipher.bytes(JSON.stringify(manifest)));
  key.fill(0);
  const head = { generation: connection.generation, keyId: connection.keyId, sequence, digest: await cipher.hash(token), manifestToken: token };
  if (sequence === 1) connection.anchor.digest = head.digest;
  return { head, objects, manifest };
}
function memoryCache() {
  return { state: null, objects: new Map(), commits: 0, failCommit: false, failPut: false,
    async load() { return this.state; }, async commit(value) { if (this.failCommit) throw new Error('合成磁盘满'); this.state = value; this.commits++; },
    async object(digest) { return this.objects.get(digest) || null; },
    async put(digest, token) { if (this.failPut) throw new Error('合成缓存失败'); this.objects.set(digest, token); },
    async prune(keep) { for (const key of this.objects.keys()) if (!keep.includes(key)) this.objects.delete(key); },
  };
}
function remote(data) {
  return { data, online: true, failObject: '', requests: [], cancel() {},
    async head() { if (!this.online) throw new Error('合成断网'); return this.data.head; },
    async object(digest) { this.requests.push(digest); if (!this.online || digest === this.failObject) throw new Error('合成对象缺失');
      if (!this.data.objects.has(digest)) throw new Error('404'); return this.data.objects.get(digest); },
  };
}
function nativeCache(context, connection, overrides = {}) {
  const fileIo = {
    OpenMode: { CREATE: 1, WRITE_ONLY: 2, TRUNC: 4 }, accessSync: fs.existsSync, mkdirSync: fs.mkdirSync,
    statSync: fs.statSync, readTextSync: file => fs.readFileSync(file, 'utf8'), listFileSync: fs.readdirSync,
    openSync: file => ({ fd: fs.openSync(file, 'w') }), writeSync: (fd, bytes) => fs.writeSync(fd, Buffer.from(bytes)),
    fsyncSync: fs.fsyncSync, closeSync: file => fs.closeSync(file.fd), renameSync: fs.renameSync, unlinkSync: fs.unlinkSync,
    rmdirSync: fs.rmdirSync, ...overrides,
  };
  const module = load('services/ReadingCacheStore.ets', { '@kit.CoreFileKit': { fileIo }, '../core/ReadingSyncFormat': format,
    './ReadingCrypto': cryptoModule }, { Uint8Array });
  return new module.ReadingCacheStore(context, connection);
}
module.exports = { load, format, model, pairing, reader, library, cipher, cryptoModule, util, connection, article, snapshot, memoryCache, remote, nativeCache, id };
