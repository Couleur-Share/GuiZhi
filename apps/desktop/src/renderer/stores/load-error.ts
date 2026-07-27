/**
 * 列表加载失败的原因文本。
 *
 * 加载类失败一律不弹 toast（用户可能只是切了个页签，不该被打断），
 * 但必须留在 store 里让列表区渲染成错误态——否则「读不出来」会被画成
 * 「什么都没有」，用户不会去重试。
 */
export function describeLoadError(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  return message.trim() || "未知错误";
}
