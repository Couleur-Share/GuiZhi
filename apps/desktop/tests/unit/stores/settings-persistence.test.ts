import { describe, expect, it } from "vitest";

import { createDefaultSettingsValues } from "../../../src/renderer/stores/settings/settings-defaults";
import {
  mergeSettingsState,
  migrateSettingsState,
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
