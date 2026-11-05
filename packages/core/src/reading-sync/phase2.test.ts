import { it, expect } from 'vitest';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { generateReadingKey } from './crypto';
import { createReadingInvitation, readingClaim, sealPairingKey, openPairingBundle, verifyReadingClaim } from './pairing';
import { ReadingOutbox, type ReadingUploadPort } from './outbox';
import { readingDigest } from './snapshot';
import type { ReadingHead } from '../../../shared/types/reading-sync';

const identity = { libraryId: 'reading_library_01', generation: 'reading_generation_01',
  objectId: 'reading_manifest_01', keyId: 'reading_content_key_01', revision: 1, kind: 'manifest' };
it('配对证明绑定手机凭证；中转替换设备或凭证不能取得内容密钥', () => {
  const invitation = createReadingInvitation('https://reading.example.com', identity.libraryId, identity.generation);
  const reader = generateReadingKey().toString('base64url'), key = generateReadingKey();
  const claim = readingClaim(invitation, 'reading_device_01', reader);
  const bundle = sealPairingKey(invitation, claim, identity.keyId, key, { manifestId: identity.objectId, sequence: 1, digest: 'a'.repeat(64) });
  expect(openPairingBundle(invitation, claim, identity.keyId, bundle).contentKey).toEqual(key);
  expect(() => verifyReadingClaim(invitation, { ...claim, deviceId: 'attacker_device_01' })).toThrow();
  expect(() => verifyReadingClaim(invitation, { ...claim, credentialHash: '0'.repeat(64) })).toThrow();
  expect(() => openPairingBundle({ ...invitation, secret: generateReadingKey().toString('base64url') }, claim, identity.keyId, bundle)).toThrow();
  expect(() => openPairingBundle(invitation, claim, 'another_key_id_01', bundle)).toThrow();
  expect(bundle).not.toContain(key.toString('base64url'));
});

it('上传前持久化密文；重启复用原密文，提交响应丢失后正确记账', async () => {
  const root = mkdtempSync(join(tmpdir(), 'reading-outbox-')), file = join(root, 'queue.json');
  try {
    let outbox = new ReadingOutbox(file, identity), head: ReadingHead = { sequence: 0, generation: identity.generation, digest: null, manifestToken: null, keyId: null };
    const puts: string[] = [];
    const port: ReadingUploadPort = { head: async () => head, put: async (_digest, token) => { puts.push(token); throw new Error('合成断网'); },
      publish: async value => { head = { sequence: value.sequence, generation: identity.generation, digest: readingDigest(value.manifestToken), manifestToken: value.manifestToken, keyId: identity.keyId }; throw new Error('合成响应丢失'); } };
    outbox.enqueue(generateReadingKey(), [{ objectId: 'reading_article_01', kind: 'article', revision: 1, bytes: Buffer.from('secret-test-body') }], 'a'.repeat(64));
    expect(readFileSync(file, 'utf8')).not.toContain('secret-test-body');
    const saved = readFileSync(file, 'utf8');
    await expect(outbox.flush(port)).rejects.toThrow('断网');
    expect(readFileSync(file, 'utf8')).toBe(saved);
    outbox = new ReadingOutbox(file, identity);
    port.put = async (_digest, token) => { puts.push(token); };
    await expect(outbox.flush(port)).rejects.toThrow('响应丢失');
    expect(puts[0]).toBe(puts[1]);
    outbox = new ReadingOutbox(file, identity);
    await outbox.flush(port);
    expect(outbox.status()).toEqual({ sequence: 1, pending: false, busy: false });
    expect(outbox.enqueue(generateReadingKey(), [], 'a'.repeat(64))).toBe(false);
    head = { ...head, sequence: 2 };
    await expect(outbox.flush(port)).rejects.toThrow('旧备份');
    expect(outbox.status().sequence).toBe(1);
  } finally { rmSync(root, { recursive: true, force: true }); }
});

it('待发送批次不可覆盖，单飞期间不能再次上传或加入新快照', async () => {
  const root = mkdtempSync(join(tmpdir(), 'reading-single-flight-'));
  try {
    const outbox = new ReadingOutbox(join(root, 'queue.json'), identity);
    outbox.enqueue(generateReadingKey(), [], 'a'.repeat(64));
    expect(() => outbox.enqueue(generateReadingKey(), [], 'b'.repeat(64))).toThrow('待发送');
    let release!: (value: ReadingHead) => void;
    const port: ReadingUploadPort = { head: () => new Promise(resolve => { release = resolve; }), put: async () => {}, publish: async () => { throw new Error('合成失败'); } };
    const first = outbox.flush(port);
    await expect(outbox.flush(port)).rejects.toThrow('正在进行');
    release({ sequence: 0, generation: identity.generation, digest: null, manifestToken: null, keyId: null });
    await expect(first).rejects.toThrow('合成失败');
    expect(outbox.status().pending).toBe(true);
  } finally { rmSync(root, { recursive: true, force: true }); }
});

it('批次中途限流后重启，从未完成对象继续；已确认对象不重复消耗限额', async () => {
  const root = mkdtempSync(join(tmpdir(), 'reading-resume-')), file = join(root, 'queue.json');
  try {
    let outbox = new ReadingOutbox(file, identity);
    outbox.enqueue(generateReadingKey(), [1, 2, 3].map(index => ({ objectId: `reading_article_0${index}`,
      kind: 'article', revision: 1, bytes: Buffer.from(`article ${index}`) })), 'c'.repeat(64));
    const received: string[] = [];
    const port: ReadingUploadPort = {
      head: async () => ({ sequence: 0, generation: identity.generation, digest: null, manifestToken: null, keyId: null }),
      put: async digest => { if (received.length === 1) throw new Error('合成限流'); received.push(digest); },
      publish: async value => ({ sequence: value.sequence, generation: identity.generation,
        digest: readingDigest(value.manifestToken), manifestToken: value.manifestToken, keyId: identity.keyId }),
    };
    await expect(outbox.flush(port)).rejects.toThrow('限流');
    outbox = new ReadingOutbox(file, identity);
    port.put = async digest => { received.push(digest); };
    await outbox.flush(port);
    expect(received).toHaveLength(3);
    expect(new Set(received).size).toBe(3);
    expect(outbox.status().sequence).toBe(1);
  } finally { rmSync(root, { recursive: true, force: true }); }
});

it('暂存租期过半后重启会重新确认所有对象，不使用可能被回收的旧断点', async () => {
  const root = mkdtempSync(join(tmpdir(), 'reading-lease-')), file = join(root, 'queue.json');
  try {
    const outbox = new ReadingOutbox(file, identity);
    outbox.enqueue(generateReadingKey(), [{ objectId: 'reading_article_01', kind: 'article', revision: 1, bytes: Buffer.from('lease') }], 'd'.repeat(64));
    const saved = JSON.parse(readFileSync(file, 'utf8'));
    writeFileSync(file + '.progress', JSON.stringify({ digest: readingDigest(saved.pending.publish.manifestToken), uploaded: 1,
      startedAt: Date.now() - 13 * 3600000 }));
    let uploads = 0;
    await new ReadingOutbox(file, identity).flush({
      head: async () => ({ generation: identity.generation, sequence: 0, digest: null, manifestToken: null, keyId: null }),
      put: async () => { uploads++; },
      publish: async value => ({ generation: identity.generation, sequence: value.sequence, digest: readingDigest(value.manifestToken),
        manifestToken: value.manifestToken, keyId: identity.keyId }),
    });
    expect(uploads).toBe(1);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
