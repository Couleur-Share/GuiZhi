import { forwardRef, type HTMLAttributes } from "react";
import { clsx } from "clsx";

export type RevealIntent = "enter" | "exit";

export type RevealVariant = "fade" | "fade-zoom" | "fade-slide-up" | "fade-slide-down";

export type RevealProps = HTMLAttributes<HTMLDivElement> & {
  /**
   * Whether the wrapper is animating in or out. Defaults to `enter`.
   * Use `exit` while the element is being removed (paired with a deferred
   * unmount in the parent).
   */
  intent?: RevealIntent;
  /**
   * Animation flavor. `fade-zoom` is the default and matches modal /
   * popover entrance; the slide variants suit toasts and dropdowns.
   */
  variant?: RevealVariant;
  /** Override the default duration token. */
  durationToken?: "instant" | "quick" | "base" | "smooth" | "slow";
};

const ENTER_BASE = "animate-in fade-in";
const EXIT_BASE = "animate-out fade-out";

const VARIANT_ENTER: Record<RevealVariant, string> = {
  fade: "",
  "fade-zoom": "zoom-in-95",
  "fade-slide-up": "slide-in-from-bottom-1",
  "fade-slide-down": "slide-in-from-top-1",
};

const VARIANT_EXIT: Record<RevealVariant, string> = {
  fade: "",
  "fade-zoom": "zoom-out-95",
  "fade-slide-up": "slide-out-to-bottom-1",
  "fade-slide-down": "slide-out-to-top-1",
};

/**
 * Intent-driven mount/unmount wrapper. Encapsulates `tailwindcss-animate`
 * so callers say what they want ("entering as a fade-zoom"), not how
 * (timings, easing, individual class names).
 *
 * Defaults: enter -> duration-base ease-enter; exit -> duration-quick
 * ease-exit.
 *
 * 意图驱动的入场 / 出场包装。把 tailwindcss-animate 的细节封住，调用方只
 * 描述意图（"以 fade-zoom 入场"），不关心时长 / 曲线 / class 名。
 *
 * 默认值：入场 duration-base ease-enter；出场 duration-quick ease-exit。
 */
export const Reveal = forwardRef<HTMLDivElement, RevealProps>(function Reveal(
  {
    intent = "enter",
    variant = "fade-zoom",
    durationToken,
    className,
    children,
    ...rest
  },
  ref,
) {
  const isEnter = intent === "enter";
  const base = isEnter ? ENTER_BASE : EXIT_BASE;
  const variantClass = isEnter ? VARIANT_ENTER[variant] : VARIANT_EXIT[variant];

  // tailwindcss-animate doesn't expose token-named utilities natively, so
  // pick the right Tailwind class based on the token.
  const durationClass = durationToken
    ? `duration-${durationToken}`
    : isEnter
      ? "duration-base"
      : "duration-quick";
  const easingClass = isEnter ? "ease-enter" : "ease-exit";

  return (
    <div
      ref={ref}
      {...rest}
      className={clsx(base, variantClass, durationClass, easingClass, className)}
    >
      {children}
    </div>
  );
});
