/**
 * 视频条目元数据引用块的解析。
 *
 * 在线视频导入时正文开头写入 `> 平台：… · 作者：… · 时长：…`
 * （AI 换标题后追加 `> 原标题：…` 行）。正文仍是数据载体
 * （全文检索 / 导出 / 旧条目兼容），渲染视图把该块剥离出来，
 * 交给界面组件（MediaMetaCard）展示。
 */

export interface VideoMetaBlock {
  platform?: string;
  author?: string;
  /** 已格式化时长（如 12:52） */
  duration?: string;
  originalTitle?: string;
  /** 平台简介（导入时压缩为单行写入引用块） */
  description?: string;
  /** 文字稿来自发布者字幕、平台 AI 字幕或音频识别，供用户判断适用范围 */
  transcriptSource?: string;
  /** 去掉元数据引用块后的正文 */
  body: string;
}

const META_FIELD_KEYS = {
  平台: "platform",
  作者: "author",
  时长: "duration",
} as const;

/** 去掉行首的引用标记（`> ` / `>`） */
function stripQuoteMarker(line: string): string {
  return line.replace(/^>\s?/, "").trim();
}

/**
 * 解析正文开头的视频元数据引用块。
 * 只有首行为 `> 平台：…` 时才视为元数据块（普通引用不受影响），
 * 否则返回 null，调用方按原文渲染。
 */
export function parseVideoMetaBlock(content: string): VideoMetaBlock | null {
  const lines = content.split("\n");
  if (!/^>\s*平台[:：]/.test(lines[0] ?? "")) {
    return null;
  }

  const meta: VideoMetaBlock = { body: "" };
  let end = 0;
  while (end < lines.length && lines[end].startsWith(">")) {
    const text = stripQuoteMarker(lines[end]);
    const originalTitle = /^原标题[:：]\s*(.+)$/.exec(text);
    const description = /^简介[:：]\s*(.+)$/.exec(text);
    const transcriptSource = /^文字稿来源[:：]\s*(.+)$/.exec(text);
    if (originalTitle) {
      meta.originalTitle = originalTitle[1].trim();
    } else if (description) {
      // 简介整行取值，不参与 · 拆分（内容本身可能含 · 字符）
      meta.description = description[1].trim();
    } else if (transcriptSource) {
      meta.transcriptSource = transcriptSource[1].trim();
    } else {
      // 首行形如「平台：X · 作者：Y · 时长：Z」，字段间以 · 分隔
      for (const part of text.split("·")) {
        const pair = /^(平台|作者|时长)[:：]\s*(.+)$/.exec(part.trim());
        if (pair) {
          meta[META_FIELD_KEYS[pair[1] as keyof typeof META_FIELD_KEYS]] =
            pair[2].trim();
        }
      }
    }
    end++;
  }

  meta.body = lines.slice(end).join("\n").replace(/^\n+/, "");
  return meta;
}
