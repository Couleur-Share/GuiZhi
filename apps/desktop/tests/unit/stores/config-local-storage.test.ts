import { beforeEach, describe, expect, it } from "vitest";
import {
  readSettingsSnapshot,
  readUiLayoutSnapshot,
  writeImportedLocalStorage,
} from "../../../src/renderer/components/settings/config-transfer/config-local-storage";

const SETTINGS_KEY = "guizhi-settings";

function seedSettings(state: Record<string, unknown>, version = 2): void {
  window.localStorage.setItem(SETTINGS_KEY, JSON.stringify({ state, version }));
}

function readState(): Record<string, unknown> {
  return JSON.parse(window.localStorage.getItem(SETTINGS_KEY)!).state;
}

describe("配置迁移的 localStorage 读写", () => {
  beforeEach(() => {
    window.localStorage.clear();
  });

  it("采集读的是持久化后的 state 与版本号", () => {
    seedSettings({ themeColor: "royal-blue", aiApiKey: "sk-1" }, 2);
    expect(readSettingsSnapshot()).toEqual({
      settings: { themeColor: "royal-blue", aiApiKey: "sk-1" },
      settingsVersion: 2,
    });
  });

  it("localStorage 为空时退回内存状态，且不含函数（函数过不了 IPC）", () => {
    const snapshot = readSettingsSnapshot();
    expect(snapshot.settingsVersion).toBeUndefined();
    expect(typeof snapshot.settings.themeColor).toBe("string");
    expect(
      Object.values(snapshot.settings).some((v) => typeof v === "function"),
    ).toBe(false);
  });

  it("持久化内容坏掉时不抛异常，退回内存状态", () => {
    window.localStorage.setItem(SETTINGS_KEY, "{ not json");
    expect(() => readSettingsSnapshot()).not.toThrow();
    expect(readSettingsSnapshot().settings).toBeTypeOf("object");
  });

  it("导入是盖在现有设置之上：文件里没有的字段保留本机原值", () => {
    // 工具路径与数据目录故意不进导出文件，整份替换会把它们抹成默认值——
    // 工具其实还在（路径存在 SQLite 里），只有界面显示成空的，最误导
    seedSettings({
      ytDlpPath: "D:/tools/yt-dlp.exe",
      dataPath: "D:/GuiZhi",
      themeColor: "old-color",
      aiModels: [{ id: "local-only" }],
    });

    writeImportedLocalStorage(
      { themeColor: "new-color", aiModels: [{ id: "imported" }] },
      2,
      undefined,
    );

    expect(readState()).toEqual({
      ytDlpPath: "D:/tools/yt-dlp.exe",
      dataPath: "D:/GuiZhi",
      themeColor: "new-color",
      aiModels: [{ id: "imported" }],
    });
  });

  it("数组整体替换而不是逐项合并：导入后模型列表就是文件里那些", () => {
    seedSettings({ aiModels: [{ id: "a" }, { id: "b" }, { id: "c" }] });
    writeImportedLocalStorage({ aiModels: [{ id: "a" }] }, 2, undefined);
    expect(readState().aiModels).toEqual([{ id: "a" }]);
  });

  it("版本号缺省时写 0，逼 store 走 migrate 做一次全量归一", () => {
    seedSettings({ themeColor: "old" }, 2);
    window.localStorage.removeItem(SETTINGS_KEY);
    writeImportedLocalStorage({ themeColor: "new" }, undefined, undefined);
    expect(JSON.parse(window.localStorage.getItem(SETTINGS_KEY)!).version).toBe(0);
  });

  it("界面偏好只认白名单，构造出来的 key 写不进 localStorage", () => {
    writeImportedLocalStorage({}, 2, {
      "ui-storage": { state: { isSidebarCollapsed: true } },
      "guizhi-settings": { state: { aiApiKey: "leak" } },
      "evil-key": { pwned: true },
    });

    expect(JSON.parse(window.localStorage.getItem("ui-storage")!)).toEqual({
      state: { isSidebarCollapsed: true },
    });
    expect(window.localStorage.getItem("evil-key")).toBeNull();
    // guizhi-settings 是设置本身的 key，不能被 uiLayout 那条路径顺手覆盖掉
    expect(readState().aiApiKey).not.toBe("leak");
  });

  it("界面偏好采集也只取白名单里的那几个", () => {
    window.localStorage.setItem("ui-storage", JSON.stringify({ state: {} }));
    window.localStorage.setItem("guizhi-table-config", JSON.stringify({ a: 1 }));
    window.localStorage.setItem("guizhi-ask-active-session", '"s1"');

    expect(Object.keys(readUiLayoutSnapshot()).sort()).toEqual([
      "guizhi-table-config",
      "ui-storage",
    ]);
  });
});
