import { describe, expect, it } from "vitest";
import {
  createAnswerStreamState,
  pushAnswerChunk,
} from "../../../src/renderer/services/knowledge-ai/answer-stream";

/** 把整段输出切成小块喂进去，返回每次的结果 */
function feed(chunks: string[]): string[] {
  const state = createAnswerStreamState();
  return chunks.map((chunk) => pushAnswerChunk(state, chunk));
}

describe("pushAnswerChunk", () => {
  it("动作是 answer 时逐块吐出累计文本", () => {
    const results = feed([
      '{"action"',
      ':"answer","te',
      'xt":"归知用 ',
      "embedding 做",
      '语义检索。"}',
    ]);
    expect(results[0]).toBe("");
    expect(results[1]).toBe("");
    expect(results[2]).toBe("归知用 ");
    expect(results[3]).toBe("归知用 embedding 做");
    expect(results[4]).toBe("归知用 embedding 做语义检索。");
  });

  it("检索动作不吐任何内容", () => {
    const results = feed(['{"action":"search","query":"语义检索"}']);
    expect(results).toEqual([""]);
  });

  it("推理模型在 JSON 之前的思考不进回答区", () => {
    const results = feed([
      "让我先想想用户在问什么。",
      '好的。{"action":"answer","text":"结论是 A。"}',
    ]);
    expect(results[0]).toBe("");
    expect(results[1]).toBe("结论是 A。");
  });

  it("转义序列被还原，半个转义留到下一块", () => {
    const results = feed([
      '{"action":"answer","text":"第一行\\',
      'n第二行 \\"引用\\" 结束"}',
    ]);
    // 反斜杠单独到达时不能吐出半个转义
    expect(results[0]).toBe("第一行");
    expect(results[1]).toBe('第一行\n第二行 "引用" 结束');
  });

  it("字符串里的引号不会被当成结束", () => {
    const state = createAnswerStreamState();
    const text = pushAnswerChunk(
      state,
      '{"action":"answer","text":"他说\\"好\\"，然后走了"}',
    );
    expect(text).toBe('他说"好"，然后走了');
  });

  it("\\u 转义还原", () => {
    const state = createAnswerStreamState();
    expect(
      pushAnswerChunk(state, '{"action":"answer","text":"\\u4f60\\u597d"}'),
    ).toBe("你好");
  });

  it("text 在 action 之前出现也能识别", () => {
    const state = createAnswerStreamState();
    expect(
      pushAnswerChunk(state, '{"text":"先给文本","action":"answer"}'),
    ).toBe("先给文本");
  });
});
