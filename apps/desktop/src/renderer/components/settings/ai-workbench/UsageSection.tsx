import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AIUsageScenarioId, AIUsageSummary } from "@guizhi/shared/types";
import { useToast } from "../../ui/Toast";

const USAGE_WINDOW_DAYS = 30;

type UsageGroup = "scenario" | "model";

/** 用 AIUsageScenarioId 做键：新增场景时这里漏配会直接编译不过 */
const SCENARIO_LABELS: Record<
  AIUsageScenarioId,
  { key: string; fallback: string }
> = {
  qa: { key: "settings.aiScenarioQa", fallback: "问答" },
  wiki: { key: "settings.aiScenarioWiki", fallback: "Wiki 编译" },
  summary: { key: "settings.aiScenarioSummary", fallback: "摘要" },
  tagging: { key: "settings.aiScenarioTagging", fallback: "标签建议" },
  ocr: { key: "settings.aiScenarioOcr", fallback: "图片识别" },
  transcription: {
    key: "settings.aiScenarioTranscription",
    fallback: "语音转写",
  },
  formatting: {
    key: "settings.aiScenarioFormatting",
    fallback: "文字稿排版",
  },
  embedding: { key: "settings.aiScenarioEmbedding", fallback: "语义索引" },
  illustration: {
    key: "settings.aiScenarioIllustration",
    fallback: "正文配图",
  },
};

function formatCount(value: number): string {
  if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(1)}M`;
  }
  if (value >= 1_000) {
    return `${(value / 1_000).toFixed(1)}k`;
  }
  return String(value);
}

/**
 * AI 用量：按场景或模型汇总最近 30 天的调用次数与 token。
 *
 * 一次提问最坏会触发十来次模型调用，Wiki 后台编译也在持续消耗，
 * 此前界面上没有任何地方能看出用了多少。按场景回答「哪类功能在烧」，
 * 按模型回答「换路由该看哪一个」——库里本来就按两者一起记，只是
 * 汇总时只滚了场景这一维。
 */
export function UsageSection() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const [summary, setSummary] = useState<AIUsageSummary | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [group, setGroup] = useState<UsageGroup>("scenario");

  const load = useCallback(
    async (interactive = false) => {
      if (interactive) setRefreshing(true);
      try {
        setSummary(await window.api.ai.usageSummary(USAGE_WINDOW_DAYS));
      } catch (error) {
        console.warn("加载 AI 用量失败:", error);
        // 用户点了刷新却毫无反馈，会当成按钮坏了；自动加载失败也 toast，
        // 否则会伪装成「还没有调用记录」。
        showToast(t("settings.aiUsageLoadFailed", "加载用量失败"), "error", {
          detail: error instanceof Error ? error.message : String(error),
        });
      } finally {
        if (interactive) setRefreshing(false);
      }
    },
    [showToast, t],
  );

  useEffect(() => {
    void load();
  }, [load]);

  if (!summary || summary.calls === 0) {
    return (
      <div className="rounded-xl border border-border p-4 text-xs text-muted-foreground">
        {t(
          "settings.aiUsageEmpty",
          "最近 {{days}} 天还没有 AI 调用记录",
          { days: USAGE_WINDOW_DAYS },
        )}
      </div>
    );
  }

  const totalTokens = summary.promptTokens + summary.completionTokens;
  // 失败次数故意不展示：用量面板只回答「烧了多少」，失败原因在 error.log；
  // 重试与限流会把 failed_calls 堆高，挂在行上只会吓人却推不出下一步。
  const rows =
    group === "scenario"
      ? summary.byScenario.map((row) => {
          const label = SCENARIO_LABELS[row.scenario as AIUsageScenarioId] as
            | { key: string; fallback: string }
            | undefined;
          return {
            key: row.scenario,
            label: label ? t(label.key, label.fallback) : row.scenario,
            calls: row.calls,
            tokens: row.promptTokens + row.completionTokens,
          };
        })
      : summary.byModel.map((row) => ({
          key: row.model,
          label: row.model,
          calls: row.calls,
          tokens: row.promptTokens + row.completionTokens,
        }));

  const groupButtonClass = (active: boolean) =>
    `rounded-md px-2 py-0.5 text-[11px] transition-colors ${
      active
        ? "bg-accent text-foreground"
        : "text-muted-foreground hover:text-foreground"
    }`;

  return (
    <div className="space-y-3 rounded-xl border border-border p-4">
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1">
        <span className="text-sm font-medium text-foreground">
          {t("settings.aiUsageTitle", "最近 {{days}} 天用量", {
            days: USAGE_WINDOW_DAYS,
          })}
        </span>
        <span className="text-xs text-muted-foreground">
          {t("settings.aiUsageTotal", "{{calls}} 次调用 · {{tokens}} tokens", {
            calls: summary.calls,
            tokens: formatCount(totalTokens),
          })}
        </span>
        <span className="min-w-0 flex-1" />
        <div className="flex shrink-0 items-center rounded-lg border border-border p-0.5">
          <button
            type="button"
            onClick={() => setGroup("scenario")}
            aria-pressed={group === "scenario"}
            className={groupButtonClass(group === "scenario")}
          >
            {t("settings.aiUsageGroupScenario", "按场景")}
          </button>
          <button
            type="button"
            onClick={() => setGroup("model")}
            aria-pressed={group === "model"}
            className={groupButtonClass(group === "model")}
          >
            {t("settings.aiUsageGroupModel", "按模型")}
          </button>
        </div>
        <button
          type="button"
          onClick={() => void load(true)}
          disabled={refreshing}
          aria-busy={refreshing}
          className="text-xs text-muted-foreground underline-offset-2 transition-colors hover:text-foreground hover:underline disabled:pointer-events-none disabled:opacity-50"
        >
          {refreshing
            ? t("common.refreshing", "刷新中")
            : t("common.refresh", "刷新")}
        </button>
      </div>

      {totalTokens === 0 ? (
        <p className="text-[11px] text-muted-foreground/80">
          {t(
            "settings.aiUsageNoTokens",
            "当前接口未回报 token 用量，只统计调用次数",
          )}
        </p>
      ) : null}

      <div className="space-y-1.5">
        {rows.map((row) => {
          const share =
            summary.calls > 0 ? (row.calls / summary.calls) * 100 : 0;
          return (
            <div key={row.key} className="flex items-center gap-3 text-xs">
              <span
                className={`shrink-0 truncate text-foreground ${
                  group === "model" ? "w-36" : "w-20"
                }`}
              >
                {row.label}
              </span>
              <div className="h-1.5 min-w-0 flex-1 overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full rounded-full bg-primary/70"
                  style={{ width: `${share}%` }}
                />
              </div>
              <span className="w-28 shrink-0 text-right text-muted-foreground">
                {t("settings.aiUsageRow", "{{calls}} 次 · {{tokens}}", {
                  calls: row.calls,
                  tokens: formatCount(row.tokens),
                })}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
