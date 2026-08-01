import { useState } from "react";
import { BookmarkPlusIcon } from "lucide-react";
import { Modal } from "../ui/Modal";
import { Input } from "../ui/Input";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import {
  createSavedLibraryView,
  readSavedLibraryViews,
  writeSavedLibraryViews,
  type SavedLibraryView,
} from "./saved-library-views";
import { LibrarySidebarRow } from "./LibrarySidebarRow";

/** 本机保存的智能视图。它们只记录结构化筛选，不保存转瞬即逝的搜索关键词。 */
export function SmartViewsSection() {
  const [views, setViews] = useState<SavedLibraryView[]>(readSavedLibraryViews);
  const [isCreating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const scope = useKnowledgeStore((state) => state.scope);
  const collectionId = useKnowledgeStore((state) => state.collectionId);
  const tagId = useKnowledgeStore((state) => state.tagId);
  const platform = useKnowledgeStore((state) => state.platform);
  const applyFacetFilters = useKnowledgeStore((state) => state.applyFacetFilters);

  const saveCurrent = () => {
    const view = createSavedLibraryView(name, {
      scope,
      collectionId,
      tagId,
      platform,
    });
    if (!view) return;
    const next = [view, ...views];
    writeSavedLibraryViews(next);
    setViews(next);
    setName("");
    setCreating(false);
  };

  const remove = (id: string) => {
    const next = views.filter((view) => view.id !== id);
    writeSavedLibraryViews(next);
    setViews(next);
  };

  return (
    <>
      <div className="mb-1 mt-4 flex items-center justify-between gap-2 px-3">
        <span className="text-[11px] font-semibold uppercase tracking-wider text-sidebar-foreground/50">
          智能视图
        </span>
        <button
          type="button"
          onClick={() => setCreating(true)}
          className="rounded-lg p-1 text-sidebar-foreground/50 transition-colors hover:bg-sidebar-accent hover:text-primary"
          aria-label="保存当前筛选为智能视图"
          title="保存当前筛选为智能视图"
        >
          <BookmarkPlusIcon className="h-3.5 w-3.5" aria-hidden="true" />
        </button>
      </div>
      {views.map((view) => (
        <LibrarySidebarRow
          key={view.id}
          icon={<BookmarkPlusIcon className="h-4 w-4" aria-hidden="true" />}
          label={view.name}
          active={
            scope === view.scope &&
            collectionId === view.collectionId &&
            tagId === view.tagId &&
            platform === view.platform
          }
          onClick={() => applyFacetFilters(view)}
          onMore={(event) => {
            event.preventDefault();
            remove(view.id);
          }}
          moreLabel="删除智能视图"
        />
      ))}
      <Modal
        isOpen={isCreating}
        onClose={() => setCreating(false)}
        title="保存智能视图"
        size="sm"
      >
        <form
          className="space-y-4"
          onSubmit={(event) => {
            event.preventDefault();
            saveCurrent();
          }}
        >
          <Input
            autoFocus
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="例如：工作库的收藏视频"
          />
          <div className="flex justify-end gap-2">
            <button type="button" onClick={() => setCreating(false)} className="h-9 rounded-lg border border-border px-3 text-sm hover:bg-muted/60">取消</button>
            <button type="submit" disabled={!name.trim()} className="h-9 rounded-lg bg-primary px-3 text-sm text-primary-foreground disabled:opacity-50">保存</button>
          </div>
        </form>
      </Modal>
    </>
  );
}
