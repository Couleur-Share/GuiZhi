import { describe, expect, it } from "vitest";
import {
  findIllustrationAnchor,
  insertIllustrations,
  listAnchorBlocks,
  listIllustrations,
  removeIllustration,
  replaceIllustration,
  splitContentBlocks,
} from "@guizhi/shared/utils/illustration-note";

/** 锚点判定要求段落不短于 60 字，测试文本照这个长度写 */
const LONG_A =
  "评测先行是这套流程的第一条：没有一份稳定的评测集，后面所有的调参都只是在碰运气，改完也说不清到底是变好了还是变差了，团队里两个人的判断也永远对不上。";
const LONG_B =
  "第二条是分块策略。把长文切成多大一块，直接决定了检索能不能命中；切得太碎会丢上下文，切得太大又会把噪音一起塞进提示词里，两头都会让最终答案变差。";

const CONTENT = [
  "> 平台：抖音 · 作者：mHe",
  "",
  "## 正文",
  "",
  LONG_A,
  "",
  LONG_B,
  "",
  "![图 1](local-image://import-abc.webp)",
].join("\n");

describe("splitContentBlocks", () => {
  it("按空行切块并记录行区间", () => {
    const blocks = splitContentBlocks("第一段\n\n第二段\n\n\n第三段");
    expect(blocks.map((block) => block.text)).toEqual([
      "第一段",
      "第二段",
      "第三段",
    ]);
    expect(blocks[2].startLine).toBe(5);
    expect(blocks[2].endLine).toBe(5);
  });

  it("代码围栏里的空行不算段落边界", () => {
    const blocks = splitContentBlocks(
      "前言\n\n```ts\nconst a = 1;\n\nconst b = 2;\n```\n\n后记",
    );
    expect(blocks).toHaveLength(3);
    expect(blocks[1].text).toContain("const a = 1;");
    expect(blocks[1].text).toContain("const b = 2;");
  });
});

describe("listAnchorBlocks", () => {
  it("跳过元数据引用块、标题块、图片块与短段", () => {
    const anchors = listAnchorBlocks(CONTENT);
    expect(anchors.map((block) => block.text)).toEqual([LONG_A, LONG_B]);
  });

  it("序号沿用全量块的序号，插入才不会错位", () => {
    const anchors = listAnchorBlocks(CONTENT);
    const all = splitContentBlocks(CONTENT);
    expect(all[anchors[0].index].text).toBe(LONG_A);
  });

  it("代码块不作为锚点", () => {
    expect(listAnchorBlocks("```\n" + LONG_A + "\n```")).toHaveLength(0);
  });
});

describe("insertIllustrations", () => {
  it("按块序号插成独立段落", () => {
    const anchors = listAnchorBlocks(CONTENT);
    const next = insertIllustrations(CONTENT, [
      { afterBlock: anchors[0].index, alt: "评测先行", assetFileName: "gen-1.png" },
    ]);
    const lines = next.split("\n");
    const at = lines.indexOf("![评测先行](local-image://gen-1.png)");
    expect(at).toBeGreaterThan(0);
    expect(lines[at - 1]).toBe("");
    expect(lines[at + 1]).toBe("");
    expect(lines[at - 2]).toBe(LONG_A);
  });

  it("多张一起插时后面的位置不会被前面的顶偏", () => {
    const anchors = listAnchorBlocks(CONTENT);
    const next = insertIllustrations(CONTENT, [
      { afterBlock: anchors[0].index, alt: "一", assetFileName: "gen-1.png" },
      { afterBlock: anchors[1].index, alt: "二", assetFileName: "gen-2.png" },
    ]);
    const lines = next.split("\n");
    expect(lines.indexOf(LONG_A)).toBeLessThan(
      lines.indexOf("![一](local-image://gen-1.png)"),
    );
    expect(lines.indexOf("![一](local-image://gen-1.png)")).toBeLessThan(
      lines.indexOf(LONG_B),
    );
    expect(lines.indexOf(LONG_B)).toBeLessThan(
      lines.indexOf("![二](local-image://gen-2.png)"),
    );
  });

  it("序号越界时追加到末尾而不是丢图", () => {
    const next = insertIllustrations(CONTENT, [
      { afterBlock: 99, alt: "兜底", assetFileName: "gen-9.png" },
    ]);
    expect(next.endsWith("![兜底](local-image://gen-9.png)")).toBe(true);
  });

  it("alt 里的方括号与换行会被清掉，不切断图片语法", () => {
    const next = insertIllustrations("正文", [
      { afterBlock: 0, alt: "带[方括号]\n的图题", assetFileName: "gen-1.png" },
    ]);
    expect(next).toContain("![带方括号 的图题](local-image://gen-1.png)");
  });
});

describe("listIllustrations", () => {
  it("只认 gen- 前缀，采集导入的图不算配图", () => {
    const content = `${CONTENT}\n\n![我画的](local-image://gen-x.png)`;
    expect(listIllustrations(content)).toEqual([
      { alt: "我画的", assetFileName: "gen-x.png" },
    ]);
  });
});

describe("removeIllustration / replaceIllustration", () => {
  const withImage = `${LONG_A}\n\n![图题](local-image://gen-1.png)\n\n${LONG_B}`;

  it("整行就是那张图时连行一起删，不留空段", () => {
    expect(removeIllustration(withImage, "gen-1.png")).toBe(
      `${LONG_A}\n\n${LONG_B}`,
    );
  });

  it("图文混排时只摘走图，保留同段文字", () => {
    const inline = `见下图 ![图题](local-image://gen-1.png) 所示`;
    expect(removeIllustration(inline, "gen-1.png")).toBe("见下图  所示");
  });

  it("原位换图，位置与前后文都不动", () => {
    const next = replaceIllustration(withImage, "gen-1.png", {
      assetFileName: "gen-2.png",
      alt: "新图题",
    });
    expect(next).toBe(
      `${LONG_A}\n\n![新图题](local-image://gen-2.png)\n\n${LONG_B}`,
    );
  });
});

describe("findIllustrationAnchor", () => {
  it("找到配图前面最近的一个可配图段落", () => {
    const withImage = `${LONG_A}\n\n![图题](local-image://gen-1.png)\n\n${LONG_B}`;
    expect(findIllustrationAnchor(withImage, "gen-1.png")?.text).toBe(LONG_A);
  });

  it("配图前面没有可配图段落时返回 null", () => {
    expect(
      findIllustrationAnchor("![图题](local-image://gen-1.png)", "gen-1.png"),
    ).toBeNull();
  });

  it("正文里没有这张图时返回 null", () => {
    expect(findIllustrationAnchor(CONTENT, "gen-nope.png")).toBeNull();
  });
});
