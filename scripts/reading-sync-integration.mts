import assert from 'node:assert/strict';
import { mkdtempSync, rmSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { createReadingServer } from '../apps/capture-relay/src/reading-sync/server';
import { ReadingOutbox } from '../packages/core/src/reading-sync/outbox';
import { generateReadingId, generateReadingKey } from '../packages/core/src/reading-sync/crypto';
import { createReadingInvitation, readingClaim, sealPairingKey, openPairingBundle } from '../packages/core/src/reading-sync/pairing';
import { openReadingSnapshot } from '../packages/core/src/reading-sync/snapshot';
import type { ReadingHead, ReadingPairStatus } from '../packages/shared/types/reading-sync';

// 独立合成闭环：真实 SQLite 服务 + PC 密文队列 + 配对密钥交换 + 只读下载解密。
const root = mkdtempSync(join(tmpdir(), 'guizhi-reading-integration-'));
const bootstrap = generateReadingKey().toString('base64url'), writer = generateReadingKey().toString('base64url');
const reader = generateReadingKey().toString('base64url'), key = generateReadingKey();
const identity = { libraryId: generateReadingId(), generation: generateReadingId(), keyId: generateReadingId(),
  objectId: generateReadingId(), kind: 'manifest', revision: 1 };
let server = createReadingServer({ database: join(root, 'reading.db'), bootstrapCredential: bootstrap });
try {
  const base = `/v1/reading/${identity.libraryId}`;
  const request = async <T>(method: 'GET' | 'POST' | 'PUT', route: string, bearer: string, body?: object): Promise<T> => {
    const result = await server.app.inject({ method, url: route, payload: body,
      headers: { authorization: `Bearer ${bearer}`, 'x-guizhi-reading': '1' } });
    assert.equal(result.statusCode, 200, result.body); return result.json();
  };
  await request('POST', '/v1/reading/libraries', bootstrap, { libraryId: identity.libraryId,
    generation: identity.generation, writerCredential: writer });
  const outbox = new ReadingOutbox(join(root, 'outbox.json'), identity);
  const articleId = generateReadingId(), imageId = generateReadingId();
  const objects = [
    { objectId: articleId, kind: 'article' as const, revision: 1, bytes: Buffer.from(JSON.stringify({ title: '合成文章', content: `![图](guizhi-sync://${imageId})` })) },
    { objectId: imageId, kind: 'image' as const, revision: 1, bytes: Buffer.from('synthetic-image-bytes') },
  ];
  const port = { head: () => request<ReadingHead>('GET', base + '/head', writer),
    put: async (digest: string, token: string) => { await request('PUT', `${base}/objects/${digest}`, writer, { token }); },
    publish: (value: object) => request<ReadingHead>('PUT', base + '/head', writer, value) };
  outbox.enqueue(key, objects, '1'.repeat(64)); await outbox.flush(port);
  const invitation = createReadingInvitation('https://reading.example.com', identity.libraryId, identity.generation);
  const claim = readingClaim(invitation, generateReadingId(), reader), pairPath = `${base}/pairs/${invitation.pairId}`;
  await request('POST', pairPath, writer);
  await request('POST', pairPath + '/claim', reader, { deviceId: claim.deviceId, proof: claim.proof });
  const pending = await request<ReadingPairStatus>('GET', pairPath, writer);
  const bundle = sealPairingKey(invitation, pending.claim!, identity.keyId, key, outbox.checkpoint());
  await request('POST', pairPath + '/approve', writer, { credentialHash: claim.credentialHash, bundle });
  const wrapped = await request<{ bundle: string }>('GET', pairPath + '/bundle', reader);
  const paired = openPairingBundle(invitation, claim, identity.keyId, wrapped.bundle), phoneKey = paired.contentKey;
  assert.deepEqual(paired.checkpoint, outbox.checkpoint());
  await server.app.close();
  server = createReadingServer({ database: join(root, 'reading.db'), bootstrapCredential: bootstrap });
  const head = await request<ReadingHead>('GET', base + '/head', reader);
  const opened = await openReadingSnapshot(phoneKey, identity, head.manifestToken!, async entry =>
    (await request<{ token: string }>('GET', `${base}/objects/${entry.ciphertextSha256}`, reader)).token);
  assert.deepEqual(opened, objects);
  // 移出全部文章后，以空清单原子撤回；旧密文不再向手机开放下载。
  const oldRefs = server.db.all<{ digest: string }>('SELECT digest FROM reading_refs');
  outbox.enqueue(key, [], '2'.repeat(64)); await outbox.flush(port);
  const empty = await request<ReadingHead>('GET', base + '/head', reader);
  assert.deepEqual(await openReadingSnapshot(phoneKey, { ...identity, revision: 2 }, empty.manifestToken!, async () => { throw new Error('空清单不应取对象'); }), []);
  const denied = await server.app.inject({ url: `${base}/objects/${oldRefs[0].digest}`,
    headers: { authorization: `Bearer ${reader}`, 'x-guizhi-reading': '1' } });
  assert.equal(denied.statusCode, 404);
  phoneKey.fill(0);
  await server.app.close();
  assert.equal(readFileSync(join(root, 'reading.db')).includes(Buffer.from('合成文章')), false);
  console.log('PASS: 密文上传、配对交换、SQLite 重启、只读下载解密、空清单撤回、服务端无明文。全部使用合成数据。');
} finally { key.fill(0); await server.app.close(); rmSync(root, { recursive: true, force: true }); }
