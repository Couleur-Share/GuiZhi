import { describe, expect, it } from "vitest";

import { preserveLocalOnlyModelFields } from "../../../src/renderer/stores/settings/settings-ai";
import type { AIModelConfig } from "../../../src/renderer/stores/settings/settings-types";

function makeModel(
  id: string,
  overrides: Partial<AIModelConfig> = {},
): AIModelConfig {
  return {
    id,
    providerId: "p_1",
    provider: "openai",
    apiProtocol: "openai",
    apiKey: "sk-test",
    apiUrl: "https://api.openai.com/v1",
    model: id,
    ...overrides,
  };
}

describe("preserveLocalOnlyModelFields：chatParams 要活过一次重启", () => {
  it("主进程回灌的模型没有 chatParams 时按 id 接回本地那份", () => {
    // ai-models.json 由 normalizeModelConfig 逐字段重建，结构里没有 chatParams
    const fromMainProcess = [makeModel("gpt-5.2"), makeModel("gemini-3-pro")];
    const merged = preserveLocalOnlyModelFields(
      [
        makeModel("gpt-5.2", { chatParams: { temperature: 0.3 } }),
        makeModel("gemini-3-pro"),
      ],
      fromMainProcess,
    );

    expect(merged[0].chatParams).toEqual({ temperature: 0.3 });
    expect(merged[1].chatParams).toBeUndefined();
  });

  it("回灌那份自己带了参数时以它为准", () => {
    const merged = preserveLocalOnlyModelFields(
      [makeModel("gpt-5.2", { chatParams: { temperature: 0.3 } })],
      [makeModel("gpt-5.2", { chatParams: { temperature: 0.9 } })],
    );

    expect(merged[0].chatParams).toEqual({ temperature: 0.9 });
  });

  it("id 对不上的不乱接：换了模型就不该继承上一个的参数", () => {
    const merged = preserveLocalOnlyModelFields(
      [makeModel("gpt-5.2", { chatParams: { temperature: 0.3 } })],
      [makeModel("claude-4-opus")],
    );

    expect(merged[0].chatParams).toBeUndefined();
  });

  it("本地没有这个模型时原样返回，不凭空造字段", () => {
    const merged = preserveLocalOnlyModelFields([], [makeModel("gpt-5.2")]);

    expect(merged).toEqual([makeModel("gpt-5.2")]);
    expect("chatParams" in merged[0]).toBe(false);
  });
});
