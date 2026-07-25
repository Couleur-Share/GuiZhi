import { useCallback, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AIUsageSummary } from "@guizhi/shared/types";
import type { AIUsageScenario } from "../../../stores/settings.store";

const USAGE_WINDOW_DAYS = 30;

/** 用 AIUsageScenario 做键：新增场景时这里漏配会直接编译不过 */
const SCENARIO_LABELS: Record<
  AIUsageScenario,
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
 * AI 用量：按场景汇总最近 30 天的调用次数与 token。
 *
 * 一次提问最坏会触发十来次模型调用，Wiki 后台编译也在持续消耗，
 * 此前界面上没有任何地方能看出用了多少。
 */
export function UsageSection() {
  const { t } = useTranslation();
  const [summary, setSummary] = useState<AIUsageSummary | null>(null);

  const load = useCallback(async () => {
    try {
      setSummary(await window.api.ai.usageSummary(USAGE_WINDOW_DAYS));
    } catch (error) {
      console.warn("加载 AI 用量失败:", error);
    }
  }, []);

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
        <button
          type="button"
          onClick={() => void load()}
          className="text-xs text-muted-foreground underline-offset-2 transition-colors hover:text-foreground hover:underline"
        >
          {t("common.refresh", "刷新")}
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
        {summary.byScenario.map((row) => {
          const label = SCENARIO_LABELS[row.scenario as AIUsageScenario] as
            | { key: string; fallback: string }
            | undefined;
          const share =
            summary.calls > 0 ? (row.calls / summary.calls) * 100 : 0;
          return (
            <div key={row.scenario} className="flex items-center gap-3 text-xs">
              <span className="w-20 shrink-0 truncate text-foreground">
                {label ? t(label.key, label.fallback) : row.scenario}
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
                  tokens: formatCount(
                    row.promptTokens + row.completionTokens,
                  ),
                })}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}
