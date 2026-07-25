export type PageType = "home" | "settings";
export type SidebarLayout = "combined" | "rail" | "panel";

export interface SidebarProps {
  currentPage: PageType;
  onNavigate: (page: PageType) => void;
  layout?: SidebarLayout;
}
