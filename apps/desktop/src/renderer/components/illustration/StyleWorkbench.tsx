import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  AlertCircleIcon,
  CopyIcon,
  DownloadIcon,
  FolderOpenIcon,
  Loader2Icon,
  PlusIcon,
  RotateCcwIcon,
  ShareIcon,
  Trash2Icon,
} from "lucide-react";
import type { IllustrationStyle } from "@guizhi/shared/types";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { StyleForm } from "./StyleForm";
import { StyleImportDialog } from "./StyleImportDialog";
import type { StyleDraftsController } from "./use-style-drafts";

/** 按分组切段，保持文件里的先后顺序；未分组的自然落在最前 */
function groupStyles(
  styles: IllustrationStyle[],
): { group: string; items: IllustrationStyle[] }[] {
  const sections: { group: string; items: IllustrationStyle[] }[] = [];
  for (const style of styles) {
    const last = sections.find((section) => section.group === style.group);
    if (last) {
      last.items.push(style);
    } else {
      sections.push({ group: style.group, items: [style] });
    }
  }
  return sections;
}

const GHOST_BUTTON =
  "inline-flex h-8 shrink-0 items-center gap-1.5 rounded-lg border border-border/70 px-2.5 text-xs text-muted-foreground transition-colors hover:border-primary/50 hover:bg-primary/5 hover:text-primary disabled:pointer-events-none disabled:opacity-60";
const PRIMARY_BUTTON =
  "inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-60";

/**
 * 风格预设的编辑主体：左列预设、右侧字段、底部动作。
 *
 * 只管画，不管自己被装在哪儿——弹窗形态传 onCancel 与自己的未保存拦截，
 * 设置页里常驻则不传。删除与恢复内置这两个确认框两处一样，留在这里。
 */
export function StyleWorkbench({
  controller,
  bodyClassName,
  onCancel,
  onSaved,
}: {
  controller: StyleDraftsController;
  /** 两栏区域的高度，弹窗与设置页给的不一样 */
  bodyClassName: string;
  onCancel?: () => void;
  /** 保存成功后的额外动作（弹窗形态用它关掉自己） */
  onSaved?: () => void;
}) {
  const { t } = useTranslation();
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmRestore, setConfirmRestore] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const { drafts, active, errors, dirty, saving } = controller;

  const save = async () => {
    if (await controller.save()) {
      onSaved?.();
    }
  };

  return (
    <>
      <div className={`flex ${bodyClassName}`}>
        <aside className="flex w-52 shrink-0 flex-col border-r border-border">
          <div className="min-h-0 flex-1 space-y-2 overflow-y-auto p-2">
            {groupStyles(drafts).map((section) => (
              <div key={section.group} className="space-y-1">
                {section.group ? (
                  <div className="px-2.5 pt-1 text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
                    {section.group}
                  </div>
                ) : null}
                {section.items.map((draft) => (
                  <button
                    key={draft.id}
                    type="button"
                    onClick={() => controller.select(draft.id)}
                    className={`flex w-full items-center gap-1.5 rounded-lg px-2.5 py-2 text-left transition-colors ${
                      draft.id === controller.activeId
                        ? "bg-primary/10 text-primary"
                        : "text-foreground hover:bg-accent/50"
                    }`}
                  >
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-sm">
                        {draft.name ||
                          t("library.illustrationStyleUnnamed", "未命名风格")}
                      </span>
                      {draft.description ? (
                        <span className="mt-0.5 block truncate text-[11px] text-muted-foreground">
                          {draft.description}
                        </span>
                      ) : null}
                    </span>
                    {errors[draft.id] ? (
                      <AlertCircleIcon
                        role="img"
                        className="h-3.5 w-3.5 shrink-0 text-destructive"
                        aria-label={t(
                          "library.illustrationStyleInvalid",
                          "这套风格有必填项没填",
                        )}
                      />
                    ) : null}
                  </button>
                ))}
              </div>
            ))}
          </div>
          <div className="shrink-0 border-t border-border p-2">
            <button
              type="button"
              onClick={controller.add}
              className={`${GHOST_BUTTON} w-full justify-center`}
            >
              <PlusIcon className="h-3.5 w-3.5" aria-hidden="true" />
              {t("library.illustrationStyleAdd", "新建风格")}
            </button>
          </div>
        </aside>

        <div className="min-w-0 flex-1 overflow-y-auto px-5 py-4">
          {active ? (
            <>
              <div className="mb-4 flex items-center justify-end gap-2">
                <button
                  type="button"
                  onClick={controller.duplicate}
                  className={GHOST_BUTTON}
                >
                  <CopyIcon className="h-3.5 w-3.5" aria-hidden="true" />
                  {t("library.illustrationStyleDuplicate", "复制一份")}
                </button>
                <button
                  type="button"
                  onClick={() => void controller.exportActive()}
                  className={GHOST_BUTTON}
                  title={t(
                    "library.illustrationStyleExportHint",
                    "复制成一段 JSON，可以直接发给别人",
                  )}
                >
                  <ShareIcon className="h-3.5 w-3.5" aria-hidden="true" />
                  {t("library.illustrationStyleExport", "导出")}
                </button>
                <button
                  type="button"
                  onClick={() => setConfirmDelete(true)}
                  disabled={drafts.length <= 1}
                  title={
                    drafts.length <= 1
                      ? t(
                          "library.illustrationStyleDeleteLast",
                          "至少要保留一套风格",
                        )
                      : undefined
                  }
                  className={`${GHOST_BUTTON} hover:border-destructive/50 hover:bg-destructive/5 hover:text-destructive`}
                >
                  <Trash2Icon className="h-3.5 w-3.5" aria-hidden="true" />
                  {t("library.illustrationStyleDelete", "删除")}
                </button>
              </div>
              <StyleForm
                style={active}
                errors={errors[active.id] ?? {}}
                onChange={controller.update}
              />
            </>
          ) : (
            <p className="py-10 text-center text-sm text-muted-foreground">
              {t(
                "library.illustrationStyleEmpty",
                "一套风格都没有了，新建一套或恢复内置预设。",
              )}
            </p>
          )}
        </div>
      </div>

      <div className="flex shrink-0 flex-wrap items-center gap-2 border-t border-border px-6 py-3">
        <button
          type="button"
          onClick={() => setConfirmRestore(true)}
          className={GHOST_BUTTON}
        >
          <RotateCcwIcon className="h-3.5 w-3.5" aria-hidden="true" />
          {t("library.illustrationStyleRestore", "恢复内置预设")}
        </button>
        <button
          type="button"
          onClick={() => setImportOpen(true)}
          className={GHOST_BUTTON}
        >
          <DownloadIcon className="h-3.5 w-3.5" aria-hidden="true" />
          {t("library.illustrationStyleImport", "导入风格")}
        </button>
        <button
          type="button"
          onClick={() => void controller.revealFile()}
          className={GHOST_BUTTON}
          title={t(
            "library.illustrationStyleRevealHint",
            "预设存在 config/illustration-styles.json，可以手动编辑",
          )}
        >
          <FolderOpenIcon className="h-3.5 w-3.5" aria-hidden="true" />
          {t("library.illustrationStyleReveal", "在文件夹中显示")}
        </button>
        <div className="flex-1" />
        {dirty ? (
          <span className="text-[11px] text-muted-foreground">
            {t("library.illustrationStyleDirty", "有改动尚未保存")}
          </span>
        ) : null}
        {onCancel ? (
          <button type="button" onClick={onCancel} className={GHOST_BUTTON}>
            {t("common.cancel", "取消")}
          </button>
        ) : null}
        <button
          type="button"
          onClick={() => void save()}
          // 不按 dirty 置灰：预设文件被手改坏时 read 会静默退回内置预设，
          // 此时草稿与「当前值」看着一模一样，置灰就等于把修复的路也堵了
          disabled={saving}
          className={PRIMARY_BUTTON}
        >
          {saving ? (
            <Loader2Icon
              className="h-3.5 w-3.5 animate-spin"
              aria-hidden="true"
            />
          ) : null}
          {t("common.save", "保存")}
        </button>
      </div>

      <ConfirmDialog
        isOpen={confirmDelete}
        onClose={() => setConfirmDelete(false)}
        onConfirm={() => {
          setConfirmDelete(false);
          controller.removeActive();
        }}
        variant="destructive"
        title={t("library.illustrationStyleDeleteTitle", "删除这套风格？")}
        message={t(
          "library.illustrationStyleDeleteMessage",
          "「{{name}}」将从列表中移除，已经生成的配图不受影响。保存后才会写入文件。",
          { name: active?.name ?? "" },
        )}
        confirmText={t("common.delete", "删除")}
        cancelText={t("common.cancel", "取消")}
      />

      <ConfirmDialog
        isOpen={confirmRestore}
        onClose={() => setConfirmRestore(false)}
        onConfirm={() => {
          setConfirmRestore(false);
          void controller.restoreBuiltIn();
        }}
        variant="destructive"
        title={t("library.illustrationStyleRestoreTitle", "恢复内置预设？")}
        message={t(
          "library.illustrationStyleRestoreMessage",
          "当前列表会被内置风格替换，你改过的内容将丢失。保存后才会写入文件。",
        )}
        confirmText={t("library.illustrationStyleRestore", "恢复内置预设")}
        cancelText={t("common.cancel", "取消")}
      />

      <StyleImportDialog
        isOpen={importOpen}
        onClose={() => setImportOpen(false)}
        onImport={(style) => {
          setImportOpen(false);
          controller.importStyle(style);
        }}
      />
    </>
  );
}
