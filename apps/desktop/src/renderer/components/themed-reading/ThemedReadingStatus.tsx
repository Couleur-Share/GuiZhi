import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  CheckIcon,
  ChevronRightIcon,
  InfoIcon,
  Loader2Icon,
} from "lucide-react";
import type { ThemedReadingTask } from "@guizhi/shared/types/themed-reading";
import { copyTextToClipboard } from "../../utils/clipboard";
import { canResumeThemeTask, isThemeTaskActive } from "./use-themed-reading";
import { themeButton } from "./ThemedReadingSetup";

const stages = {
  prepare: "准备内容",
  understand: "理解原文",
  research: "联网查证",
  write: "撰写文章",
  design: "设计页面",
  images: "生成主题图片",
  assemble: "组装页面",
  validate: "检查页面",
  done: "完成",
};
const states = {
  queued: "排队中",
  running: "进行中",
  completed: "已完成",
  partial: "部分内容尚未完成",
  failed: "本次生成未完成",
  cancelled: "已停止生成",
  interrupted: "生成已中断",
};
const stepLabels = {
  prepare: "内容",
  understand: "理解",
  research: "研究",
  write: "写作",
  design: "设计",
  images: "图片",
  assemble: "排版",
  validate: "检查",
};

/** 首次生成突出阶段进展；已有成品时保持紧凑，原因和用量按需展开。 */
export function ThemedReadingStatus({
  task,
  busy,
  readOnly,
  expanded = false,
  hasPage = false,
  onCancel,
  onResume,
  onOffline,
}: {
  task: ThemedReadingTask;
  busy?: boolean;
  readOnly?: boolean;
  expanded?: boolean;
  hasPage?: boolean;
  onCancel: () => void;
  onResume: () => void;
  onOffline?: () => void;
}) {
  const { t } = useTranslation();
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">(
    "idle",
  );
  const active = isThemeTaskActive(task),
    research = task.stage === "research";
  const researchExhausted =
    research &&
    Boolean(task.error?.startsWith("补充查证仍未完成，已达到本次自动补查上限"));
  const resumable = canResumeThemeTask(task) && !researchExhausted;
  const steps = (
    [
      "prepare",
      "understand",
      "research",
      "write",
      "design",
      "images",
      "assemble",
      "validate",
    ] as const
  ).filter(
    (stage) =>
      stage !== "images" || task.plannedImages !== 0 || task.stage === "images",
  );
  const currentStep =
    task.stage === "done" ? steps.length : steps.indexOf(task.stage);
  const stageLabel = task.stage === "assemble" && task.total > 0
    ? t("themedReading.compilingGraphics", "处理图形")
    : t(`themedReading.stage.${task.stage}`, stages[task.stage]);
  const designDetail = task.stage === "design" && task.designProgress
    ? (task.designProgress.receivedChars > 0
      ? t("themedReading.designReceiving", "正在输出页面 · 已接收 {{count}} 字符", { count: task.designProgress.receivedChars })
      : t("themedReading.designThinking", "正在构思页面布局"))
    : null;
  const title = active
    ? task.state === "queued"
      ? t("themedReading.readerQueued", "阅读页已加入队列")
      : `${task.designProgress?.attempt > 1 && task.stage === "design" ? t("themedReading.designRepairing", "调整页面设计") : stageLabel}${task.total > 0 ? ` · ${task.completed}/${task.total}` : ""}`
    : research
      ? t("themedReading.readerResearchPaused", "资料查证尚未完成")
      : t(`themedReading.readerState.${task.state}`, states[task.state]);
  const context = task.state === "completed" ? "" : hasPage
    ? active
      ? t("themedReading.readerUpdating", "正在更新，当前仍可阅读已有版本。")
      : t("themedReading.readerPreviousAvailable", "当前显示上次生成的版本。")
    : t("themedReading.readerSourceSafe", "原文已保留，可随时切回阅读。");
  const copy = async () => {
    try {
      await copyTextToClipboard(task.error ?? "");
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
  };
  const progress = active ? (
    <ol
      className={
        expanded ? "grid gap-x-1.5 gap-y-4" : "flex flex-wrap gap-x-3 gap-y-2"
      }
      style={
        expanded
          ? { gridTemplateColumns: "repeat(auto-fit, minmax(3.25rem, 1fr))" }
          : undefined
      }
      aria-label={t("themedReading.generationSteps", "生成步骤")}
    >
      {steps.map((stage, index) => {
        const done = task.state !== "queued" && index < currentStep;
        const current = task.state !== "queued" && index === currentStep;
        return (
          <li
            key={stage}
            aria-current={current ? "step" : undefined}
            className={`min-w-0 ${current ? "font-medium text-primary" : done ? "text-foreground/75" : "text-muted-foreground"}`}
          >
            {expanded ? (
              <div
                aria-hidden="true"
                className={`mb-2.5 h-1 rounded-full ${current ? "bg-primary motion-safe:animate-pulse" : done ? "bg-primary/40" : "bg-border/70"}`}
              />
            ) : null}
            <span className="flex items-center justify-center gap-1 text-xs leading-5">
              {done ? (
                <CheckIcon
                  className="h-3 w-3 shrink-0 text-primary/70"
                  aria-hidden="true"
                />
              ) : null}
              <span className="break-words">
                {t(`themedReading.step.${stage}`, stepLabels[stage])}
              </span>
            </span>
          </li>
        );
      })}
    </ol>
  ) : null;
  return (
    <section
      data-testid="reading-task-status"
      className={
        expanded
          ? "w-full max-w-xl overflow-hidden rounded-2xl border border-border/70 bg-card shadow-lg shadow-black/[0.04]"
          : "border-b border-border/50 px-4 py-2.5"
      }
    >
      <div
        className={
          expanded
            ? "flex flex-wrap items-center gap-x-4 gap-y-3 px-6 pb-6 pt-7"
            : "flex flex-wrap items-center gap-x-3 gap-y-2"
        }
      >
        <div
          className={`flex min-w-0 flex-1 items-center ${expanded ? "basis-64 gap-3.5" : "gap-2"}`}
          role="status"
          aria-atomic="true"
        >
          <span
            className={
              expanded
                ? "flex h-11 w-11 shrink-0 items-center justify-center rounded-xl bg-primary/10 text-primary ring-1 ring-inset ring-primary/10"
                : "shrink-0"
            }
          >
            {active ? (
              <Loader2Icon
                className={`${expanded ? "h-5 w-5" : "h-3.5 w-3.5"} text-primary motion-safe:animate-spin`}
                aria-hidden="true"
              />
            ) : (
              <InfoIcon
                className={`${expanded ? "h-5 w-5" : "h-3.5 w-3.5"} text-muted-foreground`}
                aria-hidden="true"
              />
            )}
          </span>
          <div className="min-w-0 text-xs leading-5">
            {expanded && active ? (
              <p className="mb-1 text-xs text-muted-foreground">
                {t("themedReading.readerGenerating", "正在生成 AI 阅读页")}
              </p>
            ) : null}
            <p
              className={
                expanded
                  ? "text-base font-semibold leading-6 tracking-tight text-foreground"
                  : "font-medium text-foreground/85"
              }
            >
              {title}
            </p>
            {active && designDetail ? <p className="mt-1 text-muted-foreground">{designDetail}</p> : null}
            <p className={`${expanded ? "mt-1.5 " : ""}text-muted-foreground`}>
              {context}
            </p>
          </div>
        </div>
        {!readOnly && active ? (
          <button className={themeButton} disabled={busy} onClick={onCancel}>
            {t("themedReading.stop", "停止生成")}
          </button>
        ) : null}
        {!readOnly && resumable ? (
          <button className={themeButton} disabled={busy} onClick={onResume}>
            {research
              ? t("themedReading.readerRetryResearch", "补查资料并继续")
              : t("themedReading.readerResume", "继续生成")}
          </button>
        ) : null}
        {!readOnly && researchExhausted && onOffline ? (
          <button className={themeButton} disabled={busy} onClick={onOffline}>
            {t("themedReading.readerOffline", "不联网继续生成")}
          </button>
        ) : null}
      </div>
      {expanded && active ? <div className="px-6 pb-6">{progress}</div> : null}
      <details
        className={
          expanded
            ? "group border-t border-border/50 bg-muted/20 px-6 text-xs text-muted-foreground"
            : "group mt-1 text-xs text-muted-foreground"
        }
      >
        <summary
          className={`flex cursor-pointer list-none items-center gap-1.5 rounded hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring [&::-webkit-details-marker]:hidden ${expanded ? "py-3" : "w-fit py-1"}`}
        >
          <ChevronRightIcon
            className="h-3.5 w-3.5 shrink-0 transition-transform group-open:rotate-90 motion-reduce:transition-none"
            aria-hidden="true"
          />
          {t("themedReading.readerTaskDetails", "查看生成详情")}
          {expanded && active && task.state !== "queued" ? (
            <span className="ml-auto pl-3 tabular-nums">
              {t(
                "themedReading.readerStageCount",
                "阶段 {{current}} / {{total}}",
                {
                  current: Math.min(currentStep + 1, steps.length),
                  total: steps.length,
                },
              )}
            </span>
          ) : null}
        </summary>
        <div className="max-h-64 space-y-3 overflow-y-auto pb-4 pt-2">
          {!expanded ? progress : null}
          {task.error ? (
            <div className="space-y-2">
              <p className="select-text whitespace-pre-wrap break-words rounded-lg bg-muted/50 p-3 leading-relaxed text-foreground/75">
                {task.error}
              </p>
              <button className={themeButton} onClick={() => void copy()}>
                {copyState === "copied"
                  ? t("themedReading.readerCopied", "已复制原因")
                  : t("themedReading.readerCopyError", "复制详细原因")}
              </button>
              {copyState === "failed" ? (
                <p role="alert">
                  {t(
                    "themedReading.readerCopyFailed",
                    "复制失败，请选择上方文字手动复制。",
                  )}
                </p>
              ) : null}
            </div>
          ) : null}
          {researchExhausted ? (
            <p>
              {t(
                "themedReading.readerResearchLimit",
                "本次补查已达上限。可以不联网继续生成，或重新开始一次生成。",
              )}
            </p>
          ) : null}
          {Number.isSafeInteger(task.plannedImages) &&
          task.plannedImages >= 0 &&
          task.plannedImages <= 5 ? (
            <p>
              {t(
                "themedReading.plannedImages",
                "计划新增 {{count}} 张主题图片",
                { count: task.plannedImages },
              )}
            </p>
          ) : null}
          {task.usage?.searchCalls !== undefined ? (
            <p>
              {t(
                "themedReading.readerResearchUsage",
                "搜索 {{search}} 次 · 获取资料 {{pages}} 篇",
                {
                  search: task.usage.searchCalls,
                  pages: task.usage.pagesRead ?? 0,
                },
              )}
            </p>
          ) : null}
          {task.firstContentAt?<p>首个内容：{((task.firstContentAt-task.createdAt)/1000).toFixed(1)} 秒</p>:null}
          {task.reused && Object.values(task.reused).some(n=>n>0)?<p>已复用：{task.reused.chapters} 章正文 · {task.reused.notes} 份笔记 · {task.reused.references} 篇资料 · {task.reused.assets} 张图片</p>:null}
          {task.timings?<p>{Object.entries(task.timings).map(([stage,ms])=>`${stages[stage]??stage} ${(ms/1000).toFixed(1)} 秒`).join(" · ")}</p>:null}
          {task.usage ? (
            <div className="flex flex-wrap gap-x-4 gap-y-1">
              <span>
                {t("themedReading.textCalls", "文本请求 {{count}} 次", {
                  count: task.usage.textCalls,
                })}
              </span>
              <span>
                {t("themedReading.imageCalls", "生图请求 {{count}} 次", {
                  count: task.usage.imageCalls,
                })}
              </span>
              <span>
                {t("themedReading.imagesSaved", "新图保存 {{count}} 张", {
                  count: task.usage.imagesSaved,
                })}
              </span>
            </div>
          ) : (
            <p>{t("themedReading.usageMissing", "此任务未记录用量")}</p>
          )}
          {active && !expanded ? (
            <p>
              {t(
                "themedReading.backgroundHint",
                "可以切换文章或关闭此面板，稍后到处理中心查看进度。",
              )}
            </p>
          ) : null}
          {!readOnly &&
          research &&
          !active &&
          !researchExhausted &&
          onOffline ? (
            <button className={themeButton} disabled={busy} onClick={onOffline}>
              {t("themedReading.readerOffline", "不联网继续生成")}
            </button>
          ) : null}
        </div>
      </details>
      {expanded && active ? (
        <p className="border-t border-border/40 px-6 py-3 text-xs leading-5 text-muted-foreground">
          {t(
            "themedReading.backgroundHint",
            "可以切换文章或关闭此面板，稍后到处理中心查看进度。",
          )}
        </p>
      ) : null}
    </section>
  );
}
