import { useLayoutEffect, useRef, type RefObject } from "react";
import { scrollElementIntoContainer } from "./scroll-into-container";

/**
 * 在滚动容器内统计 <mark> 并滚到当前命中。
 * 用于总结 / 正文 / 文字稿（不裁切内容，只跳转高亮）。
 */
export function useMarkFindNavigation({
  containerRef,
  query,
  activeIndex,
  onMatchCountChange,
  /** 内容变化时重扫（如切条目、文字稿更新） */
  contentKey,
}: {
  containerRef: RefObject<HTMLElement | null>;
  query: string;
  activeIndex: number;
  onMatchCountChange: (count: number) => void;
  contentKey: string;
}): void {
  const lastCountRef = useRef(-1);

  useLayoutEffect(() => {
    const container = containerRef.current;
    const needle = query.trim();
    if (!container || !needle) {
      if (lastCountRef.current !== 0) {
        lastCountRef.current = 0;
        onMatchCountChange(0);
      }
      return;
    }

    const marks = Array.from(container.querySelectorAll("mark"));
    const count = marks.length;
    if (count !== lastCountRef.current) {
      lastCountRef.current = count;
      onMatchCountChange(count);
    }
    if (count === 0) {
      return;
    }
    const index = ((activeIndex % count) + count) % count;
    const target = marks[index];
    if (target instanceof HTMLElement) {
      scrollElementIntoContainer(container, target);
    }
  }, [activeIndex, contentKey, containerRef, onMatchCountChange, query]);
}
