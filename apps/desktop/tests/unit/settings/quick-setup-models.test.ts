import { describe, expect, it } from "vitest";
import {
  buildQuickSetupInput,
  recommendModel,
  rolesForGoals,
} from "../../../src/renderer/components/settings/ai-quick-setup/quick-setup-models";

describe("AI quick setup model planner", () => {
  it("基础问答固定同时建立 mainText 与 fastText 路由", () => {
    const roles = rolesForGoals(new Set(["basic"]));
    const result = buildQuickSetupInput(
      {
        provider: "OpenAI",
        apiProtocol: "openai",
        apiKey: "key",
        apiUrl: "https://example.test/v1",
      },
      roles,
      { text: "gpt-4.1-mini" },
      new Set(["text"]),
    );
    expect(result.models).toHaveLength(1);
    expect(result.models[0]).toMatchObject({ verified: true, capabilities: { chat: true } });
    expect(result.routes).toEqual({ mainText: 0, fastText: 0 });
  });

  it("同一模型承担多个能力时只保存一份并合并能力", () => {
    const result = buildQuickSetupInput(
      {
        provider: "Gemini",
        apiProtocol: "gemini",
        apiKey: "key",
        apiUrl: "https://example.test",
      },
      ["text", "vision"],
      { text: "gemini-2.5-flash", vision: "gemini-2.5-flash" },
      new Set(["text", "vision"]),
    );
    expect(result.models).toHaveLength(1);
    expect(result.models[0].capabilities).toMatchObject({ chat: true, vision: true });
    expect(result.routes).toEqual({ mainText: 0, fastText: 0, visionText: 0 });
  });

  it("按能力关键词推荐模型，找不到时回退第一项", () => {
    const models = ["qwen-plus", "text-embedding-3-small", "whisper-1"];
    expect(recommendModel(models, "embedding")).toBe("text-embedding-3-small");
    expect(recommendModel(models, "audio")).toBe("whisper-1");
    expect(recommendModel(["custom-a"], "imageGen")).toBe("custom-a");
  });
});
