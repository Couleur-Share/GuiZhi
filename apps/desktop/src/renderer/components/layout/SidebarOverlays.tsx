import { ColumnResizer } from "../ui/ColumnResizer";
import {
  SIDEBAR_PANEL_WIDTH_DEFAULT,
  SIDEBAR_PANEL_WIDTH_MAX,
  SIDEBAR_PANEL_WIDTH_MIN,
} from "../../stores/ui.store";
import type { SidebarController } from "./sidebar-view-types";

export function SidebarOverlays({
  controller,
}: {
  controller: SidebarController;
}) {
  return <SidebarPanelResizer controller={controller} />;
}

function SidebarPanelResizer({
  controller,
}: {
  controller: SidebarController;
}) {
  if (controller.layout !== "panel" || controller.isCollapsed) return null;
  return (
    <div className="absolute inset-y-0 right-0 z-10 flex">
      <ColumnResizer
        currentWidth={controller.sidebarPanelWidth}
        min={SIDEBAR_PANEL_WIDTH_MIN}
        max={SIDEBAR_PANEL_WIDTH_MAX}
        defaultWidth={SIDEBAR_PANEL_WIDTH_DEFAULT}
        onResize={controller.setSidebarPanelWidth}
        ariaLabel={controller.t("sidebar.resizeAria", "调整侧栏宽度")}
      />
    </div>
  );
}
