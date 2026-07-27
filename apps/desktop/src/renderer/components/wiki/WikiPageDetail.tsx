import { useEffect, useMemo, useRef, useState } from "react";
import {
  BookOpenIcon,
  ChevronDownIcon,
  Link2Icon,
  ListTreeIcon,
  MoreHorizontalIcon,
  PencilIcon,
  RotateCcwIcon,
  Trash2Icon,
  WandSparklesIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  extractWikiToc,
  stripDuplicateTitleHeading,
} from "@guizhi/shared/utils/wiki-body";
import { useWikiStore } from "../../stores/wiki.store";
import { useKnowledgeStore } from "../../stores/knowledge.store";
import { useUIStore } from "../../stores/ui.store";
import { useToast } from "../ui/Toast";
import { ConfirmDialog } from "../ui/ConfirmDialog";
import { ContextMenu, type ContextMenuItem } from "../ui/ContextMenu";
import { Spinner } from "../ui/Spinner";
import { WikiMarkdown } from "./WikiMarkdown";
import { WikiKindBadge } from "./WikiCatalogList";
import { formatItemTime } from "../library/type-meta";

/** 标题少于这个数不值得占一块目录 */
const TOC_MIN_HEADINGS = 3;

function RelatedChips({
  label,
  icon,
  entries,
  onOpen,
}: {
  label: string;
  icon: React.ReactNode;
  entries: { id: string; title: string }[];
  onOpen: (id: string) => void;
}) {
  if (entries.length === 0) {
    return null;
  }
  return (
    <div className="flex flex-wrap items-center gap-1.5">
      <span className="inline-flex shrink-0 items-center gap-1 text-[11px] text-muted-foreground">
        {icon}
        {label}
      </span>
      {entries.map((entry) => (
        <button
          key={entry.id}
          type="button"
          onClick={() => onOpen(entry.id)}
          className="inline-flex max-w-56 items-center rounded-md border border-border/70 bg-background/60 px-1.5 py-0.5 text-[11px] text-foreground transition-colors hover:border-primary/40 hover:bg-primary/5"
        >
          <span className="min-w-0 truncate">{entry.title}</span>
        </button>
      ))}
    </div>
  );
}

/** 页内目录：正文标题多起来时才出现，默认收起，不跟正文抢第一屏 */
function PageToc({
  body,
  scrollRef,
}: {
  body: string;
  scrollRef: React.RefObject<HTMLDivElement | null>;
}) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const toc = useMemo(() => extractWikiToc(body), [body]);

  if (toc.length < TOC_MIN_HEADINGS) {
    return null;
  }

  const jump = (slug: string) => {
    const target = scrollRef.current?.querySelector(`#${CSS.escape(slug)}`);
    target?.scrollIntoView({ behavior: "smooth", block: "start" });
  };

  return (
    <div className="mb-4 rounded-lg border border-border/60 bg-background/40">
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        aria-expanded={open}
        className="flex w-full items-center gap-1.5 px-3 py-1.5 text-[11px] text-muted-foreground transition-colors hover:text-foreground"
      >
        <ListTreeIcon className="h-3.5 w-3.5" aria-hidden="true" />
        {t("wiki.toc", "本页目录（{{count}} 节）", { count: toc.length })}
        <ChevronDownIcon
          className={`ml-auto h-3.5 w-3.5 transition-transform duration-quick ${
            open ? "rotate-180" : ""
          }`}
          aria-hidden="true"
        />
      </button>
      {open ? (
        <div className="space-y-0.5 px-3 pb-2">
          {toc.map((entry) => (
            <button
              key={entry.slug}
              type="button"
              onClick={() => jump(entry.slug)}
              className={`block w-full truncate rounded px-1.5 py-0.5 text-left text-xs text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground ${
                entry.level === 3 ? "pl-5" : ""
              }`}
            >
              {entry.text}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  );
}

/**
 * Wiki 页面详情。
 *
 * 头部只放标题与出处，编辑之外的动作收进「更多」菜单——删除是不可逆的，
 * 之前它和「编辑」是同样大小的 chip 并排摆着。
 * 反向链接提到正文之前：对靠互链组织的 Wiki 来说，「谁引用了这一页」
 * 是导航信息，沉在正文末尾等于看不见。
 */
export function WikiPageDetail() {
  const { t } = useTranslation();
  const { showToast } = useToast();
  const detail = useWikiStore((state) => state.pageDetail);
  const selectedPageId = useWikiStore((state) => state.selectedPageId);
  const pageRevisions = useWikiStore((state) => state.pageRevisions);
  const restorePreviousRevision = useWikiStore(
    (state) => state.restorePreviousRevision,
  );
  const savePageBody = useWikiStore((state) => state.savePageBody);
  const deletePage = useWikiStore((state) => state.deletePage);
  const openByLinkTarget = useWikiStore((state) => state.openByLinkTarget);
  const selectPage = useWikiStore((state) => state.selectPage);
  const selectItem = useKnowledgeStore((state) => state.selectItem);
  const setAppModule = useUIStore((state) => state.setAppModule);

  const scrollRef = useRef<HTMLDivElement>(null);
  const moreButtonRef = useRef<HTMLButtonElement>(null);
  const [draft, setDraft] = useState<string | null>(null);
  const [menuAnchor, setMenuAnchor] = useState<{ x: number; y: number } | null>(
    null,
  );
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);

  // 切页时丢弃未提交的草稿，避免把 A 的编辑写进 B
  useEffect(() => {
    setDraft(null);
    scrollRef.current?.scrollTo({ top: 0 });
  }, [selectedPageId]);

  if (!selectedPageId) {
    return (
      <div className="flex h-full flex-1 items-center justify-center px-8 text-center text-sm text-muted-foreground">
        {t("wiki.noSelection", "在左侧选择一个页面")}
      </div>
    );
  }

  if (!detail) {
    return (
      <div className="flex h-full flex-1 items-center justify-center">
        <Spinner size="sm" tone="muted" />
      </div>
    );
  }

  const { page, backlinks, sources } = detail;
  // 老页面的正文里还留着与标题重复的一级标题（落库侧的清洗只对新编译生效）
  const renderedBody = stripDuplicateTitleHeading(page.body, page.title);
  const previousRevision = pageRevisions[0];

  const navigateLink = (target: string) => {
    void openByLinkTarget(target).then((found) => {
      if (!found) {
        showToast(
          t("wiki.linkNotFound", "页面「{{target}}」不存在", { target }),
          "info",
        );
      }
    });
  };

  const openSourceItem = async (itemId: string) => {
    setAppModule("library");
    await selectItem(itemId);
  };

  /** 写操作的统一回执：失败原因（若有）折进 toast 的「查看详情」 */
  const notifyMutation = (
    result: { ok: boolean; error?: string },
    successMessage: string,
    failureMessage: string,
  ) => {
    showToast(
      result.ok ? successMessage : failureMessage,
      result.ok ? "success" : "error",
      result.error ? { detail: result.error } : undefined,
    );
  };

  const restorePrevious = async () => {
    notifyMutation(
      await restorePreviousRevision(),
      t("wiki.revisionRestored", "已恢复上一版内容"),
      t("wiki.revisionRestoreFailed", "恢复失败"),
    );
  };

  const saveDraft = async () => {
    if (draft === null) {
      return;
    }
    const result = await savePageBody(draft);
    if (result.ok) {
      setDraft(null);
    }
    notifyMutation(
      result,
      t("wiki.pageSaved", "已保存，后续编译不会覆盖这一页"),
      t("wiki.pageSaveFailed", "保存失败"),
    );
  };

  const releaseToAuto = async () => {
    notifyMutation(
      await savePageBody(page.body, true),
      t("wiki.releasedToAuto", "已交回自动编译"),
      t("wiki.pageSaveFailed", "保存失败"),
    );
  };

  const confirmDelete = async () => {
    setShowDeleteConfirm(false);
    notifyMutation(
      await deletePage(page.id),
      t("wiki.pageDeleted", "页面已删除"),
      t("wiki.pageDeleteFailed", "删除失败"),
    );
  };

  const menuItems: ContextMenuItem[] = [
    ...(previousRevision
      ? [
          {
            label: t("wiki.restorePrevious", "恢复上一版"),
            description: t("wiki.restorePreviousHint", "恢复到 {{time}} 的内容", {
              time: formatItemTime(previousRevision.createdAt),
            }),
            icon: <RotateCcwIcon className="h-4 w-4" aria-hidden="true" />,
            onClick: () => void restorePrevious(),
          },
        ]
      : []),
    ...(page.manualEditedAt
      ? [
          {
            label: t("wiki.releaseToAuto", "交回自动编译"),
            description: t(
              "wiki.releaseToAutoHint",
              "清除手动标记，让后续编译重新接管这一页",
            ),
            icon: <WandSparklesIcon className="h-4 w-4" aria-hidden="true" />,
            onClick: () => void releaseToAuto(),
          },
        ]
      : []),
    {
      label: t("wiki.deletePage", "删除页面"),
      icon: <Trash2Icon className="h-4 w-4" aria-hidden="true" />,
      variant: "destructive" as const,
      onClick: () => setShowDeleteConfirm(true),
    },
  ];

  return (
    <div ref={scrollRef} className="flex h-full min-h-0 flex-1 flex-col overflow-y-auto">
      <div className="mx-auto w-full max-w-3xl px-6 py-5">
        <div className="mb-1.5 flex items-start gap-2">
          <h1 className="min-w-0 flex-1 text-xl font-semibold leading-tight text-foreground">
            {page.title}
          </h1>
          <WikiKindBadge kind={page.kind} />
        </div>

        <div className="mb-4 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground/70">
          <span>
            {page.manualEditedAt
              ? t("wiki.manualEditedAt", "手动编辑于 {{time}}", {
                  time: formatItemTime(page.manualEditedAt),
                })
              : t("wiki.generatedAt", "由 {{model}} 生成于 {{time}}", {
                  model: page.model || "AI",
                  time: formatItemTime(page.generatedAt),
                })}
          </span>
          {page.manualEditedAt ? (
            <span
              title={t(
                "wiki.manualEditedHint",
                "这一页已被手动改过，后续编译不会覆盖它的正文",
              )}
              className="inline-flex items-center gap-1 rounded-md border border-primary/40 bg-primary/10 px-1.5 py-0.5 text-[11px] text-primary"
            >
              <PencilIcon className="h-3 w-3" aria-hidden="true" />
              {t("wiki.manualEdited", "手动编辑")}
            </span>
          ) : null}
          <span className="min-w-0 flex-1" />
          {draft === null ? (
            <button
              type="button"
              onClick={() => setDraft(page.body)}
              className="inline-flex h-7 items-center gap-1 rounded-md border border-border px-2 text-[11px] text-foreground transition-colors hover:bg-accent"
            >
              <PencilIcon className="h-3 w-3" aria-hidden="true" />
              {t("wiki.editPage", "编辑")}
            </button>
          ) : null}
          <button
            ref={moreButtonRef}
            type="button"
            onClick={(event) =>
              setMenuAnchor(
                menuAnchor
                  ? null
                  : { x: event.clientX, y: event.currentTarget.getBoundingClientRect().bottom + 4 },
              )
            }
            aria-label={t("common.more", "更多")}
            className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
          >
            <MoreHorizontalIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </button>
        </div>

        {backlinks.length > 0 ? (
          <div className="mb-4">
            <RelatedChips
              label={t("wiki.backlinks", "反向链接")}
              icon={<Link2Icon className="h-3.5 w-3.5" aria-hidden="true" />}
              entries={backlinks}
              onOpen={(id) => void selectPage(id)}
            />
          </div>
        ) : null}

        {draft === null ? (
          <>
            <PageToc body={renderedBody} scrollRef={scrollRef} />
            <WikiMarkdown body={renderedBody} onNavigate={navigateLink} />
          </>
        ) : (
          <div className="space-y-2">
            <textarea
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              rows={20}
              className="w-full resize-y rounded-xl border border-border bg-background/60 p-3 font-mono text-[13px] leading-relaxed text-foreground focus:border-primary/50 focus:outline-none"
            />
            <p className="text-[11px] text-muted-foreground/70">
              {t(
                "wiki.editPageHint",
                "保存后这一页会被标记为手动编辑，后续编译不再覆盖它的正文。[[链接]] 仍然有效。",
              )}
            </p>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => void saveDraft()}
                className="inline-flex h-8 items-center gap-1.5 rounded-lg bg-primary px-3 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90"
              >
                {t("common.save", "保存")}
              </button>
              <button
                type="button"
                onClick={() => setDraft(null)}
                className="inline-flex h-8 items-center rounded-lg border border-border px-3 text-sm text-foreground transition-colors hover:bg-muted/60"
              >
                {t("common.cancel", "取消")}
              </button>
            </div>
          </div>
        )}

        {sources.length > 0 ? (
          <div className="mt-6 border-t border-border/60 pt-4">
            <RelatedChips
              label={t("wiki.sources", "来源条目")}
              icon={<BookOpenIcon className="h-3.5 w-3.5" aria-hidden="true" />}
              entries={sources.map((source) => ({
                id: source.itemId,
                title: source.title,
              }))}
              onOpen={(id) => void openSourceItem(id)}
            />
          </div>
        ) : null}
      </div>

      {menuAnchor ? (
        <ContextMenu
          x={menuAnchor.x}
          y={menuAnchor.y}
          items={menuItems}
          ignoreRef={moreButtonRef}
          onClose={() => setMenuAnchor(null)}
        />
      ) : null}

      {showDeleteConfirm ? (
        <ConfirmDialog
          isOpen={showDeleteConfirm}
          title={t("wiki.deletePage", "删除页面")}
          message={t(
            "wiki.deletePageConfirm",
            "删除「{{title}}」？指向它的链接会变成断链，下一轮编译可能重新生成这一页。",
            { title: page.title },
          )}
          confirmText={t("common.delete", "删除")}
          variant="destructive"
          onConfirm={() => void confirmDelete()}
          onClose={() => setShowDeleteConfirm(false)}
        />
      ) : null}
    </div>
  );
}
