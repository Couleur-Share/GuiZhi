import { describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({ app: {}, screen: {} }));

import {
  resolveOffscreenPosition,
  shouldPlaceWindowOffscreen,
} from "../../../src/main/testing/window-mode";

describe("shouldPlaceWindowOffscreen", () => {
  it("常规启动照常显示窗口", () => {
    expect(shouldPlaceWindowOffscreen({})).toBe(false);
  });

  it("E2E 默认走屏幕外，自动化截图不打断用户", () => {
    expect(shouldPlaceWindowOffscreen({ GUIZHI_E2E: "1" })).toBe(true);
  });

  it("GUIZHI_WINDOW_MODE=visible 让 e2e 退回可见，供人工盯着跑", () => {
    expect(
      shouldPlaceWindowOffscreen({
        GUIZHI_E2E: "1",
        GUIZHI_WINDOW_MODE: "visible",
      }),
    ).toBe(false);
  });

  it("GUIZHI_WINDOW_MODE=offscreen 在非 E2E 下同样生效（electron:dev）", () => {
    expect(shouldPlaceWindowOffscreen({ GUIZHI_WINDOW_MODE: "offscreen" })).toBe(
      true,
    );
  });
});

describe("resolveOffscreenPosition", () => {
  const size = { width: 1200, height: 800 };

  it("单显示器：落点整体在显示器左上角之外", () => {
    const display = { bounds: { x: 0, y: 0, width: 1920, height: 1080 } };
    const { x, y } = resolveOffscreenPosition([display], size);
    expect(x + size.width).toBeLessThan(display.bounds.x);
    expect(y + size.height).toBeLessThan(display.bounds.y);
  });

  it("副屏摆在主屏左上方（bounds 为负）时仍在全部显示器之外", () => {
    const displays = [
      { bounds: { x: 0, y: 0, width: 1920, height: 1080 } },
      { bounds: { x: -2560, y: -300, width: 2560, height: 1440 } },
    ];
    const { x, y } = resolveOffscreenPosition(displays, size);
    for (const { bounds } of displays) {
      expect(x + size.width).toBeLessThan(bounds.x);
      expect(y + size.height).toBeLessThan(bounds.y);
    }
  });

  it("拿不到显示器列表时给出有限坐标，而不是 -Infinity", () => {
    const { x, y } = resolveOffscreenPosition([], size);
    expect(Number.isFinite(x)).toBe(true);
    expect(Number.isFinite(y)).toBe(true);
    expect(x).toBeLessThan(-size.width);
    expect(y).toBeLessThan(-size.height);
  });
});
