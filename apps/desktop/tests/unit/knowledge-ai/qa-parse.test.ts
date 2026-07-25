import { describe, expect, it } from "vitest";
import {
  extractCitedOrdinals,
  tryParseAction,
} from "../../../src/renderer/services/knowledge-ai/qa";

describe("tryParseAction", () => {
  it("解析三种规范动作", () => {
    expect(tryParseAction('{"action":"search","query":"归知 架构"}')).toEqual({
      kind: "search",
      query: "归知 架构",
    });
    expect(tryParseAction('{"action":"read","target":3}')).toEqual({
      kind: "read",
      target: 3,
    });
    expect(tryParseAction('{"action":"answer","text":"回答内容"}')).toEqual({
      kind: "answer",
      text: "回答内容",
    });
  });

  it("容忍代码围栏与前后缀文字", () => {
    const wrapped =
      '好的，我来搜索。\n```json\n{"action":"search","query":"测试"}\n```';
    expect(tryParseAction(wrapped)).toEqual({
      kind: "search",
      query: "测试",
    });
  });

  it("read target 容忍字符串数字", () => {
    expect(tryParseAction('{"action":"read","target":"[2]"}')).toEqual({
      kind: "read",
      target: 2,
    });
  });

  it("非法输出返回 null", () => {
    expect(tryParseAction("我不知道该怎么做")).toBeNull();
    expect(tryParseAction('{"action":"unknown"}')).toBeNull();
    expect(tryParseAction('{"action":"search","query":""}')).toBeNull();
    expect(tryParseAction('{"action":"read"}')).toBeNull();
  });
});

describe("extractCitedOrdinals", () => {
  const valid = new Set([1, 2, 3, 4, 5]);

  it("提取单个与复合标注", () => {
    expect([
      ...extractCitedOrdinals("结论一 [1]。结论二 [2、3]。", valid),
    ]).toEqual([1, 2, 3]);
  });

  it("支持全角括号与逗号", () => {
    expect([...extractCitedOrdinals("如【4】所述，且 [1,2]", valid)]).toEqual([
      4, 1, 2,
    ]);
  });

  it("无效编号（幻觉）被忽略", () => {
    expect([...extractCitedOrdinals("参考 [9] 和 [2]", valid)]).toEqual([2]);
  });

  it("代码索引不误判为引用", () => {
    // arr[0] 中的 0 不在有效集合内，被自然过滤
    expect([...extractCitedOrdinals("代码 `arr[0]` 参考 [1]", valid)]).toEqual([
      1,
    ]);
  });
});
