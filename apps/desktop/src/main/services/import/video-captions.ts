/**
 * 在线视频平台字幕：先尝试发布者字幕，再尝试平台自动字幕。
 *
 * yt-dlp 已经处理了各平台字幕接口、签名与格式差异；这里仅负责把下载到
 * 临时目录的 VTT / SRT 清成可入库的文字。字幕不可用是正常分支，绝不能
 * 让它阻断后续 ASR。
 */
import fs from "fs";
import os from "os";
import path from "path";
import { randomUUID } from "crypto";

const CAPTION_TIMEOUT_MS = 90 * 1000;

export type CaptionRunCommand = (
  executable: string,
  args: string[],
  options: { timeoutMs: number; signal?: AbortSignal },
) => Promise<unknown>;

export type PlatformCaptionSource =
  "platform-subtitles" | "platform-ai-captions";

export interface PlatformCaption {
  text: string;
  source: PlatformCaptionSource;
  /** yt-dlp 文件名里携带的语言标记，仅用于留痕，不作为语言承诺 */
  language?: string;
}

/**
 * 清理 WebVTT / SRT cue 标记。保留换行而非拼成一段，方便后续全文检索与摘要；
 * 自动字幕经常重复相邻 cue，连续相同的行只留一次。
 */
export function parseCaptionText(raw: string): string {
  const lines: string[] = [];
  let skipStyleBlock = false;
  for (const original of raw.replace(/^\uFEFF/, "").split(/\r?\n/)) {
    const line = original.trim();
    if (!line) {
      skipStyleBlock = false;
      continue;
    }
    if (/^(WEBVTT|NOTE|STYLE|REGION)(\s|$)/i.test(line)) {
      skipStyleBlock = /^(STYLE|REGION)(\s|$)/i.test(line);
      continue;
    }
    if (skipStyleBlock || /^\d+$/.test(line) || line.includes("-->")) {
      continue;
    }
    const text = line
      .replace(/<[^>]*>/g, "")
      .replace(/&nbsp;/gi, " ")
      .replace(/&amp;/gi, "&")
      .replace(/&lt;/gi, "<")
      .replace(/&gt;/gi, ">")
      .replace(/\s+/g, " ")
      .trim();
    if (text && lines.at(-1) !== text) {
      lines.push(text);
    }
  }
  return lines.join("\n").trim();
}

function preferredCaptionFiles(dir: string): string[] {
  // 某些 yt-dlp 错误路径会自行清理输出目录；这与「没有字幕」等价，
  // 不能把一个正常的 ASR 回退打印成异常。
  if (!fs.existsSync(dir)) {
    return [];
  }
  const files = fs
    .readdirSync(dir)
    .filter((name) => /\.(vtt|srt)$/i.test(name));
  // 中文视频是当前产品的主场；没有中文时仍会取任意可读字幕，不人为丢内容。
  return files.sort((left, right) => {
    const score = (name: string) =>
      /\.(zh|zh-[a-z-]+|cmn|yue)\./i.test(name) ? 0 : 1;
    return score(left) - score(right) || left.localeCompare(right);
  });
}

function captionLanguage(fileName: string): string | undefined {
  const match = /^caption\.([^.]+)\.(?:vtt|srt)$/i.exec(fileName);
  return match?.[1];
}

async function downloadCaptionSet(
  executable: string,
  url: string,
  dir: string,
  run: CaptionRunCommand,
  source: PlatformCaptionSource,
  automatic: boolean,
  signal?: AbortSignal,
): Promise<PlatformCaption | null> {
  try {
    await run(
      executable,
      [
        "--no-warnings",
        "--no-playlist",
        "--skip-download",
        "--write-subs",
        ...(automatic ? ["--write-auto-subs"] : []),
        "--sub-langs",
        "all,-live_chat",
        "--sub-format",
        "vtt/srt/best",
        "-o",
        path.join(dir, "caption.%(ext)s"),
        url,
      ],
      { timeoutMs: CAPTION_TIMEOUT_MS, signal },
    );
  } catch {
    // 没有字幕、地区限制与单个平台接口变动都不该拦住 ASR 兜底。
    return null;
  }

  for (const fileName of preferredCaptionFiles(dir)) {
    const text = parseCaptionText(
      fs.readFileSync(path.join(dir, fileName), "utf8"),
    );
    if (text) {
      return { text, source, language: captionLanguage(fileName) };
    }
  }
  return null;
}

/** 发布者字幕优先；确实不存在时才向平台请求自动字幕。 */
export async function downloadPlatformCaptions(
  executable: string,
  url: string,
  run: CaptionRunCommand,
  signal?: AbortSignal,
): Promise<PlatformCaption | null> {
  const dir = path.join(
    os.tmpdir(),
    `guizhi-captions-${randomUUID().slice(0, 8)}`,
  );
  fs.mkdirSync(dir, { recursive: true });
  try {
    const published = await downloadCaptionSet(
      executable,
      url,
      dir,
      run,
      "platform-subtitles",
      false,
      signal,
    );
    if (published) {
      return published;
    }
    return downloadCaptionSet(
      executable,
      url,
      dir,
      run,
      "platform-ai-captions",
      true,
      signal,
    );
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}
