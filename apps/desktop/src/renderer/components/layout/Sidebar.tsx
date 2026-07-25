import {
  useSidebarController,
  type PageType,
  type SidebarLayout,
  type SidebarProps,
} from "./useSidebarController";
import { SidebarView } from "./SidebarView";

export type { PageType, SidebarLayout, SidebarProps };

export function Sidebar(props: SidebarProps) {
  const controller = useSidebarController(props);
  return <SidebarView controller={controller} />;
}
