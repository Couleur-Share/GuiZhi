/** NodeSeek 页面解析：按真实楼层去重，置顶回复不影响分页顺序。 */
import { parseHTML } from "linkedom";
import TurndownService from "turndown";
import { nodeseekCanonicalUrl } from "@guizhi/shared/utils/forum-platforms";
import type { ForumReply, ForumThread } from "./forum-types";

export const NODESEEK_VERIFICATION_MESSAGE =
  "[nodeseek_verification_required] NodeSeek 需要网页验证，请点击「验证并自动采集」后完成验证";

export interface NodeseekPage {
  title: string;
  node: string;
  posts: ForumReply[];
  pageCount: number;
}

export function parseNodeseekPage(
  html: string,
  topicId: string,
  page = 1,
): NodeseekPage {
  if (Buffer.byteLength(html, "utf8") > 10 * 1024 * 1024)
    throw new Error("NodeSeek 页面超过 10 MiB");
  const { document } = parseHTML(
    html.includes("<html") ? html : `<html><body>${html}</body></html>`,
  );
  const titleLink = document.querySelector<HTMLAnchorElement>(
    "h1 .post-title-link",
  );
  if (
    !titleLink ||
    new URL(titleLink.getAttribute("href") ?? "", nodeseekCanonicalUrl(topicId))
      .pathname !== `/post-${topicId}-1`
  ) {
    if (
      /Just a moment|请稍候|验证|challenge|signIn/i.test(
        document.querySelector("title")?.textContent ?? "",
      ) ||
      document.querySelector(
        "#challenge-stage, #cf-wrapper, .cf-turnstile, script[src*='/orchestrate/chl_page/'], script[src*='challenges.cloudflare.com/turnstile/']",
      )
    ) {
      throw new Error(NODESEEK_VERIFICATION_MESSAGE);
    }
    throw new Error(
      "NodeSeek 帖子不可读或页面结构已变化，未保存验证页或错误页",
    );
  }
  const markdown = new TurndownService({
    headingStyle: "atx",
    codeBlockStyle: "fenced",
  });
  markdown.remove(["script", "style", "noscript", "iframe", "form"]);
  const posts: ForumReply[] = [];
  for (const item of document.querySelectorAll<HTMLElement>(
    ".content-item[id]",
  )) {
    if (!/^\d+$/.test(item.id)) continue;
    const article = item.querySelector<HTMLElement>("article.post-content");
    if (!article)
      throw new Error("NodeSeek 楼层正文缺失，未将不完整页面保存为成功");
    for (const element of article.querySelectorAll("a[href], img[src]")) {
      const attribute = element.tagName === "A" ? "href" : "src";
      try {
        const resolved = new URL(
          element.getAttribute(attribute)!,
          nodeseekCanonicalUrl(topicId, page),
        );
        if (["https:", "http:"].includes(resolved.protocol))
          element.setAttribute(attribute, resolved.href);
        else element.removeAttribute(attribute);
      } catch {
        element.removeAttribute(attribute);
      }
    }
    const content = markdown.turndown(article.innerHTML).trim();
    if (!content) throw new Error("NodeSeek 楼层正文为空，可能需要登录后查看");
    const createdAt = Date.parse(
      item.querySelector(".date-created time")?.getAttribute("datetime") ?? "",
    );
    if (!Number.isFinite(createdAt))
      throw new Error("NodeSeek 楼层时间缺失，页面结构可能已变化");
    posts.push({
      floor: Number(item.id),
      author: item.querySelector(".author-name")?.textContent?.trim() ?? "",
      content,
      createdAt,
    });
  }
  if (!posts.length || (page === 1 && !posts.some((post) => post.floor === 0)))
    throw new Error("NodeSeek 主楼或回复未加载，未保存空白页面");
  let pageCount = page;
  for (const link of document.querySelectorAll<HTMLAnchorElement>(
    '[aria-label="pagination"] a[href]',
  )) {
    const target = new URL(
      link.getAttribute("href") ?? "",
      nodeseekCanonicalUrl(topicId),
    );
    if (target.origin !== "https://www.nodeseek.com") continue;
    const match = new RegExp(`^/post-${topicId}-([1-9]\\d*)$`).exec(
      target.pathname,
    );
    if (match) pageCount = Math.max(pageCount, Number(match[1]));
  }
  return {
    title: titleLink.textContent?.trim() ?? "",
    node:
      document.querySelector(".content-category a")?.textContent?.trim() ?? "",
    posts,
    pageCount,
  };
}

export async function fetchNodeseekThread(
  topicId: string,
  fetchPage: (url: string, signal?: AbortSignal) => Promise<string>,
  signal?: AbortSignal,
): Promise<ForumThread> {
  if (!/^[1-9]\d*$/.test(topicId)) throw new Error("无效的 NodeSeek 帖子编号");
  const floors = new Map<number, ForumReply>();
  const first = parseNodeseekPage(
    await fetchPage(nodeseekCanonicalUrl(topicId), signal),
    topicId,
  );
  first.posts.forEach((post) => floors.set(post.floor, post));
  let pageCount = first.pageCount;
  let warningReason: string | undefined;
  for (let page = 2; page <= Math.min(pageCount, 100); page++) {
    signal?.throwIfAborted();
    // 逐页串行，不并发轰击站点；任意一页失败则整次失败，保留验证重试入口。
    const next = parseNodeseekPage(
      await fetchPage(nodeseekCanonicalUrl(topicId, page), signal),
      topicId,
      page,
    );
    next.posts.forEach((post) => floors.set(post.floor, post));
    pageCount = Math.max(pageCount, next.pageCount);
  }
  if (pageCount > 100)
    warningReason = `NodeSeek 共 ${pageCount} 页，本次仅采集前 100 页，后续回复未入库`;
  const main = floors.get(0)!;
  const replies = [...floors.values()]
    .filter((post) => post.floor > 0)
    .sort((a, b) => a.floor - b.floor);
  return {
    platform: "nodeseek",
    topicId,
    title: first.title,
    author: main.author,
    node: first.node,
    createdAt: main.createdAt,
    replyCount: replies.length,
    content: main.content,
    replies,
    webpageUrl: nodeseekCanonicalUrl(topicId),
    warningReason,
  };
}
