import {
  lazy,
  Suspense,
  useEffect,
  useRef,
  useState,
  type RefObject,
} from "react";
import { createPortal } from "react-dom";
import { useTranslation } from "react-i18next";
import type { KnowledgeItem } from "@guizhi/shared/types";
import {
  readingSourceText,
  replaceReadingSource,
} from "@guizhi/shared/utils/reading-source";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useToast } from "../ui/Toast";
import { MarkdownPreview } from "../library/MarkdownPreview";
import { SnapshotToc } from "../library/SnapshotToc";
import { ThemedReadingPane } from "./ThemedReadingPane";
import type { useThemedReadingMode } from "./use-themed-reading-mode";
import { themeButton } from "./ThemedReadingSetup";
import { patchContentReadingMemory } from "../library/reading-memory";

const editDrafts = new Map<string, { text: string; baseline: string }>();
const MarkdownEditor = lazy(() =>
  import("../library/MarkdownEditor").then((m) => ({
    default: m.MarkdownEditor,
  })),
);
const readerButton =
  "inline-flex h-8 shrink-0 items-center whitespace-nowrap rounded-md px-2 text-xs text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary";
export function ReconstructionReader({
  item,
  mode,
  scrollRef,
  findQuery,
  findIndex,
  onFindCount,
  onFindOpen,
  onModeChange,
  toolbarTarget,
}: {
  item: KnowledgeItem;
  mode: ReturnType<typeof useThemedReadingMode>;
  scrollRef: RefObject<HTMLDivElement>;
  findQuery: string;
  findIndex: number;
  onFindCount: (n: number) => void;
  onFindOpen: () => void;
  onModeChange: () => void;
  toolbarTarget?: HTMLElement | null;
}) {
  const { t } = useTranslation();
  const original = readingSourceText(item, mode.sourceKind),
    editKey = `${item.id}:${mode.sourceKind}`;
  const [draft, setDraft] = useState(editDrafts.get(editKey)?.text ?? original),
    [saving, setSaving] = useState(false),
    [error, setError] = useState("");
  const [localToolbar, setLocalToolbar] = useState<HTMLDivElement | null>(null);
  const toolbar = toolbarTarget ?? localToolbar;
  const baseline = useRef(editDrafts.get(editKey)?.baseline ?? original);
  const [catalogOpen, setCatalogOpen] = useState(false);
  const [headings, setHeadings] = useState<
    { index: number; text: string; top: number }[]
  >([]);
  const [position, setPosition] = useState({
    active: null as number | null,
    progress: 0,
  });
  const changeCatalog = (open: boolean) => {
    setCatalogOpen(open);
    try {
      localStorage.setItem(`reading-original-catalog:${editKey}`, String(open));
    } catch {
      /* 不影响阅读 */
    }
  };
  useEffect(() => {
    if (mode.active || mode.editing) return;
    const element = scrollRef.current;
    if (!element) return;
    const list = [...element.querySelectorAll("h1,h2,h3")].map(
      (heading, index) => ({
        index,
        text: heading.textContent ?? "",
        top:
          heading.getBoundingClientRect().top -
          element.getBoundingClientRect().top +
          element.scrollTop,
      }),
    );
    setHeadings(list);
    const rememberPosition = () =>
      setPosition({
        active:
          list.filter((heading) => heading.top <= element.scrollTop + 80).at(-1)
            ?.index ?? null,
        progress:
          element.scrollHeight > element.clientHeight
            ? Math.min(
                100,
                Math.round(
                  (element.scrollTop /
                    (element.scrollHeight - element.clientHeight)) *
                    100,
                ),
              )
            : 100,
      });
    rememberPosition();
    element.addEventListener("scroll", rememberPosition, { passive: true });
    try {
      setCatalogOpen(
        localStorage.getItem(`reading-original-catalog:${editKey}`) === "true",
      );
    } catch {
      /* 不影响阅读 */
    }
    return () => element.removeEventListener("scroll", rememberPosition);
  }, [mode.active, mode.editing, original, scrollRef, editKey]);
  const { showToast } = useToast();
  useEffect(() => {
    if (!mode.editing) {
      setDraft(original);
      baseline.current = original;
    }
  }, [original, mode.editing]);
  const rememberOriginal = () => {
    if (!mode.active && !mode.editing && scrollRef.current)
      patchContentReadingMemory(item.id, {
        scrollTopByTab: { [mode.sourceKind]: scrollRef.current.scrollTop },
      });
  };
  const switchMode = (active: boolean) => {
    rememberOriginal();
    setError("");
    onModeChange();
    mode.select(active);
  };
  const finish = async () => {
    setSaving(true);
    setError("");
    try {
      const store = useKnowledgeStore.getState(),
        latest = store.selectedItem;
      if (!latest || latest.id !== item.id)
        throw new Error(
          t("themedReading.readerEditSwitched", "文章已切换，请返回后重试保存"),
        );
      if (
        readingSourceText(latest, mode.sourceKind) !== baseline.current &&
        readingSourceText(latest, mode.sourceKind) !== draft
      )
        throw new Error(
          t(
            "themedReading.readerEditConflict",
            "原文在编辑期间已更新，请先保留当前编辑内容，再重新载入",
          ),
        );
      store.updateSelected({
        content: replaceReadingSource(latest, mode.sourceKind, draft),
      });
      if (!(await useKnowledgeStore.getState().flushPendingSave())) return;
      if (useKnowledgeStore.getState().hasUnsavedChanges)
        throw new Error(
          t("themedReading.readerEditUnsaved", "编辑内容尚未保存，请重试"),
        );
      baseline.current = draft;
      editDrafts.delete(editKey);
      mode.finishEditing();
      onModeChange();
    } catch (e) {
      const message = e instanceof Error ? e.message : String(e);
      setError(message);
      showToast(t("themedReading.readerEditFailed", "保存原文失败"), "error", {
        detail: message,
      });
    } finally {
      setSaving(false);
    }
  };
  const actions = (
    <div
      className="flex shrink-0 items-center gap-1"
      data-testid="reading-version-actions"
    >
      {mode.editing ? (
        <>
          <span className="px-2 text-xs text-muted-foreground">
            {t("themedReading.readerEditing", "正在编辑原文")}
          </span>
          <button
            className={themeButton}
            disabled={saving}
            onClick={() => void finish()}
          >
            {saving
              ? t("themedReading.readerSaving", "正在保存…")
              : t("themedReading.readerFinishEditing", "完成编辑")}
          </button>
        </>
      ) : (
        <>
          <div
            className="flex items-center gap-0.5"
            role="group"
            aria-label={t("themedReading.readerVersion", "阅读版本")}
          >
            <button
              className={`${readerButton} ${!mode.active ? "bg-muted font-medium !text-foreground" : ""}`}
              aria-pressed={!mode.active}
              onClick={() => switchMode(false)}
            >
              {t("themedReading.readerOriginal", "原文")}
            </button>
            <button
              className={`${readerButton} ${mode.active ? "bg-muted font-medium !text-foreground" : ""}`}
              aria-pressed={mode.active}
              onClick={() => switchMode(true)}
            >
              {t("themedReading.readerAi", "AI 阅读")}
            </button>
          </div>
          {!mode.active ? (
            <button
              data-snapshot-toc-trigger
              className={readerButton}
              aria-expanded={catalogOpen}
              onClick={() => changeCatalog(!catalogOpen)}
            >
              {t("library.catalog", "目录")}
            </button>
          ) : null}
          {!item.deletedAt ? (
            <button
              className={readerButton}
              aria-label={
                mode.sourceKind === "summary"
                  ? t("themedReading.readerEditSummary", "编辑讨论总结")
                  : t("themedReading.readerEditOriginal", "编辑原文")
              }
              onClick={() => {
                rememberOriginal();
                setError("");
                const cached = editDrafts.get(editKey);
                baseline.current = cached?.baseline ?? original;
                setDraft(cached?.text ?? original);
                mode.startEditing();
                onModeChange();
              }}
            >
              {t("themedReading.readerEdit", "编辑")}
            </button>
          ) : null}
        </>
      )}
    </div>
  );
  return (
    <div
      className="relative flex h-full min-h-0 flex-col"
      data-testid="reconstruction-reader"
    >
      {!toolbarTarget ? (
        <div
          ref={setLocalToolbar}
          className="flex min-h-10 shrink-0 items-center gap-1 overflow-x-auto border-b border-border/50 px-3"
        />
      ) : null}
      {toolbar ? createPortal(actions, toolbar) : null}
      {error ? (
        <p
          role="alert"
          className="shrink-0 select-text px-4 py-2 text-sm text-destructive"
        >
          {error}
        </p>
      ) : null}
      <div className="relative min-h-0 flex-1">
        {mode.editing ? (
          <div className="h-full">
            <Suspense
              fallback={
                <p className="p-4">
                  {t("themedReading.readerLoadingEditor", "正在加载编辑器…")}
                </p>
              }
            >
              <MarkdownEditor
                docId={`${item.id}:${mode.sourceKind}`}
                value={draft}
                onChange={(text) => {
                  setDraft(text);
                  editDrafts.set(editKey, { text, baseline: baseline.current });
                }}
                showLineNumbers={false}
                placeholderText={t(
                  "themedReading.readerEditPlaceholder",
                  "编辑 Markdown 内容…",
                )}
              />
            </Suspense>
          </div>
        ) : (
          <ThemedReadingPane
            toolbarTarget={toolbar}
            item={item}
            sourceKind={mode.sourceKind}
            showPage={mode.active}
            onPageRemoved={() => switchMode(false)}
            original={
              <MarkdownPreview
                ref={scrollRef}
                content={original}
                highlightQuery={findQuery}
              />
            }
            findQuery={findQuery}
            findIndex={findIndex}
            onFindCount={onFindCount}
            onFindOpen={onFindOpen}
          />
        )}
        {!mode.active && !mode.editing ? (
          <SnapshotToc
            headings={headings}
            active={position.active}
            progress={position.progress}
            wide={false}
            open={catalogOpen}
            onOpen={changeCatalog}
            onJump={(index) =>
              scrollRef.current?.scrollTo({ top: headings[index]?.top ?? 0 })
            }
            onTop={() => scrollRef.current?.scrollTo({ top: 0 })}
          />
        ) : null}
      </div>
    </div>
  );
}
