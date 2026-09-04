import { SettingsIcon } from "lucide-react";
import type { AppModule } from "../../stores/ui.store";
import type { SidebarController } from "./sidebar-view-types";

interface SidebarRailProps {
  controller: SidebarController;
}

type RailNavItem = SidebarController["railNavItems"][number];

/** 资产流转 / 智能探索 / 任务队列——组间细线分隔，避免六枚图标等权并列 */
const RAIL_GROUPS: readonly (readonly AppModule[])[] = [
  ["library", "inbox"],
  ["ask", "wiki", "research"],
  ["imports"],
];

function RailItemButton({ item }: { item: RailNavItem }) {
  return (
    <button
      type="button"
      onClick={item.onClick}
      aria-label={item.label}
      className={`flex flex-col items-center justify-center gap-1.5 rounded-2xl px-2 py-3 text-xs font-medium transition-colors titlebar-no-drag ${item.active ? "bg-primary/15 text-foreground shadow-sm" : "text-sidebar-foreground/80 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"}`}
    >
      <span className="relative">
        <span
          aria-hidden="true"
          className={`flex h-9 w-9 items-center justify-center rounded-2xl ${item.active ? "bg-primary/10" : "bg-transparent"}`}
        >
          {item.icon}
        </span>
        {item.badge !== undefined ? (
          <span className="absolute -right-1 -top-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-foreground px-1 text-xs font-semibold leading-none text-background shadow-sm ring-2 ring-sidebar-background">
            {item.badge > 99 ? "99+" : item.badge}
          </span>
        ) : item.busy ? (
          <span
            aria-hidden="true"
            className="absolute -right-0.5 -top-0.5 h-2.5 w-2.5 animate-pulse rounded-full bg-amber-500 ring-2 ring-sidebar-background"
          />
        ) : null}
      </span>
      <span className="text-center text-xs leading-none">{item.label}</span>
    </button>
  );
}

function SidebarRailItems({ controller }: SidebarRailProps) {
  const byKey = new Map(
    controller.railNavItems.map((item) => [item.key, item] as const),
  );
  const groups = RAIL_GROUPS.map((keys) =>
    keys
      .map((key) => byKey.get(key))
      .filter((item): item is RailNavItem => item != null),
  ).filter((group) => group.length > 0);

  return (
    <div className="flex flex-1 flex-col gap-1">
      {groups.map((group, groupIndex) => (
        <div key={group.map((item) => item.key).join("-")} className="flex flex-col gap-2">
          {groupIndex > 0 ? (
            <div
              aria-hidden="true"
              className="mx-3 my-1 h-px bg-sidebar-border/70"
            />
          ) : null}
          {group.map((item) => (
            <RailItemButton key={item.key} item={item} />
          ))}
        </div>
      ))}
    </div>
  );
}

function SidebarRailSettings({ controller }: SidebarRailProps) {
  const openSettings = () => {
    controller.onNavigate("settings");
  };
  return (
    <div className="mt-auto pt-4">
      <div className="flex items-center justify-center titlebar-no-drag">
        <button
          type="button"
          data-testid="rail-settings"
          // 齿轮就是「设置」的通用写法，气泡只是把图标念一遍；可访问名交给 aria-label
          aria-label={controller.t("header.settings")}
          onClick={openSettings}
          className={`flex h-11 w-11 items-center justify-center rounded-2xl transition-colors ${controller.currentPage === "settings" ? "bg-sidebar-accent text-sidebar-foreground" : "text-sidebar-foreground/60 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"}`}
        >
          <SettingsIcon className="h-5 w-5" aria-hidden="true" />
        </button>
      </div>
    </div>
  );
}

export function SidebarRail({ controller }: SidebarRailProps) {
  return (
    <div
      className={`flex ${controller.railWidthClass} shrink-0 select-none flex-col bg-sidebar-accent/25 titlebar-drag ${controller.layout === "combined" && !controller.isCollapsed ? "border-r border-sidebar-border/60" : ""}`}
    >
      {!controller.webRuntime &&
      controller.isMac &&
      !controller.isFullscreen ? (
        <div className="h-14 titlebar-drag shrink-0" />
      ) : null}
      <div className="flex flex-1 flex-col px-2 py-3">
        <SidebarRailItems controller={controller} />
        <SidebarRailSettings controller={controller} />
      </div>
    </div>
  );
}
