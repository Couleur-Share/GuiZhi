/**
 * 行上常驻的耗时摘要：「共 X · 最慢 Y」。
 *
 * 此前任务一结束，`ProgressHint` 就整个 return null——耗时信息全部消失，连总
 * 时长都得用户拿两个时间戳的悬停气泡去减。于是「排版慢了八分钟」这种异常只能
 * 靠翻数据库发现，而用户的原话是「我在使用时没发现」。
 *
 * 行上只放这一句：一屏几十条任务，扫过去就能看出哪条不对劲，这才叫发现。
 * 完整明细在详情弹窗里——那是确认细节用的，不是筛查用的。
 */
import { useTranslation } from "react-i18next";
import type { ImportTask } from "@guizhi/shared/types";
import { formatDuration, getStageLabel } from "./import-task-meta";
import { summarizeStages } from "./ImportStageBreakdown";

export function ImportStageSummary({
  task,
  onOpenDetail,
}: {
  task: ImportTask;
  onOpenDetail: () => void;
}) {
  const { t } = useTranslation();
  const stats = task.stageStats;

  // 运行中的任务由 ProgressHint 负责：那时要回答的是「还活着吗」，
  // 不是「花在哪了」
  if (task.status === "pending" || task.status === "processing") {
    return null;
  }
  if (!stats?.length) {
    return null;
  }

  const { totalMs, slowest } = summarizeStages(stats);
  const slowestLabel = getStageLabel(slowest.stage);

  return (
    <button
      type="button"
      onClick={onOpenDetail}
      className="inline-flex items-center gap-1 rounded text-[11px] text-muted-foreground transition-colors hover:text-foreground hover:underline"
    >
      <span className="tabular-nums">
        {t("imports.totalElapsed", "共 {{duration}}", {
          duration: formatDuration(totalMs),
        })}
      </span>
      {/* 只有一个阶段时「最慢」等于「全部」，说了等于没说 */}
      {stats.length > 1 ? (
        <span className="tabular-nums">
          {`· ${t("imports.slowestStage", "最慢 {{stage}} {{duration}}", {
            stage: t(slowestLabel.key, slowestLabel.fallback),
            duration: formatDuration(slowest.ms),
          })}`}
        </span>
      ) : null}
    </button>
  );
}
