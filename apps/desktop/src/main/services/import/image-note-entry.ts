/**
 * 图文作品的条目组装：配图落盘资产库 + 逐图 OCR。抖音与小红书共用这一份。
 *
 * 图文的信息主体常常在图里（信息图、指标对比表、架构流程图），只保留文案
 * 等于把内容主体丢了。而平台的图片地址都带时效签名（抖音的 `x-expires`
 * 约一个月，小红书的地址里直接写着日期），不落盘的话过期就再也取不回来——
 * 所以采集时必须把图拉进资产库。
 *
 * OCR 让图里的文字进入全文检索与语义索引，问答才用得上它。逐图识别按张
 * 计费，因此设了张数上限，超出的图片照常入库但不识别并在正文注明。
 *
 * 平台差异只剩三处，全部收在 ImageNoteSource 里：正文里写哪个平台名、
 * 单张图怎么下（UA / Referer 各不相同）、标题要不要 AI 重拟。
 */
import fs from "fs/promises";
import path from "path";
import type { ImportStage } from "@guizhi/shared/types";
import { OCR_SECTION_HEADING } from "@guizhi/shared/utils/ocr-request";
import type { ExtractedContent } from "./connectors";
import {
  getMediaAssetDir,
  mediaProtocolUrl,
  saveMediaAsset,
} from "./media-files";
import {
  recognizeImageFile,
  resolveOcrConfig,
  type OcrModelConfig,
} from "../media/ocr";
import {
  generateContentTitle,
  resolveMediaSummaryConfig,
} from "../media/media-summary";
import type { AIClientConfig } from "@guizhi/core";

/** 逐图识别按张计费，超出部分只入库不识别 */
export const OCR_IMAGE_LIMIT = 9;
/** 失败原因写进正文时的截断长度 */
const NOTE_ERROR_MAX_LENGTH = 120;

/** 组装一条图文条目所需的全部素材（由各平台的采集模块产出） */
export interface ImageNoteSource {
  /** 正文元数据引用块里的平台名 */
  platformLabel: string;
  title: string;
  /**
   * 标题是作者在平台上自己写的（小红书有独立标题字段）。
   * 为 false 时（抖音只能取文案首行，往往是半句话）才 AI 重拟。
   */
  authoredTitle: boolean;
  /** 完整文案；与标题相同时为空 */
  description: string;
  author: string;
  /** 逐张图片的 CDN 镜像地址（同一张图给多个源，下载时逐个降级） */
  imageMirrors: string[][];
  webpageUrl: string;
  /** 单张配图的下载实现：各平台的 UA / Referer / 扩展名判定都不一样 */
  downloadImage: (
    mirrors: string[],
    signal?: AbortSignal,
  ) => Promise<{ dir: string; filePath: string }>;
}

export interface ImageNoteDeps {
  /** 测试注入：下载单张配图（默认用 source 自带的实现） */
  downloadImage?: ImageNoteSource["downloadImage"];
  /** 测试注入：落盘到资产库，返回资产文件名 */
  saveAsset?: (filePath: string) => Promise<string>;
  /** 测试注入：视觉模型解析（默认读 ai-config.json 的 visionText 路由） */
  getOcrConfig?: () => OcrModelConfig | null;
  /** 测试注入：单张图片 OCR */
  recognize?: (
    filePath: string,
    config: OcrModelConfig,
    signal?: AbortSignal,
  ) => Promise<string>;
  /** 测试注入：拟题模型解析（默认读 ai-config.json 的 mainText 路由） */
  getTitleConfig?: () => AIClientConfig | null;
  /** 测试注入：AI 拟题 */
  generateTitle?: (
    source: string,
    config: AIClientConfig,
    options?: { signal?: AbortSignal },
  ) => Promise<string | null>;
  onStage?: (stage: ImportStage) => void;
}

/**
 * 行首会被 Markdown 当成块级标记的形态：标题、列表、引用、代码围栏、分隔线。
 * 转义掉它们，纯文本才会照原样渲染。
 */
function escapeMarkdownBlockStart(line: string): string {
  // 有序列表 `1.` / `1)`：只能转义标点，`\1` 不是合法转义
  const ordered = /^(\d{1,9})[.)]\s/.exec(line);
  if (ordered) {
    return `${ordered[1]}\\${line.slice(ordered[1].length)}`;
  }
  return /^(#{1,6}(\s|$)|[-+*]\s|>|```|~~~|-{3,}$|={3,}$|_{3,}$)/.test(line)
    ? `\\${line}`
    : line;
}

/**
 * 纯文本转 Markdown 正文。
 *
 * 平台文案是纯文本，原样存进正文会被 Markdown 吃掉结构：单个换行只当空格
 * （渲染出来整篇挤成一段），行首的 `1.` 会变成列表并把后面的段落吸进去。
 * 这里逐行转义行首标记再用空行分段，渲染结果与原文逐行对齐。
 */
export function plainTextToMarkdown(text: string): string {
  return text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
    .map(escapeMarkdownBlockStart)
    .join("\n\n");
}

/** 正文顶部的元数据引用块内容（形状与视频条目一致，详情页共用解析） */
export function buildImageNoteMetaLine(source: ImageNoteSource): string {
  return [
    `平台：${source.platformLabel}`,
    source.author ? `作者：${source.author}` : "",
    `图文 ${source.imageMirrors.length} 张`,
  ]
    .filter(Boolean)
    .join(" · ");
}

interface NoteImage {
  assetFileName: string;
  /** 资产文件的绝对路径，OCR 直接读它 */
  assetPath: string;
  /** 下载失败时的可读原因；有值时前两个字段为空 */
  error?: string;
}

async function collectImages(
  source: ImageNoteSource,
  deps: ImageNoteDeps,
  signal?: AbortSignal,
): Promise<NoteImage[]> {
  const download = deps.downloadImage ?? source.downloadImage;
  const save =
    deps.saveAsset ?? ((filePath: string) => saveMediaAsset(filePath, "image"));

  const images: NoteImage[] = [];
  for (const mirrors of source.imageMirrors) {
    let tempDir: string | null = null;
    try {
      const downloaded = await download(mirrors, signal);
      tempDir = downloaded.dir;
      const assetFileName = await save(downloaded.filePath);
      images.push({
        assetFileName,
        assetPath: path.join(getMediaAssetDir("image"), assetFileName),
      });
    } catch (error) {
      if (error instanceof Error && error.message === "已取消") {
        throw error;
      }
      // 单张失败不该让整条采集失败：其余图片与文案仍然有价值
      images.push({
        assetFileName: "",
        assetPath: "",
        error: error instanceof Error ? error.message : String(error),
      });
    } finally {
      if (tempDir) {
        await fs.rm(tempDir, { recursive: true, force: true });
      }
    }
  }
  return images;
}

interface OcrOutcome {
  /** 每张图的识别结果（未识别或失败的为 null） */
  texts: (string | null)[];
  attempted: number;
  failed: number;
  /** 首个失败原因，用于在正文里如实交代 */
  firstError?: string;
}

/** 逐图识别；单张失败不影响其余 */
async function recognizeImages(
  images: NoteImage[],
  config: OcrModelConfig,
  deps: ImageNoteDeps,
  signal?: AbortSignal,
): Promise<OcrOutcome> {
  const recognize = deps.recognize ?? recognizeImageFile;
  const outcome: OcrOutcome = {
    texts: [],
    attempted: 0,
    failed: 0,
  };

  for (const [index, image] of images.entries()) {
    if (!image.assetPath || index >= OCR_IMAGE_LIMIT) {
      outcome.texts.push(null);
      continue;
    }
    outcome.attempted += 1;
    try {
      outcome.texts.push(await recognize(image.assetPath, config, signal));
    } catch (error) {
      if (signal?.aborted) {
        throw new Error("已取消", { cause: error });
      }
      console.warn(`[import] 第 ${index + 1} 张图 OCR 失败:`, error);
      outcome.failed += 1;
      outcome.firstError ??=
        error instanceof Error ? error.message : String(error);
      outcome.texts.push(null);
    }
  }
  return outcome;
}

/**
 * 识别情况的正文注记。
 * 全军覆没时必须说明原因——否则用户只看到图进来了、文字没有，无从判断是
 * 模型没配、额度用完还是图里本就没字。
 */
function buildOcrNotes(outcome: OcrOutcome): string[] {
  if (outcome.failed === 0) {
    return [];
  }
  // 服务商的报错体可能上百字，正文里给个够判断的开头，完整内容进日志
  const reason = (outcome.firstError ?? "").slice(0, NOTE_ERROR_MAX_LENGTH);
  if (outcome.failed === outcome.attempted) {
    return [`> 图中文字识别失败：${reason}。可在详情页点「识别图中文字」重试。`];
  }
  return [`> 有 ${outcome.failed} 张图的文字识别失败（${reason}），可在详情页重试。`];
}

function buildImageSection(images: NoteImage[]): string[] {
  return images.map((image, index) =>
    image.assetPath
      ? `![图 ${index + 1}](${mediaProtocolUrl("image", image.assetFileName)})`
      : `> 第 ${index + 1} 张图下载失败：${image.error}`,
  );
}

function buildOcrSection(
  images: NoteImage[],
  texts: (string | null)[],
): string[] {
  const recognized = texts
    .map((text, index) => ({ text, index }))
    .filter((entry): entry is { text: string; index: number } =>
      Boolean(entry.text),
    );
  if (recognized.length === 0) {
    return [];
  }

  const parts = [OCR_SECTION_HEADING];
  for (const { text, index } of recognized) {
    // 只有一张图时不必再分小标题
    if (images.length > 1) {
      parts.push(`### 图 ${index + 1}`);
    }
    parts.push(text);
  }
  return parts;
}

/**
 * 组装图文条目：元数据引用块 + 文案 + 配图 + 图中文字。
 * 条目类型用 image，详情页才会出现「重新识别图中文字」入口。
 */
export async function buildImageNoteEntry(
  source: ImageNoteSource,
  deps: ImageNoteDeps = {},
  signal?: AbortSignal,
): Promise<ExtractedContent> {
  deps.onStage?.("image-download");
  const images = await collectImages(source, deps, signal);

  const ocrConfig = (deps.getOcrConfig ?? resolveOcrConfig)();
  let outcome: OcrOutcome = {
    texts: images.map(() => null),
    attempted: 0,
    failed: 0,
  };
  if (ocrConfig) {
    deps.onStage?.("image-ocr");
    outcome = await recognizeImages(images, ocrConfig, deps, signal);
  }

  const parts = [`> ${buildImageNoteMetaLine(source)}`];
  const caption = plainTextToMarkdown(source.description || source.title);
  if (caption) {
    parts.push(caption);
  }
  parts.push(...buildImageSection(images));

  if (!ocrConfig) {
    parts.push(
      "> 未配置「视觉模型」，图中文字尚未识别；配置后可在详情页点「识别图中文字」。",
    );
  } else {
    parts.push(...buildOcrNotes(outcome));
    if (images.length > OCR_IMAGE_LIMIT) {
      parts.push(
        `> 图片较多，仅识别了前 ${OCR_IMAGE_LIMIT} 张的文字，其余图片已入库可在详情页查看。`,
      );
    }
  }
  parts.push(...buildOcrSection(images, outcome.texts));

  return {
    title: await resolveTitle(source, outcome.texts, deps, signal),
    content: parts.join("\n\n"),
    itemType: "image",
    sourceUri: source.webpageUrl,
  };
}

/**
 * 标题：作者写过标题的（小红书）照用，不重拟——那是人写的，改写只会丢掉
 * 可辨识度，理由与论坛条目沿用原帖标题相同。抖音没有标题字段，原始标题
 * 只是文案首行、往往是半句话，有文本素材且配了文本模型时改用 AI 拟题，
 * 与视频链路的处理一致；拟题失败不阻断导入，退回文案首行。
 */
async function resolveTitle(
  source: ImageNoteSource,
  texts: (string | null)[],
  deps: ImageNoteDeps,
  signal?: AbortSignal,
): Promise<string> {
  if (source.authoredTitle) {
    return source.title;
  }
  const config = (deps.getTitleConfig ?? resolveMediaSummaryConfig)();
  if (!config) {
    return source.title;
  }
  const material = [source.description || source.title, ...texts.filter(Boolean)]
    .join("\n\n")
    .trim();
  if (!material) {
    return source.title;
  }
  deps.onStage?.("summarizing");
  try {
    const generate = deps.generateTitle ?? generateContentTitle;
    return (await generate(material, config, { signal })) || source.title;
  } catch (error) {
    if (signal?.aborted) {
      throw new Error("已取消", { cause: error });
    }
    console.warn("[import] 图文 AI 拟题失败，保留文案首行:", error);
    return source.title;
  }
}
