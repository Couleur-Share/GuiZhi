/**
 * 终态任务的阶段耗时。
 *
 * 此前任务一结束，`ProgressHint` 就整个 return null——耗时信息全部消失，
 * 连总时长都得用户拿两个时间戳的悬停气泡去减。于是「排版慢了八分钟」这种
 * 异常只能靠翻数据库发现，而用户的原话是「我在使用时没发现」。
 *
 * 行上常驻的只有一句「共 X · 最慢 Y」：一屏几十条任务，扫过去就能看出哪条
 * 不对劲，这才叫发现。完整明细收在点开之后——那是确认细节用的，不是筛查用的。
 * 刻意不判「快慢」：什么叫慢取决于模型、网络与内容长度，写死阈值在慢渠道上
 * 会天天误报（与「不给第 N 步 / 共 M 步」同一条理由）。
 */
import { useState } from "react";
import { ChevronDownIcon, ChevronRightIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { ImportStageStat, ImportTask } from "@guizhi/shared/types";
import { formatDuration, getStageLabel } from "./import-task-meta";

/** 总耗时与最慢的那个阶段——行上只放得下这两样 */
function summarize(stats: ImportStageStat[]): {
  totalMs: number;
  slowest: ImportStageStat;
} {
  let totalMs = 0;
  let slowest = stats[0];
  for (const entry of stats) {
    totalMs += entry.ms;
    if (entry.ms > slowest.ms) {
      slowest = entry;
    }
  }
  return { totalMs, slowest };
}

function StageDetailRow({ stat }: { stat: ImportStageStat }) {
  const { t } = useTranslation();
  const label = getStageLabel(stat.stage);
  const tokens = (stat.promptTokens ?? 0) + (stat.completionTokens ?? 0);

  return (
    <div className="flex items-baseline gap-2 py-0.5">
      <span className="w-24 shrink-0 truncate text-muted-foreground">
        {t(label.key, label.fallback)}
      </span>
      <span className="w-14 shrink-0 tabular-nums text-foreground">
        {formatDuration(stat.ms)}
      </span>
      {stat.calls ? (
        <span className="min-w-0 truncate text-muted-foreground">
          {t("imports.stageCalls", "{{calls}} 次调用", { calls: stat.calls })}
          {stat.failedCalls
            ? ` · ${t("imports.stageFailedCalls", "{{count}} 次失败", {
                count: stat.failedCalls,
              })}`
            : ""}
          {tokens > 0
            ? ` · ${t("imports.stageTokens", "{{tokens}} token", { tokens })}`
            : ""}
          {stat.models?.length ? ` · ${stat.models.join("、")}` : ""}
        </span>
      ) : null}
    </div>
  );
}

/**
 * 已结束任务的阶段耗时。运行中的任务由 `ProgressHint` 负责，
 * 两者不同时出现——运行中要的是「还活着吗」，结束后要的是「花在哪了」。
 */
export function ImportStageStats({ task }: { task: ImportTask }) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(false);
  const stats = task.stageStats;

  if (task.status === "pending" || task.status === "processing") {
    return null;
  }
  if (!stats?.length) {
    return null;
  }

  const { totalMs, slowest } = summarize(stats);
  const slowestLabel = getStageLabel(slowest.stage);
  const Chevron = expanded ? ChevronDownIcon : ChevronRightIcon;

  return (
    <>
      <button
        type="button"
        onClick={() => setExpanded((value) => !value)}
        className="inline-flex items-center gap-0.5 rounded text-[11px] text-muted-foreground transition-colors hover:text-foreground"
      >
        <Chevron className="h-3 w-3" aria-hidden="true" />
        <span className="tabular-nums">
          {t("imports.totalElapsed", "共 {{duration}}", {
            duration: formatDuration(totalMs),
          })}
        </span>
        {/* 只有一个阶段时「最慢」等于「全部」，说了等于没说 */}
        {stats.length > 1 ? (
          <span className="tabular-nums">
            {` · ${t("imports.slowestStage", "最慢 {{stage}} {{duration}}", {
              stage: t(slowestLabel.key, slowestLabel.fallback),
              duration: formatDuration(slowest.ms),
            })}`}
          </span>
        ) : null}
      </button>
      {expanded ? (
        <div className="mt-1 w-full rounded-lg bg-accent/30 px-2 py-1.5 text-[11px]">
          {stats.map((stat) => (
            <StageDetailRow key={stat.stage} stat={stat} />
          ))}
        </div>
      ) : null}
    </>
  );
}
