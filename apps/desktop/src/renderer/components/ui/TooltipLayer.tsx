import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

/**
 * 全局提示层：接管所有带 `title` 的元素。
 *
 * 浏览器对 `title` 渲染的是系统级提示框——浅色底、系统字体、约一秒延迟，
 * 在深色玻璃拟态界面里非常突兀，而且没有任何 CSS 能改它。这里在悬停时
 * 把 `title` 摘下来自己渲染，移开时再装回去，因此调用方不需要任何改动，
 * 也不会永久丢掉这个可访问名。
 */

const SHOW_DELAY_MS = 320;
const VIEWPORT_MARGIN = 8;
const TRIGGER_GAP = 8;

interface TooltipState {
  text: string;
  top: number;
  left: number;
}

export function TooltipLayer() {
  const [tooltip, setTooltip] = useState<TooltipState | null>(null);
  const bubbleRef = useRef<HTMLDivElement>(null);
  const hostRef = useRef<HTMLElement | null>(null);
  const titleRef = useRef<string>("");
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pressedRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    const restoreTitle = () => {
      const host = hostRef.current;
      if (host && titleRef.current) {
        host.setAttribute("title", titleRef.current);
      }
      hostRef.current = null;
      titleRef.current = "";
    };

    const dismiss = () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      restoreTitle();
      setTooltip(null);
    };

    const schedule = (host: HTMLElement) => {
      const title = host.getAttribute("title")?.trim();
      if (!title || host === hostRef.current) {
        return;
      }

      dismiss();
      hostRef.current = host;
      titleRef.current = title;
      // 先摘掉 title，系统提示框才不会和我们的气泡同时出现
      host.removeAttribute("title");

      timerRef.current = setTimeout(() => {
        timerRef.current = null;
        if (!host.isConnected) {
          dismiss();
          return;
        }
        const rect = host.getBoundingClientRect();
        setTooltip({
          text: title,
          top: rect.bottom + TRIGGER_GAP,
          left: rect.left + rect.width / 2,
        });
      }, SHOW_DELAY_MS);
    };

    const resolveHost = (target: EventTarget | null): HTMLElement | null => {
      if (!(target instanceof Element)) {
        return null;
      }
      // 悬停中的宿主 title 已被摘走，closest 找不到它，只能认 hostRef
      if (hostRef.current?.contains(target)) {
        return hostRef.current;
      }
      return target.closest<HTMLElement>("[title]");
    };

    const handlePointerOver = (event: PointerEvent) => {
      const target = event.target;
      if (!(target instanceof Element)) {
        return;
      }

      const pressed = pressedRef.current;
      if (pressed) {
        if (pressed.contains(target)) {
          return;
        }
        pressedRef.current = null;
      }

      // 当前宿主的 title 已被摘走，closest 会越过它命中外层带 title 的祖先，
      // 于是两个提示互相顶掉。只要指针还在宿主内就什么都不做。
      if (hostRef.current?.contains(target)) {
        return;
      }

      const host = target.closest<HTMLElement>("[title]");
      if (!host) {
        dismiss();
        return;
      }
      schedule(host);
    };

    const handlePointerOut = (event: PointerEvent) => {
      const next = event.relatedTarget;
      const stillInside = (element: HTMLElement | null) =>
        Boolean(element && next instanceof Node && element.contains(next));

      if (!stillInside(pressedRef.current)) {
        pressedRef.current = null;
      }

      const host = hostRef.current;
      if (!host || stillInside(host)) {
        return;
      }
      dismiss();
    };

    // 点击后按钮会拿到焦点，focusin 若照常排期，气泡就会盖在这一下点开的
    // 菜单/弹窗上。记住按下的宿主，等指针离开它再恢复提示。
    const handlePointerDown = (event: PointerEvent) => {
      const pressed = resolveHost(event.target);
      dismiss();
      pressedRef.current = pressed;
    };

    const handleFocusIn = (event: FocusEvent) => {
      const target = event.target;
      if (target instanceof Element) {
        if (pressedRef.current?.contains(target)) {
          return;
        }
        const host = target.closest<HTMLElement>("[title]");
        if (host) {
          schedule(host);
          return;
        }
      }
      dismiss();
    };

    document.addEventListener("pointerover", handlePointerOver, true);
    document.addEventListener("pointerout", handlePointerOut, true);
    document.addEventListener("pointerdown", handlePointerDown, true);
    document.addEventListener("focusin", handleFocusIn, true);
    document.addEventListener("keydown", dismiss, true);
    window.addEventListener("scroll", dismiss, true);
    window.addEventListener("blur", dismiss);

    return () => {
      document.removeEventListener("pointerover", handlePointerOver, true);
      document.removeEventListener("pointerout", handlePointerOut, true);
      document.removeEventListener("pointerdown", handlePointerDown, true);
      document.removeEventListener("focusin", handleFocusIn, true);
      document.removeEventListener("keydown", dismiss, true);
      window.removeEventListener("scroll", dismiss, true);
      window.removeEventListener("blur", dismiss);
      pressedRef.current = null;
      dismiss();
    };
  }, []);

  // 气泡宽度要等渲染出来才知道，量到之后再夹回视口内
  useEffect(() => {
    const bubble = bubbleRef.current;
    if (!tooltip || !bubble) {
      return;
    }

    const rect = bubble.getBoundingClientRect();
    const maxLeft = window.innerWidth - rect.width / 2 - VIEWPORT_MARGIN;
    const minLeft = rect.width / 2 + VIEWPORT_MARGIN;
    const clampedLeft = Math.min(Math.max(tooltip.left, minLeft), maxLeft);
    const overflowsBottom =
      tooltip.top + rect.height + VIEWPORT_MARGIN > window.innerHeight;
    const clampedTop = overflowsBottom
      ? Math.max(VIEWPORT_MARGIN, tooltip.top - rect.height - TRIGGER_GAP * 2)
      : tooltip.top;

    if (clampedLeft !== tooltip.left || clampedTop !== tooltip.top) {
      setTooltip({ ...tooltip, left: clampedLeft, top: clampedTop });
    }
  }, [tooltip]);

  if (!tooltip || typeof document === "undefined") {
    return null;
  }

  return createPortal(
    <div
      ref={bubbleRef}
      role="tooltip"
      className="pointer-events-none fixed z-[100000] max-w-xs -translate-x-1/2 whitespace-pre-line break-words rounded-md border border-border bg-popover px-2 py-1 text-xs leading-relaxed text-popover-foreground shadow-lg animate-in fade-in-0 duration-quick"
      style={{ top: tooltip.top, left: tooltip.left }}
    >
      {tooltip.text}
    </div>,
    document.body,
  );
}

export default TooltipLayer;
