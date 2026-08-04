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
      showToast(
        t("library.reviewClearFailed", "更新复核状态失败"),
        "error",
        { detail: error instanceof Error ? error.message : String(error) },
      );
    } finally {
      setIsConfirming(false);
    }
  };

  return (
    <section
      data-testid="review-required-notice"
      className="mx-3 mt-3 shrink-0 rounded-lg border border-amber-500/35 bg-amber-500/10 px-3 py-2.5 text-sm text-foreground"
      aria-label={t("library.reviewRequired", "需要人工复核")}
    >
      <div className="flex items-start gap-2">
        <AlertTriangleIcon
          className="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400"
          aria-hidden="true"
        />
        <div className="min-w-0 flex-1">
          <p className="font-medium">
            {t("library.reviewRequired", "需要人工复核")}
          </p>
          <p className="mt-0.5 text-xs leading-5 text-muted-foreground">
            {t(
              "library.reviewRequiredHint",
              "已保留可用内容，但导入过程发现以下缺口；确认无误后可标记为已复核。",
            )}
          </p>
          {reasons.length > 0 ? (
            <ul className="mt-1.5 list-disc space-y-0.5 pl-4 text-xs leading-5 text-muted-foreground">
              {reasons.map((reason) => (
                <li key={reason}>{reason}</li>
              ))}
            </ul>
          ) : null}
        </div>
        {!disabled ? (
          <button
            type="button"
            disabled={isConfirming}
            onClick={() => void confirmReviewed()}
            className="inline-flex h-7 shrink-0 items-center gap-1 rounded-md border border-amber-500/35 bg-background/50 px-2 text-xs font-medium text-foreground transition-colors hover:bg-background disabled:cursor-not-allowed disabled:opacity-60"
          >
            {isConfirming ? (
              <Loader2Icon className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />
            ) : (
              <CheckIcon className="h-3.5 w-3.5" aria-hidden="true" />
            )}
            {t("library.markReviewed", "标记已复核")}
          </button>
        ) : null}
      </div>
    </section>
  );
}
