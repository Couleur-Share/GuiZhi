import { createHash, createHmac } from 'node:crypto';
import { readFileSync, lstatSync, realpathSync } from 'node:fs';
import path from 'node:path';
import type Database from '@guizhi/db/adapter';
import { isSafeAssetFileName } from '@guizhi/shared/utils/media-refs';
import { projectReadingArticle } from '@guizhi/core/reading-sync/projection';
import type { ReadingObjectInput } from '@guizhi/core/reading-sync/snapshot';

interface Row { id: string; title: string; content: string; summary: string | null; item_type: string; status: string; collection_name: string; }
// 全量白名单投影兼作补偿扫描；删除、移库、标签和正文变化均进入新清单，不依赖更新时间。
export function readSelectedLibrary(db: Database.Database, collections: string[], imageRoot: string, idKey: Buffer) {
  return db.transaction(() => projectSelectedLibrary(db, collections, imageRoot, idKey))();
}
function projectSelectedLibrary(db: Database.Database, collections: string[], imageRoot: string, idKey: Buffer) {
  if (collections.length > 100 || new Set(collections).size !== collections.length || collections.some(id => typeof id !== 'string' || id.length > 128)) {
    throw new Error('同步知识库选择无效');
  }
  const objects: ReadingObjectInput[] = [], sourceHash = createHash('sha256');
  const syncId = (type: string, value: string) => createHmac('sha256', idKey).update(`${type}:${value}`).digest('base64url');
  if (collections.length === 0) return { objects, sourceDigest: sourceHash.update('empty').digest('hex') };
  const marks = collections.map(() => '?').join(',');
  const selected = db.prepare(`SELECT id FROM collections WHERE id IN (${marks})`).all(...collections) as { id: string }[];
  if (selected.length !== collections.length) throw new Error('所选知识库已不存在，请重新选择');
  const rows = db.prepare(`SELECT i.id,i.title,i.content,i.summary,i.item_type,i.status,c.name collection_name
    FROM knowledge_items i JOIN collections c ON c.id=i.collection_id WHERE i.deleted_at IS NULL
    AND i.collection_id IN (${marks}) ORDER BY i.id LIMIT 10001`).all(...collections) as Row[];
  if (rows.length > 10000) throw new Error('当前阅读试点最多同步 10000 个对象，请缩小知识库范围');
  const assets = new Map<string, ReadingObjectInput>();
  let totalBytes = 0;
  const account = (bytes: number) => { totalBytes += bytes; if (totalBytes > 40 * 1024 * 1024) throw new Error('当前阅读试点单次正文与图片超过 40 MiB，请缩小范围'); };
  for (const row of rows) {
    // 本轮只读取用户当前编辑正文，不读取原始网页 HTML 或访问凭证。
    const rewrite = (text: string) => text.replace(/local-(image|video):\/\/[^\s)\]<>"']+/g, (match, kind: string) => {
      if (kind !== 'image') return '[视频原文件未同步]';
      const fileName = match.slice('local-image://'.length);
      if (!isSafeAssetFileName(fileName) || !/\.(png|jpe?g|gif|webp|avif)$/i.test(fileName)) throw new Error('存在不支持的本地图片引用，请先整理该文章');
      const objectId = syncId('image', fileName);
      if (!assets.has(objectId)) {
        const root = realpathSync(imageRoot), file = path.join(root, fileName), stat = lstatSync(file);
        if (!stat.isFile() || stat.isSymbolicLink() || path.dirname(realpathSync(file)) !== root || stat.size > 8 * 1024 * 1024) {
          throw new Error('阅读图片路径或大小无效');
        }
        account(stat.size);
        const bytes = readFileSync(file);
        if (bytes.length !== stat.size) throw new Error('阅读图片在采集时发生变化，请重试');
        assets.set(objectId, { objectId, revision: 1, kind: 'image', bytes });
      }
      return `guizhi-sync://${objectId}`;
    });
    const content = rewrite(row.content), summary = row.summary === null ? null : rewrite(row.summary);
    const tags = db.prepare('SELECT t.name FROM tags t JOIN knowledge_item_tags it ON it.tag_id=t.id WHERE it.item_id=? ORDER BY t.id').all(row.id) as { name: string }[];
    const source = db.prepare('SELECT normalized_uri FROM source_records WHERE item_id=? ORDER BY captured_at DESC LIMIT 1').get(row.id) as { normalized_uri: string | null } | undefined;
    const article = projectReadingArticle(syncId('article', row.id), { title: row.title, content, summary,
      itemType: row.item_type, status: row.status, collectionName: row.collection_name, tags: tags.map(tag => tag.name), publicSourceUrl: source?.normalized_uri ?? null });
    const bytes = Buffer.from(JSON.stringify(article)); account(bytes.length);
    objects.push({ objectId: article.id, kind: 'article', revision: 1, bytes });
  }
  objects.push(...assets.values()); objects.sort((a, b) => a.objectId.localeCompare(b.objectId));
  for (const object of objects) sourceHash.update(JSON.stringify([object.objectId, object.kind, object.bytes.length])).update(object.bytes);
  return { objects, sourceDigest: sourceHash.digest('hex') };
}
