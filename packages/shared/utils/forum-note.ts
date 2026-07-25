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

/** 回复小节标题带条数：`## 讨论（107 条）` */
const REPLIES_HEADING_LINE = /^##\s*讨论(?:（\d+\s*条）)?$/;
const SUMMARY_HEADING_LINE = /^##\s*讨论总结$/;
const BODY_HEADING_LINE = /^##\s*正文$/;
/** 逐楼回复的头行：`**1 楼 · wowo243**` */
const REPLY_HEAD_LINE = /^\*\*(\d+)\s*楼\s*·\s*(.+?)\*\*$/;
/** 二级标题（`###` 不算——总结体内的小标题用的就是三级） */
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
}

/**
 * 从已入库的正文里还原逐楼回复。
 *
 * 重新生成讨论总结时不必再去抓一次网页——回复在采集时已经完整写进正文，
 * 而原帖可能已经被删或者又多了几十楼，用库里这份反而与条目本身一致。
 */
export function parseForumReplies(content: string): ForumReplyEntry[] {
  const section = splitForumNoteSections(content).replies;
  if (!section) {
    return [];
  }

  const entries: ForumReplyEntry[] = [];
  let floor = 0;
  let author = "";
  let body: string[] = [];

  const flush = () => {
    const text = body.join("\n").trim();
    if (floor > 0 && text) {
      entries.push({ floor, author, content: text });
    }
  };

  for (const line of section.split("\n")) {
    const head = REPLY_HEAD_LINE.exec(line.trim());
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
