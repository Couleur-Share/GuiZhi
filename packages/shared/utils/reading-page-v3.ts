import type {
  ReadingGeneration,
  ReadingScriptModule,
  ReadingLibrary,
} from "../types/reading-page-v3";

const object = (v: unknown): v is Record<string, any> =>
  !!v && typeof v === "object" && !Array.isArray(v);
const text = (v: unknown, max: number) =>
  typeof v === "string" && v.length <= max;
export const readingUnitId = (v: unknown): v is string =>
  typeof v === "string" &&
  /^[a-zA-Z][\w-]{0,79}$/.test(v) &&
  !["constructor", "prototype"].includes(v) &&
  !v.startsWith("gz-system");
export function validateReadingScripts(
  value: unknown,
): asserts value is ReadingScriptModule[] {
  if (!Array.isArray(value) || value.length > 30)
    throw new Error("阅读页脚本清单无效");
  const ids = new Set<string>();
  for (const s of value) {
    if (
      !object(s) ||
      !readingUnitId(s.id) ||
      ids.has(s.id) ||
      !text(s.code, 256000) ||
      !["pending", "ready", "failed"].includes(s.status) ||
      (s.rootId !== undefined && !readingUnitId(s.rootId)) ||
      (s.error !== undefined && !text(s.error, 4000))
    )
      throw new Error("阅读页脚本模块无效");
    ids.add(s.id);
  }
}
export function validateReadingLibraries(
  value: unknown,
): asserts value is ReadingLibrary[] {
  if (
    !Array.isArray(value) ||
    value.length > 3 ||
    new Set(value).size !== value.length ||
    value.some((v) => !["echarts", "mermaid", "animation"].includes(v))
  )
    throw new Error("阅读页引用了不支持的内置库");
}
export function validateReadingGeneration(
  value: unknown,
): asserts value is ReadingGeneration {
  if (
    !object(value) ||
    !["short", "long", "research", "redesign"].includes(value.route) ||
    !Array.isArray(value.sections) ||
    value.sections.length > 100 ||
    value.sections.some(
      (s) => !object(s) || !readingUnitId(s.id) || !text(s.html, 2000000),
    ) ||
    new Set(value.sections.map((s) => s.id)).size !== value.sections.length ||
    !text(value.css, 300000) ||
    !Number.isSafeInteger(value.revision) ||
    value.revision < 0 ||
    typeof value.done !== "boolean" ||
    !object(value.repairs) ||
    Object.entries(value.repairs).some(
      ([k, v]) =>
        !readingUnitId(k) ||
        !Number.isSafeInteger(v) ||
        Number(v) < 0 ||
        Number(v) > 1,
    ) ||
    !Array.isArray(value.issues) ||
    value.issues.length > 100 ||
    value.issues.some(
      (i) =>
        !object(i) ||
        ![
          "format",
          "content",
          "resource",
          "script",
          "runtime",
          "security",
        ].includes(i.kind) ||
        !text(i.message, 4000) ||
        (i.unit !== undefined && !text(i.unit, 100)) ||
        (i.line !== undefined && (!Number.isSafeInteger(i.line) || i.line < 1)),
    )
  )
    throw new Error("阅读页生成检查点无效");
  validateReadingScripts(value.scripts);
  validateReadingLibraries(value.libraries);
  if (value.candidateCss !== undefined && !text(value.candidateCss, 300000))
    throw new Error("候选样式无效");
  if (
    value.editorCandidate !== undefined &&
    !text(value.editorCandidate, 1000000)
  )
    throw new Error("候选编辑稿无效");
  if (
    value.candidates !== undefined &&
    (!object(value.candidates) ||
      Object.keys(value.candidates).length > 100 ||
      Object.entries(value.candidates).some(
        ([id, html]) => !readingUnitId(id) || !text(html, 2000000),
      ))
  )
    throw new Error("失败章节检查点无效");
  if (
    value.sectionOrder !== undefined &&
    (!Array.isArray(value.sectionOrder) ||
      value.sectionOrder.length > 100 ||
      value.sectionOrder.some((id) => !readingUnitId(id)) ||
      new Set(value.sectionOrder).size !== value.sectionOrder.length)
  )
    throw new Error("章节顺序检查点无效");
  if (value.limited !== undefined && typeof value.limited !== "boolean")
    throw new Error("限流检查点无效");
  if (
    value.researchRoundLimit !== undefined &&
    (!Number.isSafeInteger(value.researchRoundLimit) ||
      value.researchRoundLimit < 1 ||
      value.researchRoundLimit > 20)
  )
    throw new Error("查证预算无效");
  if (
    value.draftBySection !== undefined &&
    (!object(value.draftBySection) ||
      Object.keys(value.draftBySection).length > 100 ||
      Object.entries(value.draftBySection).some(
        ([id, s]) =>
          !/^\d+$/.test(id) ||
          !object(s) ||
          !text(s.title, 2000) ||
          !text(s.markdown, 200000) ||
          !Array.isArray(s.referenceIds) ||
          s.referenceIds.some((id) => !text(id, 80)),
      ))
  )
    throw new Error("编辑稿检查点无效");
  for (const notes of [value.notesByPart, value.reducedNotes])
    if (
      notes !== undefined &&
      (!object(notes) ||
        Object.keys(notes).length > 2000 ||
        Object.values(notes).some((v) => !text(v, 20000)))
    )
      throw new Error("阅读笔记检查点无效");
  if (
    value.reusedChapters !== undefined &&
    (!Number.isSafeInteger(value.reusedChapters) ||
      value.reusedChapters < 0 ||
      value.reusedChapters > 100)
  )
    throw new Error("复用章节数量无效");
  if (
    value.evidence !== undefined &&
    (!Array.isArray(value.evidence) ||
      value.evidence.length > 128 ||
      value.evidence.some(
        (v) =>
          !object(v) ||
          !text(v.id, 80) ||
          !text(v.text, 6000) ||
          (v.sections !== undefined &&
            (!Array.isArray(v.sections) ||
              v.sections.length > 30 ||
              v.sections.some(
                (i) => !Number.isSafeInteger(i) || i < 0 || i >= 30,
              ))),
      ))
  )
    throw new Error("阅读证据包无效");
}
