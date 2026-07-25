import { describe, expect, it } from "vitest";
import { splitImageNoteSections } from "@guizhi/shared/utils/image-note";

const CONTENT = [
  "> 平台：抖音 · 作者：mHe · 图文 2 张",
  "",
  "我用这套方法做了一个生产级 RAG 系统。",
  "",
  "1\\. 评测先行。",
  "",
  "![图 1](local-image://a.webp)",
  "",
  "![图 2](local-image://b.webp)",
  "",
  "> 有 1 张图的文字识别失败（HTTP 429），可在详情页重试。",
  "",
  "## 图中文字",
  "",
  "### 图 1",
  "",
  "Recall@5 0.175 → 0.600",
].join("\n");

describe("splitImageNoteSections", () => {
  it("文案里剔除图片块，保留元数据与状态注记", () => {
    const { caption } = splitImageNoteSections(CONTENT);
    expect(caption).toContain("平台：抖音");
    expect(caption).toContain("生产级 RAG 系统");
    expect(caption).toContain("1\\. 评测先行。");
    expect(caption).toContain("识别失败");
    expect(caption).not.toContain("local-image://");
  });

  it("图中文字连同小节标题一起切出来", () => {
    const { recognized } = splitImageNoteSections(CONTENT);
    expect(recognized.startsWith("## 图中文字")).toBe(true);
    expect(recognized).toContain("Recall@5 0.175 → 0.600");
    // 文案不该再重复一遍识别结果
    expect(splitImageNoteSections(CONTENT).caption).not.toContain("Recall@5");
  });

  it("尚未识别时 recognized 为空", () => {
    const { caption, recognized } = splitImageNoteSections(
      "> 平台：抖音\n\n文案\n\n![图 1](local-image://a.webp)",
    );
    expect(recognized).toBe("");
    expect(caption).toBe("> 平台：抖音\n\n文案");
  });

  it("同一段里连着多张图也整段剔除", () => {
    const { caption } = splitImageNoteSections(
      "文案\n\n![图 1](local-image://a.webp)\n![图 2](local-image://b.webp)\n\n结尾",
    );
    expect(caption).toBe("文案\n\n结尾");
  });

  it("图文混排的段落不会被误删", () => {
    const { caption } = splitImageNoteSections(
      "见下图 ![图 1](local-image://a.webp) 所示",
    );
    expect(caption).toContain("见下图");
    expect(caption).toContain("local-image://a.webp");
  });
});
