// 运行真实 ArkTS 适配代码，系统边界使用 Node 密码实现替身；不代替真机互操作验收。
const test = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { createCipheriv, createDecipheriv, randomBytes } = require('node:crypto');
const { load } = require('./support.cjs');
const fixtures = require('../../../packages/shared/fixtures/reading-sync-v1.json');
const format = load('core/ReadingSyncFormat.ts');
const arkUtil = {
  TextEncoder: class { encodeInto(value) { return new Uint8Array(Buffer.from(value)); } },
  Base64Helper: class {
    encodeToStringSync(bytes) { return Buffer.from(bytes).toString('base64'); }
    decodeSync(text) { return new Uint8Array(Buffer.from(text, 'base64')); }
  },
};

function cryptoAdapter() {
  let cleared = 0;
  const system = {
    CryptoMode: { ENCRYPT_MODE: 1, DECRYPT_MODE: 2 },
    createRandom: () => ({ generateRandomSync: length => ({ data: new Uint8Array(randomBytes(length)) }) }),
    createSymKeyGenerator: algorithm => {
      assert.equal(algorithm, 'AES256');
      return { convertKey: async blob => ({ raw: blob.data, clearMem: () => { cleared++; } }) };
    },
    createCipher: algorithm => {
      assert.equal(algorithm, 'AES256|GCM|NoPadding');
      let cipher, encrypt;
      return {
        init: async (mode, key, params) => {
          assert.equal(params.algName, 'GcmParamsSpec'); assert.equal(params.iv.data.length, 12);
          encrypt = mode === 1;
          cipher = (encrypt ? createCipheriv : createDecipheriv)('aes-256-gcm', key.raw, params.iv.data);
          cipher.setAAD(params.aad.data);
          if (!encrypt) cipher.setAuthTag(params.authTag.data);
        },
        doFinal: async blob => {
          const result = Buffer.concat([cipher.update(blob.data), cipher.final()]);
          return { data: new Uint8Array(encrypt ? Buffer.concat([result, cipher.getAuthTag()]) : result) };
        },
      };
    },
  };
  const { ReadingCrypto } = load('services/ReadingCrypto.ets', {
    '@kit.CryptoArchitectureKit': { cryptoFramework: system }, '@kit.ArkTS': { util: arkUtil },
    '../core/ReadingSyncFormat': format,
  }, { Uint8Array });
  return { crypto: new ReadingCrypto(), cleared: () => cleared };
}

test('鸿蒙加密适配层消费公开向量并生成可由 Node 解密的标准 JWE', async () => {
  const { crypto, cleared } = cryptoAdapter();
  for (const fixture of fixtures) {
    const key = new Uint8Array(Buffer.from(fixture.keyHex, 'hex'));
    const plain = await crypto.decrypt(key, fixture.identity, fixture.token);
    assert.equal(Buffer.from(plain).toString('hex'), fixture.plainHex);
    const token = await crypto.encrypt(key, fixture.identity, plain);
    const [header, , iv, ciphertext, tag] = token.split('.');
    const node = createDecipheriv('aes-256-gcm', key, Buffer.from(iv, 'base64url'));
    node.setAAD(Buffer.from(header)); node.setAuthTag(Buffer.from(tag, 'base64url'));
    assert.equal(Buffer.concat([node.update(Buffer.from(ciphertext, 'base64url')), node.final()]).toString('hex'), fixture.plainHex);
    assert.notEqual(token, await crypto.encrypt(key, fixture.identity, plain));
  }
  assert.equal(cleared(), 12);
});

test('鸿蒙在篡改、错密钥和身份错配时拒绝结果，系统密钥始终清理', async () => {
  const { crypto, cleared } = cryptoAdapter();
  const fixture = fixtures[0], key = new Uint8Array(Buffer.from(fixture.keyHex, 'hex'));
  await assert.rejects(crypto.decrypt(new Uint8Array(32).fill(99), fixture.identity, fixture.token), /认证失败/);
  for (const index of [2, 3, 4]) {
    const parts = fixture.token.split('.'); const bytes = Buffer.from(parts[index], 'base64url');
    bytes[0] ^= 1; parts[index] = bytes.toString('base64url');
    await assert.rejects(crypto.decrypt(key, fixture.identity, parts.join('.')), /认证失败/);
  }
  await assert.rejects(crypto.decrypt(key, { ...fixture.identity, revision: 2 }, fixture.token), /不匹配/);
  assert.equal(cleared(), 4);
  await assert.rejects(crypto.encrypt(new Uint8Array(16), fixture.identity, new Uint8Array([1])));
});

test('鸿蒙测试向量与独立 WebCrypto 夹具保持一致', () => {
  const generated = load('syncdiag/ReadingFixtures.ts', { '../core/ReadingSyncFormat': format });
  assert.equal(JSON.stringify(generated.READING_FIXTURES), JSON.stringify(fixtures));
  const hvigor = readFileSync(require('node:path').join(__dirname, '../entry/hvigorfile.ts'), 'utf8');
  assert.match(hvigor, /getProductName\(\) !== SHARE_SPIKE_PRODUCT/);
});

test('阅读密钥独立存储、仅解锁可读、禁用同步；失败不回落且不覆盖旧密钥', async () => {
  const rows = new Map(); let fail = false;
  const asset = {
    Tag: { ALIAS: 1, SECRET: 2, ACCESSIBILITY: 3, SYNC_TYPE: 4, RETURN_TYPE: 5 },
    Accessibility: { DEVICE_UNLOCKED: 10 }, SyncType: { NEVER: 11 }, ReturnType: { ALL: 12 }, ErrorCode: { NOT_FOUND: 404 },
    add: async attrs => {
      assert.equal(attrs.get(3), 10); assert.equal(attrs.get(4), 11);
      const alias = Buffer.from(attrs.get(1)).toString(); assert.match(alias, /^guizhi\.reading\.key\.v1\./);
      if (fail || rows.has(alias)) throw new Error('synthetic-store-failure');
      rows.set(alias, new Uint8Array(attrs.get(2)));
    },
    query: async query => {
      if (fail) throw new Error('locked');
      const key = rows.get(Buffer.from(query.get(1)).toString());
      return key ? [new Map([[2, new Uint8Array(key)]])] : [];
    },
    remove: async query => {
      if (fail) throw new Error('locked');
      rows.delete(Buffer.from(query.get(1)).toString());
    },
  };
  const { ReadingKeyStore } = load('services/ReadingKeyStore.ets', {
    '@kit.AssetStoreKit': { asset }, '@kit.ArkTS': { util: arkUtil }, '../core/ReadingSyncFormat': format,
  });
  const store = new ReadingKeyStore(), id = 'fixture_store_key', key = new Uint8Array(32).fill(42);
  await store.save(id, key); assert.deepEqual(await store.load(id), key);
  await assert.rejects(store.save(id, new Uint8Array(32))); assert.deepEqual(await store.load(id), key);
  fail = true;
  await assert.rejects(store.load(id)); await assert.rejects(store.remove(id));
  await assert.rejects(store.save('another_store_key', key)); assert.equal(rows.size, 1);
  fail = false; await store.remove(id); assert.equal(rows.size, 0); await assert.rejects(store.load(id));
});
