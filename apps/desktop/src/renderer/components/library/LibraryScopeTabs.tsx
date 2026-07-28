import type { ReactNode } from "react";
import { ArchiveIcon, InboxIcon, LayersIcon, StarIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { KnowledgeScope } from "@guizhi/shared/types";
import { useKnowledgeStore } from "../../stores/knowledge.store";

interface ScopeTab {
  scope: Exclude<KnowledgeScope, "trash">;
  label: string;
  icon: ReactNode;
  count: number;
}

function CountBadge({ value, active }: { value: number; active: boolean }) {
  return (
    <span
      className={`absolute right-1 top-1 min-w-[15px] rounded-full px-1 text-center text-[9px] font-medium leading-[15px] ${
        active ? "bg-primary/20 text-primary" : "bg-foreground/10 text-foreground/60"
      }`}
    >
      {value > 99 ? "99+" : value}
    </span>
  );
}

/**
 * 知识库范围分段控件：未分类 / 全部 / 收藏 / 归档。
 * 选中集合、平台或标签时四个磁贴都不高亮（此时列表已被更窄的条件接管）。
 */
export function LibraryScopeTabs() {
  const { t } = useTranslation();
  const scope = useKnowledgeStore((state) => state.scope);
  const collectionId = useKnowledgeStore((state) => state.collectionId);
  const tagId = useKnowledgeStore((state) => state.tagId);
  const platform = useKnowledgeStore((state) => state.platform);
  const counts = useKnowledgeStore((state) => state.counts);
  const setScope = useKnowledgeStore((state) => state.setScope);

  const tabs: ScopeTab[] = [
    {
      scope: "uncategorized",
      label: t("library.scopeUncategorized", "未分类"),
      icon: <InboxIcon className="mb-1 h-4 w-4" aria-hidden="true" />,
      count: counts?.uncategorized ?? 0,
    },
    {
      scope: "all",
      label: t("library.scopeAllShort", "全部"),
      icon: <LayersIcon className="mb-1 h-4 w-4" aria-hidden="true" />,
      count: counts?.all ?? 0,
    },
    {
      scope: "favorites",
      label: t("library.scopeFavorites", "收藏"),
      icon: <StarIcon className="mb-1 h-4 w-4" aria-hidden="true" />,
      count: counts?.favorites ?? 0,
    },
    {
      scope: "archived",
      label: t("library.scopeArchived", "归档"),
      icon: <ArchiveIcon className="mb-1 h-4 w-4" aria-hidden="true" />,
      count: counts?.archived ?? 0,
    },
  ];

  return (
    <div className="grid grid-cols-4 gap-1 rounded-lg bg-sidebar-accent/40 p-1">
      {tabs.map((tab) => {
        const active =
          scope === tab.scope && !collectionId && !tagId && !platform;
        return (
          <button
            key={tab.scope}
            type="button"
            onClick={() => setScope(tab.scope)}
            aria-pressed={active}
            className={`relative flex flex-col items-center justify-center rounded-md py-2 transition-all duration-base ${
              active
                ? "bg-primary/15 text-primary shadow-sm"
                : "text-sidebar-foreground/60 hover:bg-sidebar-accent hover:text-sidebar-foreground"
            }`}
          >
            {tab.icon}
            <span className="max-w-full truncate px-0.5 text-[10px] font-medium leading-none">
              {tab.label}
            </span>
            {tab.count > 0 ? (
              <CountBadge value={tab.count} active={active} />
            ) : null}
          </button>
        );
      })}
    </div>
  );
}
