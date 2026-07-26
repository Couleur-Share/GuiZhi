import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { Select } from "../../../src/renderer/components/ui/Select";

/**
 * 下拉面板的翻转定位。
 *
 * 曾经的写法是「触发器顶边 − maxHeight」反推 top，选项条数撑不满 maxHeight
 * 时面板就会悬在半空（分页条的 4 个选项实测差出约 140px）。所以这里断言的
 * 不是具体像素，而是「贴哪条边」：向上展开必须锚 bottom，向下展开必须锚 top。
 */

const VIEWPORT_HEIGHT = 700;
const VIEWPORT_WIDTH = 1024;

const OPTIONS = [10, 20, 50, 100].map((size) => ({
  value: String(size),
  label: String(size),
}));

/** 把触发器摆在指定位置：jsdom 不做布局，rect 全是 0，必须自己喂 */
function stubTriggerRect(rect: { top: number; height: number }) {
  const spy = vi
    .spyOn(Element.prototype, "getBoundingClientRect")
    .mockReturnValue({
      top: rect.top,
      bottom: rect.top + rect.height,
      left: 900,
      right: 960,
      width: 60,
      height: rect.height,
      x: 900,
      y: rect.top,
      toJSON: () => ({}),
    } as DOMRect);
  return spy;
}

function openMenu(triggerTop: number) {
  window.innerHeight = VIEWPORT_HEIGHT;
  window.innerWidth = VIEWPORT_WIDTH;
  stubTriggerRect({ top: triggerTop, height: 28 });

  render(
    <Select
      value="20"
      onChange={vi.fn()}
      options={OPTIONS}
      ariaLabel="每页"
      align="end"
      menuMinWidth={88}
    />,
  );
  fireEvent.click(screen.getByRole("button", { name: "每页" }));
  return screen.getByRole("listbox");
}

describe("Select 面板翻转定位", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("触发器贴着窗口底边时向上展开，锚定底边紧贴触发器", () => {
    // 触发器顶边 650，下方只剩 22px，放不下
    const listbox = openMenu(650);

    expect(listbox.style.top).toBe("");
    // 700 - 650 + 4：面板底边距视口底 54px，正好落在触发器顶边上方 4px
    expect(listbox.style.bottom).toBe("54px");
  });

  it("空间够时向下展开，锚定顶边", () => {
    const listbox = openMenu(100);

    expect(listbox.style.bottom).toBe("");
    expect(listbox.style.top).toBe("132px");
  });

  it("面板比触发器宽时按 align=end 贴右缘，且不越过视口", () => {
    const listbox = openMenu(650);

    // 触发器右缘 960，面板宽 88（触发器只有 60）
    expect(listbox.style.width).toBe("88px");
    expect(listbox.style.left).toBe("872px");
  });
});
