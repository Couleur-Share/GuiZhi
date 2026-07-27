import { useCallback, useState } from "react";
import { useTranslation } from "react-i18next";
import type { IllustrationStyle } from "@guizhi/shared/types";
import { Modal } from "../ui/Modal";
import { UnsavedChangesDialog } from "../ui/UnsavedChangesDialog";
import { StyleWorkbench } from "./StyleWorkbench";
import { useStyleDrafts } from "./use-style-drafts";

/**
 * 配图风格编辑器（弹窗形态）。
 *
 * 从条目的配图面板里打开，改完就回去接着配图。想坐下来慢慢调的走
 * 「设置 → 正文配图」，那边是同一套 StyleWorkbench，不是另一份实现。
 */
export function StyleEditorModal({
  isOpen,
  styles,
  onClose,
  onSaved,
}: {
  isOpen: boolean;
  styles: IllustrationStyle[];
  onClose: () => void;
  onSaved: (styles: IllustrationStyle[]) => void;
}) {
  const { t } = useTranslation();
  const controller = useStyleDrafts(styles, isOpen, onSaved);
  const [confirmDiscard, setConfirmDiscard] = useState(false);

  const requestClose = useCallback(() => {
    if (controller.dirty) {
      setConfirmDiscard(true);
      return;
    }
    onClose();
  }, [controller.dirty, onClose]);

  return (
    <>
      <Modal
        isOpen={isOpen}
        onClose={requestClose}
        title={t("library.illustrationStyleEditorTitle", "配图风格")}
        subtitle={t(
          "library.illustrationStyleEditorSubtitle",
          "风格只决定画成什么样，不影响挑哪几段配图。保存后立即生效。",
        )}
        size="2xl"
        closeOnBackdrop={false}
        // 未保存提醒开着时 Esc 该只关它：两层的监听都挂在 document 上
        closeOnEscape={!confirmDiscard}
        contentClassName="flex min-h-0 flex-col"
      >
        <StyleWorkbench
          controller={controller}
          bodyClassName="h-[min(62vh,560px)]"
          onCancel={requestClose}
          onSaved={onClose}
        />
      </Modal>

      <UnsavedChangesDialog
        isOpen={confirmDiscard}
        onClose={() => setConfirmDiscard(false)}
        onDiscard={() => {
          setConfirmDiscard(false);
          onClose();
        }}
        onSave={() => {
          setConfirmDiscard(false);
          void controller.save().then((saved) => {
            if (saved) {
              onClose();
            }
          });
        }}
      />
    </>
  );
}
