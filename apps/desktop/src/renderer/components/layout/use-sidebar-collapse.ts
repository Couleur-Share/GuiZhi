import { useCallback, useEffect, useSyncExternalStore } from "react";
import { COMPACT_LAYOUT_MAX_WIDTH, useUIStore } from "../../stores/ui.store";

const COMPACT_QUERY = `(max-width: ${COMPACT_LAYOUT_MAX_WIDTH - 0.02}px)`;

function subscribeCompact(onChange: () => void): () => void {
  const media = window.matchMedia(COMPACT_QUERY);
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}

function readCompact(): boolean {
  return window.matchMedia(COMPACT_QUERY).matches;
}

export function useCompactLayout(): boolean {
  return useSyncExternalStore(subscribeCompact, readCompact, () => false);
}

/**
 * 二级侧栏的实际收起状态。宽屏沿用持久化的偏好；紧凑布局默认收起，
 * 用户展开后以浮层显示，回到宽屏时自动关掉浮层。
 */
export function useSidebarCollapse() {
  const isCompact = useCompactLayout();
  const storedCollapsed = useUIStore((state) => state.isSidebarCollapsed);
  const setStoredCollapsed = useUIStore((state) => state.setSidebarCollapsed);
  const compactOpen = useUIStore((state) => state.compactSidebarOpen);
  const setCompactOpen = useUIStore((state) => state.setCompactSidebarOpen);

  useEffect(() => {
    if (!isCompact && compactOpen) setCompactOpen(false);
  }, [isCompact, compactOpen, setCompactOpen]);

  const toggle = useCallback(() => {
    if (isCompact) setCompactOpen(!compactOpen);
    else setStoredCollapsed(!storedCollapsed);
  }, [isCompact, compactOpen, storedCollapsed, setCompactOpen, setStoredCollapsed]);
  const closeOverlay = useCallback(() => setCompactOpen(false), [setCompactOpen]);

  return {
    isCollapsed: isCompact ? !compactOpen : storedCollapsed,
    isOverlay: isCompact && compactOpen,
    toggle,
    closeOverlay,
  };
}
