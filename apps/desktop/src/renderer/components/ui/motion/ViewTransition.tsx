import { type HTMLAttributes, type ReactNode, type Key } from "react";
import { clsx } from "clsx";

export type ViewTransitionProps = HTMLAttributes<HTMLDivElement> & {
  /**
   * The key of the currently active subtree. When this changes the
   * subtree remounts and the inner div animates in.
   */
  activeKey: Key;
  children: ReactNode;
  /** Override the default duration token (defaults to base, 180ms). */
  durationToken?: "quick" | "base" | "smooth";
};

/**
 * Cross-fade between sibling views (list <-> gallery <-> kanban, settings
 * subsections, etc).
 *
 * The outer wrapper is stable; React `key` invalidation on the inner div
 * triggers the fade-in. We rely on `tailwindcss-animate`'s `animate-in`
 * for the entry animation; the previous tree disappears synchronously on
 * key change. Skipping AnimatePresence keeps the runtime cost flat and
 * avoids pulling framer-motion into the import graph.
 *
 * 视图切换的 cross-fade 容器（list 与 gallery、设置子面板等）。外层稳定，
 * 内层 key 变化触发 React remount 与 animate-in 入场动画；旧树同步消失。
 * 不引入 AnimatePresence，保持轻量并避免拉入 framer-motion。
 */
export function ViewTransition({
  activeKey,
  children,
  durationToken = "base",
  className,
  ...rest
}: ViewTransitionProps) {
  return (
    <div {...rest} className={clsx("relative", className)}>
      <div
        key={activeKey}
        className={clsx(
          "h-full w-full animate-in fade-in ease-enter",
          `duration-${durationToken}`,
        )}
      >
        {children}
      </div>
    </div>
  );
}
