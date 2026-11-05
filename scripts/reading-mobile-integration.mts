import assert from 'node:assert/strict';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createRequire } from 'node:module';
import { createReadingServer } from '../apps/capture-relay/src/reading-sync/server';
import { ReadingOutbox } from '../packages/core/src/reading-sync/outbox';
import { createReadingInvitation, readingInvitationUrl, sealPairingKey } from '../packages/core/src/reading-sync/pairing';
import { generateReadingId, generateReadingKey } from '../packages/core/src/reading-sync/crypto';
import type { ReadingHead, ReadingPairStatus } from '../packages/shared/types/reading-sync';

// PC 真实队列/配对实现 → 真实 HTTP 与 SQLite 服务 → 真实鸿蒙逻辑/缓存适配。
// 鸿蒙系统密码和文件 API 使用 Node 边界替身；不能代替真机互操作和 UI 验收。
const require = createRequire(import.meta.url);
const mobile = require('../apps/capture-harmony/tests/reading-support.cjs');
const root = mkdtempSync(join(tmpdir(), 'guizhi-mobile-e2e-'));
const bootstrap = generateReadingKey().toString('base64url'), writer = generateReadingKey().toString('base64url'), contentKey = generateReadingKey();
const identity = { libraryId: generateReadingId(), generation: generateReadingId(), keyId: generateReadingId(), objectId: generateReadingId(), revision: 1, kind: 'manifest' };
const service = createReadingServer({ database: join(root, 'service.db'), bootstrapCredential: bootstrap });
try {
  await service.app.listen({ host: '127.0.0.1', port: 0 });
  const address = service.app.server.address();
  assert.ok(address && typeof address === 'object');
  const origin = `http://127.0.0.1:${address.port}`, base = `/v1/reading/${identity.libraryId}`;
  const request = async <T>(route: string, bearer: string, method = 'GET', body?: object): Promise<T> => {
    const response = await fetch(origin + route, { method, headers: { authorization: `Bearer ${bearer}`, 'x-guizhi-reading': '1', ...(body ? { 'content-type': 'application/json' } : {}) },
      body: body ? JSON.stringify(body) : undefined });
    assert.equal(response.status, 200); return await response.json() as T;
  };
  await request('/v1/reading/libraries', bootstrap, 'POST', { libraryId: identity.libraryId, generation: identity.generation, writerCredential: writer });
  const articleId = generateReadingId(), imageId = generateReadingId();
  const article = mobile.article(articleId, `电脑关闭后离线搜索。\n![合成图片](guizhi-sync://${imageId})`);
  const imageBytes = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/lX0AAAAASUVORK5CYII=', 'base64');
  const outbox = new ReadingOutbox(join(root, 'pc-outbox.json'), identity);
  outbox.enqueue(contentKey, [{ objectId: articleId, kind: 'article', revision: 1, bytes: Buffer.from(JSON.stringify(article)) },
    { objectId: imageId, kind: 'image', revision: 1, bytes: imageBytes }], 'f'.repeat(64));
  await outbox.flush({ head: () => request<ReadingHead>(base + '/head', writer),
    put: async (digest, token) => { await request(`${base}/objects/${digest}`, writer, 'PUT', { token }); },
    publish: value => request<ReadingHead>(base + '/head', writer, 'PUT', value) });
  const invitation = createReadingInvitation('https://reading.example.com', identity.libraryId, identity.generation);
  await request(`${base}/pairs/${invitation.pairId}`, writer, 'POST');
  let connection = mobile.pairing.newReadingConnection(mobile.pairing.parseReadingInvite(readingInvitationUrl(invitation), mobile.cipher), mobile.cipher);
  const http = { RequestMethod: { GET: 'GET', POST: 'POST' }, HttpDataType: { STRING: 0 }, createHttp: () => {
    const abort = new AbortController();
    return { destroy: () => abort.abort(), request: async (url: string, options: { method: string; header: object; extraData: string; maxRedirects: number }) => {
      assert.ok(url.startsWith(invitation.origin + '/v1/reading/')); assert.equal(options.maxRedirects, 0);
      const response = await fetch(origin + url.slice(invitation.origin.length), { method: options.method, headers: options.header as Record<string, string>,
        body: options.extraData || undefined, signal: abort.signal, redirect: 'manual' });
      return { responseCode: response.status, result: await response.text() };
    } };
  } };
  const { ReadingClient } = mobile.load('services/ReadingClient.ets', { '@kit.NetworkKit': { http }, '../core/ReadingModel': mobile.model }, { setTimeout, clearTimeout });
  let client = new ReadingClient(connection);
  await client.claim(await mobile.pairing.readingClaim(connection, mobile.cipher));
  const pending = await request<ReadingPairStatus>(`${base}/pairs/${invitation.pairId}`, writer);
  const bundle = sealPairingKey(invitation, pending.claim!, identity.keyId, contentKey, outbox.checkpoint());
  await request(`${base}/pairs/${invitation.pairId}/approve`, writer, 'POST', { credentialHash: pending.claim!.credentialHash, bundle });
  connection = await mobile.pairing.acceptReadingBundle(connection, await client.bundle(), mobile.cipher);
  assert.equal(connection.anchor.digest, outbox.checkpoint().digest);
  client = new ReadingClient(connection);
  const cache = mobile.nativeCache({ filesDir: root }, connection);
  let library = new mobile.library.ReadingLibrary(connection, mobile.cipher, cache, client);
  // 从这里起 PC 不再参与；手机仅与独立服务通信。
  await library.sync(() => {});
  assert.equal(library.articles[0].content, article.content);
  assert.equal(await library.image(imageId), null);
  assert.equal((await library.saveImages(articleId, () => {})).saved, 1);
  await service.app.close();
  // 真正停止 HTTP 服务并重建客户端对象，验证从磁盘密文读取而非内存假离线。
  library.lock();
  library = new mobile.library.ReadingLibrary(connection, mobile.cipher, mobile.nativeCache({ filesDir: root }, connection), new ReadingClient(connection));
  await library.open();
  assert.equal(mobile.model.findArticles(library.articles, '电脑 离线搜索', '', '').length, 1);
  assert.equal(Buffer.compare(Buffer.from(await library.image(imageId)), imageBytes), 0);
  await assert.rejects(library.sync(() => {}), /网络不可用/);
  assert.equal(library.articles.length, 1);
  console.log('PASS: PC 加密上传 → 独立 HTTP 服务 → 鸿蒙配对/正文/图片下载 → 停服重建客户端后离线搜索与图片读取。全程合成数据。');
} finally { contentKey.fill(0); await service.app.close(); rmSync(root, { recursive: true, force: true }); }
