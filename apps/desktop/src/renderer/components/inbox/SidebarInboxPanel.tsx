import {
  AlertTriangleIcon,
  BoxesIcon,
  CircleHelpIcon,
  CompassIcon,
  InboxIcon,
  NetworkIcon,
  ScanSearchIcon,
} from "lucide-react";
import type { InboxItemKind } from "@guizhi/shared/types";
import { useInboxStore, type InboxFilter } from "../../stores/inbox.store";

const ROWS: Array<{
  id: InboxFilter;
  label: string;
  icon: typeof InboxIcon;
}> = [
  { id: "all", label: "全部待处理", icon: InboxIcon },
  { id: "review-required", label: "需要复核", icon: CircleHelpIcon },
  { id: "unclassified", label: "未归知识库", icon: BoxesIcon },
  { id: "import-issue", label: "导入问题", icon: AlertTriangleIcon },
  { id: "discovery-candidate", label: "发现候选", icon: CompassIcon },
  { id: "semantic-pending", label: "语义索引", icon: ScanSearchIcon },
  { id: "wiki-pending", label: "Wiki 编译", icon: NetworkIcon },
];

export function SidebarInboxPanel() {
  const filter = useInboxStore((state) => state.filter);
  const setFilter = useInboxStore((state) => state.setFilter);
  const counts = useInboxStore((state) => state.counts);
  const total = useInboxStore((state) => state.total);
  return (
    <div className="flex h-full min-h-0 flex-col overflow-y-auto px-2 py-3">
      <p className="px-3 pb-2 text-xs font-medium uppercase tracking-wide text-sidebar-foreground/80">
        处理中心
      </p>
      <div className="space-y-0.5">
        {ROWS.map((row) => {
          const Icon = row.icon;
          const count = row.id === "all" ? total : counts[row.id as InboxItemKind];
          return (
            <button
              key={row.id}
              type="button"
              aria-pressed={filter === row.id}
              onClick={() => setFilter(row.id)}
              className={`flex w-full items-center gap-2 rounded-lg px-3 py-1.5 text-sm transition-colors ${
                filter === row.id
                  ? "bg-primary/15 text-foreground"
                  : "text-sidebar-foreground/80 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
              }`}
            >
              <Icon className="h-4 w-4" aria-hidden="true" />
              <span className="min-w-0 flex-1 truncate text-left">{row.label}</span>
              <span className="rounded-full border border-sidebar-border px-1.5 py-0.5 text-xs tabular-nums">
                {count ?? 0}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
