import type { ReactNode } from "react";
import { MoreHorizontalIcon } from "lucide-react";

interface LibrarySidebarRowProps {
  icon: ReactNode;
  label: string;
  count?: number;
  active?: boolean;
  onClick: () => void;
  /** 提供后行尾出现「更多」按钮（悬停/聚焦时替换计数显示） */
  onMore?: (event: React.MouseEvent) => void;
  moreLabel?: string;
  onContextMenu?: (event: React.MouseEvent) => void;
}

/**
 * 知识库侧栏的通用行（集合 / 标签 / 回收站）。
 * 计数与「更多」按钮共用同一个槽位，悬停时交叉淡入淡出，避免行内元素位移。
 */
export function LibrarySidebarRow({
  icon,
  label,
  count,
  active,
  onClick,
  onMore,
  moreLabel,
  onContextMenu,
}: LibrarySidebarRowProps) {
  return (
    <div
      onContextMenu={onContextMenu}
      className={`group/row relative flex min-h-[32px] items-center rounded-lg transition-colors duration-smooth ${
        active
          ? "bg-primary/15 text-primary"
          : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
      }`}
    >
      <button
        type="button"
        onClick={onClick}
        aria-pressed={active}
        title={label}
        className="flex min-w-0 flex-1 items-center gap-2 rounded-lg py-1.5 pl-3 pr-1 text-left outline-none focus-visible:ring-2 focus-visible:ring-primary/40"
      >
        <span
          aria-hidden="true"
          className="flex h-4 w-4 shrink-0 items-center justify-center text-sm leading-none"
        >
          {icon}
        </span>
        <span
          className={`truncate text-sm ${active ? "font-medium" : "font-normal"}`}
        >
          {label}
        </span>
      </button>
      <div className="relative mr-1.5 flex h-6 w-6 shrink-0 items-center justify-center">
        {count ? (
          <span
            aria-hidden={onMore ? "true" : undefined}
            className={`text-[10px] leading-4 transition-opacity duration-quick ${
              active ? "text-primary/70" : "text-sidebar-foreground/40"
            } ${onMore ? "group-hover/row:opacity-0 group-focus-within/row:opacity-0" : ""}`}
          >
            {count > 99 ? "99+" : count}
          </span>
        ) : null}
        {onMore ? (
          <button
            type="button"
            onClick={onMore}
            title={moreLabel}
            aria-label={moreLabel}
            className="absolute inset-0 flex items-center justify-center rounded opacity-0 transition-opacity duration-quick hover:bg-foreground/10 focus-visible:opacity-100 group-hover/row:opacity-100"
          >
            <MoreHorizontalIcon className="h-4 w-4" aria-hidden="true" />
          </button>
        ) : null}
      </div>
    </div>
  );
}
