import { describe, expect, it } from "vitest";
import {
  AUTO_UPDATE_CHECK_INTERVAL_MS,
  AUTO_UPDATE_RETRY_INTERVAL_MS,
  resolveAutoUpdateSkipReason,
  type AutoUpdateCheckContext,
} from "../../../src/renderer/services/update-check";

const NOW = 1_700_000_000_000;

function createContext(
  overrides: Partial<AutoUpdateCheckContext> = {},
): AutoUpdateCheckContext {
  return {
    trigger: "startup",
    enabled: true,
    windowVisible: true,
    online: true,
    inFlight: false,
    now: NOW,
    lastAttemptAt: 0,
    lastAttemptFailed: false,
    ...overrides,
  };
}

describe("resolveAutoUpdateSkipReason", () => {
  it("条件齐备时应当发起检查", () => {
    expect(resolveAutoUpdateSkipReason(createContext())).toBeNull();
  });

  it("关闭自动检查、窗口隐藏、离线、已有检查在途时分别给出原因", () => {
    expect(
      resolveAutoUpdateSkipReason(createContext({ enabled: false })),
    ).toBe("disabled");
    expect(
      resolveAutoUpdateSkipReason(createContext({ windowVisible: false })),
    ).toBe("hidden");
    expect(resolveAutoUpdateSkipReason(createContext({ online: false }))).toBe(
      "offline",
    );
    expect(resolveAutoUpdateSkipReason(createContext({ inFlight: true }))).toBe(
      "in-flight",
    );
  });

  it("窗口显示补检：本次会话没检查过就直接检查", () => {
    expect(
      resolveAutoUpdateSkipReason(
        createContext({ trigger: "visibility", lastAttemptAt: 0 }),
      ),
    ).toBeNull();
  });

  it("窗口显示补检：上次成功后一小时内不重复检查", () => {
    const context = createContext({
      trigger: "visibility",
      lastAttemptAt: NOW - AUTO_UPDATE_CHECK_INTERVAL_MS + 1000,
    });
    expect(resolveAutoUpdateSkipReason(context)).toBe("cooldown");
    expect(
      resolveAutoUpdateSkipReason({
        ...context,
        lastAttemptAt: NOW - AUTO_UPDATE_CHECK_INTERVAL_MS,
      }),
    ).toBeNull();
  });

  it("窗口显示补检：上次失败时冷却缩短到重试间隔", () => {
    const base = createContext({
      trigger: "visibility",
      lastAttemptFailed: true,
    });
    expect(
      resolveAutoUpdateSkipReason({
        ...base,
        lastAttemptAt: NOW - AUTO_UPDATE_RETRY_INTERVAL_MS + 1000,
      }),
    ).toBe("cooldown");
    expect(
      resolveAutoUpdateSkipReason({
        ...base,
        lastAttemptAt: NOW - AUTO_UPDATE_RETRY_INTERVAL_MS,
      }),
    ).toBeNull();
  });

  it("定时器触发不受冷却限制：节流由定时器本身负责", () => {
    expect(
      resolveAutoUpdateSkipReason(
        createContext({ trigger: "interval", lastAttemptAt: NOW - 1000 }),
      ),
    ).toBeNull();
  });
});
