import { Suspense } from "react";
import { Spinner } from "../ui/Spinner";
import { MODULE_PANELS } from "./module-chunks";
import type { SidebarController } from "./sidebar-view-types";

function PanelFallback() {
  return (
    <div className="delayed-fade-in flex flex-1 items-center justify-center px-3 py-4">
      <Spinner size="sm" tone="muted" />
    </div>
  );
}

/**
 * 侧栏面板：按当前模块渲染导航内容。
 */
export function SidebarPanel({
  controller,
}: {
  controller: SidebarController;
}) {
  const Panel = MODULE_PANELS[controller.activeModule];

  return (
    <div className="relative flex min-w-0 flex-1 flex-col bg-sidebar-background/85">
      <Suspense fallback={<PanelFallback />}>
        <Panel />
      </Suspense>
    </div>
  );
}
