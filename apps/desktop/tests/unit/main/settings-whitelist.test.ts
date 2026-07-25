import { describe, expect, it } from "vitest";

import {
  filterWritableSettings,
  isPersistedSettingKey,
} from "../../../src/main/ipc/settings.ipc";
import {
  isAcceptableBinaryPath,
  rememberPickedBinaryPath,
  resetPickedBinaryPaths,
} from "../../../src/main/services/picked-binary-paths";

const acceptAny = () => true;

describe("isPersistedSettingKey", () => {
  it("放行用户设置字段", () => {
    for (const key of ["theme", "language", "networkProxy", "ytDlpPath"]) {
      expect(isPersistedSettingKey(key)).toBe(true);
    }
  });

  it("拦截主进程内部状态与未知键", () => {
    // settings 表和用户设置共用一张表，master_password 也在里面
    expect(isPersistedSettingKey("master_password")).toBe(false);
    expect(isPersistedSettingKey("security")).toBe(false);
    expect(isPersistedSettingKey("webdavPassword")).toBe(false);
    expect(isPersistedSettingKey("__proto__")).toBe(false);
  });
});

describe("filterWritableSettings", () => {
  it("非白名单键被拒并记录", () => {
    const { accepted, rejected } = filterWritableSettings(
      {
        theme: "dark",
        master_password: { salt: "x", verifier: "y" },
        arbitraryKey: 1,
      } as never,
      acceptAny,
    );

    expect(accepted).toEqual({ theme: "dark" });
    expect(rejected.sort()).toEqual(["arbitraryKey", "master_password"]);
  });

  it("可执行文件路径必须来自文件选择器", () => {
    const { accepted, rejected } = filterWritableSettings(
      { ytDlpPath: "C:\\Users\\Public\\payload.exe" },
      isAcceptableBinaryPath,
    );

    expect(accepted).toEqual({});
    expect(rejected).toEqual(["ytDlpPath"]);
  });
});

describe("picked-binary-paths", () => {
  it("只接受本会话由选择器登记过的路径", () => {
    resetPickedBinaryPaths();
    expect(isAcceptableBinaryPath("C:\\tools\\yt-dlp.exe")).toBe(false);

    rememberPickedBinaryPath("C:\\tools\\yt-dlp.exe");
    expect(isAcceptableBinaryPath("C:\\tools\\yt-dlp.exe")).toBe(true);
    expect(isAcceptableBinaryPath("C:\\tools\\other.exe")).toBe(false);
  });

  it("空值表示清除自定义路径，始终允许", () => {
    resetPickedBinaryPaths();
    expect(isAcceptableBinaryPath("")).toBe(true);
    expect(isAcceptableBinaryPath("   ")).toBe(true);
  });

  it("非字符串一律拒绝", () => {
    expect(isAcceptableBinaryPath(undefined)).toBe(false);
    expect(isAcceptableBinaryPath(123)).toBe(false);
    expect(isAcceptableBinaryPath({})).toBe(false);
  });
});
