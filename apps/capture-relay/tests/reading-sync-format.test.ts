import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { readingAdvance, readingProtectedHeader, splitReadingEnvelope } from '../../../packages/shared/utils/reading-sync-format';
import { parseReadingManifest } from '../../../packages/shared/utils/reading-sync-manifest';

const identity = { libraryId: 'fixture_library_01', generation: 'fixture_generation_01',
  objectId: 'fixture_manifest_01', keyId: 'fixture_content_key_01', revision: 1, kind: 'manifest' };

test('鸿蒙与共享 JWE 协议逐字一致，避免跨端验证规则漂移', () => {
  assert.equal(readFileSync(new URL('../../../packages/shared/utils/reading-sync-format.ts', import.meta.url), 'utf8'),
    readFileSync(new URL('../../capture-harmony/entry/src/main/ets/core/ReadingSyncFormat.ts', import.meta.url), 'utf8'));
});

test('标准 JWE profile 固定算法、用途和身份，拒绝无效输入', () => {
  const header = JSON.parse(readingProtectedHeader(identity));
  assert.equal(header.alg, 'dir'); assert.equal(header.enc, 'A256GCM');
  for (const patch of [{ revision: 0 }, { revision: Number.MAX_SAFE_INTEGER + 1 }, { kind: 'unknown' }, { libraryId: '../private' }]) {
    assert.throws(() => readingProtectedHeader({ ...identity, ...patch }));
  }
  assert.throws(() => splitReadingEnvelope('not.a.valid.jwe.token'));
});

test('完整清单绑定库世代与版本，重复、超限和额外字段全部拒绝', () => {
  const entry = { objectId: 'fixture_article_01', keyId: identity.keyId, revision: 1, kind: 'article',
    ciphertextBytes: 1200, ciphertextSha256: 'a'.repeat(64) };
  const manifest = { schema: 1, mode: 'snapshot', libraryId: identity.libraryId,
    generation: identity.generation, sequence: identity.revision, entries: [entry] };
  assert.deepEqual(parseReadingManifest(JSON.stringify(manifest), identity), manifest);
  assert.equal(parseReadingManifest(JSON.stringify({ ...manifest, entries: [] }), identity).entries.length, 0);
  for (const patch of [{ sequence: 2 }, { generation: 'other_generation' }, { entries: [entry, entry] },
    { entries: [{ ...entry, ciphertextBytes: -1 }] }, { entries: [{ ...entry, ciphertextSha256: 'bad' }] },
    { entries: [{ ...entry, url: 'https://evil.invalid' }] }, { entries: [{ ...entry, kind: 'recovery' }] },
    { entries: Array(10001).fill(entry) }, { secret: 'unexpected' }]) {
    assert.throws(() => parseReadingManifest(JSON.stringify({ ...manifest, ...patch }), identity));
  }
});

test('游标只验证不修改；同版本重放幂等，倒退和冲突失败', () => {
  const current = { libraryId: identity.libraryId, generation: identity.generation, sequence: 3, manifestDigest: 'a'.repeat(64) };
  assert.equal(readingAdvance(current, { ...current }), false);
  assert.equal(readingAdvance(current, { ...current, sequence: 4 }), true);
  assert.throws(() => readingAdvance(current, { ...current, sequence: 2 }));
  assert.throws(() => readingAdvance(current, { ...current, manifestDigest: 'b'.repeat(64) }));
  assert.equal(current.sequence, 3);
});
