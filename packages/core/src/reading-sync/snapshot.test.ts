import { it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { generateReadingKey } from './crypto';
import { sealReadingSnapshot, openReadingSnapshot, readingDigest, type ReadingObjectInput } from './snapshot';

interface Fixture { identity: { objectId: string; kind: 'article' | 'image' }; plainHex: string; }
const fixtures = JSON.parse(readFileSync(new URL('../../../shared/fixtures/reading-sync-v1.json', import.meta.url), 'utf8')) as Fixture[];
const objects: ReadingObjectInput[] = fixtures.slice(0, 3).map(vector => ({
  objectId: vector.identity.objectId, kind: vector.identity.kind, revision: 1, bytes: Buffer.from(vector.plainHex, 'hex'),
}));
const identity = { libraryId: 'fixture_library_01', generation: 'fixture_generation_01',
  objectId: 'fixture_manifest_01', keyId: 'fixture_content_key_01', revision: 1, kind: 'manifest' };

it('两篇虚构文章与图片经过加密清单、对象校验、解密完整回读', async () => {
  const key = generateReadingKey();
  const batch = sealReadingSnapshot(key, identity, objects);
  expect(batch.manifestDigest).toBe(readingDigest(batch.manifestToken));
  const restored = await openReadingSnapshot(key, identity, batch.manifestToken, async entry => batch.objects.get(entry.objectId)!);
  expect(restored).toEqual(objects);
  expect([...batch.objects.values()].join('')).not.toContain('合成文章');
});

it('缺件、篡改和错库均整体失败，不返回部分结果', async () => {
  const key = generateReadingKey(), batch = sealReadingSnapshot(key, identity, objects);
  for (const replacement of ['', 'damaged']) {
    let calls = 0;
    await expect(openReadingSnapshot(key, identity, batch.manifestToken, async entry => {
      calls++; return calls === 2 ? replacement : batch.objects.get(entry.objectId)!;
    })).rejects.toThrow(/完整性/);
    expect(calls).toBe(2);
  }
  await expect(openReadingSnapshot(key, { ...identity, libraryId: 'different_library' }, batch.manifestToken,
    async entry => batch.objects.get(entry.objectId)!)).rejects.toThrow(/不匹配/);
});

it('重复对象、用途混用与过大快照在组装时拒绝', () => {
  const key = generateReadingKey();
  expect(() => sealReadingSnapshot(key, identity, [objects[0], objects[0]])).toThrow(/重复/);
  expect(() => sealReadingSnapshot(key, { ...identity, kind: 'article' }, objects)).toThrow();
  expect(() => sealReadingSnapshot(key, identity, [{ ...objects[0], objectId: identity.objectId }])).toThrow();
  expect(() => sealReadingSnapshot(key, identity, [{ ...objects[0], bytes: Buffer.alloc(8 * 1024 * 1024 + 1) }])).toThrow();
});

it('空完整清单可表达撤回全部阅读副本，而不是下载失败', async () => {
  const key = generateReadingKey(), batch = sealReadingSnapshot(key, identity, []);
  const restored = await openReadingSnapshot(key, identity, batch.manifestToken, async () => { throw new Error('不应下载'); });
  expect(restored).toEqual([]);
});
