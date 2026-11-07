import { describe, expect, it } from "vitest";
import {
  QA_CITE_HREF_PREFIX,
  buildCitationLabels,
  linkifyCitations,
  renumberCitations,
} from "../../../src/renderer/components/ask/qa-citations";

const VALID = new Set([1, 2]);

describe("linkifyCitations", () => {
  it("单编号整个 [1] 都变成可点链接，显示文字不变", () => {
    expect(linkifyCitations("优先 DDNS + 反向代理 [1]。", VALID)).toBe(
      `优先 DDNS + 反向代理 [\\[1\\]](${QA_CITE_HREF_PREFIX}1)。`,
    );
  });

  it("全角括号同样识别（模型经常这么写）", () => {
    expect(linkifyCitations("见【2】", VALID)).toBe(
      `见[\\[2\\]](${QA_CITE_HREF_PREFIX}2)`,
    );
  });

  it("多编号逐个成链，方括号留作字面量", () => {
    expect(linkifyCitations("两处都提到 [1,2]", VALID)).toBe(
      `两处都提到 \\[[1](${QA_CITE_HREF_PREFIX}1), [2](${QA_CITE_HREF_PREFIX}2)\\]`,
    );
  });

  it("没有对应来源的编号原样保留，不做成死链", () => {
    expect(linkifyCitations("参见 [9]", VALID)).toBe("参见 [9]");
    // 混合时只链得上的那个成链
    expect(linkifyCitations("[2,9]", VALID)).toBe(
      `\\[[2](${QA_CITE_HREF_PREFIX}2), 9\\]`,
    );
  });

  it("行内代码里的方括号数字不算引用", () => {
    // 数组下标、正则字符组长得和引用一模一样
    expect(linkifyCitations("用 `items[1]` 取值 [1]", VALID)).toBe(
      `用 \`items[1]\` 取值 [\\[1\\]](${QA_CITE_HREF_PREFIX}1)`,
    );
  });

  it("围栏代码块整块跳过", () => {
    const markdown = ["前言 [1]", "```js", "const a = arr[1];", "```", "后记 [2]"].join(
      "\n",
    );
    const output = linkifyCitations(markdown, VALID);
    expect(output).toContain("const a = arr[1];");
    expect(output).toContain(`前言 [\\[1\\]](${QA_CITE_HREF_PREFIX}1)`);
    expect(output).toContain(`后记 [\\[2\\]](${QA_CITE_HREF_PREFIX}2)`);
  });

  it("没有来源或正文为空时原样返回", () => {
    expect(linkifyCitations("有 [1] 标记", new Set())).toBe("有 [1] 标记");
    expect(linkifyCitations("", VALID)).toBe("");
  });

  it("Markdown 链接语法不会被误伤", () => {
    const markdown = "见 [文档](https://example.com) 与 [1]";
    expect(linkifyCitations(markdown, VALID)).toBe(
      `见 [文档](https://example.com) 与 [\\[1\\]](${QA_CITE_HREF_PREFIX}1)`,
    );
  });
});

describe("引用展示编号", () => {
  const answer = "先看 [7]，再看 [3]；两者都提到 [3、12]。代码 `a[5]` 不算。";
  const ordinals = [3, 5, 7, 12];

  it("按正文首次出现顺序连续编号，正文未提到的来源排在最后", () => {
    expect([...buildCitationLabels(answer, ordinals)]).toEqual([
      [7, 1],
      [3, 2],
      [12, 3],
      [5, 4],
    ]);
  });

  it("显示展示编号，链接仍指向原始编号", () => {
    const labels = buildCitationLabels(answer, ordinals);
    const output = linkifyCitations(answer, new Set(ordinals), labels);
    expect(output).toContain(`[\\[1\\]](${QA_CITE_HREF_PREFIX}7)`);
    expect(output).toContain(
      `\\[[2](${QA_CITE_HREF_PREFIX}3), [3](${QA_CITE_HREF_PREFIX}12)\\]`,
    );
    expect(output).toContain("`a[5]`");
  });

  it("重排后去掉指不到来源的编号，避免与展示编号撞号", () => {
    const labels = buildCitationLabels("见 [9] 与 [3, 2]", [3]);
    expect(linkifyCitations("见 [9] 与 [3, 2]", new Set([3]), labels)).toBe(
      `见  与 [\\[1\\]](${QA_CITE_HREF_PREFIX}3)`,
    );
  });

  it("复制文本与屏幕编号一致", () => {
    const labels = buildCitationLabels(answer, ordinals);
    expect(renumberCitations(answer, new Set(ordinals), labels)).toBe(
      "先看 [1]，再看 [2]；两者都提到 [2, 3]。代码 `a[5]` 不算。",
    );
  });
});
