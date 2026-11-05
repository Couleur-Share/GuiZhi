// 仅生成公开的合成测试向量。固定密钥及 IV 严禁用于真实资料。
import { webcrypto } from 'node:crypto';
import { mkdirSync, writeFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { readingProtectedHeader } from '../packages/shared/utils/reading-sync-format';
import { projectReadingArticle } from '../packages/core/src/reading-sync/projection';

const root = new URL('../', import.meta.url);
const key = Buffer.from(Array.from({ length: 32 }, (_, i) => i));
const recovery = Buffer.from(Array.from({ length: 32 }, (_, i) => i + 32));
const identity = { libraryId: 'fixture_library_01', generation: 'fixture_generation_01',
  objectId: 'fixture_article_01', keyId: 'fixture_content_key_01', revision: 1, kind: 'article' };
const article = projectReadingArticle(identity.objectId, {
  title: '合成文章：离线阅读', content: '# 离线阅读\n电脑关机后，手机仍可阅读。😀', summary: '这是虚构测试资料。',
  itemType: 'note', status: 'active', collectionName: '合成知识库', tags: ['测试'], publicSourceUrl: null,
});
const article2 = projectReadingArticle('fixture_article_02', {
  ...article, title: '合成文章：图片', content: '![测试图片](guizhi-sync://fixture_image_01)', publicSourceUrl: null,
});
const cases = [
  { name: 'article', key, identity, plain: Buffer.from(JSON.stringify(article)) },
  { name: 'article2', key, identity: { ...identity, objectId: 'fixture_article_02' }, plain: Buffer.from(JSON.stringify(article2)) },
  { name: 'image', key, identity: { ...identity, objectId: 'fixture_image_01', kind: 'image' },
    plain: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/Z1cAAAAASUVORK5CYII=', 'base64') },
  { name: 'recovery', key: recovery, identity: { ...identity, objectId: identity.keyId, kind: 'recovery' }, plain: key },
];
const vectors = [];
for (const [index, entry] of cases.entries()) {
  const header = Buffer.from(readingProtectedHeader(entry.identity)).toString('base64url');
  const iv = Buffer.alloc(12); iv.writeUInt32BE(index + 1, 8);
  const imported = await webcrypto.subtle.importKey('raw', entry.key, { name: 'AES-GCM' }, false, ['encrypt']);
  const encrypted = Buffer.from(await webcrypto.subtle.encrypt({ name: 'AES-GCM', iv,
    additionalData: Buffer.from(header), tagLength: 128 }, imported, entry.plain));
  vectors.push({ name: entry.name, identity: entry.identity, keyHex: entry.key.toString('hex'),
    plainHex: entry.plain.toString('hex'), token: [header, '', iv.toString('base64url'),
      encrypted.subarray(0, -16).toString('base64url'), encrypted.subarray(-16).toString('base64url')].join('.') });
}
const fixturePath = new URL('packages/shared/fixtures/reading-sync-v1.json', root);
mkdirSync(new URL('.', fixturePath), { recursive: true });
writeFileSync(fixturePath, JSON.stringify(vectors, null, 2) + '\n');
const harmonyPath = new URL('apps/capture-harmony/entry/src/main/ets/syncdiag/ReadingFixtures.ts', root);
mkdirSync(new URL('.', harmonyPath), { recursive: true });
writeFileSync(harmonyPath, "// 自动生成的公开测试密钥和虚构资料，仅用于开发包。\n" +
  "import { ReadingIdentity } from '../core/ReadingSyncFormat';\n" +
  'export interface ReadingFixture { name: string; identity: ReadingIdentity; keyHex: string; plainHex: string; token: string; }\n' +
  'export const READING_FIXTURES: ReadingFixture[] = ' + JSON.stringify(vectors, null, 2) + ';\n');
console.log('已生成公开测试向量：' + fileURLToPath(fixturePath));
