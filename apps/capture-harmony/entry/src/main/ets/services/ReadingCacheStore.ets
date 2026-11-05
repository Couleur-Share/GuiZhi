import { common } from '@kit.AbilityKit';
import { fileIo as fs } from '@kit.CoreFileKit';
import { ReadingConnection, ReadingLocal } from '../core/ReadingModel';
import { ReadingIdentity, READING_MAX_TOKEN_CHARS, assertReadingId } from '../core/ReadingSyncFormat';
import { ReadingCache } from '../core/ReadingLibrary';
import { ReadingCrypto } from './ReadingCrypto';

// 原子文件保存加密清单与游标；正文/图片仍用云端 JWE，不落明文索引或临时媒体文件。
export class ReadingCacheStore implements ReadingCache {
  private root: string;
  private crypto: ReadingCrypto = new ReadingCrypto();
  private used: number = 0;
  private count: number = 0;
  private connection: ReadingConnection;
  constructor(context: common.Context, connection: ReadingConnection) {
    this.connection = connection;
    assertReadingId(connection.cacheId);
    const root = context.filesDir + '/reading-v1';
    if (!fs.accessSync(root)) fs.mkdirSync(root);
    this.root = root + '/' + connection.cacheId;
    if (!fs.accessSync(this.root)) fs.mkdirSync(this.root);
    this.measure();
  }
  private measure(): void {
    const files = fs.listFileSync(this.root); this.used = 0; this.count = files.length;
    for (let i = 0; i < files.length; i++) this.used += fs.statSync(this.root + '/' + files[i]).size;
  }
  private identity(): ReadingIdentity {
    return { libraryId: this.connection.libraryId, generation: this.connection.generation, objectId: this.connection.cacheId,
      keyId: this.connection.cacheId, revision: 1, kind: 'manifest' };
  }
  private objectPath(digest: string): string {
    if (!/^[a-f0-9]{64}$/.test(digest)) throw new Error('阅读缓存地址无效');
    return this.root + '/' + digest + '.jwe';
  }
  private read(file: string): string | null {
    if (!fs.accessSync(file)) return null;
    if (fs.statSync(file).size > READING_MAX_TOKEN_CHARS) throw new Error('本地阅读文件大小无效');
    return fs.readTextSync(file);
  }
  private atomic(file: string, token: string): void {
    const temporary = file + '.tmp';
    const handle = fs.openSync(temporary, fs.OpenMode.CREATE | fs.OpenMode.WRITE_ONLY | fs.OpenMode.TRUNC);
    try {
      const bytes = this.crypto.bytes(token);
      if (fs.writeSync(handle.fd, bytes.buffer) !== bytes.length) throw new Error('阅读磁盘空间不足，未切换可见版本');
      fs.fsyncSync(handle.fd);
    } finally { fs.closeSync(handle); }
    fs.renameSync(temporary, file);
  }
  async load(): Promise<ReadingLocal | null> {
    const token = this.read(this.root + '/current.jwe'); if (token === null) return null;
    const key = this.crypto.decode(this.connection.cacheKey);
    let bytes: Uint8Array | undefined;
    try { bytes = await this.crypto.decrypt(key, this.identity(), token); return JSON.parse(this.crypto.text(bytes)) as ReadingLocal; }
    finally { key.fill(0); if (bytes) bytes.fill(0); }
  }
  async commit(state: ReadingLocal, assertActive: () => void = () => {}): Promise<void> {
    const key = this.crypto.decode(this.connection.cacheKey), bytes = this.crypto.bytes(JSON.stringify(state));
    try {
      const token = await this.crypto.encrypt(key, this.identity(), bytes);
      // 加密等待期间可能已进入后台；取消的旧运行时不得覆盖新会话的检查点。
      assertActive(); this.atomic(this.root + '/current.jwe', token); this.measure();
    }
    finally { key.fill(0); bytes.fill(0); }
  }
  async object(digest: string): Promise<string | null> { return this.read(this.objectPath(digest)); }
  async put(digest: string, token: string): Promise<void> {
    const target = this.objectPath(digest);
    if (token.length > READING_MAX_TOKEN_CHARS) throw new Error('阅读缓存对象过大');
    const exists = fs.accessSync(target), before = exists ? fs.statSync(target).size : 0;
    if (this.used + token.length > 192 * 1024 * 1024 || this.count > 20002) throw new Error('本机阅读缓存达到上限，请清理图片缓存后重试');
    this.atomic(target, token);
    this.used += token.length - before; if (!exists) this.count++;
  }
  async prune(keep: string[]): Promise<void> {
    const allowed = new Set<string>(keep.map((digest: string): string => this.objectPath(digest)));
    const files = fs.listFileSync(this.root);
    for (let i = 0; i < files.length; i++) {
      const file = files[i];
      if ((/^[a-f0-9]{64}\.jwe$/.test(file) && !allowed.has(this.root + '/' + file)) || file.endsWith('.tmp')) fs.unlinkSync(this.root + '/' + file);
    }
    this.measure();
  }
  clear(): void {
    // 仅清除当前阅读连接自己的目录；不触碰收集草稿、正式包或其他连接。
    for (const file of fs.listFileSync(this.root)) {
      if (!/^(current|[a-f0-9]{64})\.jwe(?:\.tmp)?$/.test(file)) throw new Error('阅读目录含未知文件，已停止清理');
    }
    for (const file of fs.listFileSync(this.root)) fs.unlinkSync(this.root + '/' + file);
    fs.rmdirSync(this.root);
  }
}
