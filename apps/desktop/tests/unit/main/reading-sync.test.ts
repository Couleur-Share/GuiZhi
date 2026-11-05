// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mkdtempSync, rmSync, readFileSync, writeFileSync, mkdirSync, existsSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { randomBytes, createCipheriv, createDecipheriv } from 'node:crypto';
import Database from '@guizhi/db/adapter';
import { SCHEMA_TABLES } from '@guizhi/db/schema';
import { readSelectedLibrary } from '../../../src/main/services/reading-sync/source';
import { ReadingCredentials } from '../../../src/main/services/reading-sync/credentials';
import { ReadingController } from '../../../src/main/services/reading-sync/controller';
import { configureRuntimePaths, getImagesDir, resetRuntimePaths } from '../../../src/main/runtime-paths';
import { generateReadingId, generateReadingKey } from '@guizhi/core/reading-sync/crypto';
import { readingDigest } from '@guizhi/core/reading-sync/snapshot';

const mocks = vi.hoisted(() => ({ secure: true, request: vi.fn() }));
// 仅替换平台保险箱边界；真实加密包装和磁盘队列仍被执行。
vi.mock('electron', () => ({ safeStorage: {
  isEncryptionAvailable: () => mocks.secure, getSelectedStorageBackend: () => 'test-secure',
  encryptString: (text: string) => {
    const cipher = createCipheriv('aes-256-gcm', Buffer.alloc(32, 8), Buffer.alloc(12, 7));
    return Buffer.concat([cipher.update(text), cipher.final(), cipher.getAuthTag()]);
  },
  decryptString: (bytes: Buffer) => {
    const cipher = createDecipheriv('aes-256-gcm', Buffer.alloc(32, 8), Buffer.alloc(12, 7));
    cipher.setAuthTag(bytes.subarray(-16)); return Buffer.concat([cipher.update(bytes.subarray(0, -16)), cipher.final()]).toString();
  },
} }));
vi.mock('../../../src/main/services/reading-sync/transport', () => ({ readingRequest: (...args: unknown[]) => mocks.request(...args) }));
let root: string, db: Database.Database;
beforeEach(() => {
  root = mkdtempSync(path.join(tmpdir(), 'guizhi-reading-main-'));
  configureRuntimePaths({ userDataPath: root }); mkdirSync(getImagesDir(), { recursive: true });
  db = new Database(':memory:'); db.exec(SCHEMA_TABLES); mocks.secure = true; mocks.request.mockReset();
  db.prepare('INSERT INTO collections(id,name,created_at,updated_at) VALUES(?,?,1,1)').run('c1', '所选库');
  db.prepare('INSERT INTO collections(id,name,created_at,updated_at) VALUES(?,?,1,1)').run('c2', '私密库');
  for (const [id, collection, deleted] of [['i1', 'c1', null], ['i2', 'c2', null], ['i3', 'c1', 1]]) {
    db.prepare('INSERT INTO knowledge_items(id,title,content,collection_id,deleted_at,created_at,updated_at) VALUES(?,?,?,?,?,1,1)').run(id, `标题${id}`, '编辑正文', collection, deleted);
  }
});
afterEach(() => { db.close(); resetRuntimePaths(); rmSync(root, { recursive: true, force: true }); });

describe('桌面阅读选库与保险箱', () => {
  it('只投影所选库且排除回收站；正文、标签、删除和移库均改变快照', () => {
    const key = randomBytes(32), first = readSelectedLibrary(db, ['c1'], getImagesDir(), key);
    expect(first.objects).toHaveLength(1);
    const article = JSON.parse(Buffer.from(first.objects[0].bytes).toString());
    expect(article.title).toBe('标题i1'); expect(article.id).not.toBe('i1'); expect(article.content).toBe('编辑正文');
    db.prepare('UPDATE knowledge_items SET content=? WHERE id=?').run('最新编辑', 'i1');
    expect(readSelectedLibrary(db, ['c1'], getImagesDir(), key).sourceDigest).not.toBe(first.sourceDigest);
    db.prepare('UPDATE knowledge_items SET collection_id=? WHERE id=?').run('c2', 'i1');
    expect(readSelectedLibrary(db, ['c1'], getImagesDir(), key).objects).toHaveLength(0);
    expect(() => readSelectedLibrary(db, ['missing'], getImagesDir(), key)).toThrow('不存在');
  });
  it('重写本地图片为不透明对象，拒绝遍历和缺失资源，视频文件不上传', () => {
    const key = randomBytes(32); writeFileSync(path.join(getImagesDir(), 'one.png'), Buffer.from([1, 2, 3]));
    db.prepare('UPDATE knowledge_items SET content=? WHERE id=?').run('![图](local-image://one.png) local-video://private.mp4', 'i1');
    const source = readSelectedLibrary(db, ['c1'], getImagesDir(), key);
    expect(source.objects).toHaveLength(2);
    const article = JSON.parse(Buffer.from(source.objects.find(object => object.kind === 'article')!.bytes).toString());
    expect(article.content).toContain('guizhi-sync://'); expect(article.content).not.toContain('one.png');
    expect(article.content).not.toContain('private.mp4');
    for (const bad of ['../outside.png', 'missing.png', 'x.svg']) {
      db.prepare('UPDATE knowledge_items SET content=? WHERE id=?').run(`![图](local-image://${bad})`, 'i1');
      expect(() => readSelectedLibrary(db, ['c1'], getImagesDir(), key)).toThrow();
    }
  });
  it('安全存储不可用时停止；凭据文件不含明文密钥', () => {
    const file = path.join(root, 'secure.json'), store = new ReadingCredentials(file);
    const config = { origin: 'https://reading.example.com', libraryId: generateReadingId(), generation: generateReadingId(), keyId: generateReadingId(), manifestId: generateReadingId(),
      writerCredential: generateReadingKey().toString('base64url'), contentKey: generateReadingKey().toString('base64url'), idKey: generateReadingKey().toString('base64url'), collectionIds: ['c1'], provisioned: true };
    store.write(config); expect(store.read()).toEqual(config);
    expect(readFileSync(file, 'utf8')).not.toContain(config.contentKey);
    mocks.secure = false; expect(() => store.write(config)).toThrow('安全存储'); expect(() => store.read()).toThrow('安全存储');
  });
  it('未开启阅读同步时，备份暂停回调不创建机器配置或冻结标记', () => {
    new ReadingController(db).stop(true);
    expect(existsSync(path.join(root, '.machine', 'reading-sync'))).toBe(false);
  });
  it('真实桌面控制器完成选库上传，正常状态不返回秘密；恢复时取消请求且持久暂停', async () => {
    let generation = '', sequence = 0, digest: string | null = null;
    mocks.request.mockImplementation(async (_origin: string, route: string, _credential: string, method: string, body: Record<string, unknown>) => {
      if (route.endsWith('/libraries')) { generation = body.generation as string; return { created: true }; }
      if (route.endsWith('/head') && method === 'PUT') { sequence = body.sequence as number; digest = readingDigest(body.manifestToken as string); }
      return { sequence, generation, digest, manifestToken: null, keyId: null };
    });
    const controller = new ReadingController(db);
    await controller.perform(() => controller.activate('https://reading.example.com', generateReadingKey().toString('base64url')));
    controller.configure(['c1']);
    await controller.perform(() => controller.sync());
    expect(controller.status().sequence).toBe(1); expect(controller.status().pending).toBe(false);
    expect(JSON.stringify(controller.status())).not.toMatch(/contentKey|writerCredential|idKey/);
    const before = mocks.request.mock.calls.filter(call => call[3] === 'PUT').length;
    await controller.perform(() => controller.sync());
    expect(mocks.request.mock.calls.filter(call => call[3] === 'PUT')).toHaveLength(before);
    controller.stop(true);
    await expect(controller.perform(() => controller.sync())).rejects.toThrow('暂停');
    const restarted = new ReadingController(db);
    await expect(restarted.perform(() => restarted.sync())).rejects.toThrow('暂停');
  });
});
