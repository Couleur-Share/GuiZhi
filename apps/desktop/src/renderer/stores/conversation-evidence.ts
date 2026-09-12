import { sanitizeEvidenceJson } from '@guizhi/shared/utils/evidence-sanitizer';
import type { ArticleTarget } from '@guizhi/shared/types/article-ask';

const clearedIds = new Set<string>();
const listeners = new Set<() => void>();
/** 当前窗口也遵守清除标记，迟到流和已展开片段不能再次暴露旧证据。 */
export function clearCachedConversationEvidence(ids: string[]): void {
  for (const id of ids) clearedIds.add(id);
  for (const listener of listeners) listener();
}
export function onConversationEvidenceCleared(listener: () => void): void { listeners.add(listener); }
export function sanitizeCachedMessages<T>(messages: T[]): T[] {
  return clearedIds.size ? JSON.parse(sanitizeEvidenceJson(JSON.stringify(messages), clearedIds)) : messages;
}
export function sanitizeCachedTarget<T extends ArticleTarget | null | undefined>(target: T): T {
  return target && clearedIds.has(target.itemId) ? { itemId: target.itemId, view: target.view } as T : target;
}
