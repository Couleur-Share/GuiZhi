import type { KnowledgeItem } from './knowledge';

export interface KnowledgeDraftFields {
  title?: string;
  content?: string;
  tagNames?: string[];
}
export interface SaveKnowledgeDraftInput {
  id: string;
  requestId: string;
  base: KnowledgeDraftFields;
  patch: KnowledgeDraftFields;
}
export interface SaveKnowledgeDraftResult {
  ok: boolean;
  item?: KnowledgeItem;
  conflicts?: Array<'title' | 'content'>;
  error?: string;
}

/** 标签按相对基线的增删合并，保留另一入口新增的标签。 */
export function mergeDraftTags(base: string[], edited: string[], current: string[]): string[] {
  const key = (name: string) => name.trim().toLowerCase();
  const wanted = new Set(edited.map(key));
  const removed = new Set(base.map(key).filter(name => !wanted.has(name)));
  const result = new Map(current.filter(name => !removed.has(key(name))).map(name => [key(name), name]));
  const original = new Set(base.map(key));
  for (const name of edited) if (!original.has(key(name))) result.set(key(name), name);
  return [...result.values()];
}
