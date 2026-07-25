import { describe, expect, it } from "vitest";
import {
  buildOcrRequestBody,
  parseOcrResponse,
} from "../../../src/renderer/services/knowledge-ai/ocr";
import { applyOcrTextToContent } from "../../../src/renderer/components/library/AiOcrCard";

describe("buildOcrRequestBody", () => {
  it("构造多模态 chat 请求（文本指令 + 内联图片）", () => {
    const body = JSON.parse(
      buildOcrRequestBody("gpt-4o", "data:image/png;base64,AAAA"),
    );
    expect(body.model).toBe("gpt-4o");
    expect(body.temperature).toBe(0);
    const content = body.messages[0].content;
    expect(content[0].type).toBe("text");
    expect(content[1].type).toBe("image_url");
    expect(content[1].image_url.url).toBe("data:image/png;base64,AAAA");
  });
});

describe("parseOcrResponse", () => {
  it("支持字符串与分块数组两种 content 形态", () => {
    expect(
      parseOcrResponse(
        JSON.stringify({
          choices: [{ message: { content: "识别结果" } }],
        }),
      ),
    ).toBe("识别结果");

    expect(
      parseOcrResponse(
        JSON.stringify({
          choices: [
            {
              message: {
                content: [
                  { type: "text", text: "第一段" },
                  { type: "text", text: "第二段" },
                ],
              },
            },
          ],
        }),
      ),
    ).toBe("第一段第二段");
  });

  it("缺少内容时报错", () => {
    expect(() =>
      parseOcrResponse(JSON.stringify({ choices: [{ message: {} }] })),
    ).toThrow("缺少文本内容");
  });
});

describe("applyOcrTextToContent", () => {
  it("首次识别追加「图中文字」小节", () => {
    const next = applyOcrTextToContent("![img](local-image://a.png)", "识别文本");
    expect(next).toContain("## 图中文字");
    expect(next).toContain("识别文本");
    expect(next.startsWith("![img](local-image://a.png)")).toBe(true);
  });

  it("重复识别整节替换而非叠加", () => {
    const first = applyOcrTextToContent("正文", "旧文本");
    const second = applyOcrTextToContent(first, "新文本");
    expect(second).toContain("新文本");
    expect(second).not.toContain("旧文本");
    expect(second.match(/## 图中文字/g)).toHaveLength(1);
  });
});
