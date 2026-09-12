import { validateReadingVisualResults } from "@guizhi/shared/utils/reading-visuals";
import { validateReadingGeneration, validateReadingLibraries, validateReadingScripts } from "@guizhi/shared/utils/reading-page-v3";
import { validateReadingReconstruction } from "@guizhi/shared/utils/reading-reconstruction";
import type { ThemedReadingAsset, ThemedReadingTask, ThemedReadingVersion } from "@guizhi/shared/types";
import { isSafeAssetFileName } from "@guizhi/shared/utils/media-refs";
import { validateThemedComposition } from "@guizhi/shared/utils/themed-composition";

const kinds = new Set(["body", "summary"]);
const roles = new Set(["current", "previous", "working"]);
const states = new Set(["queued", "running", "completed", "partial", "failed", "cancelled", "interrupted"]);
const stages = new Set(["prepare", "understand", "research", "write", "design", "images", "assemble", "validate", "done"]);
const text = (value: unknown): value is string => typeof value === "string";
const id = (value: unknown): value is string => text(value) && value.length > 0 && value.length <= 256;
const timestamp = (value: unknown) => Number.isSafeInteger(value) && Number(value) >= 0;
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === "object" && !Array.isArray(value);

export function isThemedReadingAssetFileName(value: unknown): value is string {
  return text(value) && isSafeAssetFileName(value) && /\.(png|jpe?g|gif|webp)$/i.test(value);
}

function validAsset(value: unknown): value is ThemedReadingAsset {
  if (!object(value) || !id(value.id) || !["original", "generated"].includes(String(value.role)) ||
      !text(value.purpose) || !text(value.prompt) || !text(value.alt) ||
      !["16:9", "4:3", "1:1"].includes(String(value.aspectRatio)) ||
      !["pending", "ready", "failed"].includes(String(value.status)) ||
      (value.blockId !== undefined && !id(value.blockId)) ||
      (value.originalUrl !== undefined && !text(value.originalUrl)) ||
      (value.error !== undefined && !text(value.error))) return false;
  if (value.fileName !== undefined) {
    if (!isThemedReadingAssetFileName(value.fileName)) return false;
    if (value.status !== "ready" && value.sha256 === undefined && value.bytes === undefined) return true;
    return text(value.sha256) && /^[a-f0-9]{64}$/.test(value.sha256) && timestamp(value.bytes);
  }
  return value.status !== "ready" && value.sha256 === undefined && value.bytes === undefined;
}

function validDesign(value: unknown): boolean {
  return object(value) && text(value.direction) && text(value.html) && text(value.css) &&
    Array.isArray(value.assets) && value.assets.length <= 200 && value.assets.every(validAsset);
}

/** 数据库和备份均非可信输入；这里只校验数据结构，HTML 策略由主进程另行执行。 */
export function validateThemedReadingVersion(value: unknown): asserts value is ThemedReadingVersion {
  if (!object(value) || !id(value.id) || !id(value.itemId) || !kinds.has(String(value.sourceKind)) ||
      !roles.has(String(value.role)) || ![1, 2, 3].includes(value.formatVersion as number) ||
      !timestamp(value.createdAt) || !timestamp(value.updatedAt) ||
      !Array.isArray(value.warnings) || !value.warnings.every(text) ||
      (value.textModel !== undefined && !text(value.textModel)) ||
      (value.imageModel !== undefined && !text(value.imageModel))) throw new Error("主题阅读页记录格式无效");
  const source = value.source;
  if (!object(source) || !text(source.title) || !text(source.content) ||
      (source.sourceUri !== null && !text(source.sourceUri)) || !text(source.fingerprint) ||
      !/^[a-f0-9]{64}$/.test(source.fingerprint) || !Array.isArray(source.blocks) ||
      source.blocks.length > 10000 || source.blocks.some(block => !object(block) ||
        !id(block.id) || !text(block.markdown) || !text(block.html) || !text(block.text)) ||
      new Set(source.blocks.map(block => block.id)).size !== source.blocks.length) {
    throw new Error("主题阅读页内容快照格式无效");
  }
  const options = value.options;
  if (object(options) && ((options.enhancedInteraction !== undefined && typeof options.enhancedInteraction !== "boolean") || (options.researchDepth !== undefined && !["standard", "deep"].includes(String(options.researchDepth))))) throw new Error("阅读页交互或查证选项无效");
  if (!object(options) || !text(options.style) || typeof options.generateImages !== "boolean" ||
      !Number.isInteger(options.maxImages) || Number(options.maxImages) < 0 || Number(options.maxImages) > 5 ||
      (options.fromCurrent !== undefined && typeof options.fromCurrent !== "boolean")) {
    throw new Error("主题阅读页生成选项无效");
  }
  if ((options.research !== undefined && typeof options.research !== "boolean") || (options.action !== undefined && !["create", "revise", "redesign", "refresh"].includes(String(options.action)))) throw new Error("重构生成选项无效");
  if (!Array.isArray(value.assets) || value.assets.length > 200 || !value.assets.every(validAsset) ||
      new Set(value.assets.map(asset => asset.id)).size !== value.assets.length) {
    throw new Error("主题阅读页资源清单无效");
  }
  if ((value.design !== null && !validDesign(value.design)) ||
      (value.designDirection !== undefined && !text(value.designDirection)) ||
      (value.designParts !== undefined && (!Array.isArray(value.designParts) || value.designParts.length > 10000 || !value.designParts.every(validDesign)))) {
    throw new Error("主题阅读页设计或章节检查点格式无效");
  }
  if (JSON.stringify(value).length > 16 * 1024 * 1024) throw new Error("主题阅读页数据超过大小上限");
  if ((value as {formatVersion?:number}).formatVersion === 3 && new TextEncoder().encode(JSON.stringify(value)).byteLength > 16 * 1024 * 1024) throw new Error("主题阅读页数据超过 16 MiB 上限");
  const version = value as unknown as ThemedReadingVersion;
  if (version.design) validateReadingVisualResults(version.design.visualResults);
  const checkComposition = (design: ThemedReadingVersion["design"], partial: boolean) => {
    if (design?.composition === undefined) return;
    if (design.html.trim() || design.css.trim()) throw new Error("专题重构不能混用自由布局");
    const ids = design.composition?.chapters?.flatMap(chapter => chapter?.blockIds ?? []) ?? [];
    const source = partial ? { ...version.source, blocks: version.source.blocks.filter(block => ids.includes(block.id)) } : version.source;
    validateThemedComposition(design.composition, source, version.assets.map(asset => asset.id));
  };
  if (version.formatVersion >= 2) {
    validateReadingReconstruction(version.reconstruction, version.formatVersion===3?20:2);
    if (version.design?.composition || version.designParts?.length) throw new Error("新重构页不能混入旧格式章节");
  }
  if (version.formatVersion === 3) {
    if (version.generation) validateReadingGeneration(version.generation);
    if (version.design) { validateReadingScripts(version.design.scripts ?? []); validateReadingLibraries(version.design.libraries ?? []); }
  } else if (version.generation || version.design?.scripts?.length || version.design?.libraries?.length) throw new Error("旧页面不能携带模型脚本");
  checkComposition(version.design, false);
  version.designParts?.forEach(part => checkComposition(part, true));
}

export function validateThemedReadingTask(value: unknown): asserts value is ThemedReadingTask {
  if(object(value)&&value.reused!==undefined){const reused=value.reused;if(!object(reused)||["chapters","notes","references","assets"].some(k=>!timestamp(reused[k])))throw new Error("阅读页复用指标无效");}
  if(object(value)&&((value.timings!==undefined&&(!object(value.timings)||Object.entries(value.timings).some(([k,v])=>!stages.has(k)||!timestamp(v))))||(value.firstContentAt!==undefined&&!timestamp(value.firstContentAt))||(value.previewRevision!==undefined&&!timestamp(value.previewRevision))||(value.issues!==undefined&&(!Array.isArray(value.issues)||value.issues.length>100||value.issues.some(i=>!object(i)||!text(i.message)||!text(i.kind))))))throw new Error("阅读页任务指标无效");
  if (!object(value) || !id(value.id) || !id(value.itemId) || !id(value.versionId) ||
      !kinds.has(String(value.sourceKind)) || !states.has(String(value.state)) ||
      !stages.has(String(value.stage)) || !text(value.title) ||
      !timestamp(value.completed) || !timestamp(value.total) || Number(value.completed) > Number(value.total) ||
      (value.plannedImages !== undefined && (!timestamp(value.plannedImages) || Number(value.plannedImages) > 5)) ||
      (value.usage !== undefined && (!object(value.usage) || !timestamp(value.usage.textCalls) ||
        !timestamp(value.usage.imageCalls) || !timestamp(value.usage.imagesSaved) ||
        (value.usage.searchCalls !== undefined && !timestamp(value.usage.searchCalls)) ||
        (value.usage.pagesRead !== undefined && !timestamp(value.usage.pagesRead)))) ||
      !timestamp(value.createdAt) || !timestamp(value.updatedAt) ||
      (value.error !== undefined && !text(value.error)) || (value.assetId !== undefined && !id(value.assetId))) {
    throw new Error("主题阅读页任务格式无效");
  }
}
