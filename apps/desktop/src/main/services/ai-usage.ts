/**
 * 主进程侧的 AI 用量记账。
 *
 * 设置页的用量面板此前只统计渲染进程发起的三条链路（对话 / OCR / embedding），
 * 主进程的正文配图、视频总结、文字稿排版、语音转写一条都不记——而配图按张
 * 计费，是全应用最贵的一项。面板上给出的不是「少了一点」的数字，而是把大头
 * 整个漏掉的数字，比不显示更误导。
 *
 * 契约与渲染进程的 `recordAiUsage` 一致：**记账失败绝不影响主流程**。
 * 为了记一笔账把用户一次几十秒的生成搞失败，是本末倒置。
 */
import type { AIUsageScenarioId } from "@guizhi/shared/types";
import { AIUsageDB } from "@guizhi/db";
import { tryGetDatabase } from "../database";
import { reportAiCall } from "./ai-call-context";

export interface MainAiUsageEntry {
  scenario: AIUsageScenarioId;
  model: string;
  promptTokens?: number;
  completionTokens?: number;
  /** 调用失败（仍然计入次数：超时与限流同样可能产生费用） */
  failed?: boolean;
}

export function recordMainAiUsage(entry: MainAiUsageEntry): void {
  if (!entry.model) {
    return;
  }
  // 归属到当前导入任务（不在导入链路里时是空操作）。必须排在库句柄之前：
  // 任务侧的阶段统计只写在任务行上，不该因为用量库不可用而跟着一起丢
  reportAiCall(entry);
  // 库还没初始化（单测、备份恢复期间）不是故障，没什么可记的，也不该刷日志
  const db = tryGetDatabase();
  if (!db) {
    return;
  }
  try {
    new AIUsageDB(db).record({
      scenario: entry.scenario,
      model: entry.model,
      promptTokens: entry.promptTokens ?? 0,
      completionTokens: entry.completionTokens ?? 0,
      failed: entry.failed === true,
    });
  } catch (error) {
    console.warn("[usage] 记录 AI 用量失败:", error);
  }
}
