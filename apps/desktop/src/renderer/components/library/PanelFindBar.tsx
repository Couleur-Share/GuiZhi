import { type Dispatch, type RefObject, type SetStateAction } from "react";
import {
  ChevronDownIcon,
  ChevronUpIcon,
  SearchIcon,
  XIcon,
} from "lucide-react";
import { useTranslation } from "react-i18next";

/**
 * 内容面板工具栏共用查找条：输入、计数、上一个/下一个。
 * 讨论是「过滤楼层」，总结/正文/文字稿是「mark 间跳转」，壳相同。
 */
export function PanelFindBar({
  query,
  onQueryChange,
  activeIndex,
  matchCount,
  onActiveIndexChange,
  placeholder,
  inputRef,
  onClose,
}: {
  query: string;
  onQueryChange: (query: string) => void;
  activeIndex: number;
  matchCount: number;
  onActiveIndexChange: Dispatch<SetStateAction<number>>;
  placeholder: string;
  inputRef?: RefObject<HTMLInputElement>;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const filtering = query.trim().length > 0;
  const hasMatches = filtering && matchCount > 0;

  const goPrev = () => {
    if (!hasMatches) {
      return;
    }
    onActiveIndexChange((index) => (index - 1 + matchCount) % matchCount);
  };
  const goNext = () => {
    if (!hasMatches) {
      return;
    }
    onActiveIndexChange((index) => (index + 1) % matchCount);
  };

  return (
    <div className="flex h-8 w-72 max-w-[calc(100%_-_1rem)] items-center gap-1 rounded-lg border border-border bg-background/95 p-1 shadow-lg backdrop-blur-sm">
      <div className="flex h-6 min-w-0 flex-1 items-center gap-1 px-1.5">
        <SearchIcon
          className="h-3 w-3 shrink-0 text-muted-foreground/70"
          aria-hidden="true"
        />
        <input
          ref={inputRef}
          type="search"
          value={query}
          onChange={(event) => onQueryChange(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              if (event.shiftKey) {
                goPrev();
              } else {
                goNext();
              }
            } else if (event.key === "Escape") {
              event.preventDefault();
              onClose();
            }
          }}
          placeholder={placeholder}
          className="min-w-0 flex-1 bg-transparent text-[11px] text-foreground placeholder:text-muted-foreground/60 focus:outline-none"
          aria-label={placeholder}
        />
        {query ? (
          <button
            type="button"
            onClick={() => onQueryChange("")}
            aria-label={t("header.clearSearch", "清除搜索")}
            className="flex h-4 w-4 shrink-0 items-center justify-center rounded text-muted-foreground transition-colors hover:text-foreground"
          >
            <XIcon className="h-3 w-3" aria-hidden="true" />
          </button>
        ) : null}
      </div>
      {filtering ? (
        <>
          <span className="shrink-0 tabular-nums text-[10px] text-muted-foreground/80">
            {hasMatches
              ? t("library.panelFindMatchIndex", "{{current}} / {{total}}", {
                  current: activeIndex + 1,
                  total: matchCount,
                })
              : t("library.panelFindEmptyShort", "无匹配")}
          </span>
          <button
            type="button"
            onClick={goPrev}
            disabled={!hasMatches}
            aria-label={t("library.panelFindPrevMatch", "上一个匹配")}
            className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground disabled:opacity-40"
          >
            <ChevronUpIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </button>
          <button
            type="button"
            onClick={goNext}
            disabled={!hasMatches}
            aria-label={t("library.panelFindNextMatch", "下一个匹配")}
            className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground disabled:opacity-40"
          >
            <ChevronDownIcon className="h-3.5 w-3.5" aria-hidden="true" />
          </button>
        </>
      ) : null}
      <button
        type="button"
        onClick={onClose}
        aria-label={t("common.close", "关闭")}
        className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent/60 hover:text-foreground"
      >
        <XIcon className="h-3.5 w-3.5" aria-hidden="true" />
      </button>
    </div>
  );
}
