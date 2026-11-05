import { assertReadingId } from '../../../shared/utils/reading-sync-format';

// 仅接受已选定的阅读正文；数据库查询、选库授权与本地资源重写尚未接线。
export interface ReadingProjectionInput {
  title: string;
  content: string;
  summary: string | null;
  itemType: string;
  status: string;
  collectionName: string;
  tags: string[];
  publicSourceUrl: string | null;
}

export interface ReadingArticle {
  schema: number;
  id: string;
  title: string;
  content: string;
  summary: string | null;
  itemType: string;
  status: string;
  collectionName: string;
  tags: string[];
  sourceUrl: string | null;
}

function text(value: string, max: number): string {
  if (typeof value !== 'string' || Buffer.byteLength(value, 'utf8') > max) throw new Error('阅读投影文本无效或超限');
  return value;
}

function sourceUrl(value: string | null): string | null {
  if (value === null) return null;
  text(value, 8192);
  try {
    const url = new URL(value);
    if (url.protocol !== 'https:' || url.username || url.password) return null;
    // 不携带查询凭据与片段；访问原站仅能由用户主动触发。
    url.search = ''; url.hash = ''; return url.href;
  } catch { return null; }
}

export function projectReadingArticle(syncId: string, input: ReadingProjectionInput): ReadingArticle {
  assertReadingId(syncId);
  if (!input || !Array.isArray(input.tags) || input.tags.length > 100) throw new Error('阅读投影输入无效');
  if (!['note', 'webpage', 'video', 'image', 'audio', 'document', 'snippet', 'forum'].includes(input.itemType) ||
      !['active', 'archived'].includes(input.status)) throw new Error('阅读条目类型或状态无效');
  // 白名单显式构造，绝不展开数据库行。正文自身仍是用户内容，不承诺自动发现其中的秘密。
  return {
    schema: 1, id: syncId, title: text(input.title, 8192), content: text(input.content, 4 * 1024 * 1024),
    summary: input.summary === null ? null : text(input.summary, 128 * 1024),
    itemType: input.itemType, status: input.status, collectionName: text(input.collectionName, 8192),
    tags: [...new Set(input.tags.map(tag => text(tag, 1024)))], sourceUrl: sourceUrl(input.publicSourceUrl),
  };
}
