import { describe, expect, it, vi } from "vitest";
import type { AIClientConfig } from "@guizhi/core";

import {
  formatTranscript,
  rejectFormattedChunk,
  splitTranscriptChunks,
} from "../../../src/main/services/media/transcript-format";

const CONFIG: AIClientConfig = {
  provider: "openai",
  apiProtocol: "openai",
  apiKey: "sk-test",
  apiUrl: "https://api.openai.com",
  model: "fast-model",
};

/** 从 user 消息的分隔符中取出原文块 */
function extractChunk(userContent: string): string {
  const match = userContent.match(
    /【待整理文字稿开始】\n([\s\S]*)\n【待整理文字稿结束】/,
  );
  return match?.[1] ?? "";
}

/** 模拟合格排版输出：每 100 字插一个逗号（长度比≈1，标点密度达标） */
function punctuate(chunk: string): string {
  return (chunk.match(/.{1,100}/gs) ?? []).join("，");
}

describe("splitTranscriptChunks", () => {
  it("短文本单块返回，空文本返回空数组", () => {
    expect(splitTranscriptChunks("你好世界")).toEqual(["你好世界"]);
    expect(splitTranscriptChunks("   ")).toEqual([]);
  });

  it("长文本按句读边界切块，不把句子拦腰截断", () => {
    const sentence = `${"内容".repeat(30)}。`; // 61 字一句
    const text = sentence.repeat(5);
    const chunks = splitTranscriptChunks(text, 100);
    expect(chunks.length).toBeGreaterThan(1);
    for (const chunk of chunks) {
      expect(chunk.length).toBeLessThanOrEqual(100);
      expect(chunk.endsWith("。")).toBe(true);
    }
    expect(chunks.join("")).toBe(text);
  });

  it("无标点文本按空格切，完全无边界时硬切", () => {
    const spaced = `${"字".repeat(60)} ${"字".repeat(60)}`;
    const spacedChunks = splitTranscriptChunks(spaced, 100);
    expect(spacedChunks).toEqual(["字".repeat(60), "字".repeat(60)]);

    const solid = "字".repeat(250);
    const solidChunks = splitTranscriptChunks(solid, 100);
    expect(solidChunks.map((chunk) => chunk.length)).toEqual([100, 100, 50]);
  });
});

describe("rejectFormattedChunk", () => {
  const rawChunk = "字".repeat(400);

  it("合格输出（长度相当且有标点）通过", () => {
    expect(
      rejectFormattedChunk(rawChunk, { content: punctuate(rawChunk) }),
    ).toBeNull();
  });

  it("空输出 / 截断 / 长度跑偏 / 无标点复读均不合格", () => {
    expect(rejectFormattedChunk(rawChunk, { content: "  " })).toContain("为空");
    expect(
      rejectFormattedChunk(rawChunk, {
        content: punctuate(rawChunk),
        finishReason: "length",
      }),
    ).toContain("截断");
    expect(
      rejectFormattedChunk(rawChunk, { content: "太短。" }),
    ).toContain("长度异常");
    // 模型偷懒：原样复读，长度比≈1 但没有任何标点
    expect(rejectFormattedChunk(rawChunk, { content: rawChunk })).toContain(
      "缺少标点",
    );
  });
});

describe("formatTranscript", () => {
  // 默认块上限 1600 字：两句各 1001 字，恰好切成两块
  const CHUNK_A = `${"甲".repeat(1000)}。`;
  const CHUNK_B = `${"乙".repeat(1000)}。`;

  it("逐块调用 chat（指令内嵌 user 消息）并按空行拼接结果", async () => {
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => ({
        content: punctuate(extractChunk(messages[1].content)),
      }),
    );
    const result = await formatTranscript(`${CHUNK_A}${CHUNK_B}`, CONFIG, {
      chat: chat as never,
    });
    expect(chat).toHaveBeenCalledTimes(2);
    // 排版要求写在 user 消息里，原文有显式分隔符包裹
    expect(chat.mock.calls[0][1][1].content).toContain("添加或修正标点");
    expect(chat.mock.calls[0][1][1].content).toContain("【待整理文字稿开始】");
    expect(result).toBe(`${punctuate(CHUNK_A)}\n\n${punctuate(CHUNK_B)}`);
  });

  it("敷衍输出（无标点复读）触发重试，重试合格后采纳", async () => {
    let attempt = 0;
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => {
        attempt++;
        const chunk = extractChunk(messages[1].content);
        // 第一次敷衍复读（去掉标点），第二次正常排版
        return attempt === 1
          ? { content: chunk.replace(/。/g, "") }
          : { content: punctuate(chunk) };
      },
    );
    const result = await formatTranscript(CHUNK_A, CONFIG, {
      chat: chat as never,
    });
    expect(chat).toHaveBeenCalledTimes(2);
    expect(result).toBe(punctuate(CHUNK_A));
  });

  it("重试后仍不合格 → 抛错（调用方保留原始文字稿）", async () => {
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => ({
        content: extractChunk(messages[1].content).replace(/。/g, ""),
      }),
    );
    await expect(
      formatTranscript(CHUNK_A, CONFIG, { chat: chat as never }),
    ).rejects.toThrow("模型未按排版要求输出");
    expect(chat).toHaveBeenCalledTimes(2);
  });

  it("请求瞬时失败（超时/限流）重试一次，成功后正常采纳", async () => {
    let attempt = 0;
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => {
        attempt++;
        if (attempt === 1) {
          throw new Error("This operation was aborted");
        }
        return { content: punctuate(extractChunk(messages[1].content)) };
      },
    );
    const result = await formatTranscript(CHUNK_A, CONFIG, {
      chat: chat as never,
    });
    expect(chat).toHaveBeenCalledTimes(2);
    expect(result).toBe(punctuate(CHUNK_A));
  });

  it("chat 请求连续失败时抛错（由调用方保留原始文字稿）", async () => {
    const chat = vi.fn(async () => {
      throw new Error("HTTP 429");
    });
    await expect(
      formatTranscript("一段转写", CONFIG, { chat: chat as never }),
    ).rejects.toThrow("HTTP 429");
    expect(chat).toHaveBeenCalledTimes(2);
  });

  it("超长文字稿跳过排版，原样返回", async () => {
    const chat = vi.fn(async () => ({ content: "不应被调用" }));
    const huge = "字".repeat(50_001);
    const result = await formatTranscript(huge, CONFIG, {
      chat: chat as never,
    });
    expect(result).toBe(huge);
    expect(chat).not.toHaveBeenCalled();
  });
});
