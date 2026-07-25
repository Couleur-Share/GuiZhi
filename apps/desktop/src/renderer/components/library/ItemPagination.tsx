import { ChevronLeftIcon, ChevronRightIcon } from "lucide-react";
import { useTranslation } from "react-i18next";
import {
  PAGE_SIZE_OPTIONS,
  useKnowledgeStore,
} from "../../stores/knowledge.store";

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
 */
export function ItemPagination() {
  const { t } = useTranslation();
  const total = useKnowledgeStore((state) => state.total);
  const page = useKnowledgeStore((state) => state.page);
  const pageSize = useKnowledgeStore((state) => state.pageSize);
  const setPage = useKnowledgeStore((state) => state.setPage);
  const setPageSize = useKnowledgeStore((state) => state.setPageSize);

  if (total === 0) {
    return null;
  }

  const totalPages = Math.max(1, Math.ceil(total / pageSize));

  return (
    <div
      data-testid="item-pagination"
      className="flex shrink-0 items-center justify-between gap-4 border-t border-border px-4 py-2.5 text-xs text-muted-foreground"
    >
      <span>{t("library.itemCount", "共 {{count}} 个", { count: total })}</span>

      <div className="flex items-center gap-4">
        <label className="flex items-center gap-2">
          <span>{t("library.pageSize", "每页")}</span>
          <select
            value={pageSize}
            onChange={(event) => setPageSize(Number(event.target.value))}
            className="rounded-md border border-border bg-muted px-2 py-1 text-xs text-foreground"
          >
            {PAGE_SIZE_OPTIONS.map((size) => (
              <option key={size} value={size}>
                {size}
              </option>
            ))}
          </select>
        </label>

        {totalPages > 1 ? (
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => setPage(page - 1)}
              disabled={page === 1}
              aria-label={t("library.previousPage", "上一页")}
              className="rounded-md p-1.5 transition-colors hover:bg-accent disabled:cursor-not-allowed disabled:opacity-40"
            >
              <ChevronLeftIcon className="h-4 w-4" aria-hidden="true" />
            </button>
            {buildPageWindow(page, totalPages).map((candidate) => (
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
            ))}
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
    </div>
  );
}
