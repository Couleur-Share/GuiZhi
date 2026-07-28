import { describe, expect, it } from "vitest";
import type { ConfigTransferFile } from "@guizhi/shared/types";
import {
  CONFIG_TRANSFER_KIND,
  CONFIG_TRANSFER_VERSION,
} from "@guizhi/shared/types";
import {
  buildConfigPreview,
  countConfigSecrets,
  mapConfigSecrets,
  parseConfigTransferFile,
  pickTransferableLocalStorage,
  stripNonPortableSettings,
} from "@guizhi/shared/utils/config-transfer";

function makeFile(overrides?: Partial<ConfigTransferFile>): ConfigTransferFile {
  return {
    kind: CONFIG_TRANSFER_KIND,
    version: CONFIG_TRANSFER_VERSION,
    exportedAt: "2026-07-27T00:00:00.000Z",
    appVersion: "0.6.0",
    settings: {
      aiApiKey: "sk-legacy",
      aiProviders: [
        { id: "p1", provider: "openai", apiUrl: "https://a", apiKey: "sk-p1" },
      ],
      aiModels: [
        {
          id: "m1",
          provider: "openai",
          apiUrl: "https://a",
          model: "gpt",
          apiKey: "sk-m1",
        },
        {
          id: "m2",
          provider: "openai",
          apiUrl: "https://a",
          model: "gpt2",
          apiKey: "",
        },
      ],
      modelRouteDefaults: { mainText: "m1", fastText: "" },
      networkProxy: { mode: "manual", password: "proxy-pw" },
      themeColor: "royal-blue",
    },
    ...overrides,
  };
}

describe("机密字段遍历", () => {
  it("覆盖全部四处：兼容字段、服务商、模型、代理密码", () => {
    const file = makeFile();
    const seen: string[] = [];
    const stats = mapConfigSecrets(file, (value) => {
      seen.push(value);
      return `X:${value}`;
    });

    // m2 的 apiKey 是空串，不算机密
    expect(seen.sort()).toEqual(["proxy-pw", "sk-legacy", "sk-m1", "sk-p1"]);
    expect(stats).toEqual({ processed: 4, failed: 0 });
    expect(file.settings.aiApiKey).toBe("X:sk-legacy");
    expect((file.settings.aiProviders as any[])[0].apiKey).toBe("X:sk-p1");
    expect((file.settings.aiModels as any[])[0].apiKey).toBe("X:sk-m1");
    expect((file.settings.aiModels as any[])[1].apiKey).toBe("");
    expect((file.settings.networkProxy as any).password).toBe("X:proxy-pw");
  });

  it("countConfigSecrets 与遍历数一致", () => {
    expect(countConfigSecrets(makeFile())).toBe(4);
  });

  it("transform 回 null 记为失败且不改写原值", () => {
    const file = makeFile();
    const stats = mapConfigSecrets(file, () => null);
    expect(stats).toEqual({ processed: 0, failed: 4 });
    expect(file.settings.aiApiKey).toBe("sk-legacy");
  });

  it("settings 结构残缺时不抛异常", () => {
    const file = makeFile({ settings: { aiModels: "not-an-array" } });
    expect(() => mapConfigSecrets(file, (v) => v)).not.toThrow();
    expect(countConfigSecrets(file)).toBe(0);
  });
});

describe("不可移植字段", () => {
  it("剔除机器绑定字段与函数，保留其余设置", () => {
    const next = stripNonPortableSettings({
      themeColor: "royal-blue",
      dataPath: "D:/GuiZhi",
      ytDlpPath: "D:/tools/yt-dlp.exe",
      ffmpegPath: "D:/tools/ffmpeg.exe",
      launchAtStartup: true,
      backgroundImageFileName: "bg.png",
      settingsUpdatedAt: "2026-07-27",
      isDarkMode: true,
      setThemeMode: () => undefined,
      backupIntervalHours: 12,
    });

    expect(next).toEqual({ themeColor: "royal-blue", backupIntervalHours: 12 });
  });
});

describe("界面偏好白名单", () => {
  it("只放行三个已知 key，构造出来的键写不进去", () => {
    const picked = pickTransferableLocalStorage({
      "ui-storage": { state: {} },
      "guizhi-library-table-config": { columns: [] },
      "guizhi-table-config": {},
      "guizhi-settings": { state: { aiApiKey: "leak" } },
      "some-other-app": { evil: true },
    });

    expect(Object.keys(picked).sort()).toEqual([
      "guizhi-library-table-config",
      "guizhi-table-config",
      "ui-storage",
    ]);
  });

  it("入参不是对象时回空对象", () => {
    expect(pickTransferableLocalStorage(undefined)).toEqual({});
  });
});

describe("信封校验", () => {
  it("接受完整文件并收敛可选字段", () => {
    const result = parseConfigTransferFile({
      ...makeFile(),
      uiLayout: { "ui-storage": {}, hacked: {} },
      shortcuts: { accelerators: { showApp: "Alt+Shift+P" }, modes: {} },
    });

    expect(result.ok).toBe(true);
    expect(Object.keys(result.file!.uiLayout!)).toEqual(["ui-storage"]);
    expect(result.file!.shortcuts!.accelerators.showApp).toBe("Alt+Shift+P");
  });

  it("拒绝别的 JSON 文件", () => {
    expect(parseConfigTransferFile({ hello: 1 }).ok).toBe(false);
    expect(parseConfigTransferFile("nope").ok).toBe(false);
    expect(
      parseConfigTransferFile({ kind: "guizhi-ai-config", version: 1 }).ok,
    ).toBe(false);
  });

  it("拒绝更高版本的文件", () => {
    const result = parseConfigTransferFile(
      makeFile({ version: CONFIG_TRANSFER_VERSION + 1 }),
    );
    expect(result.ok).toBe(false);
    expect(result.error).toContain("更新版本");
  });

  it("拒绝越界的 scrypt 参数（防构造文件把主进程拖死）", () => {
    const withParams = (n: number, r = 8, p = 1) =>
      parseConfigTransferFile(
        makeFile({
          encryption: {
            algo: "aes-256-gcm",
            kdf: "scrypt",
            salt: "c2FsdA==",
            n,
            r,
            p,
            canary: "ENC::x",
          },
        }),
      );

    expect(withParams(16384).ok).toBe(true);
    expect(withParams(1 << 20).ok).toBe(false);
    expect(withParams(20000).ok).toBe(false); // 不是 2 的幂
    expect(withParams(16384, 64).ok).toBe(false);
    expect(withParams(16384, 8, 99).ok).toBe(false);
  });

  it("拒绝不认识的加密方式", () => {
    const result = parseConfigTransferFile(
      makeFile({
        encryption: {
          algo: "rot13" as never,
          kdf: "scrypt",
          salt: "c2FsdA==",
          n: 16384,
          r: 8,
          p: 1,
          canary: "ENC::x",
        },
      }),
    );
    expect(result.ok).toBe(false);
  });
});

describe("预览摘要", () => {
  it("按文件内容计数，空路由不计入", () => {
    const preview = buildConfigPreview(
      makeFile({
        illustrationStyles: [{ id: "s1" } as never, { id: "s2" } as never],
        shortcuts: {
          accelerators: { showApp: "Alt+Shift+P", search: "" },
          modes: {},
        },
        uiLayout: { "ui-storage": {} },
      }),
    );

    expect(preview).toMatchObject({
      encrypted: false,
      providerCount: 1,
      modelCount: 2,
      routeCount: 1,
      styleCount: 2,
      shortcutCount: 1,
      uiLayoutKeyCount: 1,
    });
  });

  it("有加密块时标记为已加密", () => {
    const preview = buildConfigPreview(
      makeFile({
        encryption: {
          algo: "aes-256-gcm",
          kdf: "scrypt",
          salt: "c2FsdA==",
          n: 16384,
          r: 8,
          p: 1,
          canary: "ENC::x",
        },
      }),
    );
    expect(preview.encrypted).toBe(true);
  });
});
