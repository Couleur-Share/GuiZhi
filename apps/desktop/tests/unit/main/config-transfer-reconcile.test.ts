import { describe, expect, it, vi } from "vitest";
import type { CoreAIConfigFile } from "@guizhi/core";

// funasr 模块链路引用 electron（runtime-paths / network-proxy），单测替换为空实现
vi.mock("electron", () => ({ app: {}, session: { defaultSession: {} } }));

import { reconcileImportedAiConfig } from "../../../src/main/services/config-transfer/ai-reconcile";
import {
  FUNASR_BASE_URL,
  FUNASR_MODEL_ID,
  FUNASR_PROVIDER_ID,
} from "../../../src/main/services/media/funasr-paths";

const cloudModel = {
  id: "m_cloud",
  provider: "openai",
  apiProtocol: "openai" as const,
  apiKey: "sk-1",
  apiUrl: "https://api.openai.com/v1",
  model: "gpt-4o",
};

const localProvider = {
  id: FUNASR_PROVIDER_ID,
  provider: "local-funasr",
  apiProtocol: "openai" as const,
  apiKey: "",
  apiUrl: FUNASR_BASE_URL,
};

const localModel = {
  id: FUNASR_MODEL_ID,
  providerId: FUNASR_PROVIDER_ID,
  provider: "local-funasr",
  apiProtocol: "openai" as const,
  apiKey: "",
  apiUrl: FUNASR_BASE_URL,
  model: "sensevoice",
};

function localConfig(overrides?: Partial<CoreAIConfigFile>): CoreAIConfigFile {
  return {
    kind: "guizhi-ai-config",
    version: 1,
    updatedAt: "2026-07-27T00:00:00.000Z",
    providers: [],
    models: [],
    modelRouteDefaults: {},
    ...overrides,
  };
}

describe("导入 AI 配置与本机对账", () => {
  it("本机装了本地转写引擎时，导入不会把它抹掉", () => {
    const result = reconcileImportedAiConfig(
      {
        providers: [],
        models: [cloudModel],
        routes: { mainText: "m_cloud" },
      },
      localConfig({
        providers: [localProvider],
        models: [localModel],
        modelRouteDefaults: { audioText: FUNASR_MODEL_ID },
      }),
    );

    expect(result.models.map((m) => m.id)).toEqual(["m_cloud", FUNASR_MODEL_ID]);
    expect(result.providers.map((p) => p.id)).toEqual([FUNASR_PROVIDER_ID]);
    // 导入方没配语音转写路由，本机原来指向内置引擎的那条接回去
    expect(result.routes.audioText).toBe(FUNASR_MODEL_ID);
    expect(result.routes.mainText).toBe("m_cloud");
    expect(result.warnings).toContain("已保留本机安装的本地转写引擎条目");
  });

  it("导入方的本地引擎条目被剔除：它指向导出设备上的本地服务", () => {
    const result = reconcileImportedAiConfig(
      {
        providers: [localProvider],
        models: [cloudModel, localModel],
        routes: { audioText: FUNASR_MODEL_ID, mainText: "m_cloud" },
      },
      localConfig(),
    );

    expect(result.models.map((m) => m.id)).toEqual(["m_cloud"]);
    expect(result.providers).toEqual([]);
    // 指向被剔除模型的路由要一起清掉，否则是一条点了必然失败的死路由
    expect(result.routes.audioText).toBeUndefined();
    expect(result.warnings.some((w) => w.includes("本地转写引擎"))).toBe(true);
  });

  it("按地址识别手工添加的本地引擎条目，不只认固定 id", () => {
    const result = reconcileImportedAiConfig(
      {
        providers: [],
        models: [
          { ...localModel, id: "m_hand_rolled", providerId: undefined },
        ],
        routes: {},
      },
      localConfig(),
    );

    expect(result.models).toEqual([]);
  });

  it("导入方明确配了云端转写路由时不被本机覆盖", () => {
    const whisper = { ...cloudModel, id: "m_whisper", model: "whisper-1" };
    const result = reconcileImportedAiConfig(
      { providers: [], models: [whisper], routes: { audioText: "m_whisper" } },
      localConfig({
        providers: [localProvider],
        models: [localModel],
        modelRouteDefaults: { audioText: FUNASR_MODEL_ID },
      }),
    );

    expect(result.routes.audioText).toBe("m_whisper");
    expect(result.models.map((m) => m.id)).toContain(FUNASR_MODEL_ID);
  });

  it("字段残缺的条目被筛掉而不是让整份导入抛错", () => {
    const result = reconcileImportedAiConfig(
      {
        providers: [{ id: "p_bad", provider: "", apiUrl: "" }],
        models: [cloudModel, { id: "m_bad", provider: "openai" }],
        routes: {},
      },
      localConfig(),
    );

    expect(result.models.map((m) => m.id)).toEqual(["m_cloud"]);
    expect(result.providers).toEqual([]);
    expect(result.warnings).toContain("已跳过 2 条字段不完整的服务商 / 模型记录");
  });

  it("指向不存在模型的路由被清掉并记一条 warning", () => {
    const result = reconcileImportedAiConfig(
      {
        providers: [],
        models: [cloudModel],
        routes: { mainText: "m_cloud", visionText: "m_gone", embedding: "  " },
      },
      localConfig(),
    );

    expect(result.routes).toEqual({ mainText: "m_cloud" });
    expect(result.warnings).toContain("已清空 1 条指向不存在模型的路由");
  });

  it("入参不是数组时当空处理", () => {
    const result = reconcileImportedAiConfig(
      { providers: null, models: undefined, routes: "nope" },
      localConfig(),
    );

    expect(result).toMatchObject({ providers: [], models: [], routes: {} });
  });
});
