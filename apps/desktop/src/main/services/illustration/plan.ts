/**
 * 配图策划：读正文挑「认知锚点」，产出 shot list。
 *
 * 走 mainText 路由（回退 fastText，与内容总结同一口径）——挑哪里值得配图
 * 是判断题，便宜模型给出的位置明显更平均、更没有取舍。
 * 这一步只花几秒、几分钱，先跑它给用户过目，再决定生成哪几张。
 */
import {
  chatCompletion,
  type AIChatMessage,
  type AIClientConfig,
} from "@guizhi/core";
import {
  buildIllustrationPlanPrompt,
  deriveShotTarget,
  ILLUSTRATION_PLAN_SYSTEM_PROMPT,
  parseIllustrationPlan,
} from "@guizhi/shared/utils/illustration-prompt";
import type { ContentBlock } from "@guizhi/shared/utils/illustration-note";
import type {
  IllustrationShot,
  IllustrationStyle,
} from "@guizhi/shared/types";

/**
 * 0.6 时同一篇文章两次策划连张数都不一样。
 *
 * 隐喻的发挥空间主要在 composition / elements 的措辞上，降到 0.4 对创意
 * 影响有限，对「挑哪几段、出几张」的稳定性帮助明显。
 */
const PLAN_TEMPERATURE = 0.4;
const PLAN_MAX_TOKENS = 4096;
const PLAN_TIMEOUT_MS = 120_000;

/** 要几张：数字 = 恰好这么多；"auto" = 按可配图段落数推导 */
export type IllustrationShotCount = number | "auto";

export interface IllustrationPlanInput {
  title: string;
  /** 候选段落（listAnchorBlocks 的结果，或重新生成单张时的那一段） */
  blocks: ContentBlock[];
  style: IllustrationStyle;
  /** 本次要几张；重新生成单张时传 1 */
  shotCount: IllustrationShotCount;
  /** 可选风格清单，让模型顺带给个建议；重新生成单张时不传 */
  catalog?: IllustrationStyle[];
}

export interface IllustrationPlanOutput {
  shots: IllustrationShot[];
  /** 模型建议改用的风格 id，已校验存在且与当前不同；没有建议则为空串 */
  suggestedStyleId: string;
}

/** 目标张数：显式指定就用它，「自动」按篇幅推导；两者都受预设与候选段落数约束 */
export function resolveShotTarget(
  requested: IllustrationShotCount,
  anchorCount: number,
  maxShots: number,
): number {
  if (requested === "auto") {
    return deriveShotTarget(anchorCount, maxShots);
  }
  return Math.max(1, Math.min(Math.trunc(requested), maxShots, anchorCount));
}

export interface IllustrationPlanOptions {
  signal?: AbortSignal;
  /** 测试注入：底层 chat 调用 */
  chat?: typeof chatCompletion;
}

/**
 * 产出配图规格。
 *
 * 候选段落只给模型看序号与内容，插入位置回来仍按同一套序号定位——
 * 不做原文片段的模糊匹配，位置就不会因为标点或空白的细微差异跑偏。
 */
/** 模型可能编一个不存在的 id，也可能把当前这套原样报回来，两种都当没建议 */
export function resolveSuggestedStyle(
  suggested: string,
  currentId: string,
  catalog: IllustrationStyle[],
): string {
  const trimmed = suggested.trim();
  if (!trimmed || trimmed === currentId) {
    return "";
  }
  return catalog.some((style) => style.id === trimmed) ? trimmed : "";
}

export async function planIllustrations(
  input: IllustrationPlanInput,
  config: AIClientConfig,
  options?: IllustrationPlanOptions,
): Promise<IllustrationPlanOutput> {
  if (input.blocks.length === 0) {
    throw new Error("正文太短或都是图片与元数据，没有值得配图的段落");
  }

  const targetShots = resolveShotTarget(
    input.shotCount,
    input.blocks.length,
    input.style.maxShots,
  );
  const messages: AIChatMessage[] = [
    { role: "system", content: ILLUSTRATION_PLAN_SYSTEM_PROMPT },
    {
      role: "user",
      content: buildIllustrationPlanPrompt({ ...input, targetShots }),
    },
  ];

  // 不上 response_format: json_object——中转站对它的支持参差不齐，
  // 解析侧本来就对「对象 / 裸数组 / 夹带解释文字」都做了容错抽取。
  const chat = options?.chat ?? chatCompletion;
  const result = await chat(config, messages, {
    temperature: PLAN_TEMPERATURE,
    maxTokens: PLAN_MAX_TOKENS,
    signal: options?.signal,
    timeoutMs: PLAN_TIMEOUT_MS,
  });

  const plan = parseIllustrationPlan(result.content, {
    allowedBlocks: input.blocks.map((block) => block.index),
    maxShots: targetShots,
    maxLabels: input.style.maxLabels,
  });
  if (plan.shots.length === 0) {
    throw new Error("模型未返回可用的配图规格");
  }
  return {
    shots: plan.shots,
    suggestedStyleId: resolveSuggestedStyle(
      plan.styleId,
      input.style.id,
      input.catalog ?? [],
    ),
  };
}
