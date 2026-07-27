import { describe, expect, it, vi } from "vitest";
import type { AIClientConfig } from "@guizhi/core";

import {
  extractGlossaryTerms,
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

describe("extractGlossaryTerms", () => {
  it("只收拉丁词，中文不进表", () => {
    expect(
      extractGlossaryTerms("React 的 useEffect 和 useState 的区别"),
    ).toEqual(["React", "useEffect", "useState"]);
    expect(extractGlossaryTerms("这两个钩子函数的区别")).toEqual([]);
  });

  it("相邻拉丁词并成一条：GitHub Actions 是一个专名", () => {
    expect(extractGlossaryTerms("CI 走 GitHub Actions 部署")).toEqual([
      "CI",
      "GitHub Actions",
    ]);
  });

  it("剥掉贴在词尾的标点，保留词内的点号与连字符", () => {
    expect(extractGlossaryTerms("用 Node.js. 起服务，模型是 GPT-4。")).toEqual([
      "Node.js",
      "GPT-4",
    ]);
  });

  it("大小写去重，保留首次出现的写法", () => {
    expect(extractGlossaryTerms("Docker 很好用", "docker 和 DOCKER")).toEqual([
      "Docker",
    ]);
  });

  it("单个字母不入表（「C 语言」的 C 噪音大于价值）", () => {
    expect(extractGlossaryTerms("C 语言和 Go 语言")).toEqual(["Go"]);
  });

  it("多个来源依次提取，超过上限即截断", () => {
    const many = Array.from({ length: 60 }, (_, i) => `Term${i}`).join(" 、 ");
    expect(extractGlossaryTerms(many)).toHaveLength(40);
  });

  it("空来源与 null 直接跳过", () => {
    expect(extractGlossaryTerms(null, undefined, "  ")).toEqual([]);
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
    expect(result.text).toBe(`${punctuate(CHUNK_A)}\n\n${punctuate(CHUNK_B)}`);
    expect(result.skippedReason).toBeUndefined();
  });

  it("传入术语表时把专名与纠正指令写进提示词", async () => {
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => ({
        content: punctuate(extractChunk(messages[1].content)),
      }),
    );
    await formatTranscript(CHUNK_A, CONFIG, {
      chat: chat as never,
      glossary: ["Docker", "GitHub Actions"],
    });
    const prompt = chat.mock.calls[0][1][1].content;
    expect(prompt).toContain("Docker、GitHub Actions");
    expect(prompt).toContain("改成表中写法");
    // 放宽只针对表内词，不能变成允许自由改写
    expect(prompt).toContain("表以外的词不要改动");
  });

  it("正文带说话人时追加保全指令，并数出前缀不符", async () => {
    const dialog = [
      `说话人 1：${"甲".repeat(300)}。`,
      `说话人 2：${"乙".repeat(300)}。`,
    ].join("\n\n");
    // 模型把前缀当赘语删掉：长度与标点都过得了关，只有数前缀能发现
    const dropping = vi.fn(async (
      _config: AIClientConfig,
      messages: { role: string; content: string }[],
    ) => ({
      content: extractChunk(messages[1].content).replace(/说话人 \d+：/g, ""),
    }));
    await expect(
      formatTranscript(dialog, CONFIG, { chat: dropping as never }),
    ).rejects.toThrow("说话人前缀数量不符");
    expect(dropping.mock.calls[0][1][1].content).toContain(
      "原样保留每个",
    );

    // 原样保留则通过
    const keeping = vi.fn(async (
      _config: AIClientConfig,
      messages: { role: string; content: string }[],
    ) => ({ content: extractChunk(messages[1].content) }));
    const result = await formatTranscript(dialog, CONFIG, {
      chat: keeping as never,
    });
    expect(result.text).toBe(dialog);
  });

  it("没有说话人前缀时不掺入保全指令", async () => {
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => ({
        content: punctuate(extractChunk(messages[1].content)),
      }),
    );
    await formatTranscript(CHUNK_A, CONFIG, { chat: chat as never });
    expect(chat.mock.calls[0][1][1].content).not.toContain("说话人 N：");
  });

  it("没有术语表时不掺入这段指令", async () => {
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => ({
        content: punctuate(extractChunk(messages[1].content)),
      }),
    );
    await formatTranscript(CHUNK_A, CONFIG, { chat: chat as never });
    expect(chat.mock.calls[0][1][1].content).not.toContain("专有名词表");
  });

  it("上报已完成块数（并发下报的是计数不是序号）", async () => {
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => ({
        content: punctuate(extractChunk(messages[1].content)),
      }),
    );
    const progress: [number, number][] = [];
    await formatTranscript(`${CHUNK_A}${CHUNK_B}`, CONFIG, {
      chat: chat as never,
      onProgress: (current, total) => progress.push([current, total]),
    });
    expect(progress).toEqual([
      [1, 2],
      [2, 2],
    ]);
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
    expect(result.text).toBe(punctuate(CHUNK_A));
  });

  it("中途某块失败 → 收下已排好的，其余接回原文，内容不丢", async () => {
    // 第一块正常，第二块两次尝试都敷衍
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => {
        const chunk = extractChunk(messages[1].content);
        return chunk.startsWith("乙")
          ? { content: chunk.replace(/。/g, "") }
          : { content: punctuate(chunk) };
      },
    );
    const result = await formatTranscript(`${CHUNK_A}${CHUNK_B}`, CONFIG, {
      chat: chat as never,
    });
    expect(result.partialReason).toContain("已排版 1/2 块");
    // 第一块是排版后的，第二块是原文——一个字都不能少
    expect(result.text).toBe(`${punctuate(CHUNK_A)}\n\n${CHUNK_B}`);
  });

  it("第一块就失败 → 照旧抛错，不粉饰成「部分成功」", async () => {
    // 一块都没成通常是模型名/鉴权配错，降级成部分成功会掩盖真问题
    const chat = vi.fn(async () => {
      throw new Error("HTTP 401");
    });
    await expect(
      formatTranscript(`${CHUNK_A}${CHUNK_B}`, CONFIG, { chat: chat as never }),
    ).rejects.toThrow("HTTP 401");
  });

  it("并发排版：多块同时在途，结果按下标归位不串行等待", async () => {
    let inFlight = 0;
    let peak = 0;
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => {
        inFlight += 1;
        peak = Math.max(peak, inFlight);
        await new Promise((resolve) => setTimeout(resolve, 20));
        inFlight -= 1;
        return { content: punctuate(extractChunk(messages[1].content)) };
      },
    );
    const result = await formatTranscript(`${CHUNK_A}${CHUNK_B}`, CONFIG, {
      chat: chat as never,
    });
    expect(peak).toBeGreaterThan(1);
    // 并发完成顺序不定，但拼回去必须还是原来的先后
    expect(result.text).toBe(`${punctuate(CHUNK_A)}\n\n${punctuate(CHUNK_B)}`);
  });

  it("超出时间预算 → 停止发新请求，已排好的保留", async () => {
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => ({
        content: punctuate(extractChunk(messages[1].content)),
      }),
    );
    // 预算为 0：第一块照发（至少要试一次），之后立刻收摊
    const result = await formatTranscript(`${CHUNK_A}${CHUNK_B}`, CONFIG, {
      chat: chat as never,
      timeBudgetMs: 0,
    });
    expect(chat).toHaveBeenCalledTimes(1);
    expect(result.partialReason).toContain("时间预算");
    expect(result.text).toBe(`${punctuate(CHUNK_A)}\n\n${CHUNK_B}`);
  });

  it("用户取消 → 抛错，不留下半成品", async () => {
    const controller = new AbortController();
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => {
        controller.abort();
        return { content: punctuate(extractChunk(messages[1].content)) };
      },
    );
    await expect(
      formatTranscript(`${CHUNK_A}${CHUNK_B}`, CONFIG, {
        chat: chat as never,
        signal: controller.signal,
      }),
    ).rejects.toThrow();
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
    expect(result.text).toBe(punctuate(CHUNK_A));
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

  it("超长文字稿跳过排版，原样返回并给出原因", async () => {
    const chat = vi.fn(async () => ({ content: "不应被调用" }));
    const huge = "字".repeat(50_001);
    const result = await formatTranscript(huge, CONFIG, {
      chat: chat as never,
    });
    expect(result.text).toBe(huge);
    // 调用方要靠它区分「排好了」和「太长没排」，只回字符串分不出来
    expect(result.skippedReason).toContain("超过自动排版上限");
    expect(chat).not.toHaveBeenCalled();
  });

  it("放开上限后同一份超长文字稿照常分块排版", async () => {
    const chat = vi.fn(
      async (
        _config: AIClientConfig,
        messages: { role: string; content: string }[],
      ) => ({
        content: punctuate(extractChunk(messages[1].content)),
      }),
    );
    // 与上一条同样超出默认上限，只是用户已确认过代价
    const huge = "字".repeat(50_001);
    const result = await formatTranscript(huge, CONFIG, {
      chat: chat as never,
      maxTotalChars: Number.MAX_SAFE_INTEGER,
    });
    expect(result.skippedReason).toBeUndefined();
    // 1600 字一块，硬切成 32 块
    expect(chat).toHaveBeenCalledTimes(32);
  });

  it("maxTotalChars 收紧后短稿也会被跳过", async () => {
    const chat = vi.fn(async () => ({ content: "不应被调用" }));
    const result = await formatTranscript("一段不长的转写", CONFIG, {
      chat: chat as never,
      maxTotalChars: 3,
    });
    expect(result.skippedReason).toContain("超过自动排版上限 3 字");
    expect(chat).not.toHaveBeenCalled();
  });
});
