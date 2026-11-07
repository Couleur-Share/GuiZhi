import { describe, expect, it } from "vitest";
import i18n, { changeLanguage, i18nReady, translate } from "../../../src/renderer/i18n";

describe("translate：非组件模块取文案", () => {
  it("初始化后按当前语言取文案，缺键时用兜底并插值", async () => {
    await i18nReady;
    await changeLanguage("en");
    expect(translate("common.cancel", "取消")).toBe("Cancel");
    expect(translate("test.notInLocale", "共 {{count}} 项", { count: 3 })).toBe("共 3 项");
  });

  it("i18n 未初始化时用兜底文案插值，不返回空", () => {
    const initialized = i18n.isInitialized;
    i18n.isInitialized = false;
    try {
      expect(translate("common.cancel", "取消 {{n}} 次，{{missing}}结束", { n: 2 })).toBe("取消 2 次，结束");
    } finally {
      i18n.isInitialized = initialized;
    }
  });
});
