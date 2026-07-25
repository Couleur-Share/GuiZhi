import { forwardRef, type ButtonHTMLAttributes } from "react";
import { clsx } from "clsx";

export type PressableProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  /** Disable the press-in scale effect (e.g. for icon-only chrome buttons). */
  disablePressEffect?: boolean;
};

/**
 * Standardized button microfeedback wrapper.
 *
 * Replaces ad-hoc `active:scale-press-in` / `active:scale-press-in` writes scattered
 * across the codebase. Always uses `scale-press-in` (0.95) plus a fast
 * transform transition so the press feels immediate but consistent.
 *
 * 标准化按钮按下微反馈。替代仓库里散落的 `active:scale-press-in` / `active:scale-press-in`
 * 写法，统一使用 `scale-press-in` (0.95) 与较快的 transform 过渡，让按下手感
 * 一致而即时。
 */
export const Pressable = forwardRef<HTMLButtonElement, PressableProps>(
  function Pressable(
    { className, children, disablePressEffect, type = "button", ...rest },
    ref,
  ) {
    return (
      <button
        ref={ref}
        type={type}
        {...rest}
        className={clsx(
          !disablePressEffect &&
            "transition-transform duration-instant ease-standard active:scale-press-in",
          className,
        )}
      >
        {children}
      </button>
    );
  },
);
