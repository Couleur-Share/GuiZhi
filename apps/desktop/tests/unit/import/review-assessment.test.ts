import { describe, expect, it } from "vitest";
import type { ExtractedContent } from "../../../src/main/services/import/connectors";
import { assessImportReview } from "../../../src/main/services/import/review-assessment";

function extracted(
  overrides: Partial<ExtractedContent> = {},
): ExtractedContent {
  return {
    title: "测试内容",
    content: "这是一段正常的内容，用于确认采集结果能顺利进入知识库并保持可检索。".repeat(3),
    itemType: "webpage",
    sourceUri: "https://example.com/article",
    ...overrides,
  };
}

describe("assessImportReview", () => {
  it("完整的网页正文不需要人工复核", () => {
    expect(assessImportReview(extracted(), "url")).toEqual({
      reviewRequired: false,
      reasons: [],
    });
  });

  it("保留连接器报告的内容缺失原因", () => {
    const result = assessImportReview(
      extracted({ warningReason: "文字稿生成失败：音频资源不可用" }),
      "url",
    );

    expect(result.reviewRequired).toBe(true);
    expect(result.reasons).toEqual(["文字稿生成失败：音频资源不可用"]);
  });

  it("结构化文档的异常短正文需要复核，但手输短笔记不需要", () => {
    const shortDocument = extracted({ itemType: "document", content: "仅有标题" });
    expect(assessImportReview(shortDocument, "file").reasons[0]).toContain(
      "正文仅 4 字",
    );
    expect(
      assessImportReview(
        extracted({ itemType: "note", content: "待办：续费" }),
        "text",
      ).reviewRequired,
    ).toBe(false);
  });

  it("替换字符与控制字符会标记解析异常", () => {
    const result = assessImportReview(
      extracted({ content: `可读内容\uFFFD\u0007仍在这里`.repeat(20) }),
      "url",
    );
    expect(result.reasons).toContain(
      "解析文本含有不可识别字符，建议与原始文件核对。",
    );
  });
});
