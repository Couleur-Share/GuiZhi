/**
 * 抖音图文作品的条目组装：配图落盘资产库 + 逐图 OCR。
 *
 * 图文的信息主体常常在图里（信息图、指标对比表、架构流程图），只保留文案
 * 等于把内容主体丢了。而抖音图片地址带 `x-expires` 时效签名（约一个月），
 * 不落盘的话过期就再也取不回来——所以采集时必须把图拉进资产库。
 *
 * OCR 让图里的文字进入全文检索与语义索引，问答才用得上它。逐图识别按张
 * 计费，因此设了张数上限，超出的图片照常入库但不识别并在正文注明。
 */
import fs from "fs/promises";
import path from "path";
import type { ImportStage } from "@guizhi/shared/types";
import { OCR_SECTION_HEADING } from "@guizhi/shared/utils/ocr-request";
import type { ExtractedContent } from "./connectors";
import {
  buildDouyinNoteMetaLine,
  downloadDouyinImage,
  plainTextToMarkdown,
  type DouyinAweme,
} from "./douyin";
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

export interface DouyinNoteDeps {
  /** 测试注入：下载单张配图 */
  downloadImage?: (
    mirrors: string[],
    signal?: AbortSignal,
  ) => Promise<{ dir: string; filePath: string }>;
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

interface NoteImage {
  assetFileName: string;
  /** 资产文件的绝对路径，OCR 直接读它 */
  assetPath: string;
  /** 下载失败时的可读原因；有值时前两个字段为空 */
  error?: string;
}

async function collectImages(
  aweme: DouyinAweme,
  deps: DouyinNoteDeps,
  signal?: AbortSignal,
): Promise<NoteImage[]> {
  const download = deps.downloadImage ?? downloadDouyinImage;
  const save =
    deps.saveAsset ?? ((filePath: string) => saveMediaAsset(filePath, "image"));

  const images: NoteImage[] = [];
  for (const mirrors of aweme.imageMirrors) {
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
  deps: DouyinNoteDeps,
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
        throw new Error("已取消");
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
export async function buildDouyinNoteEntry(
  aweme: DouyinAweme,
  deps: DouyinNoteDeps = {},
  signal?: AbortSignal,
): Promise<ExtractedContent> {
  deps.onStage?.("image-download");
  const images = await collectImages(aweme, deps, signal);

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

  const parts = [`> ${buildDouyinNoteMetaLine(aweme)}`];
  const caption = plainTextToMarkdown(aweme.description || aweme.title);
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
    title: await resolveTitle(aweme, outcome.texts, deps, signal),
    content: parts.join("\n\n"),
    itemType: "image",
    sourceUri: aweme.webpageUrl,
  };
}

/**
 * 标题：抖音图文没有标题字段，原始标题只是文案首行，往往是半句话。
 * 有文本素材且配了文本模型时改用 AI 拟题，与视频链路的处理一致；
 * 拟题失败不阻断导入，退回文案首行。
 */
async function resolveTitle(
  aweme: DouyinAweme,
  texts: (string | null)[],
  deps: DouyinNoteDeps,
  signal?: AbortSignal,
): Promise<string> {
  const config = (deps.getTitleConfig ?? resolveMediaSummaryConfig)();
  if (!config) {
    return aweme.title;
  }
  const source = [aweme.description || aweme.title, ...texts.filter(Boolean)]
    .join("\n\n")
    .trim();
  if (!source) {
    return aweme.title;
  }
  deps.onStage?.("summarizing");
  try {
    const generate = deps.generateTitle ?? generateContentTitle;
    return (await generate(source, config, { signal })) || aweme.title;
  } catch (error) {
    if (signal?.aborted) {
      throw new Error("已取消");
    }
    console.warn("[import] 图文 AI 拟题失败，保留文案首行:", error);
    return aweme.title;
  }
}
