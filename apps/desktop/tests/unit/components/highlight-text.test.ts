import { describe, expect, it } from "vitest";
import { highlightText } from "../../../src/renderer/components/library/highlight-text";

describe("highlightText", () => {
  it("无关键字原样返回", () => {
    expect(highlightText("爱尔眼科验光", "")).toBe("爱尔眼科验光");
  });

  it("标出大小写不敏感的命中段", () => {
    const nodes = highlightText("去爱尔眼科，再去爱尔眼科", "爱尔眼科");
    expect(Array.isArray(nodes)).toBe(true);
    const marks = (nodes as Array<{ type?: string; props?: { children?: string } }>).filter(
      (node) => typeof node === "object" && node !== null && "props" in node,
    );
    // 两处命中 → 两个 mark 元素
    expect(marks).toHaveLength(2);
    expect(marks[0]?.props?.children).toBe("爱尔眼科");
  });
});
