import { describe, expect, it, vi } from "vitest";

import { createDefaultSettingsValues } from "../../../src/renderer/stores/settings/settings-defaults";
import {
  mergeSettingsState,
  migrateSettingsState,
  rehydrateSettingsState,
  stripEphemeralSettings,
} from "../../../src/renderer/stores/settings/settings-persistence";
import type { SettingsState } from "../../../src/renderer/stores/settings/settings-types";

function currentDefaults(): SettingsState {
  return createDefaultSettingsValues() as SettingsState;
}

describe("settings 持久化归一（Markdown 渲染视图默认开启）", () => {
  it("rehydrate 合并：历史快照里的 editorMarkdownPreview=false 被翻转", () => {
    const merged = mergeSettingsState(
      { editorMarkdownPreview: false },
      currentDefaults(),
    );
    expect(merged.editorMarkdownPreview).toBe(true);
  });

  it("版本迁移：任意版本快照（含已写成新版本号的 false 值）都被治愈", () => {
    for (const version of [0, 1, 2]) {
      const migrated = migrateSettingsState(
        { ...currentDefaults(), editorMarkdownPreview: false },
        version,
      );
      expect(migrated.editorMarkdownPreview).toBe(true);
    }
  });
});

describe("动画偏好默认跟随系统", () => {
  it("v3 之前作为默认值写入的「标准」迁移为跟随系统", () => {
    for (const version of [0, 1, 2]) {
      const migrated = migrateSettingsState(
        { ...currentDefaults(), motionPreference: "standard" },
        version,
      );
      expect(migrated.motionPreference).toBe("system");
    }
  });

  it("v3 起用户主动选的「标准」保持不变", () => {
    const migrated = migrateSettingsState(
      { ...currentDefaults(), motionPreference: "standard" },
      3,
    );
    expect(migrated.motionPreference).toBe("standard");
  });

  it("关闭与减弱不受迁移影响，未知值归一为跟随系统", () => {
    expect(
      migrateSettingsState({ ...currentDefaults(), motionPreference: "off" }, 2)
        .motionPreference,
    ).toBe("off");
    expect(
      mergeSettingsState({ motionPreference: "bogus" }, currentDefaults())
        .motionPreference,
    ).toBe("system");
  });
});

describe("settings 持久化白名单（清理已下线字段）", () => {
  // v0.4.1 删掉的同步字段，其中两个是明文凭据
  const legacySyncKeys = {
    syncEnabled: true,
    webdavUrl: "https://dav.example.com",
    webdavUsername: "someone",
    webdavPassword: "plaintext-secret",
    s3AccessKeyId: "AKIAEXAMPLE",
    s3SecretAccessKey: "plaintext-secret",
  };

  it("合并时丢弃已下线字段，不让明文凭据回到运行时 state", () => {
    const merged = mergeSettingsState(
      { ...legacySyncKeys, themeColor: "royal-blue" },
      currentDefaults(),
    );

    for (const key of Object.keys(legacySyncKeys)) {
      expect(merged).not.toHaveProperty(key);
    }
    // 已知字段照常合并
    expect(merged.themeColor).toBe("royal-blue");
  });

  it("回写时同样过滤，脏键不会被原样存回 localStorage", () => {
    const stripped = stripEphemeralSettings({
      ...currentDefaults(),
      ...legacySyncKeys,
    } as SettingsState);

    for (const key of Object.keys(legacySyncKeys)) {
      expect(stripped).not.toHaveProperty(key);
    }
    expect(stripped.language).toBe(currentDefaults().language);
  });

  it("迁移时清理，老快照升级后不再残留", () => {
    const migrated = migrateSettingsState(
      { ...currentDefaults(), ...legacySyncKeys },
      2,
    );

    for (const key of Object.keys(legacySyncKeys)) {
      expect(migrated).not.toHaveProperty(key);
    }
  });

  it("已撤下的复制/保存提示开关从老快照中清除", () => {
    const merged = mergeSettingsState(
      { showCopyNotification: false, showSaveNotification: false },
      currentDefaults(),
    );
    expect(merged).not.toHaveProperty("showCopyNotification");
    expect(merged).not.toHaveProperty("showSaveNotification");
  });

  it("保留 actions：过滤只针对数据字段", () => {
    const noop = () => {};
    const stripped = stripEphemeralSettings({
      ...currentDefaults(),
      setThemeColor: noop,
      webdavPassword: "plaintext-secret",
    } as unknown as SettingsState);

    expect(stripped).not.toHaveProperty("webdavPassword");
    expect((stripped as unknown as { setThemeColor: unknown }).setThemeColor).toBe(
      noop,
    );
  });
});

describe("桌面通知开关同步到主进程", () => {
  it("启动时把用户关掉的通知开关交给主进程", () => {
    const sync = vi.fn(async () => undefined);
    rehydrateSettingsState(
      { ...currentDefaults(), enableNotifications: false },
      sync,
    );
    expect(sync).toHaveBeenCalledWith(
      expect.objectContaining({ enableNotifications: false }),
    );
  });

  it("没有本地快照时按默认开启同步", () => {
    const sync = vi.fn(async () => undefined);
    rehydrateSettingsState(undefined, sync);
    expect(sync).toHaveBeenCalledWith(
      expect.objectContaining({ enableNotifications: true }),
    );
  });
});
