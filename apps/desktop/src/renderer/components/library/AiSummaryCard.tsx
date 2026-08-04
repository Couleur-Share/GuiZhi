import { useEffect, useState } from "react";
import {
  ChevronDownIcon,
  ChevronUpIcon,
  Loader2Icon,
  RotateCcwIcon,
  SparklesIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { useToast } from "../ui/Toast";
import { generateSummary } from "../../services/knowledge-ai/summarize";
import { AiNotConfiguredError } from "../../services/knowledge-ai/ai-invoke";
import { MarkdownBody } from "./MarkdownPreview";
import { ACTION_CHIP } from "./detail-chips";

/**
 * AI 摘要区块：有摘要时展示卡片（附重新生成），
 * 无摘要时提供一条淡入口；生成结果直接持久化到条目 summary 字段。
 */
export function AiSummaryCard({ item }: { item: KnowledgeItem }) {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const applyServerItem = useKnowledgeStore((state) => state.applyServerItem);
  const requestSettingsSection = useUIStore(
    (state) => state.requestSettingsSection,
  );
  const [isGenerating, setIsGenerating] = useState(false);
  // 摘要可能很长，默认不抢占正文首屏；用户主动生成后再展开以便确认结果。
  const [isExpanded, setIsExpanded] = useState(false);

  // 切换条目时复位临时状态，避免上一条的 spinner 或展开状态残留。
  useEffect(() => {
    setIsGenerating(false);
    setIsExpanded(false);
  }, [item.id]);

  const generate = async () => {
    if (isGenerating) {
      return;
    }
    if (!item.content.trim()) {
      showToast(t("library.aiSummaryEmpty", "内容为空，无法生成摘要"), "error");
      return;
    }
    setIsGenerating(true);
    const itemId = item.id;
    try {
      const { text: summary, truncated } = await generateSummary(
        item.title,
        item.content,
      );
      const updated = await window.api.knowledge.update(itemId, { summary });
      if (updated) {
        applyServerItem(updated);
        setIsExpanded(true);
      }
      // 截断的摘要照常写入（半篇也比没有强），但不能装作它是完整的
      if (truncated) {
        showToast(
          t(
            "library.aiSummaryTruncated",
            "AI 摘要已生成，但模型输出达到长度上限，内容可能不完整",
          ),
          "warning",
        );
      } else {
        showToast(t("library.aiSummaryDone", "AI 摘要已生成"), "success");
      }
    } catch (error) {
      if (error instanceof AiNotConfiguredError) {
        showToast(t("ask.notConfigured", "尚未配置 AI 服务"), "error");
        requestSettingsSection("ai");
      } else {
        showToast(
          t("library.aiSummaryFailed", "摘要生成失败：{{message}}", {
            message: error instanceof Error ? error.message : String(error),
          }),
          "error",
        );
      }
    } finally {
      setIsGenerating(false);
    }
  };

  if (!item.summary && !isGenerating) {
    return (
      <button
        type="button"
        onClick={() => void generate()}
        className={ACTION_CHIP}
      >
        <SparklesIcon className="h-3.5 w-3.5" aria-hidden="true" />
        {t("library.aiSummaryGenerate", "生成 AI 摘要")}
      </button>
    );
  }

  // 与动作按钮同处一个 flex 行，摘要卡片占满整行另起一排
  return (
    <div className="w-full rounded-xl border border-primary/15 bg-primary/[0.04] px-4 py-3">
      <div className="flex items-center gap-1.5">
        <button
          type="button"
          onClick={() => setIsExpanded((expanded) => !expanded)}
          aria-expanded={isExpanded}
          aria-label={
            isExpanded
              ? t("library.aiSummaryCollapse", "收起 AI 摘要")
              : t("library.aiSummaryExpand", "展开 AI 摘要")
          }
          className="-ml-1 flex min-w-0 flex-1 items-center gap-1.5 rounded-md px-1 py-0.5 text-left transition-colors hover:bg-primary/[0.06]"
        >
          <SparklesIcon
            className="h-3.5 w-3.5 shrink-0 text-primary"
            aria-hidden="true"
          />
          <span className="text-xs font-medium text-primary">
            {t("library.aiSummary", "AI 摘要")}
          </span>
          {isExpanded ? (
            <ChevronUpIcon
              className="h-3.5 w-3.5 shrink-0 text-muted-foreground"
              aria-hidden="true"
            />
          ) : (
            <ChevronDownIcon
              className="h-3.5 w-3.5 shrink-0 text-muted-foreground"
              aria-hidden="true"
            />
          )}
        </button>
        {isGenerating ? (
          <Loader2Icon
            className="h-3.5 w-3.5 animate-spin text-muted-foreground"
            aria-hidden="true"
          />
        ) : (
          <button
            type="button"
            onClick={() => void generate()}
            title={t("library.aiSummaryRegenerate", "重新生成")}
            aria-label={t("library.aiSummaryRegenerate", "重新生成")}
            className="text-muted-foreground/60 transition-colors hover:text-primary"
          >
            <RotateCcwIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </button>
        )}
      </div>
      {item.summary && isExpanded ? (
        <div className="mt-1.5">
          <MarkdownBody content={item.summary} />
        </div>
      ) : isGenerating ? (
        <p className="mt-1.5 text-xs text-muted-foreground">
          {t("library.aiSummaryGenerating", "正在生成摘要…")}
        </p>
      ) : null}
    </div>
  );
}
