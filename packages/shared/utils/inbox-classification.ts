/** 处理中心 AI 归类的跨进程边界。 */
export const INBOX_AI_CLASSIFICATION_MAX_ITEMS = 100;
export const INBOX_AI_CLASSIFICATION_MAX_NEW_COLLECTIONS = 8;
export const INBOX_AI_COLLECTION_NAME_MAX_LENGTH = 20;

const RESERVED_NEW_COLLECTION_NAMES = new Set([
  "全部",
  "未分类",
  "未归知识库",
  "收藏",
  "归档",
  "回收站",
]);

export function normalizeAiCollectionName(value: string): string {
  return value.normalize("NFKC").replace(/\s+/g, " ").trim();
}

export function aiCollectionNameKey(value: string): string {
  return normalizeAiCollectionName(value).toLocaleLowerCase();
}

export function isValidAiCollectionName(value: string): boolean {
  const normalized = normalizeAiCollectionName(value);
  return (
    normalized.length > 0 &&
    normalized.length <= INBOX_AI_COLLECTION_NAME_MAX_LENGTH &&
    !/[\r\n\t]/.test(normalized)
  );
}

export function isReservedNewCollectionName(value: string): boolean {
  return RESERVED_NEW_COLLECTION_NAMES.has(normalizeAiCollectionName(value));
}
