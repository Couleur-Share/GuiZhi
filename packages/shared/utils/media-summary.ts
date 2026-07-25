/**
 * 「视频/音频总结」正文小节的定位与写入。
 *
 * AI 生成的结构化总结以固定二级标题小节的形式存放在条目正文（content）中：
 * 进入 FTS 检索、Wiki 编译与问答的素材范围，用户也可以直接编辑。
 * 主进程负责生成与写入，渲染进程用同一套工具判断小节是否存在。
 */

export const MEDIA_SUMMARY_HEADINGS = ["## 视频总结", "## 音频总结"] as const;

/** 按条目类型选择总结小节标题（audio → 音频总结，其余 → 视频总结） */
export function mediaSummaryHeading(itemType: string): string {
  return itemType === "audio" ? "## 音频总结" : "## 视频总结";
}

function isSummaryHeadingLine(line: string): boolean {
  const trimmed = line.trim();
  return MEDIA_SUMMARY_HEADINGS.some((heading) => trimmed === heading);
}

export function hasMediaSummarySection(content: string): boolean {
  return content.split("\n").some(isSummaryHeadingLine);
}

/** 首段是否为「元数据头」：`> ` 引用块（视频元信息）或本地资产引用行 */
function isMetadataLeadParagraph(paragraph: string): boolean {
  return paragraph.startsWith("> ") || /local-[a-z]+:\/\//.test(paragraph);
}

/**
 * 把总结写入正文：已有总结小节则原位替换，否则在元数据头之后插入，
 * 没有元数据头就置顶。
 *
 * 小节的结束边界是下一个二级标题或 `---` 分隔线；正文段落（如视频简介）
 * 没有结构标记，所以插入时在小节后补一条 `---` 作为确定性的结束锚点，
 * 保证重新生成只替换总结本身、不吞掉后续内容。
 */
export function upsertMediaSummarySection(
  content: string,
  heading: string,
  summary: string,
): string {
  const sectionLines = [heading, "", ...summary.trim().split("\n")];
  const lines = content.split("\n");

  const startIdx = lines.findIndex(isSummaryHeadingLine);
  if (startIdx >= 0) {
    let endIdx = lines.length;
    for (let i = startIdx + 1; i < lines.length; i++) {
      const trimmed = lines[i].trim();
      if (trimmed.startsWith("## ") || trimmed === "---") {
        endIdx = i;
        break;
      }
    }
    const tail = lines.slice(endIdx);
    return [
      ...lines.slice(0, startIdx),
      ...sectionLines,
      ...(tail.length > 0 ? ["", ...tail] : []),
    ].join("\n");
  }

  const trimmedContent = content.trim();
  if (!trimmedContent) {
    return sectionLines.join("\n");
  }

  const firstBlankIdx = lines.findIndex((line) => line.trim() === "");
  const leadEnd = firstBlankIdx >= 0 ? firstBlankIdx : lines.length;
  const leadParagraph = lines.slice(0, leadEnd).join("\n").trim();
  if (leadEnd > 0 && isMetadataLeadParagraph(leadParagraph)) {
    const tail = lines.slice(leadEnd);
    const hasTailContent = tail.some((line) => line.trim() !== "");
    // 小节后还有内容才需要 --- 结束锚点；小节收尾时无需多余分隔线
    const section = hasTailContent ? [...sectionLines, "", "---"] : sectionLines;
    return [...lines.slice(0, leadEnd), "", ...section, ...tail].join("\n");
  }

  return `${[...sectionLines, "", "---"].join("\n")}\n\n${content}`;
}

/**
 * AI 标题替换原标题时，把原标题记进开头的元数据引用块（`> 原标题：xxx`），
 * 保证仍能按平台原标题检索到条目。已有记录或开头不是引用块时原样返回。
 */
export function appendOriginalTitleNote(
  content: string,
  originalTitle: string,
): string {
  const title = originalTitle.trim();
  if (!title) {
    return content;
  }
  const lines = content.split("\n");
  if (!lines[0]?.startsWith("> ")) {
    return content;
  }
  let end = 0;
  while (end < lines.length && lines[end].startsWith(">")) {
    if (/^>\s*原标题[:：]/.test(lines[end])) {
      return content;
    }
    end++;
  }
  lines.splice(end, 0, `> 原标题：${title}`);
  return lines.join("\n");
}
