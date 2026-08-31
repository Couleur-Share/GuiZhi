import { useEffect, useMemo, useState } from "react";
import {
  AlertTriangleIcon,
  CheckIcon,
  CompassIcon,
  FolderInputIcon,
  NetworkIcon,
  RefreshCwIcon,
  ScanSearchIcon,
} from "lucide-react";
import type { InboxItem } from "@guizhi/shared/types";
import { useInboxStore } from "../../stores/inbox.store";
import { useCollectionStore } from "../../stores/collection.store";
import { useImportStore } from "../../stores/import.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { Select } from "../ui/Select";
import { Spinner } from "../ui/Spinner";
import { LoadErrorState } from "../ui/LoadErrorState";
import { useToast } from "../ui/Toast";

function knowledgeItemId(item: InboxItem): string | null {
  return "itemId" in item ? item.itemId : null;
}

export function InboxWorkspace() {
  const { showToast } = useToast();
  const items = useInboxStore((state) => state.items);
  const filter = useInboxStore((state) => state.filter);
  const total = useInboxStore((state) => state.total);
  const selectionIds = useInboxStore((state) => state.selectionIds);
  const toggleSelection = useInboxStore((state) => state.toggleSelection);
  const clearSelection = useInboxStore((state) => state.clearSelection);
  const refresh = useInboxStore((state) => state.refresh);
  const organize = useInboxStore((state) => state.organize);
  const markReviewed = useInboxStore((state) => state.markReviewed);
  const isLoading = useInboxStore((state) => state.isLoading);
  const loadError = useInboxStore((state) => state.loadError);
  const collections = useCollectionStore((state) => state.collections);
  const fetchCollections = useCollectionStore((state) => state.fetchCollections);
  const retryTask = useImportStore((state) => state.retryTask);
  const selectItem = useKnowledgeStore((state) => state.selectItem);
  const setScope = useKnowledgeStore((state) => state.setScope);
  const setAppModule = useUIStore((state) => state.setAppModule);
  const [collectionId, setCollectionId] = useState("");
  const [tagText, setTagText] = useState("");

  useEffect(() => {
    void refresh();
    void fetchCollections();
  }, [fetchCollections, refresh]);

  const visible = useMemo(
    () => (filter === "all" ? items : items.filter((item) => item.kind === filter)),
    [filter, items],
  );

  const openItem = async (id: string) => {
    setAppModule("library");
    setScope("all");
    await selectItem(id);
  };

  const applyOrganization = async () => {
    const tags = tagText.split(/[,，]/).map((tag) => tag.trim()).filter(Boolean);
    const changed = await organize({
      ...(collectionId ? { collectionId } : {}),
      ...(tags.length ? { addTagNames: tags } : {}),
    });
    showToast(`已整理 ${changed} 条内容`, "success");
    setTagText("");
  };

  const runSemantic = async () => {
    const { useSemanticStore } = await import("../../stores/semantic.store");
    await useSemanticStore.getState().runIndexing(false);
    await refresh();
  };

  const runWiki = async () => {
    const { useWikiStore } = await import("../../stores/wiki.store");
    await useWikiStore.getState().compileNow();
    await refresh();
  };

  return (
    <div className="flex h-full min-h-0 flex-1 flex-col overflow-hidden app-wallpaper-section">
      <div className="flex h-12 shrink-0 items-center gap-3 border-b border-border px-5">
        <h2 className="text-sm font-semibold text-foreground">处理中心</h2>
        <span className="text-xs text-muted-foreground">{total} 项待处理</span>
        <button type="button" onClick={() => void refresh()} className="ml-auto rounded-lg p-2 text-muted-foreground hover:bg-muted hover:text-foreground" title="刷新" aria-label="刷新处理中心">
          <RefreshCwIcon className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
        </button>
      </div>

      {selectionIds.length > 0 ? (
        <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-border bg-primary/5 px-5 py-2">
          <span className="text-sm text-foreground">已选 {selectionIds.length} 条</span>
          <Select value={collectionId} onChange={setCollectionId} placeholder="归入知识库" ariaLabel="归入知识库" options={collections.map((collection) => ({ value: collection.id, label: `${collection.icon ?? "📚"} ${collection.name}` }))} className="w-44" />
          <input value={tagText} onChange={(event) => setTagText(event.target.value)} placeholder="添加标签，逗号分隔" className="h-9 w-52 rounded-lg border border-border bg-background px-3 text-sm outline-none focus:border-primary/50" />
          <button type="button" onClick={() => void applyOrganization()} className="inline-flex h-9 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground"><FolderInputIcon className="h-4 w-4" />批量整理</button>
          <button type="button" onClick={clearSelection} className="h-9 rounded-lg px-3 text-sm text-muted-foreground hover:bg-muted">取消</button>
        </div>
      ) : null}

      <div className="min-h-0 flex-1 overflow-y-auto p-5">
        {isLoading && items.length === 0 ? (
          <div className="flex h-32 items-center justify-center"><Spinner size="sm" tone="muted" /></div>
        ) : loadError ? (
          <LoadErrorState message={loadError} onRetry={() => void refresh()} />
        ) : visible.length === 0 ? (
          <div className="flex h-52 flex-col items-center justify-center text-center">
            <CheckIcon className="h-10 w-10 text-emerald-500" />
            <p className="mt-3 text-sm font-medium text-foreground">当前分组已处理完</p>
          </div>
        ) : (
          <div className="space-y-2">
            {visible.map((item) => {
              if ("count" in item) {
                const semantic = item.kind === "semantic-pending";
                const Icon = semantic ? ScanSearchIcon : NetworkIcon;
                return (
                  <div key={item.id} className="flex items-center gap-3 rounded-xl border border-primary/20 bg-primary/5 p-4">
                    <Icon className="h-5 w-5 text-primary" />
                    <div className="flex-1"><p className="text-sm font-medium text-foreground">{semantic ? "语义索引待更新" : "Wiki 待编译"}</p><p className="text-xs text-muted-foreground">{item.count} 条内容等待处理；这里以聚合卡显示。</p></div>
                    <button type="button" onClick={() => void (semantic ? runSemantic() : runWiki())} className="h-8 rounded-lg border border-border px-3 text-xs hover:bg-muted">立即执行</button>
                  </div>
                );
              }
              const itemId = knowledgeItemId(item);
              const selected = itemId ? selectionIds.includes(itemId) : false;
              return (
                <div key={item.id} className="flex items-start gap-3 rounded-xl border border-border bg-background/60 p-4">
                  {itemId ? <input type="checkbox" checked={selected} onChange={() => toggleSelection(itemId)} aria-label={`选择 ${item.title}`} className="mt-1 h-4 w-4 rounded border-border accent-primary" /> : item.kind === "import-issue" ? <AlertTriangleIcon className="mt-0.5 h-4 w-4 text-amber-500" /> : <CompassIcon className="mt-0.5 h-4 w-4 text-primary" />}
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium text-foreground">{item.title || "无标题"}</p>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {item.kind === "review-required" ? item.reasons.join("；") || "采集结果需要人工复核" : item.kind === "unclassified" ? "尚未归入任何知识库" : item.kind === "import-issue" ? item.message : "定时发现的新候选，尚未导入"}
                    </p>
                  </div>
                  {itemId ? <button type="button" onClick={() => void openItem(itemId)} className="h-8 rounded-lg border border-border px-2.5 text-xs hover:bg-muted">打开</button> : null}
                  {item.kind === "review-required" ? <button type="button" onClick={() => void markReviewed([item.itemId])} className="h-8 rounded-lg bg-primary px-2.5 text-xs text-primary-foreground">标记已复核</button> : null}
                  {item.kind === "import-issue" && item.status === "failed" ? <button type="button" onClick={() => void retryTask(item.taskId).then(refresh)} className="h-8 rounded-lg border border-border px-2.5 text-xs hover:bg-muted">重试</button> : null}
                  {item.kind === "import-issue" && (item.duplicateItemId || item.resultItemId) ? <button type="button" onClick={() => void openItem(item.duplicateItemId || item.resultItemId!)} className="h-8 rounded-lg border border-border px-2.5 text-xs hover:bg-muted">查看条目</button> : null}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
