import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TooltipLayer } from "../../../src/renderer/components/ui/TooltipLayer";

/**
 * TooltipLayer 是全局劫持型组件：悬停时把 `title` 摘下来自绘，移开再装回去。
 * 这里盯住的是「摘」与「还」这一对必须闭合的动作——漏还就等于永久抹掉了
 * 元素的可访问名。
 */

const TITLE = "重新生成讨论总结";

interface RectSpec {
  top: number;
  left: number;
  width: number;
  height: number;
}

function toRect(spec: RectSpec): DOMRect {
  return {
    ...spec,
    x: spec.left,
    y: spec.top,
    right: spec.left + spec.width,
    bottom: spec.top + spec.height,
    toJSON: () => ({}),
  };
}

/** jsdom 不做布局，getBoundingClientRect 恒为 0，定位逻辑得自己喂尺寸 */
function stubLayout(host: RectSpec, bubble: RectSpec) {
  vi.spyOn(Element.prototype, "getBoundingClientRect").mockImplementation(
    function (this: Element) {
      return toRect(this.getAttribute("role") === "tooltip" ? bubble : host);
    },
  );
}

function renderScene() {
  return render(
    <>
      <TooltipLayer />
      <button type="button" data-testid="host" title={TITLE}>
        <span data-testid="icon">icon</span>
      </button>
      <button type="button" data-testid="plain">
        无提示
      </button>
    </>,
  );
}

function hover(element: HTMLElement) {
  fireEvent.pointerOver(element);
  act(() => {
    vi.advanceTimersByTime(400);
  });
}

describe("TooltipLayer", () => {
  beforeEach(() => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("悬停立即摘掉 title，延迟到了才渲染自绘气泡", () => {
    renderScene();
    const host = screen.getByTestId("host");

    fireEvent.pointerOver(host);
    expect(host.hasAttribute("title")).toBe(false);
    expect(screen.queryByRole("tooltip")).toBeNull();

    act(() => {
      vi.advanceTimersByTime(400);
    });
    expect(screen.getByRole("tooltip").textContent).toBe(TITLE);
  });

  it("移开后收起气泡并把 title 装回去", () => {
    renderScene();
    const host = screen.getByTestId("host");

    hover(host);
    expect(screen.getByRole("tooltip")).toBeTruthy();

    fireEvent.pointerOut(host, { relatedTarget: screen.getByTestId("plain") });
    expect(screen.queryByRole("tooltip")).toBeNull();
    expect(host.getAttribute("title")).toBe(TITLE);
  });

  it("指针移到子元素上不算离开，气泡保持不动", () => {
    renderScene();
    const host = screen.getByTestId("host");
    const icon = screen.getByTestId("icon");

    hover(host);
    fireEvent.pointerOut(host, { relatedTarget: icon });
    hover(icon);

    expect(screen.getByRole("tooltip").textContent).toBe(TITLE);
    expect(host.hasAttribute("title")).toBe(false);
  });

  it("按下指针会收起，且不吞掉 title", () => {
    renderScene();
    const host = screen.getByTestId("host");

    hover(host);
    fireEvent.pointerDown(host);

    expect(screen.queryByRole("tooltip")).toBeNull();
    expect(host.getAttribute("title")).toBe(TITLE);
  });

  it("点击后焦点落回按钮，气泡不再冒出来盖住刚打开的菜单", () => {
    renderScene();
    const host = screen.getByTestId("host");

    hover(host);
    fireEvent.pointerDown(host);
    // 浏览器会在 mousedown 的默认行为里把焦点给按钮
    fireEvent.focusIn(host);
    act(() => {
      vi.advanceTimersByTime(600);
    });

    expect(screen.queryByRole("tooltip")).toBeNull();
    expect(host.getAttribute("title")).toBe(TITLE);
  });

  it("点击后指针移开再回来，提示恢复正常", () => {
    renderScene();
    const host = screen.getByTestId("host");
    const plain = screen.getByTestId("plain");

    hover(host);
    fireEvent.pointerDown(host);
    fireEvent.pointerOut(host, { relatedTarget: plain });
    fireEvent.pointerOver(plain);
    hover(host);

    expect(screen.getByRole("tooltip").textContent).toBe(TITLE);
  });

  it("延迟未到就移开，气泡不会迟到出现", () => {
    renderScene();
    const host = screen.getByTestId("host");

    fireEvent.pointerOver(host);
    act(() => {
      vi.advanceTimersByTime(100);
    });
    fireEvent.pointerOut(host, { relatedTarget: document.body });
    act(() => {
      vi.advanceTimersByTime(600);
    });

    expect(screen.queryByRole("tooltip")).toBeNull();
    expect(host.getAttribute("title")).toBe(TITLE);
  });

  it("组件卸载时把 title 还回去，不留后遗症", () => {
    const scene = renderScene();
    const host = screen.getByTestId("host");

    fireEvent.pointerOver(host);
    expect(host.hasAttribute("title")).toBe(false);

    scene.unmount();
    expect(host.getAttribute("title")).toBe(TITLE);
  });

  it("放得下就挂在触发元素下方", () => {
    stubLayout(
      { top: 100, left: 400, width: 44, height: 44 },
      { top: 0, left: 0, width: 60, height: 28 },
    );
    renderScene();
    hover(screen.getByTestId("host"));

    const bubble = screen.getByRole("tooltip");
    expect(bubble.style.top).toBe("152px");
    expect(bubble.style.left).toBe("422px");
  });

  it("贴着窗口底边时翻到上方，不能压在触发元素身上", () => {
    // 左下角 h-11 的设置按钮：翻转锚点取错会让气泡正好盖住齿轮图标
    const host = { top: 700, left: 11, width: 44, height: 44 };
    stubLayout(host, { top: 0, left: 0, width: 44, height: 28 });
    renderScene();
    hover(screen.getByTestId("host"));

    const bubble = screen.getByRole("tooltip");
    const top = Number.parseFloat(bubble.style.top);
    expect(top + 28).toBeLessThanOrEqual(host.top);
    expect(top).toBe(664);
  });
});
