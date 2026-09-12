import type {
  ReadingOutline,
  ReadingDraftSection,
} from "@guizhi/shared/types/reading-reconstruction";

/** 修改请求只替换指定章节；未受影响的正文逐字保留。 */
export function applyReadingDraftUpdates(
  previous: ReadingDraftSection[],
  updates: unknown,
): ReadingDraftSection[] {
  if (!Array.isArray(updates) || updates.length > 100)
    throw new Error("修改稿缺少 updates 列表");
  const draft = structuredClone(previous),
    seen = new Set<number>();
  for (const update of updates) {
    if (
      !update ||
      !Number.isSafeInteger(update.index) ||
      update.index < 0 ||
      update.index > draft.length ||
      seen.has(update.index)
    )
      throw new Error("修改稿章节索引无效或重复");
    seen.add(update.index);
    draft[update.index] = {
      title: update.title,
      markdown: update.markdown,
      referenceIds: update.referenceIds,
    };
  }
  return draft;
}

/** 问题列表是研究计划元数据；离线不创建研究问题，也不裁掉正文。 */
export function normalizeV3Outline(
  value: unknown,
  online: boolean,
): ReadingOutline {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("编辑规划缺少 outline 对象");
  const raw = value as ReadingOutline;
  if (
    typeof raw.title !== "string" ||
    !raw.title.trim() ||
    typeof raw.direction !== "string" ||
    !Array.isArray(raw.sections) ||
    !raw.sections.length
  )
    throw new Error("编辑规划缺少标题、设计方向或章节");
  if (
    online &&
    raw.questions !== undefined &&
    (!Array.isArray(raw.questions) ||
      raw.questions.some((q) => typeof q !== "string"))
  )
    throw new Error("查证问题必须为文本列表");
  return { ...raw, questions: online ? (raw.questions ?? []).slice(0, 3) : [] };
}
