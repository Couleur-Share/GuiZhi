import { describe, expect, it } from "vitest";

import { getDesktopNotificationsEnabled } from "../../../src/main/settings/settings-readers";

function dbWithSetting(value?: string) {
  return {
    prepare: () => ({
      get: () => (value === undefined ? undefined : { value }),
    }),
  } as never;
}

describe("getDesktopNotificationsEnabled", () => {
  it("从未同步过时默认开启", () => {
    expect(getDesktopNotificationsEnabled(dbWithSetting())).toBe(true);
  });

  it("用户关掉后主进程读到关闭", () => {
    expect(getDesktopNotificationsEnabled(dbWithSetting("false"))).toBe(false);
  });

  it("显式开启", () => {
    expect(getDesktopNotificationsEnabled(dbWithSetting("true"))).toBe(true);
  });
});
