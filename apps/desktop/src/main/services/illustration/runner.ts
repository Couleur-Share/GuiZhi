/**
 * 配图生成的编排：逐张生图 → 落盘 → 写回条目正文。
 *
 * 串行而不是并发：生图按张计费且很慢，串行才有稳定的进度反馈与「点一次停」，
 * 并发还容易撞上 provider 的每分钟图片数限额。
 * 单张失败不阻断其余——五张里坏一张，剩下四张照样写进正文，失败的在结果里报出来。
 */
import { KnowledgeItemDB } from "@guizhi/db";
import {
  insertIllustrations,
  replaceIllustration,
  type IllustrationInsert,
} from "@guizhi/shared/utils/illustration-note";
import { buildIllustrationImagePrompt } from "@guizhi/shared/utils/illustration-prompt";
import type {
  IllustrationFailure,
  IllustrationProgress,
  IllustrationShot,
  IllustrationStyle,
  KnowledgeItem,
} from "@guizhi/shared/types";
import { logAppError } from "../../diagnostic-log";
import {
  generateImage,
  isModerationBlockedError,
  saveIllustrationAsset,
  type GeneratedImage,
  type ImageGenModelConfig,
} from "./image-gen";

export type IllustrationProgressReporter = (
  progress: Omit<IllustrationProgress, "itemId">,
) => void;

export interface RunIllustrationsOptions {
  signal?: AbortSignal;
  onProgress?: IllustrationProgressReporter;
}

export interface RunIllustrationsResult {
  item: KnowledgeItem | null;
  generated: number;
  failures: IllustrationFailure[];
  /** 用户中途点了停止 */
  aborted: boolean;
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * 把失败项的段落序号修正到插入之后的正文上。
 *
 * 每张成功的图都是一个新段落块，排在它前面的失败项不受影响，排在它后面的
 * 整体后移一位。不修正的话「补生成那一张」会插到相邻的错误段落——
 * 同一批里成功的越多，偏得越远。
 */
export function shiftFailedAnchors(
  failures: IllustrationFailure[],
  inserts: IllustrationInsert[],
): IllustrationFailure[] {
  return failures.map((failure) => ({
    ...failure,
    afterBlock:
      failure.afterBlock +
      inserts.filter((insert) => insert.afterBlock < failure.afterBlock).length,
  }));
}

/**
 * 内容安全拦截时的第二次机会：去掉风格的排除项重画一次。
 *
 * 图像接口没有 negative 参数，排除项是**拼进正向提示词**的
 * （见 `buildIllustrationImagePrompt`）——安全分类器读到的是 anime、chibi
 * 这些词本身，而不是「不要它们」。内置预设的措辞已经绕开了这类词，但预设文件
 * 首次运行就落到用户机器上、之后不再被覆盖，那份修正到不了已经在用的人手上，
 * 所以兜底只能放在代码里。被拦下的请求不出图也不计费，多试这一次不额外花钱。
 *
 * 返回 null 表示没有第二条路：不是内容安全拦截，或这套风格本来就没写排除项。
 */
export function buildFallbackPrompt(
  style: IllustrationStyle,
  shot: IllustrationShot,
  error: unknown,
): string | null {
  if (!isModerationBlockedError(error) || !style.negative.trim()) {
    return null;
  }
  return buildIllustrationImagePrompt({ ...style, negative: "" }, shot);
}

async function renderShot(
  shot: IllustrationShot,
  style: IllustrationStyle,
  config: ImageGenModelConfig,
  signal?: AbortSignal,
): Promise<string> {
  const draw = (prompt: string) =>
    generateImage(prompt, style.aspectRatio, config, { signal });

  let image: GeneratedImage;
  try {
    image = await draw(buildIllustrationImagePrompt(style, shot));
  } catch (error) {
    const fallback = buildFallbackPrompt(style, shot, error);
    if (!fallback) {
      throw error;
    }
    console.warn(
      `[illustration] 「${shot.topic}」被内容安全拦截，去掉风格排除项再试一次`,
    );
    image = await draw(fallback);
  }
  return saveIllustrationAsset(image);
}

/**
 * 按 shot list 逐张生成并插入正文。
 *
 * 正文以**库里那份**为准（调用方应先让渲染进程落盘未保存的编辑）：
 * 块序号在策划与插入之间必须指向同一份文本。
 */
export async function runIllustrations(
  items: KnowledgeItemDB,
  item: KnowledgeItem,
  shots: IllustrationShot[],
  style: IllustrationStyle,
  config: ImageGenModelConfig,
  options?: RunIllustrationsOptions,
): Promise<RunIllustrationsResult> {
  const inserts: IllustrationInsert[] = [];
  const failures: IllustrationFailure[] = [];
  let aborted = false;

  for (const [index, shot] of shots.entries()) {
    // 中途停止不丢已经画好的：图已经生成、钱已经花了，照样写进正文
    if (options?.signal?.aborted) {
      aborted = true;
      break;
    }
    options?.onProgress?.({
      index: index + 1,
      total: shots.length,
      topic: shot.topic,
      phase: "generating",
    });
    try {
      const assetFileName = await renderShot(shot, style, config, options?.signal);
      inserts.push({
        afterBlock: shot.afterBlock,
        alt: shot.topic,
        assetFileName,
      });
      options?.onProgress?.({
        index: index + 1,
        total: shots.length,
        topic: shot.topic,
        phase: "done",
      });
    } catch (error) {
      if (options?.signal?.aborted) {
        aborted = true;
        break;
      }
      const message = describe(error);
      console.warn(`[illustration] 「${shot.topic}」生成失败:`, error);
      // toast 关掉就没了，失败率得能事后从日志里数出来；带上第几张/共几张，
      // 分母才有着落——只记失败条数看不出「五张挂一张」还是「两张挂一张」
      logAppError({
        scope: "illustration",
        action: "正文配图",
        message,
        itemId: item.id,
        topic: shot.topic,
        index: index + 1,
        total: shots.length,
      });
      failures.push({
        afterBlock: shot.afterBlock,
        topic: shot.topic,
        error: message,
      });
      options?.onProgress?.({
        index: index + 1,
        total: shots.length,
        topic: shot.topic,
        phase: "failed",
        error: message,
      });
    }
  }

  if (inserts.length === 0) {
    return { item: null, generated: 0, failures, aborted };
  }

  // 生成期间用户可能改过正文，按最新一份插入
  const latest = items.get(item.id) ?? item;
  const updated = items.update(item.id, {
    content: insertIllustrations(latest.content, inserts),
  });
  console.log(
    `[illustration] 已为条目 ${item.id} 写入 ${inserts.length} 张配图`,
  );
  return {
    item: updated ?? null,
    generated: inserts.length,
    failures: shiftFailedAnchors(failures, inserts),
    aborted,
  };
}

/** 重新生成正文里已有的某一张：原位替换，位置与前后文都不动 */
export async function regenerateIllustration(
  items: KnowledgeItemDB,
  item: KnowledgeItem,
  assetFileName: string,
  shot: IllustrationShot,
  style: IllustrationStyle,
  config: ImageGenModelConfig,
  signal?: AbortSignal,
): Promise<KnowledgeItem | null> {
  const nextAssetFileName = await renderShot(shot, style, config, signal);
  const latest = items.get(item.id) ?? item;
  return (
    items.update(item.id, {
      content: replaceIllustration(latest.content, assetFileName, {
        assetFileName: nextAssetFileName,
        alt: shot.topic,
      }),
    }) ?? null
  );
}
