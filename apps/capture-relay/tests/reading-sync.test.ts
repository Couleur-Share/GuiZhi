import { test, type TestContext } from 'node:test';
import assert from 'node:assert/strict';
import { createCipheriv, randomBytes } from 'node:crypto';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { DatabaseSync } from 'node:sqlite';
import { createReadingServer } from '../src/reading-sync/server';
import { ReadingDatabase } from '../src/reading-sync/database';
import { hash } from '../src/reading-sync/validation';
import { readingProtectedHeader } from '../../../packages/shared/utils/reading-sync-format';

const secret = () => randomBytes(32).toString('base64url');
const libraryId = 'reading_library_01', generation = 'reading_generation_01', keyId = 'reading_content_key_01';
function token(kind: string, objectId: string, revision = 1, library = libraryId) {
  const key = randomBytes(32), iv = randomBytes(12);
  const header = Buffer.from(readingProtectedHeader({ libraryId: library, generation, objectId, keyId, kind, revision })).toString('base64url');
  const cipher = createCipheriv('aes-256-gcm', key, iv); cipher.setAAD(Buffer.from(header));
  return [header, '', iv.toString('base64url'), Buffer.concat([cipher.update('合成正文，不得在服务端明文出现'), cipher.final()]).toString('base64url'), cipher.getAuthTag().toString('base64url')].join('.');
}
async function fixture(t: TestContext, limits?: { bytes: number; objects: number; libraries: number }) {
  const root = mkdtempSync(join(tmpdir(), 'guizhi-reading-test-')), file = join(root, 'reading.db');
  const bootstrap = secret(), writer = secret(), reader = secret();
  let service = createReadingServer({ database: file, bootstrapCredential: bootstrap, limits });
  t.after(async () => { await service.app.close(); rmSync(root, { recursive: true, force: true }); });
  const request = async (method: string, path: string, credential = writer, body?: unknown) => service.app.inject({
    method: method as 'GET' | 'POST' | 'PUT' | 'DELETE', url: path,
    headers: { authorization: `Bearer ${credential}`, 'x-guizhi-reading': '1' }, payload: body as object,
  });
  assert.equal((await request('POST', '/v1/reading/libraries', bootstrap, { libraryId, generation, writerCredential: writer })).statusCode, 200);
  return { request, writer, reader, bootstrap, file, db: service.db,
    restart: async () => { await service.app.close(); service = createReadingServer({ database: file, bootstrapCredential: bootstrap, limits }); } };
}
test('阅读同步分库、只读授权、原子发布、响应重放与重启持久性', async t => {
  const f = await fixture(t), base = `/v1/reading/${libraryId}`;
  const article = token('article', 'reading_article_01'), articleHash = hash(article), manifest = token('manifest', 'reading_manifest_01');
  const publish = { sequence: 1, parentDigest: null, manifestToken: manifest, objectDigests: [articleHash] };
  assert.equal((await f.request('PUT', base + '/head', f.reader, publish)).statusCode, 403);
  assert.equal((await f.request('PUT', base + '/head', f.writer, publish)).statusCode, 409);
  assert.equal((await f.request('GET', base + '/head')).json().sequence, 0);
  assert.equal((await f.request('PUT', `${base}/objects/${articleHash}`, f.writer, { token: article })).statusCode, 200);
  assert.equal((await f.request('PUT', `${base}/objects/${articleHash}`, f.writer, { token: article })).statusCode, 200);
  assert.equal((await f.request('GET', `${base}/objects/${articleHash}`)).statusCode, 404, '暂存对象不可读');
  assert.equal((await f.request('PUT', base + '/head', f.writer, publish)).statusCode, 200);
  assert.equal((await f.request('PUT', base + '/head', f.writer, publish)).statusCode, 200);
  assert.equal((await f.request('PUT', base + '/head', f.writer, { ...publish, objectDigests: [] })).statusCode, 409);
  const pairId = 'reading_pairing_01';
  await f.request('POST', `${base}/pairs/${pairId}`);
  assert.equal((await f.request('POST', `${base}/pairs/${pairId}/claim`, f.reader,
    { deviceId: 'reading_device_01', proof: 'a'.repeat(64) })).statusCode, 200);
  assert.equal((await f.request('GET', base + '/head', f.reader)).statusCode, 401);
  const bundle = token('pairing', pairId);
  assert.equal((await f.request('POST', `${base}/pairs/${pairId}/approve`, f.writer,
    { credentialHash: hash(f.reader), bundle })).statusCode, 200);
  assert.equal((await f.request('GET', `${base}/objects/${articleHash}`, f.reader)).json().token, article);
  assert.equal((await f.request('PUT', base + '/head', f.reader, publish)).statusCode, 403);
  await f.restart();
  assert.equal((await f.request('GET', base + '/head', f.reader)).json().digest, hash(manifest));
  assert.equal((await f.request('GET', `${base}/objects/${articleHash}`, f.reader)).json().token, article);
  assert.equal((await f.request('DELETE', base + '/reader')).statusCode, 200);
  assert.equal((await f.request('GET', base + '/head', f.reader)).statusCode, 401);
  assert.equal((await f.request('PUT', base + '/head', f.writer, publish)).json().error, 'key_rotation_required');
  assert.equal(readFileSync(f.file).includes(Buffer.from('合成正文')), false);
});

test('跨库、旧收集凭证、过期配对、非密文和配额失败都保留旧版本', async t => {
  const f = await fixture(t, { bytes: 10000, objects: 1, libraries: 2 }), base = `/v1/reading/${libraryId}`;
  const article = token('article', 'reading_article_01'), articleHash = hash(article);
  await f.request('PUT', `${base}/objects/${articleHash}`, f.writer, { token: article });
  const second = token('image', 'reading_image_01');
  assert.equal((await f.request('PUT', `${base}/objects/${hash(second)}`, f.writer, { token: second })).statusCode, 413);
  assert.equal((await f.request('PUT', `${base}/objects/${hash('plaintext')}`, f.writer, { token: 'plaintext' })).statusCode, 400);
  const other = token('article', 'reading_article_01', 1, 'another_library_01');
  assert.equal((await f.request('PUT', `${base}/objects/${hash(other)}`, f.writer, { token: other })).statusCode, 400);
  assert.equal((await f.request('GET', base + '/head', secret())).statusCode, 401);
  const pair = 'reading_pairing_02'; await f.request('POST', `${base}/pairs/${pair}`);
  f.db.run('UPDATE reading_pairs SET expires=0 WHERE id=?', pair);
  assert.equal((await f.request('POST', `${base}/pairs/${pair}/claim`, f.reader,
    { deviceId: 'reading_device_01', proof: 'a'.repeat(64) })).statusCode, 410);
  const manifest = token('manifest', 'reading_manifest_01');
  await f.request('PUT', base + '/head', f.writer, { sequence: 1, parentDigest: null, manifestToken: manifest, objectDigests: [articleHash] });
  assert.equal((await f.request('PUT', base + '/head', f.writer, { sequence: 1, parentDigest: null,
    manifestToken: token('manifest', 'reading_manifest_01'), objectDigests: [] })).statusCode, 409);
  assert.equal((await f.request('GET', base + '/head')).json().digest, hash(manifest));
});

test('阅读服务拒绝误用已有收集数据库，旧表不受修改', t => {
  const dir = mkdtempSync(join(tmpdir(), 'guizhi-reading-wrong-db-')), file = join(dir, 'capture.db');
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const existing = new DatabaseSync(file); existing.exec('CREATE TABLE captures(id TEXT); INSERT INTO captures VALUES(\'keep\')'); existing.close();
  assert.throws(() => new ReadingDatabase(file), /wrong_database/);
  const read = new DatabaseSync(file); assert.equal(read.prepare('SELECT id FROM captures').get()?.id, 'keep'); read.close();
});

test('断点重试续租旧暂存对象，后续上传回收时不会删除刚确认的对象', async t => {
  const f = await fixture(t), base = `/v1/reading/${libraryId}`;
  const first = token('article', 'reading_article_01'), second = token('image', 'reading_image_01');
  await f.request('PUT', `${base}/objects/${hash(first)}`, f.writer, { token: first });
  f.db.run('UPDATE reading_objects SET created=0 WHERE library=?', libraryId);
  await f.request('PUT', `${base}/objects/${hash(first)}`, f.writer, { token: first });
  await f.request('PUT', `${base}/objects/${hash(second)}`, f.writer, { token: second });
  const manifest = token('manifest', 'reading_manifest_01');
  assert.equal((await f.request('PUT', base + '/head', f.writer, { sequence: 1, parentDigest: null,
    manifestToken: manifest, objectDigests: [hash(first), hash(second)] })).statusCode, 200);
});
