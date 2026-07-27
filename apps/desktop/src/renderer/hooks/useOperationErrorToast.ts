import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useToast } from "../components/ui/Toast";
import { useOperationErrorStore } from "../stores/operation-error.store";

/**
 * 把 store 投出来的变更失败渲染成 toast。挂在应用根部一次即可。
 *
 * 概要是「批量更新失败」这类一句话，原始报错折叠在「查看详情」里——
 * SQLite 的 SQLITE_BUSY、磁盘满、约束冲突彼此的处置完全不同，
 * 只说「失败」用户没法判断下一步该做什么。
 */
export function useOperationErrorToast(): void {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const latest = useOperationErrorStore((state) => state.latest);
  const clear = useOperationErrorStore((state) => state.clear);

  useEffect(() => {
    if (!latest) {
      return;
    }
    showToast(
      t("common.operationFailed", "{{action}}失败", {
        action: t(latest.actionKey, latest.actionFallback),
      }),
      "error",
      { detail: latest.message },
    );
    clear();
  }, [latest, showToast, clear, t]);
}
