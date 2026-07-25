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

export type CaptureDraft =
  | { kind: "empty" }
  | { kind: "urls"; urls: string[] }
  | { kind: "text"; text: string };

/**
 * 解析采集框内容。
 *
 * 从浏览器书签或笔记里复制一批链接粘进来是采集的核心用例，而此前只要输入
 * 含任何空白就判定为非链接，整段被塞进一条文本笔记——没有报错也没有提示，
 * 用户以为导入了 20 个网页，实际得到一条纯文本。
 *
 * 现在按空白切分：全部是 http(s) 链接才作为链接批处理，只要混进一段说明
 * 文字就整体按文本保存（避免丢掉用户写的上下文）。
 */
export function parseCaptureDraft(value: string): CaptureDraft {
  const trimmed = value.trim();
  if (!trimmed) {
    return { kind: "empty" };
  }

  const tokens = trimmed.split(/\s+/).filter(Boolean);
  if (tokens.length > 0 && tokens.every(isHttpUrlLike)) {
    // 同一批里重复粘贴的链接去重，避免生成重复任务
    return { kind: "urls", urls: [...new Set(tokens)] };
  }

  return { kind: "text", text: trimmed };
}
