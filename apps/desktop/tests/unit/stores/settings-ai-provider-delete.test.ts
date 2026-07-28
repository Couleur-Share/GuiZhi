import { describe, expect, it } from "vitest";

import { createAISettingsActions } from "../../../src/renderer/stores/settings/settings-ai-actions";
import type { SettingsActionContext } from "../../../src/renderer/stores/settings/settings-action-context";
import { createDefaultSettingsValues } from "../../../src/renderer/stores/settings/settings-defaults";
import type {
  AIModelConfig,
  AIProviderConfig,
  SettingsState,
} from "../../../src/renderer/stores/settings/settings-types";

function makeProvider(id: string, apiUrl: string): AIProviderConfig {
  return {
    id,
    provider: "openai",
    apiProtocol: "openai",
    apiKey: "sk-test",
    apiUrl,
  };
}

function makeModel(
  id: string,
  providerId: string | undefined,
  overrides: Partial<AIModelConfig> = {},
): AIModelConfig {
  return {
    id,
    providerId,
    provider: "openai",
    apiProtocol: "openai",
    apiKey: "sk-test",
    apiUrl: "https://api.example.com/v1",
    model: id,
    ...overrides,
  };
}

/**
 * store 的 action 只依赖 context 的 get / commitAISettings，这里用一份
 * 同步的假 context 直接驱动，顺便数提交次数——级联删除必须一次提交完。
 */
function createHarness(initial: Partial<SettingsState>) {
  let state = {
    ...createDefaultSettingsValues(),
    ...initial,
  } as SettingsState;
  const commits: Partial<SettingsState>[] = [];
  const apply = (partial: Partial<SettingsState>) => {
    state = { ...state, ...partial };
  };
  const context = {
    set: apply,
    get: () => state,
    setTouched: apply,
    commitAISettings: (partial: Partial<SettingsState>) => {
      commits.push(partial);
      apply(partial);
    },
    syncSettingsToMain: async () => {},
  } as unknown as SettingsActionContext;

  return {
    actions: createAISettingsActions(context),
    commits,
    getState: () => state,
  };
}

describe("deleteAiProvider：供应商与旗下模型一起删", () => {
  it("删掉 provider 记录与旗下模型，只提交一次", () => {
    const harness = createHarness({
      aiProviders: [
        makeProvider("p_1", "https://api.example.com/v1"),
        makeProvider("p_2", "https://api.other.com/v1"),
      ],
      aiModels: [
        makeModel("m_1", "p_1"),
        makeModel("m_2", "p_1"),
        makeModel("m_keep", "p_2"),
      ],
    });

    harness.actions.deleteAiProvider({
      providerId: "p_1",
      modelIds: ["m_1", "m_2"],
    });

    expect(harness.getState().aiProviders.map((item) => item.id)).toEqual([
      "p_2",
    ]);
    expect(harness.getState().aiModels.map((item) => item.id)).toEqual([
      "m_keep",
    ]);
    // 逐条删会写 N 次 localStorage 并同步 N 次主进程
    expect(harness.commits).toHaveLength(1);
  });

  it("解绑而不删模型会让那一行炸成 N 行，所以模型不得残留", () => {
    const harness = createHarness({
      aiProviders: [makeProvider("p_1", "https://api.example.com/v1")],
      aiModels: [makeModel("m_1", "p_1"), makeModel("m_2", "p_1")],
    });

    harness.actions.deleteAiProvider({
      providerId: "p_1",
      modelIds: ["m_1", "m_2"],
    });

    expect(harness.getState().aiModels).toEqual([]);
  });

  it("清掉指向被删模型的路由，其余路由不动", () => {
    const harness = createHarness({
      aiProviders: [makeProvider("p_1", "https://api.example.com/v1")],
      aiModels: [makeModel("m_1", "p_1"), makeModel("m_keep", undefined)],
      modelRouteDefaults: { mainText: "m_1", fastText: "m_keep" },
      scenarioModelDefaults: { chat: "m_1", summary: "m_keep" },
    });

    harness.actions.deleteAiProvider({
      providerId: "p_1",
      modelIds: ["m_1"],
    });

    expect(harness.getState().modelRouteDefaults).toEqual({
      fastText: "m_keep",
    });
    expect(harness.getState().scenarioModelDefaults).toEqual({
      summary: "m_keep",
    });
  });

  it("删掉的是默认模型时，默认标记转移到剩下的第一个", () => {
    const harness = createHarness({
      aiProviders: [makeProvider("p_1", "https://api.example.com/v1")],
      aiModels: [
        makeModel("m_default", "p_1", { isDefault: true }),
        makeModel("m_keep", undefined, { model: "gpt-keep" }),
      ],
    });

    harness.actions.deleteAiProvider({
      providerId: "p_1",
      modelIds: ["m_default"],
    });

    const state = harness.getState();
    expect(state.aiModels.map((item) => item.id)).toEqual(["m_keep"]);
    expect(state.aiModels[0].isDefault).toBe(true);
    // legacy 单模型字段跟着走，否则问答会继续指着已删掉的模型
    expect(state.aiModel).toBe("gpt-keep");
  });

  it("合成分组没有 provider 记录：只删模型，不误伤其他供应商", () => {
    const harness = createHarness({
      aiProviders: [makeProvider("p_1", "https://api.example.com/v1")],
      aiModels: [makeModel("m_orphan", undefined), makeModel("m_1", "p_1")],
    });

    harness.actions.deleteAiProvider({
      providerId: undefined,
      modelIds: ["m_orphan"],
    });

    expect(harness.getState().aiProviders.map((item) => item.id)).toEqual([
      "p_1",
    ]);
    expect(harness.getState().aiModels.map((item) => item.id)).toEqual(["m_1"]);
  });
});
