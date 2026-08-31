import { useCallback, useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import { EyeIcon, EyeOffIcon, RotateCcwIcon, SettingsIcon } from "lucide-react";
import type { LibraryColumn, LibraryColumnId } from "./item-table-config";

/**
 * 列设置：勾选要显示的列 + 一键恢复默认。
 * 复选切换后菜单保持打开，方便一次调整多列。
 */
export function ColumnConfigMenu({
  columns,
  onToggle,
  onReset,
}: {
  columns: LibraryColumn[];
  onToggle: (id: LibraryColumnId) => void;
  onReset: () => void;
}) {
  const { t } = useTranslation();
  const [anchor, setAnchor] = useState<{ top: number; right: number } | null>(
    null,
  );
  const menuRef = useRef<HTMLDivElement>(null);
  const buttonRef = useRef<HTMLButtonElement>(null);

  const close = useCallback(() => setAnchor(null), []);

  useEffect(() => {
    if (!anchor) {
      return;
    }
    const handlePointerDown = (event: MouseEvent) => {
      const target = event.target as Node;
      if (
        menuRef.current?.contains(target) ||
        buttonRef.current?.contains(target)
      ) {
        return;
      }
      close();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        close();
      }
    };
    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [anchor, close]);

  const label = t("library.columnConfig", "列设置");
  // 复选框列与操作列是表格骨架，不允许隐藏
  const configurable = columns.filter(
    (column) => column.id !== "checkbox" && column.id !== "actions",
  );

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        onClick={(event) => {
          if (anchor) {
            close();
            return;
          }
          const rect = event.currentTarget.getBoundingClientRect();
          setAnchor({
            top: rect.bottom + 4,
            right: window.innerWidth - rect.right,
          });
        }}
        aria-haspopup="menu"
        aria-expanded={anchor !== null}
        className={`inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs transition-colors ${
          anchor
            ? "border-primary/30 bg-primary/10 text-primary"
            : "border-border text-muted-foreground hover:bg-accent hover:text-foreground"
        }`}
      >
        <SettingsIcon className="h-3.5 w-3.5" aria-hidden="true" />
        <span>{label}</span>
      </button>

      {anchor
        ? createPortal(
            <div
              ref={menuRef}
              role="menu"
              aria-label={label}
              className="fixed z-[9999] w-60 rounded-lg border border-border bg-popover py-2 shadow-xl"
              style={{ top: anchor.top, right: anchor.right }}
            >
              <div className="border-b border-border px-3 pb-2">
                <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                  {label}
                </span>
                <p className="mt-0.5 text-xs text-muted-foreground">
                  {t("library.columnConfigHint", "点击切换列的显示/隐藏")}
                </p>
              </div>

              <div className="max-h-72 overflow-y-auto py-1">
                {configurable.map((column) => (
                  <button
                    key={column.id}
                    type="button"
                    role="menuitemcheckbox"
                    aria-checked={column.visible}
                    onClick={() => onToggle(column.id)}
                    className="flex w-full items-center gap-3 px-3 py-1.5 text-left transition-colors hover:bg-accent"
                  >
                    {column.visible ? (
                      <EyeIcon
                        className="h-4 w-4 shrink-0 text-primary"
                        aria-hidden="true"
                      />
                    ) : (
                      <EyeOffIcon
                        className="h-4 w-4 shrink-0 text-muted-foreground"
                        aria-hidden="true"
                      />
                    )}
                    <span
                      className={`min-w-0 flex-1 truncate text-sm ${
                        column.visible ? "text-foreground" : "text-muted-foreground"
                      }`}
                    >
                      {t(column.labelKey, column.fallback)}
                    </span>
                  </button>
                ))}
              </div>

              <div className="border-t border-border pt-1">
                <button
                  type="button"
                  onClick={() => {
                    onReset();
                    close();
                  }}
                  className="flex w-full items-center gap-2 px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                >
                  <RotateCcwIcon className="h-4 w-4" aria-hidden="true" />
                  <span>{t("common.reset", "重置")}</span>
                </button>
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}
