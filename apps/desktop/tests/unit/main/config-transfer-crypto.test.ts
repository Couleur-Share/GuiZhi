import { describe, expect, it, vi } from "vitest";
import type { ConfigTransferFile } from "@guizhi/shared/types";
import { CONFIG_TRANSFER_KIND, CONFIG_TRANSFER_VERSION } from "@guizhi/shared/types";

// security.ts 只用到 node crypto，但链路上的 Database 类型经 electron 侧导入
vi.mock("electron", () => ({ app: {}, session: { defaultSession: {} } }));

import {
  decryptConfigSecrets,
  encryptConfigSecrets,
} from "../../../src/main/services/config-transfer/config-crypto";

function makeFile(): ConfigTransferFile {
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
      ],
      networkProxy: { mode: "manual", password: "proxy-pw" },
      themeColor: "royal-blue",
    },
  };
}

describe("配置文件机密加解密", () => {
  it("加密后往返一致，非机密字段保持明文", () => {
    const file = makeFile();
    encryptConfigSecrets(file, "hunter2");

    expect(file.encryption?.algo).toBe("aes-256-gcm");
    expect(file.settings.aiApiKey).toMatch(/^ENC::/);
    expect((file.settings.aiModels as any[])[0].apiKey).toMatch(/^ENC::/);
    // 只加密机密字段：文件里有哪些模型、什么路由仍然看得见
    expect(file.settings.themeColor).toBe("royal-blue");

    const result = decryptConfigSecrets(file, "hunter2");
    expect(result).toMatchObject({ ok: true, failed: 0 });
    expect(file.settings.aiApiKey).toBe("sk-legacy");
    expect((file.settings.aiProviders as any[])[0].apiKey).toBe("sk-p1");
    expect((file.settings.aiModels as any[])[0].apiKey).toBe("sk-m1");
    expect((file.settings.networkProxy as any).password).toBe("proxy-pw");
    // 解开之后加密块要摘掉，否则再走一遍就会拿密文当明文
    expect(file.encryption).toBeUndefined();
  });

  it("同一密码两次导出的密文不同（盐与 IV 都是随机的）", () => {
    const a = makeFile();
    const b = makeFile();
    encryptConfigSecrets(a, "hunter2");
    encryptConfigSecrets(b, "hunter2");

    expect(a.encryption?.salt).not.toBe(b.encryption?.salt);
    expect(a.settings.aiApiKey).not.toBe(b.settings.aiApiKey);
  });

  it("密码错时靠 canary 当场判定，不留下被改坏的字段", () => {
    const file = makeFile();
    encryptConfigSecrets(file, "hunter2");
    const cipher = file.settings.aiApiKey;

    const result = decryptConfigSecrets(file, "wrong");
    expect(result.ok).toBe(false);
    expect(result.wrongPassword).toBe(true);
    expect(file.settings.aiApiKey).toBe(cipher);
    expect(file.encryption).toBeDefined();
  });

  it("加密文件不给密码时报「需要密码」而不是解出空值", () => {
    const file = makeFile();
    encryptConfigSecrets(file, "hunter2");

    const result = decryptConfigSecrets(file, "");
    expect(result).toMatchObject({ ok: false, wrongPassword: true });
  });

  it("没有加密块的文件直接放行", () => {
    const file = makeFile();
    expect(decryptConfigSecrets(file, "")).toEqual({ ok: true });
    expect(file.settings.aiApiKey).toBe("sk-legacy");
  });

  it("密码对但个别字段损坏时只记失败数，不整份作废", () => {
    const file = makeFile();
    encryptConfigSecrets(file, "hunter2");
    (file.settings.aiModels as any[])[0].apiKey = "ENC::bm90LXJlYWxseS1jaXBoZXI=";

    const result = decryptConfigSecrets(file, "hunter2");
    expect(result.ok).toBe(true);
    expect(result.failed).toBe(1);
    expect(file.settings.aiApiKey).toBe("sk-legacy");
  });
});
