import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";
import type { LibraryColumn, LibraryColumnId } from "./item-table-config";

/** 键盘调整列宽的步进（px） */
const KEYBOARD_STEP = 16;

/**
 * 可拖拽调宽的表头单元格。拖拽手柄同时支持左右方向键，
 * 键盘用户不至于被挡在列宽调整之外。
 */
export function ItemTableHeaderCell({
  column,
  onResize,
  children,
}: {
  column: LibraryColumn;
  onResize: (id: LibraryColumnId, width: number) => void;
  children: ReactNode;
}) {
  const { t } = useTranslation();
  const [isDragging, setIsDragging] = useState(false);
  const cleanupRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    return () => {
      cleanupRef.current?.();
      cleanupRef.current = null;
    };
  }, []);

  const handleMouseDown = useCallback(
    (event: React.MouseEvent) => {
      event.preventDefault();
      event.stopPropagation();
      cleanupRef.current?.();

      const startX = event.clientX;
      const startWidth = column.width;
      const previousCursor = document.body.style.cursor;
      const previousUserSelect = document.body.style.userSelect;

      const handleMouseMove = (moveEvent: MouseEvent) => {
        onResize(column.id, startWidth + (moveEvent.clientX - startX));
      };
      const cleanup = () => {
        document.removeEventListener("mousemove", handleMouseMove);
        document.removeEventListener("mouseup", handleMouseUp);
        document.body.style.cursor = previousCursor;
        document.body.style.userSelect = previousUserSelect;
        cleanupRef.current = null;
        setIsDragging(false);
      };
      function handleMouseUp() {
        cleanup();
      }

      cleanupRef.current = cleanup;
      document.addEventListener("mousemove", handleMouseMove);
      document.addEventListener("mouseup", handleMouseUp);
      document.body.style.cursor = "col-resize";
      document.body.style.userSelect = "none";
      setIsDragging(true);
    },
    [column, onResize],
  );

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent) => {
      if (event.key === "ArrowLeft") {
        event.preventDefault();
        onResize(column.id, column.width - KEYBOARD_STEP);
      } else if (event.key === "ArrowRight") {
        event.preventDefault();
        onResize(column.id, column.width + KEYBOARD_STEP);
      }
    },
    [column, onResize],
  );

  const resizeLabel = t("library.resizeColumn", "调整「{{name}}」列宽", {
    name: t(column.labelKey, column.fallback),
  });

  return (
    <th scope="col" className="relative p-0" style={{ width: column.width }}>
      <div
        className={`h-full whitespace-nowrap px-4 py-2.5 text-xs font-medium text-muted-foreground ${
          column.centered ? "text-center" : "text-left"
        }`}
      >
        {children}
      </div>

      {column.resizable ? (
        <div
          role="separator"
          aria-orientation="vertical"
          aria-label={resizeLabel}
          title={resizeLabel}
          tabIndex={0}
          onMouseDown={handleMouseDown}
          onKeyDown={handleKeyDown}
          className="group absolute right-0 top-0 z-30 h-full w-2 translate-x-1/2 cursor-col-resize focus:outline-none"
        >
          <span
            aria-hidden="true"
            className={`absolute inset-y-1 left-1/2 w-0.5 -translate-x-1/2 rounded-full transition-colors duration-quick ${
              isDragging
                ? "bg-primary"
                : "bg-transparent group-hover:bg-primary/70 group-focus-visible:bg-primary/70"
            }`}
          />
        </div>
      ) : null}
    </th>
  );
}
