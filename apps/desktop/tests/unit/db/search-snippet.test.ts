import { describe, expect, it } from "vitest";
import { makeSearchSnippet, makeSnippet } from "@guizhi/db/knowledge";
import { formatItemTime } from "../../../src/renderer/components/library/type-meta";

describe("搜索摘要", () => {
  const filler = "前文铺垫。".repeat(40);

  it("命中靠后时以命中处为中心截取，并带省略号", () => {
    const snippet = makeSearchSnippet(`${filler}关键词出现在这里，后面还有内容。${filler}`, "关键词");
    expect(snippet.startsWith("…")).toBe(true);
    expect(snippet).toContain("关键词");
  });

  it("命中在开头或正文里找不到时退回普通摘要", () => {
    const content = `关键词在开头。${filler}`;
    expect(makeSearchSnippet(content, "关键词")).toBe(makeSnippet(content));
    expect(makeSearchSnippet(content, "不存在的词")).toBe(makeSnippet(content));
  });

  it("多个搜索词取最早命中的位置，大小写不敏感", () => {
    const snippet = makeSearchSnippet(`${filler}Alpha 先出现，Beta 在后。${filler}`, "beta ALPHA");
    expect(snippet).toContain("Alpha");
  });
});

describe("条目时间格式", () => {
  it("跨年使用全数字日期，比带「年月日」的写法更短", () => {
    const old = new Date(2020, 4, 6, 12).getTime();
    const text = formatItemTime(old);
    expect(text).toContain("2020");
    expect(text).not.toMatch(/年|月|日/);
  });
});
