/**
 * 内容源连接器：把原始输入抽取为待入库的知识条目草稿。
 */
import fs from "fs/promises";
import path from "path";
import { Readability } from "@mozilla/readability";
import { parseHTML } from "linkedom";
import TurndownService from "turndown";
import type {
  ImportSourceKind,
  ImportStage,
  KnowledgeItemType,
} from "@guizhi/shared/types";
import { fetchHtml } from "./safe-fetch";
import {
  classifyMediaFile,
  MEDIA_SIZE_LIMITS,
  mediaProtocolUrl,
  saveMediaAsset,
  type MediaFileKind,
} from "./media-files";

const TEXT_FILE_MAX_BYTES = 5 * 1024 * 1024;
const TITLE_MAX_LENGTH = 120;

export interface ExtractedContent {
  title: string;
  /** Markdown 正文 */
  content: string;
  itemType: KnowledgeItemType;
  /** 参与去重的源 URI（url 导入为规范化前的最终 URL；文件为绝对路径） */
  sourceUri: string | null;
  /** 口播转写稿（在线视频导入时生成） */
  transcript?: string | null;
  /**
   * 抽取降级的用户可读原因（抓取失败 / 抽不出正文 / 依赖缺失）。
   *
   * 有值时队列不会入库：写进知识库只会留下一条空壳，而它的 sourceUri
   * 还会占住 source_records.normalized_uri，让同一链接此后永远判重为重复。
   * 任务改标 failed，用户可以在导入列表里看到原因并重试。
   */
  degradedReason?: string;
  /**
   * 入库了、但内容有缺失的用户可读原因（转写失败 / 未配置转写模型…）。
   *
   * 与 degradedReason 的分界是「条目值不值得留」：抓不到正文的空壳留着有害，
   * 而拿到了元数据只是缺文字稿的视频仍然有用，所以照常入库、任务照常 completed。
   * 但那一行注记此前只在正文里，列表上是一枚绿色的「已完成」——降级得说出来。
   */
  warningReason?: string;
}

/** 连接器运行环境（由 import-service 注入，避免连接器直接依赖 DB） */
export interface ImportConnectorContext {
  /** 设置里配置的 yt-dlp 路径（空表示查 PATH） */
  getYtDlpPath?: () => string | null;
  /** 设置里配置的 ffmpeg 路径（空表示托管版 / PATH） */
  getFfmpegPath?: () => string | null;
  /** 导入时是否区分说话人（默认关；仅内置本地引擎支持） */
  getDiarize?: () => boolean;
  /** 上报当前子阶段：视频链路可跑几十分钟，只报「抓取中」等于没有进度 */
  onStage?: (stage: ImportStage) => void;
}

function firstLineTitle(text: string): string {
  const firstLine =
    text
      .trim()
      .split(/\r?\n/, 1)[0]
      ?.replace(/^#{1,6}\s+/, "")
      .trim() ?? "";
  return firstLine.length > TITLE_MAX_LENGTH
    ? `${firstLine.slice(0, TITLE_MAX_LENGTH)}…`
    : firstLine;
}

function createTurndown(): TurndownService {
  const turndown = new TurndownService({
    headingStyle: "atx",
    codeBlockStyle: "fenced",
    bulletListMarker: "-",
  });
  turndown.remove(["script", "style", "noscript", "iframe"]);
  return turndown;
}

async function extractText(input: string): Promise<ExtractedContent> {
  const content = input.trim();
  return {
    title: firstLineTitle(content),
    content,
    itemType: "note",
    sourceUri: null,
  };
}

/** 媒体文件导入：资产化拷贝 + 生成引用条目（预览/播放走自定义协议） */
async function extractMediaFile(
  resolved: string,
  kind: MediaFileKind,
  sizeBytes: number,
): Promise<ExtractedContent> {
  const limit = MEDIA_SIZE_LIMITS[kind];
  if (sizeBytes > limit) {
    throw new Error(
      `文件超过 ${Math.round(limit / (1024 * 1024))}MB 上限`,
    );
  }

  const assetFileName = await saveMediaAsset(resolved, kind);
  const assetUrl = mediaProtocolUrl(kind, assetFileName);
  const baseName = path.basename(resolved);

  if (kind === "image") {
    return {
      title: baseName,
      content: `![${baseName}](${assetUrl})\n\n> 图片已导入本地资产库，可在详情页预览；配置视觉模型后可识别图中文字。`,
      itemType: "image",
      sourceUri: resolved,
    };
  }

  const kindLabel = kind === "audio" ? "音频" : "视频";
  return {
    title: baseName,
    content: `[${baseName}](${assetUrl})\n\n> ${kindLabel}文件已导入本地资产库，可在详情页播放；配置「语音转写」模型后可生成文字稿。`,
    itemType: kind,
    sourceUri: resolved,
  };
}

async function extractFile(filePath: string): Promise<ExtractedContent> {
  const resolved = path.resolve(filePath);
  const extension = path.extname(resolved).toLowerCase();

  const stat = await fs.stat(resolved);
  if (!stat.isFile()) {
    throw new Error("不是有效的文件");
  }

  const mediaKind = classifyMediaFile(extension);
  if (mediaKind) {
    return extractMediaFile(resolved, mediaKind, stat.size);
  }

  if (![".txt", ".md", ".markdown"].includes(extension)) {
    throw new Error(
      `暂不支持的文件类型: ${extension || "(无扩展名)"}（支持文本 / 图片 / 音频 / 视频）`,
    );
  }
  if (stat.size > TEXT_FILE_MAX_BYTES) {
    throw new Error("文件超过 5MB 上限");
  }

  const content = (await fs.readFile(resolved, "utf8")).trim();
  const baseName = path.basename(resolved, extension);
  return {
    title: firstLineTitle(content) || baseName,
    content,
    itemType: extension === ".txt" ? "document" : "note",
    sourceUri: resolved,
  };
}

async function extractWebpage(
  url: string,
  signal?: AbortSignal,
): Promise<ExtractedContent> {
  let fetched;
  try {
    fetched = await fetchHtml(url, signal);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (message === "已取消") {
      throw error;
    }
    return {
      title: url,
      content: "",
      itemType: "webpage",
      sourceUri: null,
      degradedReason: `网页抓取失败：${message}`,
    };
  }

  const { document } = parseHTML(fetched.html);
  const pageTitle = document.querySelector("title")?.textContent?.trim() ?? "";

  let article: { title?: string | null; content?: string | null } | null;
  try {
    article = new Readability(document as unknown as Document, {
      charThreshold: 100,
    }).parse();
  } catch {
    article = null;
  }

  if (!article?.content) {
    return {
      title: pageTitle || fetched.finalUrl,
      content: "",
      itemType: "webpage",
      sourceUri: null,
      degradedReason:
        "未能抽取网页正文（页面可能由脚本动态渲染，或需要登录后才能访问）",
    };
  }

  const markdown = createTurndown().turndown(article.content).trim();
  const title = (article.title || pageTitle || fetched.finalUrl).trim();
  return {
    title:
      title.length > TITLE_MAX_LENGTH
        ? `${title.slice(0, TITLE_MAX_LENGTH)}…`
        : title,
    content: `${markdown}\n\n---\n\n来源：<${fetched.finalUrl}>`,
    itemType: "webpage",
    sourceUri: fetched.finalUrl,
  };
}

export async function extractContent(
  kind: ImportSourceKind,
  input: string,
  signal?: AbortSignal,
  context?: ImportConnectorContext,
): Promise<ExtractedContent> {
  switch (kind) {
    case "text":
      return extractText(input);
    case "file":
      return extractFile(input);
    case "url": {
      const url = input.trim();
      const { detectVideoPlatform, extractVideoUrl } = await import(
        "./video-url"
      );
      const platform = detectVideoPlatform(url);
      if (platform) {
        return extractVideoUrl(
          url,
          platform,
          {
            getYtDlpPath: context?.getYtDlpPath ?? (() => null),
            getFfmpegPath: context?.getFfmpegPath,
            getDiarize: context?.getDiarize,
            onStage: context?.onStage,
          },
          signal,
        );
      }

      // 论坛帖子走平台接口取结构化的主楼与回复：通用抓取会把头像、
      // 楼层号一并卷进正文，还抓不全回复（见 import/v2ex.ts）
      const { detectForumPlatform } = await import(
        "@guizhi/shared/utils/forum-platforms"
      );
      const forumTarget = detectForumPlatform(url);
      if (forumTarget) {
        const { extractForumPost } = await import("./forum-post");
        return extractForumPost(
          forumTarget,
          { onStage: context?.onStage },
          signal,
        );
      }

      return extractWebpage(url, signal);
    }
    default:
      throw new Error(`未知的导入类型: ${kind satisfies never}`);
  }
}
