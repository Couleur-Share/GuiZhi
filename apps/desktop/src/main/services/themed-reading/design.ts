import { chatCompletion, type AIClientConfig } from "@guizhi/core";
import type { ThemedReadingAsset, ThemedReadingVersion } from "@guizhi/shared/types";
import { recordMainAiUsage } from "../ai-usage";
import { describeThemedBlock, THEMED_READING_PLAN_GUIDANCE } from "./design-guidance";
import { COMPOSITION_GUIDANCE } from "./composition-guidance";

export function parseDesignJson(text: string): Record<string, unknown> {
  if (text.length > 1_000_000) throw new Error("主题设计响应超过大小上限");
  const firstContainer = /[[{]/.exec(text);
  if (firstContainer?.[0] === "[") throw new Error("主题设计必须是 JSON 对象，不能是数组");
  const start = text.indexOf("{"), end = text.lastIndexOf("}");
  if (start < 0 || end <= start) throw new Error("模型未返回有效的主题设计 JSON");
  const parsed: unknown = JSON.parse(text.slice(start, end + 1));
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("主题设计格式不正确");
  return parsed as Record<string, unknown>;
}

export async function callDesignModel(config: AIClientConfig, prompt: string, signal: AbortSignal, chat = chatCompletion, onRequest?: () => void, system = COMPOSITION_GUIDANCE, budget?: { maxTokens: number; timeoutMs: number; onProgress?: (receivedChars: number) => void }): Promise<Record<string, unknown>> {
  signal.throwIfAborted();
  onRequest?.();
  let result: Awaited<ReturnType<typeof chatCompletion>>;
  try {
    result = await chat(config, [{ role: "system", content: system }, { role: "user", content: prompt }], {
      signal, temperature: 0.5, maxTokens: budget?.maxTokens ?? 12_000, timeoutMs: budget?.timeoutMs ?? 180_000, ...(budget ? {stream:true, onProgress: budget.onProgress} : {}),
    });
  } catch (error) {
    recordMainAiUsage({ scenario: "themedReading", model: config.model, failed: true });
    throw error;
  }
  recordMainAiUsage({ scenario: "themedReading", model: config.model, promptTokens: result.usage?.promptTokens, completionTokens: result.usage?.completionTokens });
  signal.throwIfAborted();
  return parseDesignJson(result.content);
}

export async function planTheme(version: ThemedReadingVersion, config: AIClientConfig, signal: AbortSignal, onRequest?: () => void) {
  const pendingImages = version.assets.filter(a => a.role === "generated" && a.status !== "ready").length;
  const max = version.options.generateImages ? Math.max(0, version.options.maxImages - pendingImages) : 0;
  const step = Math.max(1, Math.ceil(version.source.blocks.length / 80));
  // 全局规划只需分布均匀的结构预览；明确标记采样并保留尾部，各章节随后得到完整内容块。
  const outline = version.source.blocks.filter((_b, index, blocks) => index % step === 0 || index === blocks.length - 1)
    .map(b => ({ id: b.id, ...describeThemedBlock(b), preview: b.text.slice(0, 180) }));
  const result = await callDesignModel(config, JSON.stringify({
    task: `规划这篇文章的统一视觉方向与所需插画，不输出布局。返回{direction:string,assets:[{id,purpose,prompt,alt,aspectRatio,blockId}]}。${THEMED_READING_PLAN_GUIDANCE}插画不带文字，不作为事实证据。blockId必须是给出的正文ID。生成 minImages 到 maxImages 张与文章内容相关的主题插画；minImages 为1时不能返回空数组。已有原图或SVG不能代替本次要求生成的插画。素材ID用g0、g1等。`,
    title: version.source.title, preference: version.options.style, minImages: max > 0 ? 1 : 0, maxImages: max,
    existingImages: version.assets.map(a => ({ id: a.id, alt: a.alt, role: a.role })),
    outline, totalBlocks: version.source.blocks.length, sampledOutline: outline.length < version.source.blocks.length,
  }), signal, undefined, onRequest);
  const direction = String(result.direction ?? "").trim().slice(0, 6000);
  if (!direction) throw new Error("模型未返回页面视觉方案");
  const ids = new Set(version.source.blocks.map(b => b.id));
  const assets: ThemedReadingAsset[] = (Array.isArray(result.assets) ? result.assets : []).slice(0, max).map((value, i): ThemedReadingAsset => {
    const raw = value && typeof value === "object" ? value as Record<string, unknown> : {};
    return {
      id: `g${version.id.slice(0, 8)}-${i}`, role: "generated", purpose: String(raw.purpose ?? "主题插画").slice(0, 200),
      prompt: String(raw.prompt ?? "").trim().slice(0, 6000), alt: String(raw.alt ?? raw.purpose ?? "AI 主题插画").slice(0, 300),
      aspectRatio: ["16:9", "4:3", "1:1"].includes(String(raw.aspectRatio)) ? raw.aspectRatio as ThemedReadingAsset["aspectRatio"] : "16:9",
      blockId: ids.has(String(raw.blockId)) ? String(raw.blockId) : version.source.blocks[0]?.id,
      status: "pending",
    };
  }).filter(a => a.prompt);
  if (max > 0 && !assets.length) throw new Error("已勾选生成主题图片，但模型未提供有效配图方案，请重试；本次尚未调用生图接口");
  return { direction, assets };
}
