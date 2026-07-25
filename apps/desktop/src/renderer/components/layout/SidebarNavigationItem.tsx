import { memo } from "react";

interface SidebarNavigationItemProps {
  icon: React.ReactNode;
  label: string;
  count?: number | string;
  active?: boolean;
  onClick: () => void;
  collapsed?: boolean;
}

export const SidebarNavigationItem = memo(function SidebarNavigationItem({
  icon,
  label,
  count,
  active,
  onClick,
  collapsed,
}: SidebarNavigationItemProps) {
  return (
    <div className="w-full py-0.5">
      <button
        type="button"
        onClick={onClick}
        aria-label={label}
        title={label}
        className={`flex items-center rounded-lg transition-all duration-smooth relative group ${collapsed ? "h-10 w-10 justify-center" : "w-full justify-start gap-3 px-3 py-2"} ${active ? "bg-primary text-white shadow-sm" : "text-sidebar-foreground/60 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"}`}
      >
        <span
          aria-hidden="true"
          className={`flex shrink-0 items-center justify-center transition-transform duration-smooth ${collapsed ? "w-5 h-5 group-hover:scale-110" : "w-4 h-4"}`}
        >
          {icon}
        </span>
        {!collapsed ? (
          <>
            <span className="min-w-0 flex-1 truncate text-left text-sm">
              {label}
            </span>
            {count !== undefined ? (
              <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-sidebar-accent/80 text-sidebar-foreground/50 border border-white/5">
                {count}
              </span>
            ) : null}
          </>
        ) : null}
      </button>
    </div>
  );
});
