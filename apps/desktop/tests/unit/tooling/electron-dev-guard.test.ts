import { describe, expect, it, vi } from "vitest";

import { createDevBuildGuard } from "../../../scripts/electron-dev-guard.mjs";

/** 模拟 Rollup 的一轮构建：buildStart 之后以 buildEnd 报告成败（无参即成功）。 */
function runBuild(guard: ReturnType<typeof createDevBuildGuard>, error?: Error) {
  guard.plugin.buildStart();
  guard.plugin.buildEnd(error);
}

function createGuard(label = "main", skipped = "重启 Electron") {
  const log = vi.fn();
  return { guard: createDevBuildGuard({ label, skipped, log }), log };
}

describe("createDevBuildGuard", () => {
  it("构建成功时执行动作，且不输出提示", () => {
    const { guard, log } = createGuard();
    const action = vi.fn();

    runBuild(guard);

    expect(guard.run(action)).toBe(true);
    expect(action).toHaveBeenCalledTimes(1);
    expect(log).not.toHaveBeenCalled();
  });

  it("构建失败时跳过动作，并留下带入口名与被跳过动作的提示", () => {
    const { guard, log } = createGuard("main", "重启 Electron");
    const action = vi.fn();

    runBuild(guard, new Error("Unexpected end of file"));

    expect(guard.run(action)).toBe(false);
    expect(action).not.toHaveBeenCalled();
    expect(log).toHaveBeenCalledTimes(1);
    expect(log.mock.calls[0][0]).toContain("main 构建失败");
    expect(log.mock.calls[0][0]).toContain("重启 Electron");
  });

  it("失败之后下一次成功的重建自动恢复", () => {
    const { guard } = createGuard();
    const action = vi.fn();

    runBuild(guard, new Error("boom"));
    guard.run(action);
    expect(action).not.toHaveBeenCalled();

    runBuild(guard);
    expect(guard.run(action)).toBe(true);
    expect(action).toHaveBeenCalledTimes(1);
  });

  it("连续失败时每次都提示，且始终不执行动作", () => {
    const { guard, log } = createGuard();
    const action = vi.fn();

    runBuild(guard, new Error("first"));
    guard.run(action);
    runBuild(guard, new Error("second"));
    guard.run(action);

    expect(action).not.toHaveBeenCalled();
    expect(log).toHaveBeenCalledTimes(2);
  });

  it("失败标记随下一轮构建开始而清除，不会误伤进行中的重建", () => {
    const { guard } = createGuard();
    const action = vi.fn();

    runBuild(guard, new Error("boom"));
    guard.plugin.buildStart();
    guard.plugin.buildEnd();

    expect(guard.run(action)).toBe(true);
  });

  it("各入口的守卫互相独立：main 失败不影响 preload", () => {
    const main = createGuard("main", "重启 Electron");
    const preload = createGuard("preload", "页面重载");
    const reload = vi.fn();

    runBuild(main.guard, new Error("main broken"));
    runBuild(preload.guard);

    expect(preload.guard.run(reload)).toBe(true);
    expect(reload).toHaveBeenCalledTimes(1);
    expect(main.guard.run(vi.fn())).toBe(false);
    expect(main.guard.plugin.name).not.toBe(preload.guard.plugin.name);
  });
});
