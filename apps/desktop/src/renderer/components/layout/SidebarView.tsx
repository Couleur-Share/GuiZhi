import { SidebarOverlays } from "./SidebarOverlays";
import { SidebarPanel } from "./SidebarPanel";
import { SidebarRail } from "./SidebarRail";
import type { SidebarController } from "./sidebar-view-types";

interface SidebarViewProps {
  controller: SidebarController;
}

export function SidebarView({ controller }: SidebarViewProps) {
  return (
    <aside
      ref={controller.sidebarRef}
      className={`relative z-20 flex shrink-0 overflow-hidden transition-all duration-smooth ease-in-out ${controller.asideClassName}`}
      style={controller.panelStyle}
    >
      {controller.showRail ? <SidebarRail controller={controller} /> : null}
      {controller.showPanel ? <SidebarPanel controller={controller} /> : null}
      <SidebarOverlays controller={controller} />
    </aside>
  );
}
