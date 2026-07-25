import { describe, expect, it } from "vitest";
import { computeContentHash } from "../../../src/main/services/import/content-hash";

describe("computeContentHash", () => {
  it("相同内容哈希一致", () => {
    expect(computeContentHash("你好，世界")).toBe(
      computeContentHash("你好，世界"),
    );
  });

  it("排版差异（换行/空白/大小写）不影响哈希", () => {
    const base = computeContentHash("Hello World\n第二行");
    expect(computeContentHash("hello   world\r\n\r\n第二行")).toBe(base);
    expect(computeContentHash("  Hello World\n第二行  ")).toBe(base);
  });

  it("内容不同哈希不同", () => {
    expect(computeContentHash("内容甲")).not.toBe(computeContentHash("内容乙"));
  });
});
