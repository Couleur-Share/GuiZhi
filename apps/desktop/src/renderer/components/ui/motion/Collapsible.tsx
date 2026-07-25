import { forwardRef, type HTMLAttributes, type ReactNode } from "react";
import { clsx } from "clsx";

export type CollapsibleProps = HTMLAttributes<HTMLDivElement> & {
  /** Whether the content is visible. */
  open: boolean;
  /** The content to collapse / reveal. */
  children: ReactNode;
  /** Override the default duration token (defaults to smooth, 280ms). */
  durationToken?: "quick" | "base" | "smooth" | "slow";
};

/**
 * CSS-only collapsible container.
 *
 * Uses the `grid-rows-[0fr]` <-> `grid-rows-[1fr]` trick: the outer grid
 * row interpolates from 0fr to 1fr, while the inner `min-h-0 overflow-hidden`
 * child shrinks accordingly. No JS height measurement, no layout shift
 * jitter, no need for ResizeObserver.
 *
 * 纯 CSS 折叠容器。利用 grid-rows 从 0fr 过渡到 1fr 的技巧实现高度动画，
 * 不需要 JS 测高，不需要 ResizeObserver，没有布局抖动。
 */
export const Collapsible = forwardRef<HTMLDivElement, CollapsibleProps>(
  function Collapsible(
    { open, children, durationToken = "smooth", className, ...rest },
    ref,
  ) {
    return (
      <div
        ref={ref}
        {...rest}
        data-state={open ? "open" : "closed"}
        className={clsx(
          "grid transition-[grid-template-rows] ease-emphasized",
          `duration-${durationToken}`,
          open ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
          className,
        )}
        aria-hidden={!open}
      >
        <div className="min-h-0 overflow-hidden">{children}</div>
      </div>
    );
  },
);
