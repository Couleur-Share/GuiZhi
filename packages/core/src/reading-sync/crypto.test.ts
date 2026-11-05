import { describe, it, expect } from 'vitest';
import { webcrypto, randomBytes } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { encryptReadingObject, decryptReadingObject, createRecoveryKey, wrapReadingKey, unwrapReadingKey } from './crypto';
import { projectReadingArticle } from './projection';
import { readingAdvance, READING_MAX_BYTES, type ReadingIdentity } from '../../../shared/utils/reading-sync-format';

const identity: ReadingIdentity = { libraryId: 'fixture_library_01', generation: 'fixture_generation_01',
  objectId: 'fixture_article_01', keyId: 'fixture_content_key_01', revision: 1, kind: 'article' };
const key = Buffer.from(Array.from({ length: 32 }, (_, i) => i));
const plain = Buffer.from('虚构正文与 emoji 😀');

describe('阅读同步密码与恢复', () => {
  it('随机 IV；独立 WebCrypto 能解开 Node JWE', async () => {
    const token = encryptReadingObject(key, identity, plain);
    expect(encryptReadingObject(key, identity, plain)).not.toBe(token);
    const [header, , iv, ciphertext, tag] = token.split('.');
    const imported = await webcrypto.subtle.importKey('raw', key, 'AES-GCM', false, ['decrypt']);
    const result = await webcrypto.subtle.decrypt({ name: 'AES-GCM', iv: Buffer.from(iv, 'base64url'),
      additionalData: Buffer.from(header), tagLength: 128 }, imported,
    Buffer.concat([Buffer.from(ciphertext, 'base64url'), Buffer.from(tag, 'base64url')]));
    expect(Buffer.from(result)).toEqual(plain);
  });

  it('Node 解开 WebCrypto 生成的两篇文章、图片与恢复封装', () => {
    const vectors = JSON.parse(readFileSync(new URL('../../../shared/fixtures/reading-sync-v1.json', import.meta.url), 'utf8'));
    expect(vectors).toHaveLength(4);
    for (const vector of vectors) {
      expect(decryptReadingObject(Buffer.from(vector.keyHex, 'hex'), vector.identity, vector.token).toString('hex')).toBe(vector.plainHex);
    }
  });

  it('错密钥、篡改 IV/密文/标签均不返回明文', () => {
    const token = encryptReadingObject(key, identity, plain);
    expect(() => decryptReadingObject(randomBytes(32), identity, token)).toThrow(/认证失败/);
    for (const index of [2, 3, 4]) {
      const parts = token.split('.'); const bytes = Buffer.from(parts[index], 'base64url');
      bytes[0] ^= 1; parts[index] = bytes.toString('base64url');
      expect(() => decryptReadingObject(key, identity, parts.join('.'))).toThrow();
    }
  });

  it('期望身份必须来自可信清单，拒绝库/世代/条目/版本/密钥/用途错配', () => {
    const token = encryptReadingObject(key, identity, plain);
    for (const patch of [{ libraryId: 'different_library' }, { generation: 'different_generation' },
      { objectId: 'different_object' }, { revision: 2 }, { keyId: 'different_key_id' }, { kind: 'image' }]) {
      expect(() => decryptReadingObject(key, { ...identity, ...patch }, token)).toThrow();
    }
    const parts = token.split('.');
    parts[0] = Buffer.from(JSON.stringify({ ...JSON.parse(Buffer.from(parts[0], 'base64url').toString()), alg: 'none' })).toString('base64url');
    expect(() => decryptReadingObject(key, identity, parts.join('.'))).toThrow();
  });

  it('拒绝非规范编码、额外字段、错误长度和超限', () => {
    const token = encryptReadingObject(key, identity, plain);
    for (const invalid of [token + '.', token + '=', 'x'.repeat(12_000_000), 'a..b.c.d']) {
      expect(() => decryptReadingObject(key, identity, invalid)).toThrow();
    }
    const parts = token.split('.');
    parts[0] = Buffer.from(Buffer.from(parts[0], 'base64url').toString().replace('"alg":', '"extra":1,"alg":')).toString('base64url');
    expect(() => decryptReadingObject(key, identity, parts.join('.'))).toThrow();
    expect(() => encryptReadingObject(key, identity, Buffer.alloc(0))).toThrow();
    expect(() => encryptReadingObject(key, identity, Buffer.alloc(READING_MAX_BYTES + 1))).toThrow();
    expect(() => encryptReadingObject(Buffer.alloc(16), identity, plain)).toThrow();
  });

  it('恢复密钥能取回内容密钥，错误密钥和跨用途恢复失败', () => {
    const recovery = createRecoveryKey();
    const context = { ...identity, objectId: identity.keyId, kind: 'recovery' };
    const token = wrapReadingKey(key, recovery, context);
    expect(token).not.toContain(recovery);
    expect(unwrapReadingKey(token, recovery, context)).toEqual(key);
    expect(() => unwrapReadingKey(token, createRecoveryKey(), context)).toThrow();
    expect(() => unwrapReadingKey(token, recovery, identity)).toThrow();
    expect(() => wrapReadingKey(key, 'password', context)).toThrow();
  });
});

describe('阅读投影与版本', () => {
  it('白名单排除原始 ID、访问凭据、配置；源链接移除查询和片段', () => {
    const input = { title: '标题', content: '编辑后的阅读正文', summary: null, itemType: 'note', status: 'active',
      collectionName: '学习', tags: ['标签', '标签'], publicSourceUrl: 'https://example.com/read?a=1&token=secret#secret',
      id: 'private-db-id', access_uri: 'secret', apiKey: 'secret', cookie: 'secret', rawHtml: '<script>secret</script>' };
    const result = projectReadingArticle(identity.objectId, input);
    expect(Object.keys(result).sort()).toEqual(['schema', 'id', 'title', 'content', 'summary', 'itemType', 'status', 'collectionName', 'tags', 'sourceUrl'].sort());
    expect(JSON.stringify(result)).not.toMatch(/secret|private-db-id/);
    expect(result.content).toBe(input.content); expect(result.tags).toEqual(['标签']);
    expect(result.sourceUrl).toBe('https://example.com/read');
    for (const url of ['file:///private', 'https://user:password@example.com', 'javascript:alert(1)']) {
      expect(projectReadingArticle(identity.objectId, { ...input, publicSourceUrl: url }).sourceUrl).toBeNull();
    }
    expect(() => projectReadingArticle(identity.objectId, { ...input, tags: Array(101).fill('x') })).toThrow();
    expect(() => projectReadingArticle(identity.objectId, { ...input, content: '中'.repeat(1_500_000) })).toThrow();
  });

  it('已认证清单的检查点拒绝回退、同版本冲突和世代切换，重放幂等', () => {
    const current = { libraryId: identity.libraryId, generation: identity.generation, sequence: 2, manifestDigest: 'a'.repeat(64) };
    expect(readingAdvance(current, current)).toBe(false);
    expect(readingAdvance(current, { ...current, sequence: 3 })).toBe(true);
    for (const patch of [{ sequence: 1 }, { sequence: NaN }, { manifestDigest: 'b'.repeat(64) }, { generation: 'other_generation' }]) {
      expect(() => readingAdvance(current, { ...current, ...patch })).toThrow();
    }
    expect(current.sequence).toBe(2);
  });
});
