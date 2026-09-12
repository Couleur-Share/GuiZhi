import { describe, expect, it } from "vitest";
import { prepareDesignCopy } from "../../src/main/services/themed-reading/design-copy";

const draft = [{ title: "说明", markdown: "这是**需要保留**的正文。\n\n- 第一步\n- 第二步", referenceIds: [] }];
describe("设计复用编辑稿", () => {
  it("短引用完整回填正文与列表，同时保留自由布局", () => {
    const copy = prepareDesignCopy(draft);
    const html = copy.expand('<section class="custom"><div data-reading-copy="s0b0"></div><aside>自由图解</aside><div data-reading-copy="s0b1"></div></section>');
    expect(html).toContain('<strong>需要保留</strong>'); expect(html).toContain('<li>第二步</li>');
    expect(html).toContain('class="custom"'); expect(html).toContain('自由图解'); expect(html).not.toContain('data-reading-copy');
    expect(copy.manuscript[0].blocks[0]).toEqual({ copyId: "s0b0", markdown: draft[0].markdown.split("\n\n")[0] });
  });
  it("拒绝重复、未知、非空与嵌套引用", () => {
    const copy = prepareDesignCopy(draft);
    for (const html of ['<div data-reading-copy="unknown"></div>', '<div data-reading-copy="s0b0">别的文字</div>', '<div data-reading-copy="s0b0"><div data-reading-copy="s0b1"></div></div>', '<div data-reading-copy="s0b0"></div><div data-reading-copy="s0b0"></div>']) expect(() => copy.expand(html)).toThrow();
  });
  it("仍支持自由 HTML，不接受图片为普通正文引用", () => {
    const copy = prepareDesignCopy([{ ...draft[0], markdown: '![图](https://example.com/image.png)' }]);
    expect(copy.manuscript[0].blocks[0].copyId).toBeUndefined();
    expect(copy.expand('<p>完全自由的HTML</p>')).toBe('<p>完全自由的HTML</p>');
  });
});
