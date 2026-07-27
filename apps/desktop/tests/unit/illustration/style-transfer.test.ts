/**
 * 单套风格的导入导出。
 *
 * 分享风格的实际形态就是贴一段 JSON，所以解析要宽容：导出的信封、
 * 从预设文件里抠出来的裸对象都得认，越界值一律夹回合法区间。
 */
import { describe, expect, it } from "vitest";
import {
  exportStyleJson,
  parseStyleJson,
} from "../../../src/renderer/components/illustration/style-transfer";
import type { IllustrationStyle } from "@guizhi/shared/types";

const MESSAGES = { invalid: "不是有效 JSON", incomplete: "缺必填项" };

const style: IllustrationStyle = {
  id: "warm-life",
  name: "暖调生活",
  description: "米白底 + 赭橙柔色块",
  group: "生活与人物",
  visualDna: "Warm off-white background.",
  character: "",
  negative: "No cold palette.",
  aspectRatio: "4:3",
  maxShots: 5,
  maxLabels: 5,
};

describe("风格的导出与导入", () => {
  it("导出再导入还是同一套", () => {
    const parsed = parseStyleJson(exportStyleJson(style), MESSAGES);
    expect(parsed.style).toEqual(style);
  });

  it("从预设文件里抠出来的裸对象也认", () => {
    const parsed = parseStyleJson(JSON.stringify(style), MESSAGES);
    expect(parsed.style?.name).toBe("暖调生活");
  });

  it("不是 JSON 就说不是 JSON", () => {
    expect(parseStyleJson("随便写点什么", MESSAGES).error).toBe(
      MESSAGES.invalid,
    );
  });

  it("缺名称或画法时拒绝，并说清缺的是必填项", () => {
    const raw = JSON.stringify({ ...style, visualDna: "  " });
    const parsed = parseStyleJson(raw, MESSAGES);
    expect(parsed.style).toBeUndefined();
    expect(parsed.error).toBe(MESSAGES.incomplete);
  });

  it("越界的张数与标注数夹回合法区间", () => {
    const raw = JSON.stringify({ ...style, maxShots: 99, maxLabels: 0 });
    const parsed = parseStyleJson(raw, MESSAGES);
    expect(parsed.style?.maxShots).toBe(12);
    expect(parsed.style?.maxLabels).toBe(5);
  });

  it("画幅不在候选里时退回 16:9", () => {
    const raw = JSON.stringify({ ...style, aspectRatio: "21:9" });
    expect(parseStyleJson(raw, MESSAGES).style?.aspectRatio).toBe("16:9");
  });

  it("没带 id 时补一个，不至于存进去撞成空", () => {
    const raw = JSON.stringify({ ...style, id: "" });
    expect(parseStyleJson(raw, MESSAGES).style?.id).toBeTruthy();
  });
});
