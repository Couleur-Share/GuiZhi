import { lazy, Suspense } from "react";
import { Spinner } from "../ui/Spinner";
import type { SidebarController } from "./sidebar-view-types";

const SidebarLibraryPanel = lazy(() =>
  import("../library/SidebarLibraryPanel").then((module) => ({
    default: module.SidebarLibraryPanel,
  })),
);
const SidebarAskPanel = lazy(() =>
  import("../ask/SidebarAskPanel").then((module) => ({
    default: module.SidebarAskPanel,
  })),
);
const SidebarWikiPanel = lazy(() =>
  import("../wiki/SidebarWikiPanel").then((module) => ({
    default: module.SidebarWikiPanel,
  })),
);
const SidebarImportsPanel = lazy(() =>
  import("../imports/SidebarImportsPanel").then((module) => ({
    default: module.SidebarImportsPanel,
  })),
);

function PanelFallback() {
  return (
    <div className="flex flex-1 items-center justify-center px-3 py-4">
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
  const renderPanel = () => {
    switch (controller.activeModule) {
      case "ask":
        return <SidebarAskPanel />;
      case "wiki":
        return <SidebarWikiPanel />;
      case "imports":
        return <SidebarImportsPanel />;
      case "library":
      default:
        return <SidebarLibraryPanel />;
    }
  };

  return (
    <div className="relative flex min-w-0 flex-1 flex-col bg-sidebar-background/85">
      <Suspense fallback={<PanelFallback />}>{renderPanel()}</Suspense>
    </div>
  );
}
