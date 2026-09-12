import { useState } from "react";
import { AlertTriangleIcon, CheckIcon, Loader2Icon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useToast } from "../ui/Toast";

/**
 * 导入管线已经拿到可用内容、但某一步有缺口时的详情页提醒。
 *
 * 它不拦住阅读或问答：本地知识库的正确降级是先保存可用部分，再把风险
 * 显式留给用户。确认后仅清掉复核标记，不会悄悄改写正文或删除来源资料。
 */
export function ReviewRequiredNotice({
  item,
  disabled,
}: {
  item: KnowledgeItem;
  disabled: boolean;
}) {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const applyServerItem = useKnowledgeStore((state) => state.applyServerItem);
  const [isConfirming, setIsConfirming] = useState(false);

  if (item.reviewStatus !== "needs_review") {
    return null;
  }

  const reasons = item.reviewReasons?.filter(Boolean) ?? [];
  const confirmReviewed = async () => {
    setIsConfirming(true);
    try {
      const updated = await window.api.knowledge.update(item.id, {
        reviewStatus: "clear",
        reviewReasons: [],
      });
      if (!updated) {
        throw new Error(t("library.reviewMissingItem", "条目已不存在"));
      }
      applyServerItem(updated);
      showToast(t("library.reviewCleared", "已标记为复核完成"), "success");
    } catch (error) {
      showToast(t("library.reviewClearFailed", "更新复核状态失败"), "error", {
        detail: error instanceof Error ? error.message : String(error),
      });
    } finally {
      setIsConfirming(false);
    }
  };

  return (
    <section
      data-testid="review-required-notice"
      className="shrink-0 border-b border-border/50 bg-muted/25 px-6 py-2.5 text-sm"
      aria-label={t("library.reviewRequired", "需要人工复核")}
    >
      <details>
        <summary className="flex cursor-pointer list-none flex-wrap items-center gap-2 rounded-sm text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          <AlertTriangleIcon
            className="h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400"
            aria-hidden="true"
          />
          <span className="font-medium">
            {t("articleReader.importIncomplete", "采集内容有待核对")}
          </span>
          <span className="text-muted-foreground">
            {t("articleReader.importReadable", "已保留可读内容")}
          </span>
          <span className="ml-auto text-muted-foreground">
            {t("articleReader.reviewDetails", "查看缺口与复核")}
          </span>
        </summary>
        <div className="max-h-56 space-y-3 overflow-y-auto pb-1 pl-6 pt-3">
          <p className="text-xs leading-5 text-muted-foreground">
            {t(
              "library.reviewRequiredHint",
              "已保留可用内容，但导入过程发现以下缺口；确认无误后可标记为已复核。",
            )}
          </p>
          {reasons.length > 0 ? (
            <ul className="list-disc space-y-1 pl-4 text-xs leading-relaxed text-muted-foreground">
              {reasons.map((reason) => (
                <li className="select-text break-words" key={reason}>
                  {reason}
                </li>
              ))}
            </ul>
          ) : null}
          {!disabled ? (
            <button
              type="button"
              disabled={isConfirming}
              onClick={() => void confirmReviewed()}
              className="inline-flex min-h-8 items-center gap-1.5 rounded-lg border border-border bg-background px-3 text-xs font-medium hover:bg-accent disabled:opacity-60"
            >
              {isConfirming ? (
                <Loader2Icon
                  className="h-3.5 w-3.5 animate-spin"
                  aria-hidden="true"
                />
              ) : (
                <CheckIcon className="h-3.5 w-3.5" aria-hidden="true" />
              )}
              {t("library.markReviewed", "标记已复核")}
            </button>
          ) : null}
        </div>
      </details>
    </section>
  );
}
