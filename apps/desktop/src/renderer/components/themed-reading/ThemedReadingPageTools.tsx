import { useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { ChevronDownIcon, ImageOffIcon } from "lucide-react";
import { ContextMenu, type ContextMenuItem } from "../ui/ContextMenu";
import { Modal } from "../ui/Modal";
import { themeButton } from "./ThemedReadingSetup";
import type { useThemedReading } from "./use-themed-reading";

/** 低频操作集中到菜单；素材与来源资料按需打开，不再推挤正文。 */
export function ThemedReadingPageTools({
  view,
  disabled,
  onSetup,
  onDelete,
}: {
  view: ReturnType<typeof useThemedReading>;
  disabled: boolean;
  onSetup: (mode: "new" | "adjust") => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation();
  const trigger = useRef<HTMLButtonElement>(null);
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const [panel, setPanel] = useState<"references" | "assets" | "notes" | null>(
    null,
  );
  const page = view.result?.page;
  if (!page) return null;
  const references = (page.reconstruction?.references ?? []).filter(
    (reference) =>
      reference.status === "ready" &&
      page.reconstruction.draft.some((section) =>
        section.referenceIds.includes(reference.id),
      ),
  );
  const items: ContextMenuItem[] = [
    {
      label:
        page.formatVersion >= 2
          ? t("themedReading.readerAdjust", "调整阅读页")
          : t("themedReading.readerUpgrade", "重新设计阅读页"),
      disabled,
      onClick: () => onSetup(page.formatVersion >= 2 ? "adjust" : "new"),
    },
    {
      label: t("themedReading.readerRegenerate", "按最新原文重新生成"),
      disabled,
      onClick: () => onSetup("new"),
    },
    {
      label: t("themedReading.readerReferences", "参考资料"),
      onClick: () => setPanel("references"),
    },
    {
      label: t("themedReading.assets", "页面图片"),
      onClick: () => setPanel("assets"),
    },
    {
      label: t("themedReading.export", "导出 HTML"),
      disabled: view.busy,
      onClick: () => void view.exportHtml(),
    },
    {
      label: t("themedReading.exportText", "不含图片导出"),
      disabled: view.busy,
      onClick: () => void view.exportHtml(true),
    },
    ...(page.formatVersion === 3 ? [{label:"导出静态 HTML",disabled:view.busy,onClick:()=>void view.exportHtml(false,true)}] : []),
    ...(view.result?.previous
      ? [
          {
            label: t("themedReading.readerRestore", "恢复上一版 AI 阅读页"),
            disabled,
            onClick: () => void view.restore(),
          },
        ]
      : []),
    ...(page.warnings.length || view.result?.stale
      ? [
          {
            label: t("themedReading.readerPageNotes", "版本说明"),
            onClick: () => setPanel("notes"),
          },
        ]
      : []),
    {
      label: t("themedReading.readerDelete", "删除 AI 阅读页"),
      variant: "destructive",
      disabled,
      onClick: onDelete,
    },
  ];
  return (
    <>
      <button
        ref={trigger}
        type="button"
        className="inline-flex h-8 items-center gap-1 whitespace-nowrap rounded-md px-2 text-xs text-muted-foreground hover:bg-accent hover:text-foreground"
        aria-haspopup="menu"
        aria-expanded={!!menu}
        onClick={(event) => {
          const rect = event.currentTarget.getBoundingClientRect();
          setMenu(menu ? null : { x: rect.right - 220, y: rect.bottom + 4 });
        }}
      >
        {t("themedReading.readerSettings", "阅读页设置")}
        <ChevronDownIcon className="h-3 w-3" aria-hidden="true" />
      </button>
      {menu ? (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          ignoreRef={trigger}
          items={items}
          onClose={() => setMenu(null)}
        />
      ) : null}
      {panel ? (
        <Modal
          isOpen
          onClose={() => setPanel(null)}
          title={
            panel === "references"
              ? t("themedReading.readerReferences", "参考资料")
              : panel === "assets"
                ? t("themedReading.assets", "页面图片")
                : t("themedReading.readerPageNotes", "版本说明")
          }
          size="lg"
        >
          <div className="space-y-3">
            {panel === "references" ? (
              references.length ? (
                references.map((reference) => (
                  <article
                    key={reference.id}
                    className="border-b border-border/50 pb-3 text-sm"
                  >
                    <a
                      href={reference.url}
                      target="_blank"
                      rel="noreferrer"
                      className="text-primary underline-offset-4 hover:underline"
                    >
                      {reference.title}
                    </a>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {t("themedReading.readerCapturedAt", "获取于 {{date}}", {
                        date: new Date(
                          reference.capturedAt,
                        ).toLocaleDateString(),
                      })}
                    </p>
                  </article>
                ))
              ) : (
                <p className="text-sm text-muted-foreground">
                  {t(
                    "themedReading.readerNoReferences",
                    "此版本没有使用外部参考资料。",
                  )}
                </p>
              )
            ) : null}
            {panel === "notes" ? (
              <>
                {view.result?.stale ? (
                  <p className="text-sm text-muted-foreground">
                    {t(
                      "themedReading.readerStaleDetail",
                      "原文已更新。当前阅读页保留生成时的内容，可按最新原文重新生成。",
                    )}
                  </p>
                ) : null}
                {page.warnings.map((warning, index) => (
                  <p
                    key={index}
                    className="select-text text-sm leading-relaxed text-muted-foreground"
                  >
                    {warning}
                  </p>
                ))}
              </>
            ) : null}
            {panel === "assets" ? (
              page.assets.length ? (
                page.assets.map((asset) => (
                  <article
                    key={asset.id}
                    className="flex items-start gap-3 rounded-lg bg-muted/30 p-3"
                  >
                    {asset.fileName && asset.status === "ready" ? (
                      <img
                        className="h-16 w-20 shrink-0 rounded object-cover"
                        src={`local-image://${asset.fileName}`}
                        alt={asset.alt}
                      />
                    ) : (
                      <span className="flex h-16 w-20 shrink-0 items-center justify-center rounded bg-muted text-muted-foreground">
                        <ImageOffIcon className="h-5 w-5" aria-hidden="true" />
                      </span>
                    )}
                    <div className="min-w-0 flex-1 space-y-1 text-xs">
                      <p>{asset.purpose || asset.alt}</p>
                      <p className="text-muted-foreground">
                        {asset.role === "generated"
                          ? t("themedReading.generatedAsset", "AI 生成插画")
                          : t("themedReading.originalAsset", "原有图片")}
                      </p>
                      {asset.error ? (
                        <p className="select-text break-words text-destructive">
                          {asset.error}
                        </p>
                      ) : null}
                      {asset.role === "generated" ||
                      asset.status !== "ready" ? (
                        <button
                          className={themeButton}
                          disabled={disabled}
                          onClick={() => void view.regenerateAsset(asset.id)}
                        >
                          {asset.role === "generated"
                            ? t("themedReading.redraw", "重做这张图")
                            : t("themedReading.retryOriginal", "重试原图")}
                        </button>
                      ) : null}
                    </div>
                  </article>
                ))
              ) : (
                <p className="text-sm text-muted-foreground">
                  {t("themedReading.noAssets", "这份页面没有图片。")}
                </p>
              )
            ) : null}
          </div>
        </Modal>
      ) : null}
    </>
  );
}
