/**
 * 论坛条目正文的分段：讨论总结 / 主楼正文 / 逐楼回复。
 *
 * 三段存在同一份 Markdown 里（全文检索与语义索引都指望它），
 * 但一屏里从总结一路滚到第 107 楼没法读，详情页据此拆成标签页。
 *
 * 小节标题由采集端写入（main/services/import/forum-post.ts），
 * 两边必须用同一组常量。
 */
import { parseVideoMetaBlock } from "./video-meta";

export const FORUM_SUMMARY_HEADING = "## 讨论总结";
export const FORUM_BODY_HEADING = "## 正文";
export const FORUM_REPLIES_HEADING = "## 讨论";

/**
 * 回复小节标题：`## 讨论（107 条）` 或 NGA 的
 * `## 讨论（楼主 12 条 · 原帖共 2040 条）`
 */
const REPLIES_HEADING_LINE =
  /^##\s*讨论(?:（(?:楼主\s*)?\d+\s*条(?:\s*·\s*原帖共\s*\d+\s*条)?）)?$/;
const SUMMARY_HEADING_LINE = /^##\s*讨论总结$/;
const BODY_HEADING_LINE = /^##\s*正文$/;
/** 旧格式：`**1 楼 · wowo243**` */
const REPLY_HEAD_BOLD = /^\*\*(\d+)\s*楼\s*·\s*(.+?)\*\*$/;
/** 新格式：`### 1 楼 · wowo243`（卡片视图与上下文块用这个） */
const REPLY_HEAD_H3 = /^###\s*(\d+)\s*楼\s*·\s*(.+)$/;
/** `> 回复 @某人：摘要` 或 `> 回复 @某人（12 楼）：摘要` */
const REPLY_TO_LINE = /^>\s*回复\s*@(.+?)(?:（(\d+)\s*楼）)?：(.*)$/;
/** 二级标题（`###` 不算——总结体内的小标题与楼层头用的就是三级） */
const SECTION_HEADING_LINE = /^##\s/;
/** 采集时留下的总结状态注记，重新生成成功后它们就过时了 */
const SUMMARY_NOTE_LINE = /^>\s*(?:未配置文本模型|讨论总结生成失败)/;

export interface ForumNoteSections {
  /** 讨论总结（不含小节标题）；未生成时为空 */
  summary: string;
  /** 主楼正文与状态注记（不含元数据引用块）；空帖时为空 */
  body: string;
  /** 逐楼回复（不含小节标题）；无回复时为空 */
  replies: string;
}

type SectionKind = keyof ForumNoteSections;

/**
 * 按小节标题切分。用户可能在详情页编辑过正文，找不到的小节返回空串，
 * 首个小节标题之前的内容一律归入 body，不丢内容。
 */
export function splitForumNoteSections(content: string): ForumNoteSections {
  // 元数据引用块交给来源 chip 展示，三段里都不重复它
  const stripped = parseVideoMetaBlock(content)?.body ?? content;
  const lines = stripped.split("\n");

  const markers: { kind: SectionKind; index: number }[] = [];
  for (const [index, line] of lines.entries()) {
    const trimmed = line.trim();
    // 「讨论总结」也以「## 讨论」开头，先判它
    if (SUMMARY_HEADING_LINE.test(trimmed)) {
      markers.push({ kind: "summary", index });
    } else if (BODY_HEADING_LINE.test(trimmed)) {
      markers.push({ kind: "body", index });
    } else if (REPLIES_HEADING_LINE.test(trimmed)) {
      markers.push({ kind: "replies", index });
    }
  }

  const sections: ForumNoteSections = { summary: "", body: "", replies: "" };
  const preamble = lines.slice(0, markers[0]?.index ?? lines.length);

  for (const [order, marker] of markers.entries()) {
    const end = markers[order + 1]?.index ?? lines.length;
    const text = lines.slice(marker.index + 1, end).join("\n").trim();
    // 同名小节重复出现时按先后拼接，不覆盖
    sections[marker.kind] = sections[marker.kind]
      ? `${sections[marker.kind]}\n\n${text}`
      : text;
  }

  const preambleText = preamble.join("\n").trim();
  if (preambleText) {
    sections.body = sections.body
      ? `${preambleText}\n\n${sections.body}`
      : preambleText;
  }
  return sections;
}

export interface ForumReplyEntry {
  floor: number;
  author: string;
  content: string;
  replyTo?: {
    author: string;
    floor?: number;
    snippet: string;
  };
}

/**
 * 被回复楼摘要给人扫的，不是 Markdown。
 * NGA lite=js 正文常夹 `<br/>`，若不收成空白，卡片里会露出字面量标签。
 */
export function normalizeForumSnippet(raw: string, maxLen = 200): string {
  return raw
    .replace(/<br\s*\/?>/gi, " ")
    .replace(/<\/?p>/gi, " ")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, maxLen);
}

/**
 * 把一条回复写成讨论段里的一块（采集与单测共用）。
 * 楼层用 ###，上下文用引用行，正文另起，方便卡片解析。
 */
export function formatForumReplyBlock(reply: {
  floor: number;
  author: string;
  content: string;
  replyTo?: { author: string; floor?: number; snippet: string };
}): string {
  const parts = [`### ${reply.floor} 楼 · ${reply.author || "匿名"}`];
  if (reply.replyTo) {
    const who = reply.replyTo.author.trim() || "某人";
    const snippet = normalizeForumSnippet(reply.replyTo.snippet);
    const floorPart =
      reply.replyTo.floor != null && Number.isFinite(reply.replyTo.floor)
        ? `（${reply.replyTo.floor} 楼）`
        : "";
    parts.push(
      snippet
        ? `> 回复 @${who}${floorPart}：${snippet}`
        : `> 回复 @${who}${floorPart}：`,
    );
  }
  if (reply.content.trim()) {
    parts.push(reply.content.trim());
  }
  return parts.join("\n\n");
}

/**
 * 从已入库的正文里还原逐楼回复。
 *
 * 重新生成讨论总结时不必再去抓一次网页——回复在采集时已经完整写进正文，
 * 而原帖可能已经被删或者又多了几十楼，用库里这份反而与条目本身一致。
 * 同时认旧的 `**N 楼 · 作者**` 与新的 `### N 楼 · 作者`。
 */
export function parseForumReplies(content: string): ForumReplyEntry[] {
  const section = splitForumNoteSections(content).replies;
  if (section) {
    return parseForumReplySection(section);
  }
  // 详情页「讨论」标签传入的已是 replies 段正文（无 ## 讨论 标题）
  if (
    REPLY_HEAD_H3.test(content.trim().split("\n")[0] ?? "") ||
    REPLY_HEAD_BOLD.test(content.trim().split("\n")[0] ?? "") ||
    content.includes("\n### ") ||
    /^\*\*\d+\s*楼/m.test(content)
  ) {
    return parseForumReplySection(content);
  }
  return [];
}

/** 解析「## 讨论」小节内部（不含小节标题本身） */
export function parseForumReplySection(section: string): ForumReplyEntry[] {
  if (!section.trim()) {
    return [];
  }

  const entries: ForumReplyEntry[] = [];
  let floor = 0;
  let author = "";
  let body: string[] = [];

  const flush = () => {
    if (floor <= 0) {
      return;
    }
    const { replyTo, content: text } = splitReplyToPrefix(body.join("\n"));
    if (!text && !replyTo) {
      return;
    }
    entries.push({
      floor,
      author,
      content: text,
      replyTo,
    });
  };

  for (const line of section.split("\n")) {
    const trimmed = line.trim();
    const headH3 = REPLY_HEAD_H3.exec(trimmed);
    const headBold = REPLY_HEAD_BOLD.exec(trimmed);
    const head = headH3 ?? headBold;
    if (head) {
      flush();
      floor = Number(head[1]);
      author = head[2].trim();
      body = [];
      continue;
    }
    body.push(line);
  }
  flush();
  return entries;
}

/** 从楼层正文开头剥离「> 回复 @x：…」上下文行 */
function splitReplyToPrefix(raw: string): {
  replyTo?: ForumReplyEntry["replyTo"];
  content: string;
} {
  const lines = raw.split("\n");
  let index = 0;
  while (index < lines.length && lines[index].trim() === "") {
    index += 1;
  }
  if (index >= lines.length) {
    return { content: "" };
  }
  const match = REPLY_TO_LINE.exec(lines[index].trim());
  if (!match) {
    return { content: raw.trim() };
  }
  const floorRaw = match[2];
  const floorNum = floorRaw ? Number(floorRaw) : undefined;
  const replyTo: ForumReplyEntry["replyTo"] = {
    author: match[1].trim(),
    snippet: normalizeForumSnippet(match[3] ?? ""),
    ...(floorNum != null && Number.isFinite(floorNum) ? { floor: floorNum } : {}),
  };
  const rest = lines.slice(index + 1).join("\n").trim();
  return { replyTo, content: rest };
}

/**
 * 解析「回复 @x」要点哪一楼：优先写明的楼层；否则在已入库楼层里按作者唯一匹配。
 * 找不到或多名同作者时返回 null（调用方提示未入库/无法定位）。
 */
export function resolveReplyTargetFloor(
  replies: ForumReplyEntry[],
  replyTo: { author: string; floor?: number },
): number | null {
  if (replyTo.floor != null && Number.isFinite(replyTo.floor)) {
    return replies.some((r) => r.floor === replyTo.floor) ? replyTo.floor : null;
  }
  const author = replyTo.author.trim().toLowerCase();
  if (!author) {
    return null;
  }
  const hits = replies.filter(
    (r) => (r.author || "").trim().toLowerCase() === author,
  );
  return hits.length === 1 ? hits[0].floor : null;
}

/**
 * 讨论区楼层过滤：扫楼层号、作者、正文、被回复摘要。
 * 空关键字原样返回；大小写不敏感（中文不受影响）。
 */
export function filterForumReplies(
  replies: ForumReplyEntry[],
  query: string,
): ForumReplyEntry[] {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return replies;
  }
  return replies.filter((reply) => forumReplySearchText(reply).includes(needle));
}

function forumReplySearchText(reply: ForumReplyEntry): string {
  return [
    String(reply.floor),
    `${reply.floor} 楼`,
    reply.author,
    reply.content,
    reply.replyTo?.author ?? "",
    reply.replyTo?.snippet ?? "",
  ]
    .join("\n")
    .toLowerCase();
}

/**
 * 把讨论总结写回正文：已有小节原位替换，否则插到第一个二级标题
 * （`## 正文` / `## 讨论`）之前，也就是元数据引用块之后。
 *
 * 采集期留下的「未配置文本模型」「生成失败」注记一并清掉，
 * 否则会和刚生成出来的总结自相矛盾。
 */
export function upsertForumSummarySection(
  content: string,
  summary: string,
): string {
  const lines = content
    .split("\n")
    .filter((line) => !SUMMARY_NOTE_LINE.test(line.trim()));
  const sectionLines = [
    FORUM_SUMMARY_HEADING,
    "",
    ...summary.trim().split("\n"),
  ];

  const startIdx = lines.findIndex((line) =>
    SUMMARY_HEADING_LINE.test(line.trim()),
  );
  if (startIdx >= 0) {
    let endIdx = lines.length;
    for (let i = startIdx + 1; i < lines.length; i++) {
      if (SECTION_HEADING_LINE.test(lines[i].trim())) {
        endIdx = i;
        break;
      }
    }
    return joinBlocks([
      ...lines.slice(0, startIdx),
      ...sectionLines,
      "",
      ...lines.slice(endIdx),
    ]);
  }

  const firstHeadingIdx = lines.findIndex((line) =>
    SECTION_HEADING_LINE.test(line.trim()),
  );
  const insertAt = firstHeadingIdx >= 0 ? firstHeadingIdx : lines.length;
  return joinBlocks([
    ...lines.slice(0, insertAt),
    "",
    ...sectionLines,
    "",
    ...lines.slice(insertAt),
  ]);
}

/** 删行与插入都会留下连续空行，统一折叠成段落间距 */
function joinBlocks(lines: string[]): string {
  return lines.join("\n").replace(/\n{3,}/g, "\n\n").trim();
}
