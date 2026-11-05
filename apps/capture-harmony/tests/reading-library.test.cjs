const test = require('node:test');
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const h = require('./reading-support.cjs');
const { ReadingLibrary } = h.library;
const noop = () => {};

test('鸿蒙配对 HKDF/HMAC 与 Node 一致；包绑定接收者、密钥和可信首次检查点', async () => {
  const c = h.connection(), invite = { origin: c.origin, libraryId: c.libraryId, generation: c.generation, pairId: c.pairId, secret: c.secret };
  const link = `${c.origin}/#reading=${Buffer.from(JSON.stringify(invite)).toString('base64url')}`;
  assert.equal(h.pairing.parseReadingInvite(link, h.cipher).pairId, c.pairId);
  const claim = await h.pairing.readingClaim(c, h.cipher);
  const info = purpose => JSON.stringify(['guizhi-reading-pair-v1', purpose, c.origin, c.libraryId, c.generation]);
  const derive = purpose => Buffer.from(crypto.hkdfSync('sha256', Buffer.from(c.secret, 'base64url'), Buffer.from(c.pairId), Buffer.from(info(purpose)), 32));
  const credentialHash = crypto.createHash('sha256').update(c.credential).digest('hex');
  assert.equal(claim.proof, crypto.createHmac('sha256', derive('proof')).update(JSON.stringify([c.deviceId, credentialHash])).digest('hex'));
  const token = await h.cipher.encrypt(derive('wrap'), { libraryId: c.libraryId, generation: c.generation, keyId: c.keyId, objectId: c.pairId, revision: 1, kind: 'pairing' },
    h.cipher.bytes(JSON.stringify({ deviceId: c.deviceId, credentialHash, contentKey: c.contentKey, checkpoint: c.anchor })));
  const accepted = await h.pairing.acceptReadingBundle(c, token, h.cipher);
  assert.equal(accepted.contentKey, c.contentKey); assert.equal(accepted.anchor.digest, c.anchor.digest); assert.equal(accepted.secret, '');
  await assert.rejects(h.pairing.acceptReadingBundle({ ...c, deviceId: h.id() }, token, h.cipher), /接收者/);
  await assert.rejects(h.pairing.acceptReadingBundle({ ...c, secret: crypto.randomBytes(32).toString('base64url') }, token, h.cipher));
  for (const raw of [link.replace('https:', 'http:'), link.replace('/#', '/other#'), link.replace(c.origin, 'https://evil.example.com'), link + '&extra=1']) assert.throws(() => h.pairing.parseReadingInvite(raw, h.cipher));
});

test('下载正文后断网重启仍能全文搜索、分类和标签筛选；图片不提前下载', async () => {
  const c = h.connection(), imageId = h.id(), a = h.article(undefined, `本地搜索中文 ![图](guizhi-sync://${imageId})`);
  const data = await h.snapshot(c, [a], 1, [{ objectId: imageId, kind: 'image', bytes: new Uint8Array([1, 2, 3]) }]);
  const cache = h.memoryCache(), remote = h.remote(data), lib = new ReadingLibrary(c, h.cipher, cache, remote);
  await lib.sync(noop); assert.equal(cache.commits, 1); assert.equal(remote.requests.length, 1);
  assert.equal(await lib.image(imageId), null);
  remote.online = false;
  const reopened = new ReadingLibrary(c, h.cipher, cache, remote); await reopened.open();
  assert.equal(reopened.articles[0].content, a.content);
  assert.equal(h.model.findArticles(reopened.articles, '搜索 中文', '合成知识库', '离线').length, 1);
  assert.equal(h.model.findArticles(reopened.articles, '未下载的内容', '', '').length, 0);
  assert.equal(h.model.findArticles(reopened.articles, '', '其他库', '').length, 0);
  await assert.rejects(reopened.sync(noop), /断网/); assert.equal(reopened.current.head.sequence, 1);
});

test('断网和落盘失败不推进可见版本；重试复用已校验密文并整批提交', async () => {
  const c = h.connection(), cache = h.memoryCache(), first = await h.snapshot(c, [h.article()]), remote = h.remote(first);
  const lib = new ReadingLibrary(c, h.cipher, cache, remote); await lib.sync(noop);
  const second = await h.snapshot(c, [h.article(), h.article()], 2); remote.data = second;
  remote.failObject = second.manifest.entries[1].ciphertextSha256;
  await assert.rejects(lib.sync(noop), /缺失/); assert.equal(cache.state.head.sequence, 1); assert.equal(lib.articles.length, 1);
  remote.failObject = ''; cache.failCommit = true;
  await assert.rejects(lib.sync(noop), /磁盘满/); assert.equal(cache.state.head.sequence, 1);
  const before = remote.requests.length; cache.failCommit = false; await lib.sync(noop);
  assert.equal(remote.requests.length, before); assert.equal(lib.articles.length, 2); assert.equal(cache.state.head.sequence, 2);
});

test('已配对检查点拒绝首次回退；已缓存版本拒绝回退、同序号冲突和换密钥', async () => {
  const c = h.connection(), cache = h.memoryCache(), first = await h.snapshot(c, [h.article()]), remote = h.remote(first);
  c.anchor.sequence = 2; const lib = new ReadingLibrary(c, h.cipher, cache, remote);
  await assert.rejects(lib.sync(noop), /回退/); assert.equal(cache.commits, 0); c.anchor.sequence = 1;
  await lib.sync(noop);
  const second = await h.snapshot(c, [h.article()], 2); remote.data = second; await lib.sync(noop);
  remote.data = first; await assert.rejects(lib.sync(noop), /回退/);
  remote.data = { ...second, head: { ...second.head, digest: 'b'.repeat(64) } }; await assert.rejects(lib.sync(noop), /冲突/);
  remote.data = { ...second, head: { ...second.head, keyId: h.id() } }; await assert.rejects(lib.sync(noop), /身份/);
  assert.equal(cache.state.head.sequence, 2);
});

test('损坏缓存不能呈现为空库；联网相同清单可修复，清单哈希篡改整体拒绝', async () => {
  const c = h.connection(), cache = h.memoryCache(), data = await h.snapshot(c, [h.article()]), remote = h.remote(data);
  await new ReadingLibrary(c, h.cipher, cache, remote).sync(noop);
  cache.objects.clear(); const lib = new ReadingLibrary(c, h.cipher, cache, remote);
  await assert.rejects(lib.open(), /缺失/); await lib.sync(noop); assert.equal(lib.articles.length, 1);
  remote.data = { ...data, head: { ...data.head, manifestToken: data.head.manifestToken + 'x' } };
  await assert.rejects(lib.sync(noop), /哈希/); assert.equal(lib.articles.length, 1);
});

test('图片按需保存并逐项报告失败；离线读取缓存；新空清单移除旧正文与图片', async () => {
  const c = h.connection(), imageIds = [h.id(), h.id()], a = h.article(undefined, imageIds.map(id => `![图](guizhi-sync://${id})`).join('\n'));
  const data = await h.snapshot(c, [a], 1, imageIds.map(id => ({ objectId: id, kind: 'image', bytes: new Uint8Array([7, 8, 9]) })));
  const cache = h.memoryCache(), remote = h.remote(data), lib = new ReadingLibrary(c, h.cipher, cache, remote); await lib.sync(noop);
  remote.failObject = data.manifest.entries[2].ciphertextSha256;
  const partial = await lib.saveImages(a.id, noop); assert.equal(partial.saved, 1); assert.equal(partial.failed.length, 1);
  remote.online = false; assert.equal(Buffer.from(await lib.image(imageIds[0])).toString('hex'), '070809');
  await lib.clearImages(); assert.equal(await lib.image(imageIds[0]), null); assert.equal(lib.articles.length, 1);
  remote.online = true; remote.failObject = ''; remote.data = await h.snapshot(c, [], 2); await lib.sync(noop);
  assert.equal(lib.articles.length, 0); assert.equal(cache.objects.size, 0); assert.equal(lib.current.head.sequence, 2);
});

test('取消与并发保护：在途响应不能推进游标或重新暴露锁定前正文', async () => {
  const c = h.connection(), data = await h.snapshot(c, [h.article()]), cache = h.memoryCache(), remote = h.remote(data);
  let resolve; remote.head = () => new Promise(done => { resolve = done; });
  const lib = new ReadingLibrary(c, h.cipher, cache, remote), task = lib.sync(noop);
  while (!resolve) await new Promise(done => setImmediate(done));
  await assert.rejects(lib.sync(noop), /正在进行/); lib.lock(); resolve(data.head);
  await assert.rejects(task, /取消/); assert.equal(cache.commits, 0); assert.equal(lib.articles.length, 0);
});

test('真实缓存适配仅写密文；原子替换失败保留旧游标；重启解密且隔离其他目录', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'reading-cache-')); t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const c = h.connection(), data = await h.snapshot(c, [h.article(undefined, '绝不可明文落盘')]);
  const cache = h.nativeCache({ filesDir: root }, c), remote = h.remote(data);
  await new ReadingLibrary(c, h.cipher, cache, remote).sync(noop);
  const folder = path.join(root, 'reading-v1', c.cacheId);
  for (const file of fs.readdirSync(folder)) {
    const value = fs.readFileSync(path.join(folder, file), 'utf8');
    assert.ok(!value.includes('绝不可明文落盘')); assert.ok(!value.includes(c.contentKey)); assert.ok(!value.includes('manifestToken'));
  }
  const broken = h.nativeCache({ filesDir: root }, c, { renameSync: () => { throw new Error('合成原子切换失败'); } });
  await assert.rejects(broken.commit({ schema: 1, head: { ...data.head, sequence: 2 }, syncedAt: Date.now() }), /切换失败/);
  assert.equal((await cache.load()).head.sequence, 1);
  const offline = new ReadingLibrary(c, h.cipher, h.nativeCache({ filesDir: root }, c), { head: async () => { throw new Error('offline'); }, object: async () => { throw new Error('offline'); }, cancel() {} });
  await offline.open(); assert.equal(offline.articles[0].content, '绝不可明文落盘');
  await assert.rejects(cache.object('../escape'));
});

test('后台取消发生在检查点加密期间时，旧运行时不能覆盖新会话已提交的版本', async t => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'reading-cancel-')); t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const c = h.connection(), first = await h.snapshot(c, [h.article()]), cache = h.nativeCache({ filesDir: root }, c), remote = h.remote(first);
  const old = new ReadingLibrary(c, h.cipher, cache, remote); await old.sync(noop);
  const encrypt = cache.crypto.encrypt.bind(cache.crypto); let release;
  cache.crypto.encrypt = async (...args) => { await new Promise(done => { release = done; }); return encrypt(...args); };
  remote.data = await h.snapshot(c, [h.article()], 2);
  const task = old.sync(noop), rejected = assert.rejects(task, /取消/);
  while (!release) await new Promise(done => setImmediate(done));
  old.lock();
  const latest = await h.snapshot(c, [h.article(undefined, '新会话版本')], 3);
  const reopened = new ReadingLibrary(c, h.cipher, h.nativeCache({ filesDir: root }, c), h.remote(latest));
  await reopened.open(); await reopened.sync(noop);
  release(); await rejected;
  assert.equal((await cache.load()).head.sequence, 3); assert.equal(old.articles.length, 0);
  assert.equal(reopened.articles[0].content, '新会话版本');
});

test('受限阅读器将 HTML 当文字，仅识别同步图片；链接校验拒绝主动协议和凭证', () => {
  const id = h.id(), blocks = h.reader.readingBlocks(`# 标题\n<script>alert(1)</script>\n![本地](guizhi-sync://${id})\n![远程](https://tracking.example.com/pixel)`);
  assert.equal(blocks.filter(block => block.kind === 'image').length, 1);
  assert.equal(blocks.filter(block => block.kind === 'external').length, 1);
  assert.ok(blocks.some(block => block.text.includes('<script>')));
  for (const url of ['javascript:alert(1)', 'file:///private', 'http://example.com', 'https://a@evil.example.com', 'https://good.example.com\\@bad.example.com']) assert.equal(h.model.safeReadingLink(url), false);
  assert.equal(h.model.safeReadingLink('https://example.com/article'), true);
});
