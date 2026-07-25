import { vi } from "vitest";

type MockRecord = Record<string, any>;

type DeepPartial<T> = {
  [K in keyof T]?: T[K] extends MockRecord ? DeepPartial<T[K]> : T[K];
};

function isPlainObject(value: unknown): value is MockRecord {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function mergeMocks<T extends MockRecord>(
  base: T,
  overrides?: DeepPartial<T>,
): T {
  if (!overrides) {
    return base;
  }

  const output: MockRecord = { ...base };

  for (const [key, value] of Object.entries(overrides)) {
    const current = output[key];
    if (isPlainObject(current) && isPlainObject(value)) {
      output[key] = mergeMocks(current, value);
      continue;
    }
    output[key] = value;
  }

  return output as T;
}

export function createWindowApiMock(overrides?: DeepPartial<MockRecord>) {
  return mergeMocks(
    {
      minimize: vi.fn(),
      maximize: vi.fn(),
      close: vi.fn(),
      settings: {
        get: vi.fn().mockResolvedValue(null),
        set: vi.fn().mockResolvedValue(true),
      },
      security: {
        status: vi.fn().mockResolvedValue({
          masterPasswordConfigured: false,
          unlocked: true,
        }),
        setMasterPassword: vi.fn().mockResolvedValue({ success: true }),
        changeMasterPassword: vi.fn().mockResolvedValue({ success: true }),
        unlock: vi.fn().mockResolvedValue({ success: true }),
        lock: vi.fn().mockResolvedValue({ success: true }),
      },
      ai: {
        httpRequest: vi.fn().mockResolvedValue({
          ok: false,
          status: 0,
          statusText: "mocked",
          headers: {},
          bodyText: "",
        }),
        httpStream: vi.fn(),
        cancelHttpStream: vi.fn(),
      },
      on: vi.fn(),
      off: vi.fn(),
    },
    overrides,
  );
}

export function createWindowElectronMock(overrides?: DeepPartial<MockRecord>) {
  return mergeMocks(
    {
      minimize: vi.fn(),
      maximize: vi.fn(),
      close: vi.fn(),
      toggleVisibility: vi.fn(),
      isFullscreen: vi.fn().mockResolvedValue(false),
      isVisible: vi.fn().mockResolvedValue(true),
      setAutoLaunch: vi.fn(),
      setMinimizeToTray: vi.fn(),
      setCloseAction: vi.fn(),
      setDebugMode: vi.fn(),
      getShortcuts: vi.fn().mockResolvedValue({}),
      setShortcuts: vi.fn().mockResolvedValue(true),
      setShortcutMode: vi.fn(),
      onShortcutTriggered: vi.fn().mockReturnValue(() => {}),
      onShortcutsUpdated: vi.fn().mockReturnValue(() => {}),
      onAppCommand: vi.fn().mockReturnValue(() => {}),
      onShowCloseDialog: vi.fn().mockReturnValue(() => {}),
      sendCloseDialogResult: vi.fn(),
      sendCloseDialogCancel: vi.fn(),
      selectFolder: vi.fn().mockResolvedValue(null),
      openPath: vi.fn().mockResolvedValue({ success: true }),
      showNotification: vi.fn().mockResolvedValue(true),
      getDataPath: vi.fn().mockResolvedValue(""),
      getDataPathStatus: vi.fn().mockResolvedValue({
        currentPath: "",
        configuredPath: null,
        needsRestart: false,
      }),
      updater: {
        check: vi.fn().mockResolvedValue({ success: true }),
        download: vi.fn().mockResolvedValue({ success: true }),
        install: vi.fn().mockResolvedValue({ success: true }),
        getInstallSource: vi.fn().mockResolvedValue("direct"),
        openDownloadedUpdate: vi.fn().mockResolvedValue({ success: false }),
        getVersion: vi.fn().mockResolvedValue("0.0.0-test"),
        getPlatform: vi.fn().mockResolvedValue("win32"),
        openReleases: vi.fn().mockResolvedValue(undefined),
        onStatus: vi.fn().mockReturnValue(() => {}),
        offStatus: vi.fn(),
      },
      selectImage: vi.fn().mockResolvedValue([]),
      saveImage: vi.fn().mockResolvedValue([]),
      listImages: vi.fn().mockResolvedValue([]),
      imageExists: vi.fn().mockResolvedValue(false),
      clearImages: vi.fn().mockResolvedValue(true),
      webdav: {
        testConnection: vi
          .fn()
          .mockResolvedValue({ success: false, message: "mocked" }),
        ensureDirectory: vi.fn().mockResolvedValue({ success: true }),
        upload: vi.fn().mockResolvedValue({ success: true }),
        download: vi.fn().mockResolvedValue({ success: false, notFound: true }),
        stat: vi.fn().mockResolvedValue({ success: false, notFound: true }),
      },
      s3: {
        testConnection: vi
          .fn()
          .mockResolvedValue({ success: false, message: "mocked" }),
        upload: vi.fn().mockResolvedValue({ success: true }),
        download: vi.fn().mockResolvedValue({ success: false, notFound: true }),
        stat: vi.fn().mockResolvedValue({ success: false, notFound: true }),
      },
    },
    overrides,
  );
}

export function installWindowMocks(overrides?: {
  api?: DeepPartial<MockRecord>;
  electron?: DeepPartial<MockRecord>;
}): void {
  Object.defineProperty(window, "api", {
    configurable: true,
    writable: true,
    value: createWindowApiMock(overrides?.api),
  });
  Object.defineProperty(window, "electron", {
    configurable: true,
    writable: true,
    value: createWindowElectronMock(overrides?.electron),
  });
}
