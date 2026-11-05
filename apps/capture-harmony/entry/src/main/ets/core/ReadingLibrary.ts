import { ReadingArticle, ReadingConnection, ReadingCryptoPort, ReadingEntry, ReadingHead, ReadingLocal, ReadingManifest,
  entryIdentity, manifestIdentity, parseArticle, parseManifest, validateHead } from './ReadingModel';
import { readingImageIds } from './ReadingReader';

export interface ReadingRemote { head(): Promise<ReadingHead>; object(digest: string): Promise<string>; cancel(): void; }
export interface ReadingCache {
  load(): Promise<ReadingLocal | null>;
  commit(state: ReadingLocal, assertActive: () => void): Promise<void>;
  object(digest: string): Promise<string | null>;
  put(digest: string, token: string): Promise<void>;
  prune(keep: string[]): Promise<void>;
}
export interface ReadingProgress { completed: number; total: number; message: string; }
export interface ReadingImagesResult { saved: number; failed: string[]; }
export class ReadingLibrary {
  articles: ReadingArticle[] = [];
  current: ReadingLocal | null = null;
  manifest: ReadingManifest | null = null;
  busy: boolean = false;
  private epoch: number = 0;
  constructor(private connection: ReadingConnection, private crypto: ReadingCryptoPort, private cache: ReadingCache, private remote: ReadingRemote) {}
  cancel(): void { this.epoch++; this.remote.cancel(); }
  lock(): void { this.cancel(); this.articles = []; this.manifest = null; this.current = null; }
  private assertActive(epoch: number): void { if (epoch !== this.epoch) throw new Error('阅读操作已取消，已下载的密文保留'); }
  private async manifestOf(head: ReadingHead, key: Uint8Array): Promise<ReadingManifest> {
    validateHead(this.connection, null, head);
    if (await this.crypto.hash(head.manifestToken!) !== head.digest) throw new Error('阅读清单哈希不匹配');
    const plain = await this.crypto.decrypt(key, manifestIdentity(this.connection, head.sequence), head.manifestToken!);
    try { return parseManifest(this.crypto.text(plain), manifestIdentity(this.connection, head.sequence)); }
    finally { plain.fill(0); }
  }
  private async checked(entry: ReadingEntry, key: Uint8Array, online: boolean, epoch: number): Promise<Uint8Array> {
    let token = await this.cache.object(entry.ciphertextSha256);
    this.assertActive(epoch);
    if (token && (token.length !== entry.ciphertextBytes || await this.crypto.hash(token) !== entry.ciphertextSha256)) token = null;
    if (!token) {
      if (!online) throw new Error('本地阅读文件缺失或损坏，请联网重新同步');
      token = await this.remote.object(entry.ciphertextSha256); this.assertActive(epoch);
      if (typeof token !== 'string' || token.length !== entry.ciphertextBytes || await this.crypto.hash(token) !== entry.ciphertextSha256) throw new Error('阅读对象缺失或被篡改');
      // 验证认证标签后才缓存；所有磁盘对象始终是原始密文。
      const plain = await this.crypto.decrypt(key, entryIdentity(this.connection, entry), token);
      try { this.assertActive(epoch); await this.cache.put(entry.ciphertextSha256, token); return plain; }
      catch (error) { plain.fill(0); throw error as Error; }
    }
    const plain = await this.crypto.decrypt(key, entryIdentity(this.connection, entry), token);
    try { this.assertActive(epoch); return plain; } catch (error) { plain.fill(0); throw error as Error; }
  }
  private async loadArticles(manifest: ReadingManifest, key: Uint8Array, online: boolean, epoch: number,
    progress: (value: ReadingProgress) => void): Promise<ReadingArticle[]> {
    const entries = manifest.entries.filter((entry: ReadingEntry): boolean => entry.kind === 'article'), articles: ReadingArticle[] = [];
    for (let i = 0; i < entries.length; i++) {
      const plain = await this.checked(entries[i], key, online, epoch);
      try { articles.push(parseArticle(this.crypto.text(plain), entries[i].objectId)); }
      finally { plain.fill(0); }
      this.assertActive(epoch); progress({ completed: i + 1, total: entries.length, message: '正在校验正文' });
    }
    return articles;
  }
  async open(): Promise<void> {
    if (this.busy) throw new Error('阅读操作正在进行');
    this.busy = true; const epoch = this.epoch, key = this.crypto.decode(this.connection.contentKey);
    try {
      const local = await this.cache.load(); this.assertActive(epoch);
      if (!local) return;
      if (local.schema !== 1 || !Number.isSafeInteger(local.syncedAt)) throw new Error('本地阅读检查点损坏');
      const manifest = await this.manifestOf(local.head, key);
      const articles = await this.loadArticles(manifest, key, false, epoch, () => {});
      this.assertActive(epoch); this.current = local; this.manifest = manifest; this.articles = articles;
    } finally { key.fill(0); this.busy = false; }
  }
  async sync(progress: (value: ReadingProgress) => void): Promise<void> {
    if (this.busy) throw new Error('阅读操作正在进行');
    this.busy = true; const epoch = this.epoch, key = this.crypto.decode(this.connection.contentKey);
    try {
      // 即使首次加载因缺件失败，也先读取已有检查点，不能降级回配对时的旧序号。
      const previous = this.current || await this.cache.load(); this.assertActive(epoch);
      const previousManifest = previous ? await this.manifestOf(previous.head, key) : null;
      const head = await this.remote.head(); this.assertActive(epoch);
      validateHead(this.connection, previous ? previous.head : null, head);
      const manifest = await this.manifestOf(head, key);
      // 只清理既不属于当前版本、也不属于候选版本的中断残留，空间不足时仍保留当前可读版本。
      const keep = manifest.entries.map((entry: ReadingEntry): string => entry.ciphertextSha256);
      if (previousManifest) previousManifest.entries.forEach((entry: ReadingEntry) => keep.push(entry.ciphertextSha256));
      this.assertActive(epoch); await this.cache.prune(keep);
      progress({ completed: 0, total: manifest.entries.filter((entry: ReadingEntry): boolean => entry.kind === 'article').length, message: '正在下载正文' });
      const articles = await this.loadArticles(manifest, key, true, epoch, progress);
      this.assertActive(epoch);
      const next: ReadingLocal = { schema: 1, head: head, syncedAt: Date.now() };
      // 一个加密原子文件同时切换可见清单与游标；此前的密文缓存不改变可见版本。
      await this.cache.commit(next, () => this.assertActive(epoch)); this.assertActive(epoch);
      this.current = next; this.manifest = manifest; this.articles = articles;
      // 提交后回收撤回对象；失败可重试，不伪装成全部完成。
      await this.cache.prune(manifest.entries.map((entry: ReadingEntry): string => entry.ciphertextSha256));
    } finally { key.fill(0); this.busy = false; }
  }
  async image(id: string): Promise<Uint8Array | null> {
    const entry = this.manifest?.entries.find((value: ReadingEntry): boolean => value.kind === 'image' && value.objectId === id);
    if (!entry) throw new Error('图片不在当前阅读清单内，请先同步正文');
    if (!await this.cache.object(entry.ciphertextSha256)) return null;
    const key = this.crypto.decode(this.connection.contentKey);
    try { return await this.checked(entry, key, false, this.epoch); } finally { key.fill(0); }
  }
  async saveImages(articleId: string, progress: (value: ReadingProgress) => void): Promise<ReadingImagesResult> {
    if (this.busy) throw new Error('阅读操作正在进行');
    const article = this.articles.find((value: ReadingArticle): boolean => value.id === articleId);
    if (!article || !this.manifest) throw new Error('请先下载这篇正文');
    this.busy = true; const epoch = this.epoch, key = this.crypto.decode(this.connection.contentKey);
    const result: ReadingImagesResult = { saved: 0, failed: [] }, ids = readingImageIds(article.content);
    try {
      for (let i = 0; i < ids.length; i++) {
        this.assertActive(epoch);
        const entry = this.manifest.entries.find((value: ReadingEntry): boolean => value.kind === 'image' && value.objectId === ids[i]);
        try {
          if (!entry) throw new Error('图片不在同步清单内');
          const bytes = await this.checked(entry, key, true, epoch); bytes.fill(0); result.saved++;
        } catch (error) { this.assertActive(epoch); result.failed.push(`图片 ${i + 1}：${(error as Error).message}`); }
        progress({ completed: i + 1, total: ids.length, message: '正在保存图片' });
      }
      return result;
    } finally { key.fill(0); this.busy = false; }
  }
  async clearImages(): Promise<void> {
    if (this.busy || !this.manifest) throw new Error('请等待当前阅读操作完成');
    this.busy = true;
    try { await this.cache.prune(this.manifest.entries.filter((entry: ReadingEntry): boolean => entry.kind === 'article').map((entry: ReadingEntry): string => entry.ciphertextSha256)); }
    finally { this.busy = false; }
  }
}
