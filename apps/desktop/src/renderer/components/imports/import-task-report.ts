/**
 * 导入任务诊断文本：把一条任务序列化成可直接粘进 issue 或丢给 AI 的 Markdown。
 *
 * 存在的理由与 AI 交接稿同源——用户报「这批采集特别慢」「失败率特别高」时，
 * 双方手上都拿不出数。界面上看得见的东西必须能一键带走，否则等于没有。
 *
 * 与界面显示有一处**故意的不同**：这里列出全部阶段，不折叠亚秒的那几个。
 * 折叠是为了扫视时的信噪比，而诊断文本的读者是排查的人（或模型），
 * 少一行就是少一条线索，宁可啰嗦。
 */
import type { ImportStageStat, ImportTask } from "@guizhi/shared/types";
import { detectPlatformCapturePlatform } from "@guizhi/shared/utils/platform-capture";
import {
  STAGE_LABELS,
  STATUS_LABELS,
  formatDuration,
  formatImportTaskErrorForReport,
  formatImportTaskWarning,
} from "./import-task-meta";

/** i18next 的 `t(key, fallback, options)`，只取用得上的这一段签名 */
export type ReportTranslate = (
  key: string,
  fallback: string,
  options?: Record<string, unknown>,
) => string;

export interface ImportTaskReportContext {
  translate: ReportTranslate;
  /** 应用版本与平台（`window.electron.updater`）；取不到就不写这一行 */
  appVersion?: string;
  platform?: string;
  captureBrowser?: string;
  /** 测试注入：绝对时间的格式化 */
  formatTime?: (ms: number) => string;
}

/** 表格单元格里的 `|` 会把列切断 */
function escapeCell(value: string): string {
  return value.replace(/\|/g, "\\|").replace(/\n+/g, " ");
}

function shareOf(ms: number, totalMs: number): string {
  const share = totalMs > 0 ? (ms / totalMs) * 100 : 0;
  const rounded = Math.round(share);
  return rounded === 0 && ms > 0 ? "<1%" : `${rounded}%`;
}

function aiDetailOf(stat: ImportStageStat, t: ReportTranslate): string {
  if (!stat.calls) {
    return "";
  }
  const tokens = (stat.promptTokens ?? 0) + (stat.completionTokens ?? 0);
  return [
    t("imports.stageCalls", "{{calls}} 次调用", { calls: stat.calls }),
    stat.failedCalls
      ? t("imports.stageFailedCalls", "{{count}} 次失败", {
          count: stat.failedCalls,
        })
      : "",
    tokens > 0 ? t("imports.stageTokens", "{{tokens}} token", { tokens }) : "",
    stat.models?.length ? stat.models.join("、") : "",
  ]
    .filter(Boolean)
    .join(" · ");
}

export function buildImportTaskReport(
  task: ImportTask,
  context: ImportTaskReportContext,
): string {
  const t = context.translate;
  const formatTime =
    context.formatTime ?? ((ms: number) => new Date(ms).toLocaleString());
  const status = STATUS_LABELS[task.status] ?? STATUS_LABELS.failed;
  const capturePlatform = task.sourceKind === "url"
    ? detectPlatformCapturePlatform(task.sourceInput)
    : null;
  const safeSource = task.captureStrategy === "authenticated"
    ? capturePlatform ?? "authenticated-platform"
    : task.sourceInput;

  const authenticated = task.captureStrategy === "authenticated";
  const lines: string[] = [
    `# ${t("imports.reportTitle", "导入任务诊断")}`,
    "",
    `- ${t("imports.reportStatus", "状态")}：${t(status.key, status.fallback)}`,
  ];
  if (!authenticated) {
    lines.push(`- ${t("imports.reportTask", "任务")}：${task.displayName || safeSource}`);
  }
  if (task.itemType) {
    lines.push(`- ${t("imports.reportType", "类型")}：${task.itemType}`);
  }
  lines.push(`- ${t("imports.reportSource", "来源")}：${safeSource}`);
  if (authenticated) {
    lines.push(`- ${t("imports.reportCaptureStrategy", "采集策略")}：authenticated`);
  }
  if (!authenticated) {
    lines.push(
      `- ${t("imports.reportQueuedAt", "入队")}：${formatTime(task.createdAt)}`,
      `- ${t("imports.reportUpdatedAt", "更新")}：${formatTime(task.updatedAt)}`,
      `- ${t("imports.reportTaskId", "任务 ID")}：${task.id}`,
    );
  }
  if (context.appVersion) {
    lines.push(
      `- ${t("imports.reportApp", "应用")}：GuiZhi ${context.appVersion}${
        context.platform ? ` · ${context.platform}` : ""
      }`,
    );
  }

  const stats = task.stageStats ?? [];
  if (stats.length > 0) {
    const totalMs = stats.reduce((total, entry) => total + entry.ms, 0);
    lines.push(
      "",
      `## ${t("imports.reportStages", "阶段耗时")}（${t(
        "imports.totalElapsed",
        "共 {{duration}}",
        { duration: formatDuration(totalMs) },
      )}）`,
      "",
      `| ${t("imports.reportStageColumn", "阶段")} | ${t(
        "imports.reportDurationColumn",
        "耗时",
      )} | ${t("imports.reportShareColumn", "占比")} | ${t(
        "imports.reportAiColumn",
        "AI 调用",
      )} |`,
      "| --- | ---: | ---: | --- |",
    );
    for (const stat of stats) {
      const label = STAGE_LABELS[stat.stage];
      const name = label ? t(label.key, label.fallback) : stat.stage;
      lines.push(
        `| ${escapeCell(name)} | ${formatDuration(stat.ms)} | ${shareOf(
          stat.ms,
          totalMs,
        )} | ${authenticated ? "" : escapeCell(aiDetailOf(stat, t))} |`,
      );
    }
  }

  if (task.error) {
    const safeError = authenticated
      ? (task.error.match(/^\[([a-z_]+)\]/)?.[1] ?? "unknown")
      : formatImportTaskErrorForReport(task.error, t);
    lines.push(
      "",
      `## ${t("imports.reportError", "报错")}`,
      "",
      safeError,
    );
  }
  if (task.warning) {
    lines.push(
      "",
      `## ${t("imports.reportWarning", "缺失提示")}`,
      "",
      authenticated
        ? t("imports.authenticatedWarningPresent", "认证采集存在非阻断 warning")
        : formatImportTaskWarning(task.warning, t),
    );
  }
  if (context.captureBrowser) {
    lines.push(`- ${t("imports.reportBrowser", "浏览器")}：${context.captureBrowser}`);
  }

  return `${lines.join("\n")}\n`;
}
