/**
 * 论坛帖子的条目组装：元数据引用块 + 讨论总结 + 主楼正文 + 讨论区回复。
 *
 * V2EX / Discourse / 2Libra 把回复完整入库；NGA 长帖只保留楼主回复，另用采样素材
 * 做总结，避免镜像几千楼水帖。三个小节标题同时是详情页分段锚点，改动需
 * 同步 shared/utils/forum-note.ts。
 */
import type { ImportStage } from "@guizhi/shared/types";
import type { ForumTarget } from "@guizhi/shared/utils/forum-platforms";
import {
  FORUM_BODY_HEADING,
  FORUM_REPLIES_HEADING,
  FORUM_SUMMARY_HEADING,
  formatForumReplyBlock,
} from "@guizhi/shared/utils/forum-note";
import { appendOriginalTitleNote } from "@guizhi/shared/utils/media-summary";
import type { AIClientConfig } from "@guizhi/core";
import type { ExtractedContent } from "./connectors";
import {
  generateForumSummary,
  type ForumSummaryInput,
  type ForumSummaryResult,
} from "./forum-summary";
import type { ForumReply, ForumThread } from "./forum-types";
import { fetchAppinnThread } from "./appinn";
import { fetchLinuxdoThread } from "./linuxdo";
import { fetchNgaThread } from "./nga";
import { fetchTwolibraThread } from "./twolibra";
import { fetchV2exThread } from "./v2ex";
import { resolveMediaSummaryConfig } from "../media/media-summary";

const PLATFORM_LABELS: Record<ForumTarget["platform"], string> = {
  v2ex: "V2EX",
  nga: "NGA",
  linuxdo: "LINUX DO",
  appinn: "小众软件",
  twolibra: "2Libra",
};

/** 失败原因写进正文时的截断长度 */
const SUMMARY_ERROR_MAX_LENGTH = 120;

export interface ForumPostDeps {
  /** 测试注入：帖子抓取 */
  fetchThread?: (
    target: ForumTarget,
    signal?: AbortSignal,
  ) => Promise<ForumThread>;
  /** LINUX DO Cloudflare 降级：经 Electron 会话拉 JSON */
  fetchAuthenticatedJson?: <T>(
    url: string,
    signal?: AbortSignal,
  ) => Promise<T>;
  /** 测试注入：总结模型解析（默认读 ai-config.json 的 mainText 路由） */
  getSummaryConfig?: () => AIClientConfig | null;
  /** 测试注入：讨论总结 */
  summarize?: (
    input: ForumSummaryInput,
    config: AIClientConfig,
    options?: { signal?: AbortSignal },
  ) => Promise<ForumSummaryResult | null>;
  onStage?: (stage: ImportStage) => void;
}

async function fetchThreadByPlatform(
  target: ForumTarget,
  deps: ForumPostDeps,
  signal?: AbortSignal,
): Promise<ForumThread> {
  switch (target.platform) {
    case "v2ex":
      return fetchV2exThread(target.topicId, {}, signal);
    case "nga":
      return fetchNgaThread(target.topicId, {}, signal);
    case "linuxdo":
      return fetchLinuxdoThread(
        target.topicId,
        { fetchAuthenticatedJson: deps.fetchAuthenticatedJson },
        signal,
      );
    case "appinn":
      return fetchAppinnThread(target.topicId, {}, signal);
    case "twolibra":
      return fetchTwolibraThread(target.topicId, {}, signal);
    default:
      throw new Error(
        `暂不支持的论坛: ${target.platform satisfies never}`,
      );
  }
}

/** 本地时区的 YYYY-MM-DD */
function formatDate(timestamp: number): string {
  const date = new Date(timestamp);
  const month = `${date.getMonth() + 1}`.padStart(2, "0");
  const day = `${date.getDate()}`.padStart(2, "0");
  return `${date.getFullYear()}-${month}-${day}`;
}

function retentionNote(thread: ForumThread): string {
  if (thread.replyRetention === "op-only") {
    return "条目讨论区仅保留楼主回复，完整楼层见原帖链接。";
  }
  return "原始讨论已完整入库。";
}

/** 交给模型的讨论素材：NGA 用采样页，其余用入库回复 */
function summaryMaterial(thread: ForumThread): ForumReply[] {
  return thread.summaryReplies ?? thread.replies;
}

/**
 * 元数据引用块。首字段沿用「平台：」，让详情页的来源 chip
 * （parseVideoMetaBlock）不必为论坛条目另开一套解析。
 *
 * 两行之间只能是换行不能空行——元数据块的解析靠「连续的 > 行」界定，
 * 中间断开会让后面几行漏进正文。
 */
export function buildForumMetaBlock(thread: ForumThread): string {
  const facts = [`平台：${PLATFORM_LABELS[thread.platform]}`];
  if (thread.author) {
    facts.push(`作者：${thread.author}`);
  }
  if (thread.node) {
    facts.push(`节点：${thread.node}`);
  }
  if (thread.replyRetention === "op-only") {
    facts.push(
      `${thread.replyCount} 条回复（入库保留楼主 ${thread.replies.length} 条）`,
    );
  } else {
    facts.push(`${thread.replyCount} 条回复`);
  }

  return [
    `> ${facts.join(" · ")}`,
    `> 发布：${formatDate(thread.createdAt)}`,
  ].join("\n");
}

/** 逐楼回复。楼层用 ###，可选上下文引用行，正文另起 */
function buildRepliesSection(thread: ForumThread): string[] {
  const replies = thread.replies;
  if (replies.length === 0) {
    return [];
  }
  const heading =
    thread.replyRetention === "op-only"
      ? `${FORUM_REPLIES_HEADING}（楼主 ${replies.length} 条 · 原帖共 ${thread.replyCount} 条）`
      : `${FORUM_REPLIES_HEADING}（${replies.length} 条）`;
  const parts = [heading];
  for (const reply of replies) {
    parts.push(formatForumReplyBlock(reply));
  }
  return parts;
}

interface SummarySection {
  /** 写进正文的段落；总结没生成时是状态注记，或者空 */
  parts: string[];
  /** 模型重拟的标题（仅原标题说不清内容时才有），否则 null */
  title: string | null;
}

/**
 * 生成讨论总结；未配置模型或生成失败都不阻断采集，
 * 改为在正文里如实交代。
 */
async function buildSummarySection(
  thread: ForumThread,
  deps: ForumPostDeps,
  signal?: AbortSignal,
): Promise<SummarySection> {
  const material = summaryMaterial(thread);
  if (material.length === 0) {
    return { parts: [], title: null };
  }

  const note = retentionNote(thread);
  const config = (deps.getSummaryConfig ?? resolveMediaSummaryConfig)();
  if (!config) {
    return {
      parts: [`> 未配置文本模型，讨论总结未生成；${note}`],
      title: null,
    };
  }

  deps.onStage?.("summarizing");
  try {
    const summarize = deps.summarize ?? generateForumSummary;
    const result = await summarize(
      {
        title: thread.title,
        content: thread.content,
        replies: material,
      },
      config,
      { signal },
    );
    return {
      parts: result ? [FORUM_SUMMARY_HEADING, result.summary] : [],
      title: result?.title ?? null,
    };
  } catch (error) {
    if (signal?.aborted) {
      throw new Error("已取消", { cause: error });
    }
    console.warn("[import] 论坛讨论总结失败:", error);
    const reason = (
      error instanceof Error ? error.message : String(error)
    ).slice(0, SUMMARY_ERROR_MAX_LENGTH);
    return {
      parts: [`> 讨论总结生成失败：${reason}。${note}`],
      title: null,
    };
  }
}

/**
 * 抓取论坛帖子并组装为知识条目。
 * 抓取失败返回带 degradedReason 的空壳，由队列标记任务失败（不入库）。
 */
export async function extractForumPost(
  target: ForumTarget,
  deps: ForumPostDeps = {},
  signal?: AbortSignal,
): Promise<ExtractedContent> {
  // 抓取主楼与回复要串多个请求，先把阶段报出去，
  // 否则这段时间界面只显示笼统的「抓取中」
  deps.onStage?.("forum-replies");

  let thread: ForumThread;
  try {
    thread = deps.fetchThread
      ? await deps.fetchThread(target, signal)
      : await fetchThreadByPlatform(target, deps, signal);
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    if (message === "已取消") {
      throw error;
    }
    return {
      title: "",
      content: "",
      itemType: "forum",
      sourceUri: null,
      degradedReason: `论坛帖子抓取失败：${message}`,
    };
  }

  const summarySection = await buildSummarySection(thread, deps, signal);
  const parts = [buildForumMetaBlock(thread), ...summarySection.parts];

  if (thread.content) {
    parts.push(FORUM_BODY_HEADING, thread.content);
  }
  parts.push(...buildRepliesSection(thread));

  let title = thread.title;
  let content = parts.join("\n\n");
  // 原标题说不清内容时才换成 AI 拟的，原标题记进元数据引用块（来源 chip 可见，也仍能检索到）
  if (summarySection.title && summarySection.title !== title) {
    content = appendOriginalTitleNote(content, title);
    title = summarySection.title;
  }

  return {
    title,
    content,
    itemType: "forum",
    sourceUri: thread.webpageUrl,
    warningReason: thread.warningReason,
  };
}
