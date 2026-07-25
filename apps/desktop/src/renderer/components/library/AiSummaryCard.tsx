import { useEffect, useState } from "react";
import { Loader2Icon, RotateCcwIcon, SparklesIcon } from "lucide-react";
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

  // 切换条目时复位生成状态（避免上一条的 spinner 残留）
  useEffect(() => {
    setIsGenerating(false);
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
      const summary = await generateSummary(item.title, item.content);
      const updated = await window.api.knowledge.update(itemId, { summary });
      if (updated) {
        applyServerItem(updated);
      }
    } catch (error) {
      if (error instanceof AiNotConfiguredError) {
        showToast(
          t("ask.notConfigured", "尚未配置 AI 服务"),
          "error",
        );
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
      <button type="button" onClick={() => void generate()} className={ACTION_CHIP}>
        <SparklesIcon className="h-3.5 w-3.5" aria-hidden="true" />
        {t("library.aiSummaryGenerate", "生成 AI 摘要")}
      </button>
    );
  }

  // 与动作按钮同处一个 flex 行，摘要卡片占满整行另起一排
  return (
    <div className="w-full rounded-xl border border-primary/15 bg-primary/[0.04] px-4 py-3">
      <div className="mb-1.5 flex items-center gap-1.5">
        <SparklesIcon className="h-3.5 w-3.5 text-primary" aria-hidden="true" />
        <span className="text-xs font-medium text-primary">
          {t("library.aiSummary", "AI 摘要")}
        </span>
        <span className="min-w-0 flex-1" />
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
      {item.summary ? (
        <MarkdownBody content={item.summary} />
      ) : (
        <p className="text-xs text-muted-foreground">
          {t("library.aiSummaryGenerating", "正在生成摘要…")}
        </p>
      )}
    </div>
  );
}
