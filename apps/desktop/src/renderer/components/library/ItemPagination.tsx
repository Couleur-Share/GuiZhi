import { useMemo } from "react";
import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  PAGE_SIZE_OPTIONS,
  useKnowledgeStore,
} from "../../stores/knowledge.store";
import { Select } from "../ui/Select";

/** 页码按钮最多显示几个 */
const PAGE_WINDOW = 5;

/** 以当前页为中心的页码窗口 */
export function buildPageWindow(current: number, total: number): number[] {
  const size = Math.min(PAGE_WINDOW, total);
  const start =
    total <= PAGE_WINDOW
      ? 1
      : Math.min(Math.max(current - 2, 1), total - PAGE_WINDOW + 1);
  return Array.from({ length: size }, (_, index) => start + index);
}

/**
 * 列表底部分页条。
 *
 * 分页在服务端做（store 的 page / pageSize 直接进 SQL 的 LIMIT/OFFSET），
 * 卡片视图与列表视图共用同一套状态，两边看到的数据范围始终一致。
 *
 * 这里不再显示条目总数：顶部工具条（`ItemListToolbar`）已经有一份，而这条与
 * 那条在两个视图里总是成对出现，底下这份纯属重复。它还是折行的直接原因——
 * 卡片视图的列表栏最窄只有 240px，总数 + 每页 + 页码按钮挤在一行，flex 压缩
 * 后每块文字各自折成两行。
 *
 * 同理页码按钮只在全宽表格（`wide`）里摆：七颗按钮就要 220px，窄栏根本放不下，
 * 改用「‹ 1 / 2 ›」。窄栏即便被拖宽也保持紧凑形态，省得同一个控件在拖动过程中
 * 变形。
 */
export function ItemPagination({ wide = false }: { wide?: boolean }) {
  const { t } = useTranslation();
  const total = useKnowledgeStore((state) => state.total);
  const page = useKnowledgeStore((state) => state.page);
  const pageSize = useKnowledgeStore((state) => state.pageSize);
  const setPage = useKnowledgeStore((state) => state.setPage);
  const setPageSize = useKnowledgeStore((state) => state.setPageSize);
  const pageSizeOptions = useMemo(
    () =>
      PAGE_SIZE_OPTIONS.map((size) => ({
        value: String(size),
        label: String(size),
      })),
    [],
  );

  if (total === 0) {
    return null;
  }

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div
      data-testid="item-pagination"
      className="flex shrink-0 items-center justify-between gap-3 border-t border-border px-3 py-2 text-xs text-muted-foreground"
    >
      <div className="flex min-w-0 items-center gap-2">
        {/* 放不下时截断标签，而不是让整条折行 */}
        <span className="truncate">{t("library.pageSize", "每页")}</span>
        <Select
          value={String(pageSize)}
          onChange={(value) => setPageSize(Number(value))}
          options={pageSizeOptions}
          ariaLabel={t("library.pageSize", "每页")}
          className="shrink-0"
          menuMinWidth={88}
          triggerClassName="flex h-7 min-w-[56px] cursor-pointer items-center justify-between gap-1 rounded-md border border-border bg-muted px-2 text-xs text-foreground transition-colors hover:bg-accent focus:outline-none focus-visible:ring-2 focus-visible:ring-primary/50"
        />
      </div>

      {totalPages > 1 ? (
        <div className="flex shrink-0 items-center gap-1">
          <button
            type="button"
            onClick={() => setPage(page - 1)}
            disabled={page === 1}
            aria-label={t("library.previousPage", "上一页")}
            className="rounded-md p-1.5 transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
          >
            <ChevronLeftIcon className="h-4 w-4" aria-hidden="true" />
          </button>
          {wide ? (
            buildPageWindow(page, totalPages).map((candidate) => (
              <button
                key={candidate}
                type="button"
                onClick={() => setPage(candidate)}
                aria-label={t("library.pageNumber", "第 {{page}} 页", {
                  page: candidate,
                })}
                aria-current={candidate === page ? "page" : undefined}
                className={`h-7 w-7 rounded-md transition-colors ${
                  candidate === page
                    ? "bg-primary text-primary-foreground"
                    : "hover:bg-accent"
                }`}
              >
                {candidate}
              </button>
            ))
          ) : (
            // tabular-nums：页码变宽时不让两侧箭头跟着挪位
            <span className="px-1 tabular-nums text-foreground">
              {page} / {totalPages}
            </span>
          )}
          <button
            type="button"
            onClick={() => setPage(page + 1)}
            disabled={page === totalPages}
            aria-label={t("library.nextPage", "下一页")}
            className="rounded-md p-1.5 transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
          >
            <ChevronRightIcon className="h-4 w-4" aria-hidden="true" />
          </button>
        </div>
      ) : null}
    </div>
  );
}
