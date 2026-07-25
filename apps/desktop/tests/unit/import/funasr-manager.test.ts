import { describe, expect, it, vi } from "vitest";
import type { CoreAIConfigFile } from "@guizhi/core";

// funasr 模块链路引用 electron（network-proxy），单测中替换为空实现
vi.mock("electron", () => ({
  session: { defaultSession: {} },
  app: {},
}));

import {
  getPythonDownloadUrls,
  removeBuiltinTranscription,
  upsertBuiltinTranscription,
} from "../../../src/main/services/media/funasr-manager";
import {
  FUNASR_BASE_URL,
  FUNASR_MODEL_ID,
  FUNASR_PROVIDER_ID,
} from "../../../src/main/services/media/funasr-paths";
import { isManagedFunasrUrl } from "../../../src/main/services/media/funasr-service";

function makeConfig(
  overrides?: Partial<CoreAIConfigFile>,
): CoreAIConfigFile {
  return {
    kind: "guizhi-ai-config",
    version: 1,
    updatedAt: "2026-07-24T00:00:00.000Z",
    providers: [
      {
        id: "provider_cloud",
        provider: "custom",
        apiProtocol: "openai",
        apiKey: "sk-cloud",
        apiUrl: "https://api.example.com/v1",
      },
    ],
    models: [
      {
        id: "model_chat",
        provider: "custom",
        apiProtocol: "openai",
        apiKey: "sk-cloud",
        apiUrl: "https://api.example.com/v1",
        model: "gpt-x",
        isDefault: true,
        capabilities: { chat: true },
      },
    ],
    modelRouteDefaults: { mainText: "model_chat" },
    ...overrides,
  };
}

describe("getPythonDownloadUrls", () => {
  it("Windows 官方源在前（+ 号已编码）、镜像随后；其余平台不提供", () => {
    const urls = getPythonDownloadUrls("win32");
    expect(urls[0]).toContain(
      "github.com/astral-sh/python-build-standalone/releases/download/",
    );
    expect(urls[0]).toContain("%2B");
    expect(urls[0]).toContain("x86_64-pc-windows-msvc-install_only.tar.gz");
    expect(urls.length).toBeGreaterThanOrEqual(3);

    expect(getPythonDownloadUrls("linux")).toEqual([]);
    expect(getPythonDownloadUrls("darwin")).toEqual([]);
  });
});

describe("upsertBuiltinTranscription", () => {
  it("写入内置服务商 / 模型并接上 audioText 路由，不影响既有条目", () => {
    const next = upsertBuiltinTranscription(
      makeConfig(),
      "2026-07-24T12:00:00.000Z",
    );

    expect(next.providers.map((p) => p.id)).toEqual([
      "provider_cloud",
      FUNASR_PROVIDER_ID,
    ]);
    const model = next.models.find((m) => m.id === FUNASR_MODEL_ID);
    expect(model).toMatchObject({
      model: "sensevoice",
      apiUrl: FUNASR_BASE_URL,
      capabilities: { chat: false, audioTranscription: true },
      lastVerifiedAt: "2026-07-24T12:00:00.000Z",
    });
    expect(next.modelRouteDefaults).toEqual({
      mainText: "model_chat",
      audioText: FUNASR_MODEL_ID,
    });
  });

  it("重复执行幂等：已有条目被覆盖更新而非追加", () => {
    const once = upsertBuiltinTranscription(makeConfig(), "2026-07-24T12:00:00.000Z");
    const twice = upsertBuiltinTranscription(once, "2026-07-25T12:00:00.000Z");

    expect(
      twice.providers.filter((p) => p.id === FUNASR_PROVIDER_ID),
    ).toHaveLength(1);
    expect(twice.models.filter((m) => m.id === FUNASR_MODEL_ID)).toHaveLength(1);
    expect(
      twice.models.find((m) => m.id === FUNASR_MODEL_ID)?.lastVerifiedAt,
    ).toBe("2026-07-25T12:00:00.000Z");
  });
});

describe("removeBuiltinTranscription", () => {
  it("移除内置条目并清掉指向它的路由", () => {
    const installed = upsertBuiltinTranscription(
      makeConfig(),
      "2026-07-24T12:00:00.000Z",
    );
    const next = removeBuiltinTranscription(installed);

    expect(next.providers.map((p) => p.id)).toEqual(["provider_cloud"]);
    expect(next.models.map((m) => m.id)).toEqual(["model_chat"]);
    expect(next.modelRouteDefaults).toEqual({ mainText: "model_chat" });
  });

  it("路由指向其他模型时保持不动", () => {
    const installed = upsertBuiltinTranscription(
      makeConfig(),
      "2026-07-24T12:00:00.000Z",
    );
    const rerouted = {
      ...installed,
      modelRouteDefaults: {
        ...installed.modelRouteDefaults,
        audioText: "model_cloud_whisper",
      },
    };
    const next = removeBuiltinTranscription(rerouted);
    expect(next.modelRouteDefaults.audioText).toBe("model_cloud_whisper");
  });
});

describe("isManagedFunasrUrl", () => {
  it("识别托管服务地址（含 localhost / # 后缀）", () => {
    expect(isManagedFunasrUrl("http://127.0.0.1:8620/v1")).toBe(true);
    expect(isManagedFunasrUrl("http://localhost:8620/v1")).toBe(true);
    expect(isManagedFunasrUrl("http://127.0.0.1:8620/v1#")).toBe(true);
  });

  it("其他地址不误判", () => {
    expect(isManagedFunasrUrl("https://api.openai.com/v1")).toBe(false);
    expect(isManagedFunasrUrl("http://127.0.0.1:8000/v1")).toBe(false);
    expect(isManagedFunasrUrl("not-a-url")).toBe(false);
  });
});
