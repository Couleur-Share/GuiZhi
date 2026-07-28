import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { buildAiHandoff } from "@guizhi/shared/utils/ai-handoff";
import type { AiHandoffResult } from "@guizhi/shared/utils/ai-handoff";
import type { KnowledgeItem } from "@guizhi/shared/types";
import { useCollectionStore } from "../../stores/collection.store";
import { useToast } from "../ui/Toast";
import { copyTextToClipboard } from "../../utils/clipboard";

/** 千分位：交接稿动辄几千上万字，`8432` 一眼读不出量级 */
function withThousands(value: number): string {
  return String(value).replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

/**
 * 「复制/导出给 AI」的共用逻辑：详情页按钮与列表右键菜单两个入口。
 *
 * 右键菜单拿到的是列表投影（没有 content / transcript），所以一律按 id
 * 重新取一次完整条目，两个入口共用同一条路径。
 */
export function useAiHandoff() {
  const { t } = useTranslation();
  const { showToast } = useToast();

  const build = useCallback(
    async (
      itemId: string,
      includeFullText: boolean,
    ): Promise<{ item: KnowledgeItem; doc: AiHandoffResult } | null> => {
      const item = await window.api.knowledge.get(itemId);
      if (!item) {
        showToast(t("library.aiHandoffMissing", "条目不存在"), "error");
        return null;
      }
      if (!item.content.trim() && !item.transcript?.trim()) {
        showToast(
          t("library.aiHandoffEmpty", "条目内容为空，没有可交给 AI 的素材"),
          "error",
        );
        return null;
      }

      const collectionName = item.collectionId
        ? (useCollectionStore
            .getState()
            .collections.find(
              (collection) => collection.id === item.collectionId,
            )?.name ?? null)
        : null;

      return {
        item,
        doc: buildAiHandoff({ ...item, collectionName }, { includeFullText }),
      };
    },
    [showToast, t],
  );

  const copyToClipboard = useCallback(
    async (itemId: string, includeFullText: boolean) => {
      try {
        const built = await build(itemId, includeFullText);
        if (!built) {
          return;
        }
        await copyTextToClipboard(built.doc.text);
        showToast(
          built.doc.omittedChars > 0
            ? t(
                "library.aiHandoffCopiedBrief",
                "已复制精简版 {{size}} 字（略去 {{omitted}} 字长文本）",
                {
                  size: withThousands(built.doc.charCount),
                  omitted: withThousands(built.doc.omittedChars),
                },
              )
            : t(
                "library.aiHandoffCopied",
                "已复制 {{size}} 字，可直接粘贴给 AI",
                { size: withThousands(built.doc.charCount) },
              ),
          "success",
        );
      } catch (error) {
        showToast(t("library.aiHandoffFailed", "复制失败"), "error", {
          detail: error instanceof Error ? error.message : String(error),
        });
      }
    },
    [build, showToast, t],
  );

  /** 另存为始终给完整版：存成文件就是为了让 AI 反复引用，没有省字数的理由 */
  const saveToFile = useCallback(
    async (itemId: string) => {
      try {
        const built = await build(itemId, true);
        if (!built) {
          return;
        }
        const result = await window.api.backup.exportAiHandoff({
          title: built.item.title,
          text: built.doc.text,
        });
        if (result.canceled) {
          return;
        }
        if (result.success) {
          showToast(
            t("library.aiHandoffSaved", "已保存到 {{path}}", {
              path: result.filePath,
            }),
            "success",
          );
        } else {
          showToast(t("library.aiHandoffSaveFailed", "保存失败"), "error", {
            detail: result.error,
          });
        }
      } catch (error) {
        showToast(t("library.aiHandoffSaveFailed", "保存失败"), "error", {
          detail: error instanceof Error ? error.message : String(error),
        });
      }
    },
    [build, showToast, t],
  );

  return { copyToClipboard, saveToFile };
}
