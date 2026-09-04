import {
  BoxIcon,
  LayersIcon,
  LightbulbIcon,
  ListIcon,
  PencilIcon,
  UnlinkIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  countCatalogByFilter,
  useWikiStore,
  type WikiCatalogFilter,
} from "../../stores/wiki.store";

function FilterRow({
  icon,
  label,
  count,
  active,
  onClick,
}: {
  icon: React.ReactNode;
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-sm transition-colors duration-smooth ${
        active
          ? "bg-primary/15 text-foreground"
          : "text-sidebar-foreground/80 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
      }`}
    >
      <span className="flex h-4 w-4 shrink-0 items-center justify-center">
        {icon}
      </span>
      <span
        className={`min-w-0 flex-1 truncate text-left ${active ? "font-medium" : ""}`}
      >
        {label}
      </span>
      <span
        className={`rounded-full border px-1.5 py-0.5 text-xs tabular-nums ${
          active
            ? "border-primary/20 bg-primary/10 text-foreground"
            : "border-sidebar-border bg-sidebar-accent/80 text-sidebar-foreground/80"
        }`}
      >
        {count}
      </span>
    </button>
  );
}

/**
 * Wiki 模块侧栏：目录的筛选轴。
 *
 * 这里原本放的是「最近更新」——`catalog.slice(0, 15)`，和中间目录列
 * 同源同序，页面标题在屏幕上被列了两遍。侧栏该给的是「看哪一批」，
 * 不是再抄一遍列表。编译入口与状态统一收在工作区顶栏，也不再两处各一份。
 */
export function SidebarWikiPanel() {
  const { t } = useTranslation();
  const catalog = useWikiStore((state) => state.catalog);
  const backlinkCounts = useWikiStore((state) => state.backlinkCounts);
  const filter = useWikiStore((state) => state.catalogFilter);
  const setFilter = useWikiStore((state) => state.setCatalogFilter);
  const viewMode = useWikiStore((state) => state.viewMode);
  const setViewMode = useWikiStore((state) => state.setViewMode);

  const counts = countCatalogByFilter(catalog, backlinkCounts);

  const groups: {
    titleKey: string;
    titleFallback: string;
    rows: { id: WikiCatalogFilter; icon: React.ReactNode; label: string }[];
  }[] = [
    {
      titleKey: "wiki.panelBrowse",
      titleFallback: "浏览",
      rows: [
        {
          id: "all",
          icon: <LayersIcon className="h-4 w-4" aria-hidden="true" />,
          label: t("wiki.panelAll", "全部页面"),
        },
      ],
    },
    {
      titleKey: "wiki.panelByKind",
      titleFallback: "按类型",
      rows: [
        {
          id: "topic",
          icon: <ListIcon className="h-4 w-4" aria-hidden="true" />,
          label: t("wiki.kindTopic", "主题"),
        },
        {
          id: "entity",
          icon: <BoxIcon className="h-4 w-4" aria-hidden="true" />,
          label: t("wiki.kindEntity", "实体"),
        },
        {
          id: "concept",
          icon: <LightbulbIcon className="h-4 w-4" aria-hidden="true" />,
          label: t("wiki.kindConcept", "概念"),
        },
      ],
    },
    {
      titleKey: "wiki.panelNeedsAttention",
      titleFallback: "需要留意",
      rows: [
        {
          id: "orphan",
          icon: <UnlinkIcon className="h-4 w-4" aria-hidden="true" />,
          label: t("wiki.panelOrphan", "孤立页"),
        },
        {
          id: "manual",
          icon: <PencilIcon className="h-4 w-4" aria-hidden="true" />,
          label: t("wiki.panelManual", "手动编辑过"),
        },
      ],
    },
  ];

  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto px-2 pb-3 pt-3">
      {viewMode === "graph" ? (
        <div className="rounded-lg border border-dashed border-sidebar-border px-3 py-3 text-center">
          <p className="text-xs text-sidebar-foreground/60">
            {t("wiki.panelGraphHint", "图谱视图下按节点浏览")}
          </p>
          <button
            type="button"
            onClick={() => setViewMode("catalog")}
            className="mt-2 text-xs text-primary transition-colors hover:underline"
          >
            {t("wiki.viewCatalog", "目录视图")}
          </button>
        </div>
      ) : (
        groups.map((group) => (
          <div key={group.titleKey} className="pt-2 first:pt-0">
            <div className="flex items-center gap-2 px-3 pb-1">
              <span className="text-xs font-medium uppercase tracking-wide text-sidebar-foreground/80">
                {t(group.titleKey, group.titleFallback)}
              </span>
              <div className="h-px flex-1 bg-sidebar-border/60" />
            </div>
            <div className="space-y-0.5">
              {group.rows.map((row) => (
                <FilterRow
                  key={row.id}
                  icon={row.icon}
                  label={row.label}
                  count={counts[row.id]}
                  active={filter === row.id}
                  onClick={() => setFilter(row.id)}
                />
              ))}
            </div>
          </div>
        ))
      )}

      {viewMode === "catalog" && counts.orphan > 0 ? (
        <p className="mt-auto px-3 pt-4 text-xs leading-relaxed text-sidebar-foreground/75">
          {t(
            "wiki.panelOrphanHint",
            "孤立页没有被任何页面引用，通常是知识网络还没连上的边角。",
          )}
        </p>
      ) : null}
    </div>
  );
}
