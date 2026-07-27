import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { IllustrationStyle } from "@guizhi/shared/types";
import { Modal } from "../ui/Modal";
import { parseStyleJson } from "./style-transfer";

const TEXTAREA =
  "h-56 w-full resize-none rounded-lg border border-border/70 bg-background/60 p-3 font-mono text-xs leading-relaxed text-foreground placeholder:text-muted-foreground/70 focus:border-primary/50 focus:outline-none";
const GHOST_BUTTON =
  "inline-flex h-8 items-center gap-1.5 rounded-lg border border-border/70 px-2.5 text-xs text-muted-foreground transition-colors hover:border-primary/50 hover:bg-primary/5 hover:text-primary";
const PRIMARY_BUTTON =
  "inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:pointer-events-none disabled:opacity-60";

/** 粘贴导入一套风格：先解析出预览，确认了才追加进列表 */
export function StyleImportDialog({
  isOpen,
  onClose,
  onImport,
}: {
  isOpen: boolean;
  onClose: () => void;
  onImport: (style: IllustrationStyle) => void;
}) {
  const { t } = useTranslation();
  const [text, setText] = useState("");

  useEffect(() => {
    if (isOpen) {
      setText("");
    }
  }, [isOpen]);

  const trimmed = text.trim();
  const parsed = trimmed
    ? parseStyleJson(trimmed, {
        invalid: t("library.illustrationStyleImportInvalid", "这不是一段有效的 JSON"),
        incomplete: t(
          "library.illustrationStyleImportIncomplete",
          "缺少名称或画法与配色，这两项是必需的",
        ),
      })
    : {};

  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      title={t("library.illustrationStyleImport", "导入风格")}
      subtitle={t(
        "library.illustrationStyleImportHint",
        "把别人给你的风格 JSON 粘在这里；整份预设文件里的一条也认。",
      )}
      size="lg"
      contentClassName="flex min-h-0 flex-col"
    >
      <div className="space-y-3 px-6 py-4">
        <textarea
          value={text}
          onChange={(event) => setText(event.target.value)}
          aria-label={t("library.illustrationStyleImport", "导入风格")}
          placeholder='{ "kind": "guizhi-illustration-style", ... }'
          className={TEXTAREA}
        />
        {parsed.error ? (
          <p className="text-xs text-destructive">{parsed.error}</p>
        ) : parsed.style ? (
          <div className="rounded-lg border border-border/60 bg-muted/20 px-3 py-2 text-xs">
            <div className="font-medium text-foreground">
              {parsed.style.name}
            </div>
            <div className="mt-0.5 text-muted-foreground">
              {parsed.style.description ||
                t("library.illustrationStyleImportNoDesc", "（没有写说明）")}
            </div>
            <div className="mt-1 text-[11px] text-muted-foreground">
              {t(
                "library.illustrationStyleImportMeta",
                "{{ratio}} · 单篇最多 {{shots}} 张 · 单图标注 {{labels}} 处",
                {
                  ratio: parsed.style.aspectRatio,
                  shots: parsed.style.maxShots,
                  labels: parsed.style.maxLabels,
                },
              )}
            </div>
          </div>
        ) : null}
      </div>

      <div className="flex shrink-0 items-center justify-end gap-2 border-t border-border px-6 py-3">
        <button type="button" onClick={onClose} className={GHOST_BUTTON}>
          {t("common.cancel", "取消")}
        </button>
        <button
          type="button"
          disabled={!parsed.style}
          onClick={() => {
            if (parsed.style) {
              onImport(parsed.style);
            }
          }}
          className={PRIMARY_BUTTON}
        >
          {t("library.illustrationStyleImportConfirm", "加入列表")}
        </button>
      </div>
    </Modal>
  );
}
