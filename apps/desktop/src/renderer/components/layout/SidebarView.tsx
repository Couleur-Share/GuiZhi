import { useEffect, useLayoutEffect } from "react";
import { SidebarOverlays } from "./SidebarOverlays";
import { SidebarPanel } from "./SidebarPanel";
import { SidebarRail } from "./SidebarRail";
import type { SidebarController } from "./sidebar-view-types";

interface SidebarViewProps {
  controller: SidebarController;
}

export function SidebarView({ controller }: SidebarViewProps) {
  const { isPanelOverlay, closeOverlay, sidebarRef } = controller;
  const panelCollapsed = controller.layout === "panel" && controller.isCollapsed;

  // 收起的侧栏只是透明加零宽，不设 inert 时里面的按钮仍能被 Tab 聚焦、被读屏读到
  useLayoutEffect(() => {
    sidebarRef.current?.toggleAttribute("inert", panelCollapsed);
  }, [sidebarRef, panelCollapsed]);

  useEffect(() => {
    if (!isPanelOverlay) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !event.defaultPrevented) closeOverlay();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [isPanelOverlay, closeOverlay]);

  return (
    <>
      {isPanelOverlay ? (
        <button
          type="button"
          tabIndex={-1}
          aria-label={controller.t("common.collapse", "收起")}
          onClick={closeOverlay}
          className="absolute inset-0 z-20 cursor-default bg-black/20"
        />
      ) : null}
      <aside
        ref={controller.sidebarRef}
        className={`${controller.asidePositionClassName} flex shrink-0 overflow-hidden transition-all duration-smooth ease-in-out ${controller.asideClassName}`}
        style={controller.panelStyle}
      >
        {controller.showRail ? <SidebarRail controller={controller} /> : null}
        {controller.showPanel ? <SidebarPanel controller={controller} /> : null}
        <SidebarOverlays controller={controller} />
      </aside>
    </>
  );
}
