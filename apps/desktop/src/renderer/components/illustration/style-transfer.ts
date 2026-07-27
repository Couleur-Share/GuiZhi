/**
 * 单套风格的导出与导入。
 *
 * 走剪贴板文本而不是文件：风格是一段 JSON，实际分享的样子就是贴进聊天窗口。
 * 导入不读剪贴板（Electron 里 readText 未必拿得到权限，失败还是得回落到
 * 粘贴框），一律让用户粘进来——从文件导入的人也是打开文件复制内容，路径一样通。
 *
 * 这里重写了一遍归一化而不是复用 core：core 的那份 import 了 fs，
 * 渲染进程搬不动。两边的字段约束保持一致，越界值一律夹回。
 */
import {
  ILLUSTRATION_ASPECT_RATIOS,
  type IllustrationAspectRatio,
  type IllustrationStyle,
} from "@guizhi/shared/types";

const TRANSFER_KIND = "guizhi-illustration-style";
const MAX_SHOTS_LIMIT = 12;
const MAX_LABELS_LIMIT = 10;

export function exportStyleJson(style: IllustrationStyle): string {
  return `${JSON.stringify(
    { kind: TRANSFER_KIND, version: 1, style },
    null,
    2,
  )}\n`;
}

function readText(source: Record<string, unknown>, key: string): string {
  const value = source[key];
  return typeof value === "string" ? value.trim() : "";
}

function clamp(value: unknown, fallback: number, max: number): number {
  const parsed = Math.trunc(Number(value));
  if (!Number.isFinite(parsed) || parsed < 1) {
    return fallback;
  }
  return Math.min(parsed, max);
}

export interface StyleImportResult {
  style?: IllustrationStyle;
  error?: string;
}

/**
 * 解析粘贴进来的风格。
 *
 * 整份预设文件里的一条（裸对象）和导出的信封两种形态都收：用户多半是从
 * illustration-styles.json 里抠一段出来发给别人的。
 */
export function parseStyleJson(
  text: string,
  messages: { invalid: string; incomplete: string },
): StyleImportResult {
  let parsed: unknown;
  try {
    parsed = JSON.parse(text.trim());
  } catch {
    return { error: messages.invalid };
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return { error: messages.invalid };
  }
  const envelope = parsed as Record<string, unknown>;
  const raw =
    envelope.style && typeof envelope.style === "object"
      ? (envelope.style as Record<string, unknown>)
      : envelope;

  const name = readText(raw, "name");
  const visualDna = readText(raw, "visualDna");
  if (!name || !visualDna) {
    return { error: messages.incomplete };
  }
  const aspectRatio = raw.aspectRatio;
  return {
    style: {
      // id 撞了由 core 在保存时顺延后缀，这里保留原值方便认出来源
      id: readText(raw, "id") || `style-${Date.now().toString(36)}`,
      name,
      description: readText(raw, "description"),
      group: readText(raw, "group"),
      visualDna,
      character: readText(raw, "character"),
      negative: readText(raw, "negative"),
      aspectRatio: ILLUSTRATION_ASPECT_RATIOS.includes(
        aspectRatio as IllustrationAspectRatio,
      )
        ? (aspectRatio as IllustrationAspectRatio)
        : "16:9",
      maxShots: clamp(raw.maxShots, 4, MAX_SHOTS_LIMIT),
      maxLabels: clamp(raw.maxLabels, 5, MAX_LABELS_LIMIT),
    },
  };
}
