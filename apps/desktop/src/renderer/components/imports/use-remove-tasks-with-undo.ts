import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { useToast } from "../ui/Toast";
import { useImportStore } from "../../stores/import.store";

/**
 * 从导入列表删除任务：先隐藏，8 秒内可在提示条上撤销，之后才真正提交。
 *
 * 此前这个动作不确认也不能撤销，下一行滑到指针下面时连点两下就会误删。
 */
export function useRemoveTasksWithUndo() {
  const { t } = useTranslation();
  const { showUndoToast } = useToast();
  const scheduleRemoveTasks = useImportStore((state) => state.scheduleRemoveTasks);
  return useCallback(
    (ids: string[]) => {
      const removal = scheduleRemoveTasks(ids);
      if (!removal) return;
      showUndoToast(
        t("imports.removedFromList", "已从列表删除（{{count}} 项）", {
          count: removal.count,
        }),
        removal.undo,
      );
    },
    [scheduleRemoveTasks, showUndoToast, t],
  );
}
