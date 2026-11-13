import { afterEach, describe, expect, it, vi, type MockInstance } from "vitest";

type I18nModule = typeof import("../../../src/renderer/i18n");

let systemLanguageSpy: MockInstance<() => string> | undefined;

async function startI18n(
  systemLanguage: string,
  savedState?: Record<string, unknown>,
): Promise<I18nModule> {
  vi.resetModules();
  localStorage.clear();
  if (savedState) {
    localStorage.setItem("guizhi-settings", JSON.stringify({ state: savedState, version: 2 }));
  }
  systemLanguageSpy = vi
    .spyOn(window.navigator, "language", "get")
    .mockReturnValue(systemLanguage);
  const i18nModule = await import("../../../src/renderer/i18n");
  await i18nModule.i18nReady;
  return i18nModule;
}

/**
 * 复现启动竞态：main.tsx 并行加载 App 与语言包，App 分包先求值时 settings store
 * 在 i18n 初始化之前创建，此时 i18n.language 为 undefined。
 * 调用前须等 i18nReady 完成，避免初始化链在等待期间改写这里模拟的状态。
 */
async function withI18nUninitialized<T>(
  i18nModule: I18nModule,
  create: () => T | Promise<T>,
): Promise<T> {
  const i18n = i18nModule.default;
  const { language, isInitialized } = i18n;
  i18n.language = undefined as unknown as string;
  i18n.isInitialized = false;
  try {
    return await create();
  } finally {
    i18n.language = language;
    i18n.isInitialized = isInitialized;
  }
}

afterEach(() => {
  systemLanguageSpy?.mockRestore();
  systemLanguageSpy = undefined;
  localStorage.clear();
});

// 首次安装（无已保存语言）固定默认中文，不跟随系统语言；已保存的语言设置优先。
describe("设置默认语言与 i18n 初始语言同源", () => {
  it.each([
    { systemLanguage: "zh-CN", savedState: undefined, expected: "zh" },
    { systemLanguage: "zh-TW", savedState: undefined, expected: "zh" },
    { systemLanguage: "en-US", savedState: undefined, expected: "zh" },
    { systemLanguage: "fr-FR", savedState: undefined, expected: "zh" },
    { systemLanguage: "zh-CN", savedState: { language: "en" }, expected: "en" },
    { systemLanguage: "en-US", savedState: { language: "zh" }, expected: "zh" },
    { systemLanguage: "zh-CN", savedState: { autoSave: false }, expected: "zh" },
  ])(
    "系统 $systemLanguage、已存 $savedState → $expected，不读未初始化的 i18n.language",
    async ({ systemLanguage, savedState, expected }) => {
      const i18nModule = await startI18n(systemLanguage, savedState);
      expect(i18nModule.initialLanguage).toBe(expected);
      expect(i18nModule.default.language).toBe(expected);

      const { createDefaultSettingsValues } = await import(
        "../../../src/renderer/stores/settings/settings-defaults"
      );
      const defaults = await withI18nUninitialized(i18nModule, createDefaultSettingsValues);

      expect(defaults.language).toBe(expected);
    },
  );

  it("首次启动时 store 先于 i18n 创建，语言默认中文且与界面语言一致", async () => {
    const i18nModule = await startI18n("en-US"); // 系统是英文也默认中文

    const { useSettingsStore } = await withI18nUninitialized(
      i18nModule,
      () => import("../../../src/renderer/stores/settings.store"),
    );

    // App 启动时以 store 为准同步 i18n；两者一致才不会把界面切成英文
    expect(useSettingsStore.getState().language).toBe("zh");
    expect(i18nModule.default.language).toBe("zh");

    useSettingsStore.setState({ autoSave: false });
    const persisted = JSON.parse(localStorage.getItem("guizhi-settings") ?? "{}");
    expect(persisted.state.language).toBe("zh");
  });
});
