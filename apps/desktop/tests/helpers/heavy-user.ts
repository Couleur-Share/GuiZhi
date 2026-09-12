import type { KnowledgeItem } from '@guizhi/shared/types';

export function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (reason: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
export function heavyItem(id: string, content = '原文'): KnowledgeItem {
  return { id, title: id, content, transcript: null, summary: null, itemType: 'note', status: 'active',
    collectionId: null, tags: [], isFavorite: false, isPinned: false, sourceUri: null,
    reviewStatus: 'clear', reviewReasons: [], deletedAt: null, createdAt: 1, updatedAt: 1 };
}
export const longArticle = '# 开头\n\n开头唯一事实。\n\n' + '普通段落。\n\n'.repeat(500) + '## 中部\n\n中部唯一事实。\n\n' + '普通段落。\n\n'.repeat(500) + '## 结尾\n\n结尾唯一事实。';
export const makeHeavyItems = (count: number) => Array.from({ length: count }, (_, index) => heavyItem(`item-${String(index).padStart(6, '0')}`, `资料 ${index} 的正文`));
