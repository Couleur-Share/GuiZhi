import { validateReadingVisuals } from "./reading-visuals";
import type { ReadingReconstruction, ReadingInteraction, ReadingResearchBatch } from "../types/reading-reconstruction";
import { evaluateReadingExpression } from "./reading-expression";

const obj = (v: unknown): v is Record<string, any> => !!v && typeof v === "object" && !Array.isArray(v);
const text = (v: unknown, max: number) => typeof v === "string" && v.length <= max;
const list = (v: unknown, max: number): v is any[] => Array.isArray(v) && v.length <= max;
const id = (v: unknown) => typeof v === "string" && /^[a-zA-Z][\w-]{0,79}$/.test(v);
function fail(): never { throw new Error("AI 重构数据格式无效"); }
export function isReadingPublicUrl(value: unknown): value is string {
  if (!text(value, 4000)) return false;
  try { const u = new URL(value as string); return ["http:", "https:"].includes(u.protocol) && !u.username && !u.password && !!u.hostname; } catch { return false; }
}
export function validateReadingInteractions(value: unknown): asserts value is ReadingInteraction[] {
  if (!list(value, 30)) fail();
  const ids = new Set<string>();
  for (const entry of value) {
    if (!obj(entry) || !id(entry.id) || ids.has(entry.id) || !["tabs", "filter", "scenario", "calculator"].includes(entry.kind)) fail();
    ids.add(entry.id);
    if (entry.kind === "calculator") {
      if (!list(entry.inputs, 8) || !entry.inputs.length || !text(entry.expression, 500) || (entry.unit !== undefined && !text(entry.unit, 40))) fail();
      const names = new Set<string>();
      for (const field of entry.inputs) {
        if (!obj(field) || !/^[a-zA-Z][a-zA-Z0-9_]{0,29}$/.test(field.name) || ["min", "max", "constructor", "prototype", "__proto__"].includes(field.name) || names.has(field.name) || !text(field.label, 100) || (field.unit !== undefined && !text(field.unit, 40))) fail();
        if ([field.min, field.max].some(n => n !== undefined && (!Number.isFinite(n) || Math.abs(n) > 1e9)) || (field.min !== undefined && field.max !== undefined && field.min >= field.max)) fail();
        names.add(field.name);
      }
      // 常量或变量导致除零可在运行时处理，其余语法必须有效。
      try { evaluateReadingExpression(entry.expression, Object.fromEntries([...names].map(n => [n, 2.718]))); }
      catch (error) { if (!(error instanceof Error) || !/除数不能|结果超出/.test(error.message)) fail(); }
    } else if (!list(entry.choices, 12) || !entry.choices.length || entry.choices.some(c => !obj(c) || !text(c.label, 100) || !id(c.value)) || new Set(entry.choices.map(c => c.value)).size !== entry.choices.length) fail();
  }
}
function validateResearchBatch(value: unknown): void {
  if (!obj(value) || !list(value.queries, 3)) fail();
  for (const q of value.queries) if (!obj(q) || !text(q.query, 400) || typeof q.done !== "boolean" || !list(q.results, 5) || q.results.some(r => !obj(r) || !text(r.title, 500) || !isReadingPublicUrl(r.url) || (r.text !== undefined && !text(r.text, 60000)) || (r.publishedAt !== undefined && !text(r.publishedAt, 100)))) fail();
  if (value.selectedUrls !== undefined && (!list(value.selectedUrls, 8) || new Set(value.selectedUrls).size !== value.selectedUrls.length || value.selectedUrls.some(url => !value.queries.some((q: ReadingResearchBatch["queries"][number]) => q.results.some(r => r.url === url))))) fail();
  if (value.readUrls !== undefined && (!list(value.readUrls, 8) || new Set(value.readUrls).size !== value.readUrls.length || value.readUrls.some(url => !value.selectedUrls?.includes(url)))) fail();
  if (value.researchReview !== undefined) {
    const r = value.researchReview;
    if (!obj(r) || typeof r.adequate !== "boolean" || !text(r.missing, 2000) || !list(r.followUpQueries, 3) || r.followUpQueries.some(q => !text(q, 400) || !q.trim()) || new Set(r.followUpQueries).size !== r.followUpQueries.length) fail();
  }
}
export function validateReadingReconstruction(value: unknown, maxResearchBatches=2): asserts value is ReadingReconstruction {
  if (!obj(value) || !list(value.notes, 1000) || value.notes.some(n => !text(n, 20000)) || !list(value.references, 128) || !list(value.draft, 100) || (value.researchComplete !== undefined && typeof value.researchComplete !== "boolean")) fail();
  validateResearchBatch(value);
  if (value.researchBatches !== undefined) {
    if (!list(value.researchBatches, maxResearchBatches)) fail();
    value.researchBatches.forEach(validateResearchBatch);
  }
  if (value.editorNotes !== undefined && !text(value.editorNotes, 80000)) fail();
  if (value.outline !== undefined) {
    const o = value.outline;
    if (!obj(o) || !text(o.title, 300) || !text(o.direction, 6000) || !list(o.questions, 3) || o.questions.some(q => !text(q, 400)) || !list(o.sections, 30) || !o.sections.length || o.sections.some(s => !obj(s) || !text(s.title, 300) || !text(s.brief, 4000) || (s.reuseFrom !== undefined && (!Number.isSafeInteger(s.reuseFrom) || s.reuseFrom < 0 || s.reuseFrom >= 100)) || (s.referenceIds !== undefined && (!list(s.referenceIds,128) || s.referenceIds.some(r=>!id(r)))))) fail();
  }
  const refs = new Set<string>();
  const allRefs = new Set<string>();
  for (const r of value.references) {
    if (!obj(r) || !id(r.id) || allRefs.has(r.id) || !text(r.title, 500) || !isReadingPublicUrl(r.url) || !Number.isSafeInteger(r.capturedAt) || r.capturedAt < 0 || !text(r.text, maxResearchBatches > 2 ? 8 * 1024 * 1024 : 60000) || !["ready", "failed"].includes(r.status) || (r.status === "ready" && !r.text.trim()) || (r.error !== undefined && !text(r.error, 1000)) || (r.publishedAt !== undefined && !text(r.publishedAt, 100))) fail();
    allRefs.add(r.id);
    if (r.status === "ready") refs.add(r.id);
  }
  if (value.revisionDraft !== undefined && !list(value.revisionDraft,100)) fail();
  for (const s of [...value.draft,...(value.revisionDraft ?? [])]) if (!obj(s) || !text(s.title, 300) || !text(s.markdown, 60000) || !list(s.referenceIds, 128) || s.referenceIds.some(r => !refs.has(r))) fail();
  validateReadingInteractions(value.interactions);
  if (value.visualVersion !== undefined && value.visualVersion !== 1) fail();
  validateReadingVisuals(value.visuals, value.animations, value.draft);
}
