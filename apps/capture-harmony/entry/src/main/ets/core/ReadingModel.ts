import { assertReadingId, ReadingIdentity, readingAdvance, READING_MAX_TOKEN_CHARS } from './ReadingSyncFormat';

export interface ReadingInvite { origin: string; libraryId: string; generation: string; pairId: string; secret: string; }
export interface ReadingAnchor { manifestId: string; sequence: number; digest: string; }
export class ReadingConnection {
  schema: number = 1;
  origin: string = '';
  libraryId: string = '';
  generation: string = '';
  pairId: string = '';
  secret: string = '';
  deviceId: string = '';
  credential: string = '';
  cacheKey: string = '';
  cacheId: string = '';
  keyId: string = '';
  contentKey: string = '';
  anchor: ReadingAnchor | null = null;
}
export interface ReadingHead { generation: string; sequence: number; digest: string | null; keyId: string | null; manifestToken: string | null; }
export interface ReadingEntry { objectId: string; keyId: string; revision: number; kind: string; ciphertextBytes: number; ciphertextSha256: string; }
export interface ReadingManifest { schema: number; mode: string; libraryId: string; generation: string; sequence: number; entries: ReadingEntry[]; }
export interface ReadingArticle { schema: number; id: string; title: string; content: string; summary: string | null;
  itemType: string; status: string; collectionName: string; tags: string[]; sourceUrl: string | null; }
export interface ReadingLocal { schema: number; head: ReadingHead; syncedAt: number; }
export interface ReadingCryptoPort {
  generateKey(): Uint8Array;
  encode(bytes: Uint8Array): string;
  decode(value: string): Uint8Array;
  text(bytes: Uint8Array): string;
  bytes(text: string): Uint8Array;
  hash(text: string): Promise<string>;
  hmac(key: Uint8Array, text: string): Promise<string>;
  derive(secret: Uint8Array, salt: string, info: string): Promise<Uint8Array>;
  encrypt(key: Uint8Array, identity: ReadingIdentity, plain: Uint8Array): Promise<string>;
  decrypt(key: Uint8Array, identity: ReadingIdentity, token: string): Promise<Uint8Array>;
}
export function readingOrigin(origin: string): string {
  // 与桌面规范化后的 HTTPS origin 一致；不接受用户信息、路径、转义或 IPv6 混淆。
  if (typeof origin !== 'string' || !/^https:\/\/[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[1-9][0-9]{0,4})?$/.test(origin) || origin.length > 300) {
    throw new Error('阅读服务地址必须是 HTTPS 根地址');
  }
  const port = origin.match(/:([0-9]+)$/);
  if (port && (Number(port[1]) > 65535 || Number(port[1]) === 443)) throw new Error('阅读服务端口无效或非规范');
  return origin;
}
export function assertAnchor(anchor: ReadingAnchor): void {
  if (!anchor) throw new Error('可信阅读检查点缺失');
  assertReadingId(anchor.manifestId);
  if (!Number.isSafeInteger(anchor.sequence) || anchor.sequence < 1 || !/^[a-f0-9]{64}$/.test(anchor.digest)) throw new Error('可信阅读检查点无效');
}
export function validateConnection(value: ReadingConnection, crypto: ReadingCryptoPort): void {
  if (!value || value.schema !== 1) throw new Error('阅读连接格式无效');
  readingOrigin(value.origin);
  [value.libraryId, value.generation, value.pairId, value.deviceId, value.cacheId].forEach((id: string) => assertReadingId(id));
  const secrets: string[] = [value.credential, value.cacheKey];
  if (value.anchor) { assertAnchor(value.anchor); assertReadingId(value.keyId); secrets.push(value.contentKey); }
  else secrets.push(value.secret);
  for (let i = 0; i < secrets.length; i++) {
    const bytes = crypto.decode(secrets[i]);
    const valid = bytes.length === 32; bytes.fill(0);
    if (!valid) throw new Error('阅读连接密钥无效');
  }
}
export function manifestIdentity(connection: ReadingConnection, sequence: number): ReadingIdentity {
  if (!connection.anchor) throw new Error('阅读配对尚未确认');
  return { libraryId: connection.libraryId, generation: connection.generation, keyId: connection.keyId,
    objectId: connection.anchor.manifestId, kind: 'manifest', revision: sequence };
}
export function entryIdentity(connection: ReadingConnection, entry: ReadingEntry): ReadingIdentity {
  return { libraryId: connection.libraryId, generation: connection.generation, keyId: connection.keyId,
    objectId: entry.objectId, kind: entry.kind, revision: entry.revision };
}
export function validateHead(connection: ReadingConnection, current: ReadingHead | null, next: ReadingHead): void {
  if (!connection.anchor || !next || next.generation !== connection.generation || next.keyId !== connection.keyId ||
    typeof next.manifestToken !== 'string' || typeof next.digest !== 'string') throw new Error('阅读库身份改变，请重新确认连接');
  const sequence = current ? current.sequence : connection.anchor.sequence;
  const digest = current ? current.digest! : connection.anchor.digest;
  readingAdvance({ libraryId: connection.libraryId, generation: connection.generation, sequence: sequence, manifestDigest: digest },
    { libraryId: connection.libraryId, generation: next.generation, sequence: next.sequence, manifestDigest: next.digest });
}
export function parseManifest(text: string, identity: ReadingIdentity): ReadingManifest {
  if (text.length > 4 * 1024 * 1024) throw new Error('阅读清单过大');
  const manifest = JSON.parse(text) as ReadingManifest;
  if (!manifest || manifest.schema !== 1 || manifest.mode !== 'snapshot' || manifest.libraryId !== identity.libraryId ||
    manifest.generation !== identity.generation || manifest.sequence !== identity.revision || !Array.isArray(manifest.entries) || manifest.entries.length > 10000) {
    throw new Error('阅读清单身份或容量无效');
  }
  const ids = new Set<string>(); let bytes = 0;
  for (let i = 0; i < manifest.entries.length; i++) {
    const entry = manifest.entries[i];
    if (!entry) throw new Error('阅读清单对象缺失');
    assertReadingId(entry.objectId);
    if (entry.objectId === identity.objectId || ids.has(entry.objectId) || entry.keyId !== identity.keyId ||
      !Number.isSafeInteger(entry.revision) || entry.revision < 1 || !['article', 'image'].includes(entry.kind) ||
      !Number.isSafeInteger(entry.ciphertextBytes) || entry.ciphertextBytes < 1 || entry.ciphertextBytes > READING_MAX_TOKEN_CHARS ||
      !/^[a-f0-9]{64}$/.test(entry.ciphertextSha256)) throw new Error('阅读清单对象无效或重复');
    bytes += entry.ciphertextBytes; ids.add(entry.objectId);
  }
  if (bytes > 64 * 1024 * 1024) throw new Error('当前阅读快照超过 64 MiB，请在电脑缩小范围');
  return manifest;
}
function bounded(value: string, limit: number): boolean { return typeof value === 'string' && value.length <= limit; }
export function parseArticle(text: string, objectId: string): ReadingArticle {
  const article = JSON.parse(text) as ReadingArticle;
  if (!article || article.schema !== 1 || article.id !== objectId || !bounded(article.title, 8192) ||
    !bounded(article.content, 4 * 1024 * 1024) || (article.summary !== null && !bounded(article.summary, 128 * 1024)) ||
    !bounded(article.collectionName, 8192) || !Array.isArray(article.tags) || article.tags.length > 100 ||
    article.tags.some((tag: string): boolean => !bounded(tag, 1024)) ||
    !['active', 'archived'].includes(article.status) || !['note', 'webpage', 'video', 'image', 'audio', 'document', 'snippet', 'forum'].includes(article.itemType)) {
    throw new Error('阅读正文格式无效');
  }
  // 原生文本阅读器不执行 HTML、脚本或嵌入资源；来源只能经用户主动操作打开。
  if (article.sourceUrl !== null && !safeReadingLink(article.sourceUrl)) article.sourceUrl = null;
  return article;
}
export function safeReadingLink(value: string): boolean {
  return typeof value === 'string' && value.length <= 8192 && /^https:\/\/[a-z0-9.-]+(?::[0-9]{1,5})?(?:\/[^\s]*)?$/i.test(value) && !/[@\\\u0000-\u0020]/.test(value);
}
export function findArticles(articles: ReadingArticle[], query: string, collection: string, tag: string): ReadingArticle[] {
  const terms = query.trim().toLocaleLowerCase().split(/\s+/).filter((word: string): boolean => word !== '');
  return articles.filter((article: ReadingArticle): boolean => {
    if ((collection && article.collectionName !== collection) || (tag && !article.tags.includes(tag))) return false;
    const text = [article.title, article.content, article.summary || '', article.collectionName, article.tags.join(' ')].join('\n').toLocaleLowerCase();
    return terms.every((word: string): boolean => text.includes(word));
  });
}
