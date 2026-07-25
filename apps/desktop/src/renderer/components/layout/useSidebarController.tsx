import { useRef } from "react";
import type { SidebarProps } from "./sidebar-controller-types";
import { useSidebarShellController } from "./useSidebarShellController";

export type {
  PageType,
  SidebarLayout,
  SidebarProps,
} from "./sidebar-controller-types";

export function useSidebarController({
  currentPage,
  onNavigate,
  layout = "combined",
}: SidebarProps) {
  const shell = useSidebarShellController(currentPage, onNavigate, layout);
  const sidebarRef = useRef<HTMLElement>(null);
  return {
    currentPage,
    onNavigate,
    layout,
    sidebarRef,
    ...shell,
  };
}
