/**
 * 阶段耗时的可视化：一条堆叠时间条 + 一份图例。
 *
 * 主体是条而不是一列数字：阶段是顺序的、各段之和恰好等于总耗时，所以堆叠条
 * 不只是图表，它就是这条任务的时间线，段宽即占比。九行数字要靠脑子逐个比
 * 大小才知道谁最贵，而「时间花在哪」本该是一眼的事。
 *
 * 刻意不判「快慢」：什么叫慢取决于模型、网络与内容长度，写死阈值在慢渠道上
 * 会天天误报（与「不给第 N 步 / 共 M 步」同一条理由）。
 */
import { useState } from "react";
import { useTranslation } from "react-i18next";
import type { ImportStage, ImportStageStat } from "@guizhi/shared/types";
import { formatDuration, getStageLabel } from "./import-task-meta";

/**
 * 段的着色。相邻两段交替深浅即可区分，不需要九种不同的颜色——条与图例都按
 * 时间顺序排，对应关系靠位置而不是靠颜色。颜色只承载一个信息：哪段最慢。
 * 走 primary 的透明度档位而不是硬编码色值，换主题时跟着走；琥珀色与
 * 「完成（有缺失）」徽标同一套语言。
 */
const SEGMENT_TONES = ["bg-primary/75", "bg-primary/40"];
const SLOWEST_TONE = "bg-amber-500/80";

/** 四舍五入显示成 0:00 的阶段：单独占一行是纯噪音，但完全不提会让人以为漏了阶段 */
const NEGLIGIBLE_MS = 1_000;

function toneOf(index: number, isSlowest: boolean): string {
  return isSlowest ? SLOWEST_TONE : SEGMENT_TONES[index % SEGMENT_TONES.length];
}

/** 总耗时与最慢的那个阶段 */
export function summarizeStages(stats: ImportStageStat[]): {
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

/**
 * 占比文案。四舍五入到 0 但确实花了时间的阶段写「<1%」而不是「0%」——
 * 后者与「一点没花」撞脸，而它旁边那一列明明写着 0:01。
 */
function formatShare(ms: number, totalMs: number): string {
  const share = totalMs > 0 ? (ms / totalMs) * 100 : 0;
  const rounded = Math.round(share);
  return rounded === 0 && ms > 0 ? "<1%" : `${rounded}%`;
}

/**
 * 堆叠时间条。亚秒阶段照样进条里（它们的宽度本就看不见），只是不进图例——
 * 把它们从条里摘掉会让各段之和不再等于总耗时，为了一个看不见的差别说谎不划算。
 *
 * 悬停时把其余段压暗而不是给当前段描边：段高只有 10px，描边挤不下，
 * 而「其余变淡」在任意窄的段上都看得出来。
 */
function StageTimeline({
  stats,
  totalMs,
  slowest,
  hovered,
  onHover,
}: {
  stats: ImportStageStat[];
  totalMs: number;
  slowest: ImportStageStat;
  hovered: ImportStage | null;
  onHover: (stage: ImportStage) => void;
}) {
  return (
    <div className="flex h-2.5 w-full overflow-hidden rounded-full bg-border/60">
      {stats.map((stat, index) => (
        <div
          key={stat.stage}
          onMouseEnter={() => onHover(stat.stage)}
          className={`${toneOf(index, stat === slowest)} transition-opacity duration-quick ${
            hovered && hovered !== stat.stage ? "opacity-30" : ""
          }`}
          style={{ width: `${(stat.ms / totalMs) * 100}%` }}
        />
      ))}
    </div>
  );
}

/**
 * 一行一个阶段，但**整块图例是同一个 grid**，逐行只吐单元格。
 *
 * 每行各自成 grid 的话，名称列的 `auto` 会按各自内容取宽，行与行之间就对不齐；
 * 而给死宽度在两种语言下必有一边难看——中文阶段名四到六个字，英文最长的
 * 「Extracting text from images」有 27 个字符，固定宽度要么截断英文、要么让
 * 中文那列空出一大片，眼睛得横跨半个面板才够到数字。共享一个 grid 之后
 * 名称列按最宽的那行自适应，两种语言都紧凑，数字列照样逐行对齐。
 */
function StageLegendCells({
  stat,
  index,
  totalMs,
  isSlowest,
  isHovered,
  onHover,
}: {
  stat: ImportStageStat;
  index: number;
  totalMs: number;
  isSlowest: boolean;
  isHovered: boolean;
  onHover: (stage: ImportStage) => void;
}) {
  const { t } = useTranslation();
  const label = getStageLabel(stat.stage);
  const tokens = (stat.promptTokens ?? 0) + (stat.completionTokens ?? 0);
  // 逐格挂 enter、由容器统一清空：单元格之间有 gap，靠各格的 leave 清的话
  // 横向划过一行就会在缝隙处闪烁
  const hover = { onMouseEnter: () => onHover(stat.stage) };

  return (
    <>
      <span
        {...hover}
        className={`h-2 w-2 rounded-sm ${toneOf(index, isSlowest)}`}
        aria-hidden="true"
      />
      <span
        {...hover}
        className={`truncate ${isHovered ? "text-foreground" : "text-muted-foreground"}`}
      >
        {t(label.key, label.fallback)}
      </span>
      <span {...hover} className="text-right tabular-nums text-foreground">
        {formatDuration(stat.ms)}
      </span>
      <span
        {...hover}
        className={`text-right tabular-nums ${isHovered ? "text-foreground" : "text-muted-foreground"}`}
      >
        {formatShare(stat.ms, totalMs)}
      </span>
      <span {...hover} className="truncate text-muted-foreground">
        {stat.calls
          ? [
              t("imports.stageCalls", "{{calls}} 次调用", {
                calls: stat.calls,
              }),
              stat.failedCalls
                ? t("imports.stageFailedCalls", "{{count}} 次失败", {
                    count: stat.failedCalls,
                  })
                : "",
              tokens > 0
                ? t("imports.stageTokens", "{{tokens}} token", { tokens })
                : "",
              stat.models?.length ? stat.models.join("、") : "",
            ]
              .filter(Boolean)
              .join(" · ")
          : ""}
      </span>
    </>
  );
}

export function ImportStageBreakdown({ stats }: { stats: ImportStageStat[] }) {
  const { t } = useTranslation();
  const [showAll, setShowAll] = useState(false);
  const [hovered, setHovered] = useState<ImportStage | null>(null);
  const { totalMs, slowest } = summarizeStages(stats);
  const active = hovered
    ? (stats.find((stat) => stat.stage === hovered) ?? null)
    : null;
  const activeLabel = active ? getStageLabel(active.stage) : null;

  // 全部阶段都不足一秒时（纯文本笔记）照原样列出：此时「另有 N 个」的「另」
  // 指不到任何东西，一行不剩更像是渲染坏了
  const notable = stats.filter((stat) => stat.ms >= NEGLIGIBLE_MS);
  const legend = showAll || notable.length === 0 ? stats : notable;
  const negligibleCount = stats.length - legend.length;

  return (
    <div className="text-[11px]" onMouseLeave={() => setHovered(null)}>
      {totalMs > 0 ? (
        <div className="mb-2">
          {/* 常驻一行读数，悬停时就地换成那一段——空着的话悬停要么撑出一行
              （界面跳动）、要么只能走 title 气泡（有 320ms 延迟，在条上划过时很钝） */}
          <div className="mb-1.5 flex items-baseline justify-between gap-2">
            <span className={active ? "text-foreground" : "text-muted-foreground"}>
              {active && activeLabel
                ? t(activeLabel.key, activeLabel.fallback)
                : t("imports.totalElapsed", "共 {{duration}}", {
                    duration: formatDuration(totalMs),
                  })}
            </span>
            {active ? (
              <span className="tabular-nums text-muted-foreground">
                {`${formatDuration(active.ms)} · ${formatShare(active.ms, totalMs)}`}
              </span>
            ) : null}
          </div>
          <StageTimeline
            stats={stats}
            totalMs={totalMs}
            slowest={slowest}
            hovered={hovered}
            onHover={setHovered}
          />
        </div>
      ) : null}
      <div className="grid grid-cols-[0.5rem_auto_3rem_2.5rem_minmax(0,1fr)] items-center gap-x-2 gap-y-1">
        {legend.map((stat) => (
          <StageLegendCells
            key={stat.stage}
            stat={stat}
            index={stats.indexOf(stat)}
            totalMs={totalMs}
            isSlowest={stat === slowest}
            isHovered={hovered === stat.stage}
            onHover={setHovered}
          />
        ))}
        {negligibleCount > 0 ? (
          <button
            type="button"
            onClick={() => setShowAll(true)}
            className="col-span-5 pl-4 text-left text-muted-foreground/70 transition-colors hover:text-muted-foreground"
          >
            {t("imports.stageNegligible", "另有 {{count}} 个阶段不足 1 秒", {
              count: negligibleCount,
            })}
          </button>
        ) : null}
      </div>
    </div>
  );
}
