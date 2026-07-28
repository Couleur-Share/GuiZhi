import { describe, expect, it } from "vitest";

import { dropProviderNameAliases } from "../../../src/renderer/stores/settings/settings-ai";
import type {
  AIModelConfig,
  AIProviderConfig,
} from "../../../src/renderer/stores/settings/settings-types";

function makeProvider(overrides: Partial<AIProviderConfig> = {}): AIProviderConfig {
  return {
    id: "p_1",
    name: "云雾API",
    provider: "custom",
    apiProtocol: "openai",
    apiKey: "sk-test",
    apiUrl: "https://api3.wlai.vip/v1",
    ...overrides,
  };
}

function makeModel(
  id: string,
  overrides: Partial<AIModelConfig> = {},
): AIModelConfig {
  return {
    id,
    providerId: "p_1",
    provider: "custom",
    apiProtocol: "openai",
    apiKey: "sk-test",
    apiUrl: "https://api3.wlai.vip/v1",
    model: id,
    ...overrides,
  };
}

describe("dropProviderNameAliases：模型别名不得与供应商同名", () => {
  it("清掉被端点编辑灌进来的供应商名，回落到模型 id", () => {
    const models = dropProviderNameAliases(
      [makeProvider()],
      [
        makeModel("gpt-5.2", { name: "云雾API" }),
        makeModel("gemini-3-pro", { name: "云雾API" }),
      ],
    );

    // 下拉里两项都叫「云雾API」时，用户分不出选中的是哪个模型
    expect(models.map((model) => model.name)).toEqual([undefined, undefined]);
    expect(models.map((model) => model.model)).toEqual([
      "gpt-5.2",
      "gemini-3-pro",
    ]);
  });

  it("用户自己起的别名不动", () => {
    const models = dropProviderNameAliases(
      [makeProvider()],
      [makeModel("gpt-5.2", { name: "写作专用" })],
    );

    expect(models[0].name).toBe("写作专用");
  });

  it("同名判定按所属供应商，不误伤别家同名的模型", () => {
    const models = dropProviderNameAliases(
      [
        makeProvider(),
        makeProvider({
          id: "p_2",
          name: "本地转写引擎",
          apiUrl: "http://127.0.0.1:8620/v1",
        }),
      ],
      [
        makeModel("sensevoice", {
          providerId: "p_2",
          name: "云雾API",
          apiUrl: "http://127.0.0.1:8620/v1",
        }),
      ],
    );

    expect(models[0].name).toBe("云雾API");
  });

  it("没有 provider 记录的合成分组一律不动", () => {
    const models = dropProviderNameAliases(
      [],
      [makeModel("gpt-5.2", { providerId: undefined, name: "云雾API" })],
    );

    expect(models[0].name).toBe("云雾API");
  });
});
