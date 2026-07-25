/** 判断输入整体是否为单个 http(s) 链接（用于采集自动识别）。 */
export function isHttpUrlLike(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed || /\s/.test(trimmed) || !/^https?:\/\//i.test(trimmed)) {
    return false;
  }
  try {
    const parsed = new URL(trimmed);
    return parsed.protocol === "http:" || parsed.protocol === "https:";
  } catch {
    return false;
  }
}
